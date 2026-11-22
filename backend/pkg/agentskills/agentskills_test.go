package agentskills

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"backend/pkg/mcp"
)

// readTestFile reads a fixture verbatim so a case can assert on the document
// string rather than a loaded skill.
func readTestFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	return string(raw), err
}

const skillsRoot = "testdata/skills"

func testNow() time.Time { return time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC) }

func openLoader(t *testing.T) *Loader {
	t.Helper()
	l, err := Open(skillsRoot)
	if err != nil {
		t.Fatalf("Open(%q): %v", skillsRoot, err)
	}
	return l
}

// TestLoadValidSkill proves the happy path end to end: frontmatter fields are
// typed, pinned refs parse, and a bare tool name stays advisory.
func TestLoadValidSkill(t *testing.T) {
	s, err := openLoader(t).Load("pdf-report", TrustPrivate)
	if err != nil {
		t.Fatalf("Load(pdf-report): %v", err)
	}
	if s.Name != "pdf-report" {
		t.Errorf("Name = %q, want pdf-report", s.Name)
	}
	if !strings.HasPrefix(s.Description, "Turn tabular query results") {
		t.Errorf("Description = %q, want the frontmatter sentence", s.Description)
	}
	if s.License != "MIT" {
		t.Errorf("License = %q, want MIT", s.License)
	}
	if got, want := strings.Join(s.Compatibility, ","), "claude-code,vibesdk"; got != want {
		t.Errorf("Compatibility = %q, want %q", got, want)
	}
	if s.Metadata["category"] != "reporting" {
		t.Errorf("Metadata[category] = %q, want reporting", s.Metadata["category"])
	}
	if s.Trust != TrustPrivate {
		t.Errorf("Trust = %q, want private", s.Trust)
	}
	// Two pinned refs, sorted; the bare `render_pdf` is advisory only.
	if len(s.AllowedTools) != 2 {
		t.Fatalf("AllowedTools = %v, want 2 pinned refs", s.AllowedTools)
	}
	if s.AllowedTools[0].String() != "mcp/reports/render_pdf@1.2.0" {
		t.Errorf("AllowedTools[0] = %s, want the render_pdf ref", s.AllowedTools[0])
	}
	if len(s.UnpinnedTools) != 1 || s.UnpinnedTools[0] != "render_pdf" {
		t.Errorf("UnpinnedTools = %v, want [render_pdf]", s.UnpinnedTools)
	}
}

// TestCardIsResolveCheap proves the resolve payload is name + description and
// fits the ~100 token budget: no body text can leak through a card.
func TestCardIsResolveCheap(t *testing.T) {
	s, err := openLoader(t).Load("pdf-report", TrustPrivate)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	card := s.Card()
	if card.Name != s.Name || card.Description != s.Description {
		t.Errorf("Card = %+v, want name+description only", card)
	}
	if strings.Contains(card.Description, "## Steps") {
		t.Error("Card description leaked the body")
	}
	if !card.WithinBudget() {
		t.Errorf("Card tokens = %d, want <= %d", card.Tokens, ResolveTokenBudget)
	}
}

// TestUnknownFrontmatterKeysArePreserved proves a newer upstream field is kept
// in Extensions rather than dropped or fatal.
func TestUnknownFrontmatterKeysArePreserved(t *testing.T) {
	doc := "---\nname: extra-fields\ndescription: Carries a field this loader does not interpret.\nicon: sparkle\n---\n\nBody.\n"
	s, err := parseSkill(doc, "extra-fields", TrustPrivate)
	if err != nil {
		t.Fatalf("parseSkill: %v", err)
	}
	if s.Extensions["icon"] != "sparkle" {
		t.Errorf("Extensions[icon] = %v, want sparkle", s.Extensions["icon"])
	}
}

// TestActivateLoadsBodyLazily proves the body is available on activation,
// excludes the frontmatter, and is refused when it blows the token budget.
func TestActivateLoadsBodyLazily(t *testing.T) {
	s, err := openLoader(t).Load("pdf-report", TrustPrivate)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	body, err := s.Activate()
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if !strings.HasPrefix(body, "# PDF Report") {
		t.Errorf("body starts with %q, want the markdown heading without frontmatter", firstLine(body))
	}
	if strings.Contains(body, "allowed-tools") {
		t.Error("body still contains frontmatter")
	}

	huge := "---\nname: huge-body\ndescription: A body over the activation budget.\n---\n\n" + strings.Repeat("token ", 6000)
	big, err := parseSkill(huge, "huge-body", TrustPrivate)
	if err != nil {
		t.Fatalf("parseSkill: %v", err)
	}
	if _, err := big.Activate(); err == nil || !strings.Contains(err.Error(), "over the 5000 budget") {
		t.Errorf("Activate(huge) err = %v, want a body-budget refusal", err)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// TestRejectsInvalidFrontmatter is the validation table: each case names the
// field that must be blamed, so a broken skill points at its own defect.
func TestRejectsInvalidFrontmatter(t *testing.T) {
	files := []struct{ file, field string }{
		{"bad-name.md", "name"},
		{"missing-description.md", "description"},
		{"no-frontmatter.md", "frontmatter"},
		{"bad-license.md", "license"},
	}
	for _, tc := range files {
		t.Run(tc.file, func(t *testing.T) {
			raw, err := readTestFile("testdata/invalid/" + tc.file)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			_, err = parseSkill(raw, "", TrustPrivate)
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("err = %v, want *ValidationError", err)
			}
			if ve.Field != tc.field {
				t.Errorf("Field = %q, want %q (%v)", ve.Field, tc.field, err)
			}
		})
	}

	inline := []struct{ name, doc, field, dir string }{
		{"empty-frontmatter", "---\n---\n\nBody.", "frontmatter", ""},
		{"yaml-syntax", "---\nname: [unclosed\n---\n\nBody.", "frontmatter", ""},
		{"leading-hyphen", "---\nname: -bad\ndescription: x\n---\n", "name", ""},
		{"double-hyphen", "---\nname: bad--name\ndescription: x\n---\n", "name", ""},
		{"name-too-long", "---\nname: " + strings.Repeat("a", 65) + "\ndescription: x\n---\n", "name", ""},
		{"description-too-long", "---\nname: long-desc\ndescription: " + strings.Repeat("a", 1025) + "\n---\n", "description", ""},
		{"blank-description", "---\nname: blank-desc\ndescription: \"   \"\n---\n", "description", ""},
		{"empty-allowed-tools-entry", "---\nname: empty-tool\ndescription: x\nallowed-tools:\n  - \"\"\n---\n", "allowed-tools", ""},
		{"unpinned-mcp-ref", "---\nname: unpinned\ndescription: x\nallowed-tools:\n  - mcp/crm/lookup@latest\n---\n", "allowed-tools", ""},
		{"malformed-mcp-ref", "---\nname: malformed\ndescription: x\nallowed-tools:\n  - mcp/crm/lookup\n---\n", "allowed-tools", ""},
		{"name-folder-mismatch", "---\nname: other-name\ndescription: x\n---\n", "name", "other-folder"},
	}
	for _, tc := range inline {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSkill(tc.doc, tc.dir, TrustPrivate)
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("err = %v, want *ValidationError", err)
			}
			if ve.Field != tc.field {
				t.Errorf("Field = %q, want %q (%v)", ve.Field, tc.field, err)
			}
		})
	}
}

// TestUnknownTrustRejected proves trust is validated input, not a free string
// that would silently skip the community read-only rule.
func TestUnknownTrustRejected(t *testing.T) {
	_, err := parseSkill("---\nname: x-name\ndescription: x\n---\n", "", Trust("unreviewed"))
	if err == nil || !strings.Contains(err.Error(), "must be private, verified or community") {
		t.Fatalf("err = %v, want a trust refusal", err)
	}
}

// TestLoadRejectsUnsafeFolderName proves the loader cannot be walked out of
// its root through the skill name.
func TestLoadRejectsUnsafeFolderName(t *testing.T) {
	l := openLoader(t)
	for _, name := range []string{"..", "../etc", "a/b", ".hidden"} {
		if _, err := l.Load(name, TrustPrivate); err == nil {
			t.Errorf("Load(%q) succeeded, want refusal", name)
		}
	}
}

// TestResourceLoadsOnDemand proves resources are read from the skill's own
// directories, that traversal is denied, and that an unknown kind is refused.
func TestResourceLoadsOnDemand(t *testing.T) {
	s, err := openLoader(t).Load("pdf-report", TrustPrivate)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	raw, err := s.Resource(KindReferences, "columns.md")
	if err != nil {
		t.Fatalf("Resource(references/columns.md): %v", err)
	}
	if !strings.Contains(string(raw), "account_name") {
		t.Errorf("reference content = %q, want the column table", firstLine(string(raw)))
	}
	if names, err := s.ResourceNames(KindScripts); err != nil || len(names) != 1 || names[0] != "render.sh" {
		t.Errorf("ResourceNames(scripts) = %v, %v; want [render.sh]", names, err)
	}
	if names, err := s.ResourceNames(KindAssets); err != nil || len(names) != 0 {
		t.Errorf("ResourceNames(assets) = %v, %v; want empty (optional dir)", names, err)
	}
	for _, bad := range []string{"../SKILL.md", "..", "a/b", ".hidden"} {
		if _, err := s.Resource(KindReferences, bad); err == nil {
			t.Errorf("Resource(%q) succeeded, want refusal", bad)
		}
	}
	if _, err := s.Resource(Kind("secrets"), "x"); err == nil {
		t.Error("Resource(secrets) succeeded, want refusal")
	}
}

// TestCommunityScriptsDenied proves the read-only rule: a community skill's
// text is readable, its scripts are refused until a Verified review.
func TestCommunityScriptsDenied(t *testing.T) {
	l := openLoader(t)
	community, err := l.Load("community-lint", TrustCommunity)
	if err != nil {
		t.Fatalf("Load(community-lint): %v", err)
	}
	if community.ScriptsAllowed() {
		t.Error("ScriptsAllowed() = true for a community skill")
	}
	_, err = community.Resource(KindScripts, "lint.sh")
	var denied *DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("Resource(scripts) err = %v, want *DeniedError", err)
	}
	if !strings.Contains(denied.Reason, "Verified review") {
		t.Errorf("reason = %q, want it to name the Verified review", denied.Reason)
	}
	// The body is text, not code: reading it is allowed.
	if _, err := community.Activate(); err != nil {
		t.Errorf("Activate(community) err = %v, want the body to be readable", err)
	}

	verified, err := l.Load("community-lint", TrustVerified)
	if err != nil {
		t.Fatalf("Load(community-lint, verified): %v", err)
	}
	if _, err := verified.Resource(KindScripts, "lint.sh"); err != nil {
		t.Errorf("Resource(scripts) under Verified err = %v, want allowed", err)
	}
}

// TestBindIntersectsGrants proves Bind only ever narrows: a live grant of the
// exact pinned ref is allowed, and revoked, expired or different versions are
// denied. The advisory bare name never becomes authority.
func TestBindIntersectsGrants(t *testing.T) {
	s, err := openLoader(t).Load("pdf-report", TrustVerified)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	now := testNow()
	policy := mcp.ToolPolicy{
		Server: "reports", Tool: "render_pdf", ToolManifestVersion: "1.2.0",
		Permissions: []string{"reports:write"}, SideEffect: mcp.SideEffectSafe,
	}
	renderRef := mcp.ServerRef{Server: "reports", Tool: "render_pdf", Version: "1.2.0"}
	live, err := mcp.NewStore().Issue(
		mcp.Identity{Tenant: "tenant-a"}, renderRef,
		policy, "user-1", false, now.Add(time.Hour), now)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	otherVersion := live
	otherVersion.Ref.Version = "1.1.0"
	revoked := live
	revoked.Revoked = true
	expired := live
	expired.ExpiresAt = now.Add(-time.Minute)

	cases := []struct {
		name    string
		grants  []mcp.Grant
		allowed bool
	}{
		{"live grant", []mcp.Grant{live}, true},
		{"no grants", nil, false},
		{"revoked", []mcp.Grant{revoked}, false},
		{"expired", []mcp.Grant{expired}, false},
		{"other version", []mcp.Grant{otherVersion}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := s.Bind(tc.grants, now)
			if got := b.Allows(renderRef); got != tc.allowed {
				t.Errorf("Allows(render_pdf@1.2.0) = %v, want %v", got, tc.allowed)
			}
			if len(b.Advisory) != 1 || b.Advisory[0] != "render_pdf" {
				t.Errorf("Advisory = %v, want the bare name only", b.Advisory)
			}
			// upload has no grant in any case, so it must stay denied.
			if len(b.Denied) == 0 {
				t.Error("Denied is empty: an ungranted requested ref must be reported")
			}
		})
	}
}

// countingTransport is a stand-in MCP server: it records every invocation so a
// test can prove that a denial happened *before* anything ran.
type countingTransport struct {
	calls []string
}

func (c *countingTransport) DiscoverTools(mcp.ServerRegistration) ([]mcp.DiscoveredTool, error) {
	return []mcp.DiscoveredTool{{Name: "render_pdf", SideEffect: mcp.SideEffectSafe}}, nil
}

func (c *countingTransport) InvokeTool(_ mcp.ServerRegistration, tool, _ string) (string, error) {
	c.calls = append(c.calls, tool)
	return `{"ok":true}`, nil
}

type gateHarness struct {
	gate   *mcp.Gate
	grants *mcp.Store
	audit  *mcp.MemorySink
	tr     *countingTransport
	id     mcp.Identity
	ref    mcp.ServerRef
	policy mcp.ToolPolicy
}

func newGateHarness(t *testing.T) *gateHarness {
	t.Helper()
	h := &gateHarness{
		grants: mcp.NewStore(),
		audit:  mcp.NewMemorySink(),
		tr:     &countingTransport{},
		id:     mcp.Identity{Tenant: "tenant-a", User: "user-1", Agent: "reporter"},
		ref:    mcp.ServerRef{Server: "reports", Tool: "render_pdf", Version: "1.2.0"},
		policy: mcp.ToolPolicy{
			Server: "reports", Tool: "render_pdf", ToolManifestVersion: "1.2.0",
			Permissions: []string{"reports:write"}, SideEffect: mcp.SideEffectSafe,
		},
	}
	reg, policies := mcp.NewRegistry(), mcp.NewPolicyStore()
	if err := reg.Register(mcp.ServerRegistration{
		Name: "reports", Endpoint: "stdio://reports", Tools: []string{"render_pdf"},
		RegisteredBy: "user-1", RegisteredAt: testNow(),
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := policies.Put(h.policy); err != nil {
		t.Fatalf("policy Put: %v", err)
	}
	gate, err := mcp.NewGate(mcp.GateConfig{
		Registry: reg, Policies: policies, Grants: h.grants, Audit: h.audit,
		Now: testNow,
	})
	if err != nil {
		t.Fatalf("NewGate: %v", err)
	}
	h.gate = gate
	return h
}

// TestGrantlessSkillScriptDeniedBeforeSpawn is the backlog's gate-integration
// criterion: a skill that requests a tool gets nothing until a grant exists,
// and the refusal happens before the transport is touched.
func TestGrantlessSkillScriptDeniedBeforeSpawn(t *testing.T) {
	h := newGateHarness(t)
	s, err := openLoader(t).Load("pdf-report", TrustVerified)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if b := s.Bind(h.grants.List(h.id), testNow()); b.Allows(h.ref) {
		t.Fatal("Bind allowed a ref with no grant: a skill must not authorize itself")
	}

	_, err = h.gate.Call(mcp.CallRequest{
		Identity: h.id, Ref: h.ref, GrantID: "", ArgsJSON: `{}`,
	}, h.tr)
	var denied *mcp.DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("gate.Call err = %v, want *mcp.DeniedError", err)
	}
	if !strings.Contains(denied.Verdict, "no grant") {
		t.Errorf("verdict = %q, want the no-grant refusal", denied.Verdict)
	}
	if len(h.tr.calls) != 0 {
		t.Errorf("transport ran %v, want nothing spawned before authorization", h.tr.calls)
	}
	if got := len(h.audit.Events()); got != 1 {
		t.Errorf("audit events = %d, want the denial recorded", got)
	}
}

// TestGrantedSkillScriptExecutesOnlyThroughTheGate proves the other half: once
// a real grant exists, Bind reports it and the Gate is the path that runs it.
func TestGrantedSkillScriptExecutesOnlyThroughTheGate(t *testing.T) {
	h := newGateHarness(t)
	s, err := openLoader(t).Load("pdf-report", TrustVerified)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	now := testNow()
	if _, err := h.grants.Issue(h.id, h.ref, h.policy, "user-1", false, now.Add(time.Hour), now); err != nil {
		t.Fatalf("Issue: %v", err)
	}

	b := s.Bind(h.grants.List(h.id), now)
	if !b.Allows(h.ref) {
		t.Fatalf("Bind after Issue: allowed=%v, want the live grant", b.Allowed)
	}
	grant := b.Allowed[0]

	if _, err := h.gate.Call(mcp.CallRequest{
		Identity: h.id, Ref: h.ref, GrantID: grant.ID, ArgsJSON: `{}`,
	}, h.tr); err != nil {
		t.Fatalf("gate.Call with a live grant: %v", err)
	}
	if len(h.tr.calls) != 1 || h.tr.calls[0] != "render_pdf" {
		t.Errorf("transport calls = %v, want exactly one render_pdf", h.tr.calls)
	}

	// A grant never crosses tenants: the loader cannot help a caller whose
	// own store does not hold it.
	other := mcp.Identity{Tenant: "tenant-b", User: "user-2", Agent: "reporter"}
	if b := s.Bind(h.grants.List(other), now); b.Allows(h.ref) {
		t.Error("Bind for tenant-b allowed tenant-a's grant")
	}
	if _, err := h.gate.Call(mcp.CallRequest{
		Identity: other, Ref: h.ref, GrantID: grant.ID, ArgsJSON: `{}`,
	}, h.tr); err == nil {
		t.Error("gate.Call for tenant-b succeeded with tenant-a's grant")
	}
	if len(h.tr.calls) != 1 {
		t.Errorf("transport calls = %v, want no cross-tenant execution", h.tr.calls)
	}
}
