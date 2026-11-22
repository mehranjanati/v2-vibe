package toolsearch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backend/pkg/agentskills"
	"backend/pkg/mcp"
	"backend/pkg/openapiimport"
)

func testNow() time.Time { return time.Date(2026, 11, 23, 14, 0, 0, 0, time.UTC) }

// buildCatalog indexes the two loaders' real fixtures: the five-operation Leads
// API and the pdf-report skill's pinned tools.
func buildCatalog(t *testing.T) *Catalog {
	t.Helper()
	raw, err := os.ReadFile("../openapiimport/testdata/leads-api.yaml")
	if err != nil {
		t.Fatalf("read OpenAPI fixture: %v", err)
	}
	spec, err := openapiimport.Load(raw)
	if err != nil {
		t.Fatalf("Load(OpenAPI): %v", err)
	}
	imported, err := openapiimport.Import(spec, openapiimport.Options{
		Publisher: "acme", License: "Apache-2.0",
		Allow: []string{"listLeads", "createLead", "getLead", "updateLead", "deleteLead"},
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	loader, err := agentskills.Open("../agentskills/testdata/skills")
	if err != nil {
		t.Fatalf("agentskills.Open: %v", err)
	}
	skill, err := loader.Load("pdf-report", agentskills.TrustVerified)
	if err != nil {
		t.Fatalf("Load(pdf-report): %v", err)
	}

	catalog := NewCatalog()
	if err := catalog.AddImported(imported); err != nil {
		t.Fatalf("AddImported: %v", err)
	}
	if err := catalog.AddSkill(skill); err != nil {
		t.Fatalf("AddSkill: %v", err)
	}
	return catalog
}

func names(matches []Match) []string {
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.Candidate.Name())
	}
	return out
}

// TestRankingGolden pins the ranking: the same catalog and query must produce
// the same order, so a change in scoring shows up as a failing expectation
// instead of a silently different prompt.
func TestRankingGolden(t *testing.T) {
	catalog := buildCatalog(t)
	if got := catalog.Len(); got != 7 {
		t.Fatalf("catalog holds %d candidates, want 5 OpenAPI operations + 2 skill tools", got)
	}

	cases := []struct {
		query string
		limit int
		want  []string
	}{
		// A query naming the operation ranks it first; description-only
		// matches follow in name order (the tie-break is part of the contract).
		{"create a lead", 2, []string{"createlead", "deletelead"}},
		{"delete lead", 2, []string{"deletelead", "createlead"}},
		// A query only the list operation's description matches returns one
		// candidate, not a padded slice.
		{"list leads with filters", 3, []string{"listleads"}},
		// A skill's tools are found through the skill description; the
		// underscored name normalises to words.
		{"pdf report", 2, []string{"render_pdf", "upload"}},
		// A method-specific query still prefers the exact tool.
		{"update lead score", 2, []string{"updatelead", "createlead"}},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			got := names(catalog.Search(tc.query, tc.limit).Matches)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("Search(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

// TestContextSliceIsBounded is the first acceptance criterion: a slice has an
// upper bound in both candidates and estimated tokens, whatever the caller asks
// for.
func TestContextSliceIsBounded(t *testing.T) {
	catalog := buildCatalog(t)

	for _, limit := range []int{-5, 0, 1, MaxCandidates, MaxCandidates + 5, 1000} {
		res := catalog.Search("lead report upload pdf score", limit)
		if len(res.Matches) > MaxCandidates {
			t.Errorf("limit=%d returned %d matches, over MaxCandidates=%d", limit, len(res.Matches), MaxCandidates)
		}
		if limit > 0 && limit < MaxCandidates && len(res.Matches) > limit {
			t.Errorf("limit=%d returned %d matches", limit, len(res.Matches))
		}
		if res.Tokens > ContextTokenBudget && len(res.Matches) > 1 {
			t.Errorf("limit=%d slice costs %d tokens, over the %d budget", limit, res.Tokens, ContextTokenBudget)
		}
		if want := len(res.Lines()); want != len(res.Matches) {
			t.Errorf("Lines() = %d, want %d", want, len(res.Matches))
		}
	}

	// The default answers with a handful, not the catalogue.
	if got := len(catalog.Search("lead", 0).Matches); got != DefaultLimit {
		t.Errorf("default limit returned %d matches, want %d", got, DefaultLimit)
	}
	// Truncation is reported, not silent: a broad query with a small limit.
	truncated := catalog.Search("lead pdf report upload render", 2)
	if !truncated.Truncated || !strings.Contains(truncated.Note, "showing") {
		t.Errorf("Truncated = %v, note = %q; want an explicit truncation note", truncated.Truncated, truncated.Note)
	}
	if len(truncated.Matches) != 2 {
		t.Errorf("truncated slice holds %d matches, want the requested 2", len(truncated.Matches))
	}
}

// TestMissedCapabilityIsReported is the second acceptance criterion: a query
// with no match returns nothing and says so, rather than degrading into "here is
// everything".
func TestMissedCapabilityIsReported(t *testing.T) {
	catalog := buildCatalog(t)
	for _, query := range []string{"reconcile supplier invoices", "transcribe audio", ""} {
		res := catalog.Search(query, DefaultLimit)
		if !res.Missed {
			t.Errorf("Search(%q).Missed = false, want true", query)
		}
		if len(res.Matches) != 0 {
			t.Errorf("Search(%q) returned %v on a miss, want nothing", query, names(res.Matches))
		}
		if res.Note == "" {
			t.Errorf("Search(%q) has no note explaining the miss", query)
		}
		if res.Tokens != 0 {
			t.Errorf("Search(%q) costs %d tokens on a miss, want 0", query, res.Tokens)
		}
	}
}

// countingTransport stands in for the tool's transport: it records invocations
// so a test can prove a denial happened before anything ran.
type countingTransport struct{ calls []string }

func (c *countingTransport) DiscoverTools(mcp.ServerRegistration) ([]mcp.DiscoveredTool, error) {
	return nil, nil
}

func (c *countingTransport) InvokeTool(_ mcp.ServerRegistration, tool, _ string) (string, error) {
	c.calls = append(c.calls, tool)
	return `{"ok":true}`, nil
}

// TestSurfacedToolStillNeedsAGrant is the third acceptance criterion: search
// hands out pinned refs and nothing else. A surfaced tool that the tenant has no
// live grant for is refused before the transport runs; with a grant, the very
// same match executes through the Gate exactly once.
func TestSurfacedToolStillNeedsAGrant(t *testing.T) {
	catalog := buildCatalog(t)
	res := catalog.Search("list leads", 1)
	if len(res.Matches) != 1 {
		t.Fatalf("search returned %d matches, want the list operation", len(res.Matches))
	}
	match := res.Matches[0]

	// The Gate is wired with the imported tool's own policy snapshot.
	policy := mcp.ToolPolicy{
		Server: match.Candidate.Ref.Server, Tool: match.Candidate.Ref.Tool,
		ToolManifestVersion: match.Candidate.Ref.Version,
		Permissions:         []string{"leads:read"}, SideEffect: mcp.SideEffectSafe,
	}
	grants, audit, tr := mcp.NewStore(), mcp.NewMemorySink(), &countingTransport{}
	registry := mcp.NewRegistry()
	if err := registry.Register(mcp.ServerRegistration{
		Name: policy.Server, Endpoint: "https://api.leads.example.com/v2", Tools: []string{policy.Tool},
		RegisteredBy: "user-1", RegisteredAt: testNow(),
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	policies := mcp.NewPolicyStore()
	if err := policies.Put(policy); err != nil {
		t.Fatalf("Put(policy): %v", err)
	}
	gate, err := mcp.NewGate(mcp.GateConfig{Registry: registry, Policies: policies, Grants: grants, Audit: audit, Now: testNow})
	if err != nil {
		t.Fatalf("NewGate: %v", err)
	}
	id := mcp.Identity{Tenant: "tenant-a", User: "user-1", Agent: "reporter"}

	ungranted := Verify(res.Matches, grants.List(id), testNow())[0]
	if ungranted.Granted {
		t.Fatal("Verify reported a grant that does not exist")
	}
	if _, err := ungranted.CallRequest(id, `{}`); err == nil {
		t.Fatal("CallRequest produced a request for an ungranted match")
	} else {
		var denied *mcp.DeniedError
		if !errors.As(err, &denied) {
			t.Fatalf("err = %v, want *mcp.DeniedError", err)
		}
	}

	// A foreign grant does not help: the Gate re-checks the tenant itself.
	other, err := grants.Issue(mcp.Identity{Tenant: "tenant-b"}, match.Candidate.Ref, policy, "user-2", false, testNow().Add(time.Hour), testNow())
	if err != nil {
		t.Fatalf("Issue(tenant-b): %v", err)
	}
	before := len(tr.calls)
	if _, err := gate.Call(mcp.CallRequest{Identity: id, Ref: match.Candidate.Ref, GrantID: other.ID, ArgsJSON: `{}`}, tr); err == nil {
		t.Fatal("cross-tenant call succeeded")
	}
	if len(tr.calls) != before {
		t.Fatal("transport ran during a denied cross-tenant call")
	}

	// With a real grant the same match becomes callable.
	live, err := grants.Issue(id, match.Candidate.Ref, policy, "user-1", false, testNow().Add(time.Hour), testNow())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	verified := Verify(res.Matches, grants.List(id), testNow())[0]
	if !verified.Granted || verified.GrantID != live.ID {
		t.Fatalf("Verify = %+v, want the live grant %s", verified, live.ID)
	}
	req, err := verified.CallRequest(id, `{}`)
	if err != nil {
		t.Fatalf("CallRequest: %v", err)
	}
	if _, err := gate.Call(req, tr); err != nil {
		t.Fatalf("gate.Call with a grant: %v", err)
	}
	if len(tr.calls) != 1 {
		t.Errorf("transport calls = %v, want exactly one", tr.calls)
	}
}

// TestGrantIDIgnoresRevokedAndExpired proves the helper never reports a dead
// grant as authority.
func TestGrantIDIgnoresRevokedAndExpired(t *testing.T) {
	match := Match{Candidate: Candidate{
		Ref:    mcp.ServerRef{Server: "reports", Tool: "render_pdf", Version: "1.2.0"},
		Source: SourceSkill,
	}}
	base := mcp.Grant{ID: "g_1", Tenant: "tenant-a", Ref: match.Candidate.Ref, CreatedAt: testNow()}
	revoked := base
	revoked.Revoked = true
	expired := base
	expired.ExpiresAt = testNow().Add(-time.Minute)

	for name, grants := range map[string][]mcp.Grant{
		"revoked": {revoked},
		"expired": {expired},
		"none":    nil,
	} {
		if id, ok := match.GrantID(grants, testNow()); ok {
			t.Errorf("%s: GrantID = %q, want no grant", name, id)
		}
	}
	if id, ok := match.GrantID([]mcp.Grant{base}, testNow()); !ok || id != "g_1" {
		t.Errorf("live grant: GrantID = %q/%v, want g_1", id, ok)
	}
}

// fakeSandbox counts what a sandbox was asked to run.
type fakeSandbox struct{ programs []string }

func (f *fakeSandbox) Run(program string) (string, error) {
	f.programs = append(f.programs, program)
	return "sandboxed:" + program, nil
}

// TestCodeModeRefusesWithoutSandbox is the fourth acceptance criterion: the
// spike cannot be reached without both an explicit flag and a sandbox.
func TestCodeModeRefusesWithoutSandbox(t *testing.T) {
	if _, err := NewCodeMode(false, &fakeSandbox{}); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("NewCodeMode(false, sandbox) err = %v, want a disabled refusal", err)
	}
	if _, err := NewCodeMode(true, nil); err == nil || !strings.Contains(err.Error(), "requires a sandbox") {
		t.Errorf("NewCodeMode(true, nil) err = %v, want a sandbox refusal", err)
	}
	if _, err := NewCodeMode(false, nil); err == nil {
		t.Error("NewCodeMode(false, nil) succeeded, want a refusal")
	}

	// The zero value is inert even if a caller builds one by hand.
	var zero CodeMode
	if _, err := zero.Run("print(1)"); err == nil || !strings.Contains(err.Error(), "no sandbox") {
		t.Errorf("zero CodeMode.Run err = %v, want a refusal", err)
	}
	var nilHandle *CodeMode
	if _, err := nilHandle.Run("print(1)"); err == nil {
		t.Error("nil CodeMode.Run succeeded, want a refusal")
	}

	live, err := NewCodeMode(true, &fakeSandbox{})
	if err != nil {
		t.Fatalf("NewCodeMode(true, sandbox): %v", err)
	}
	if _, err := live.Run("   "); err == nil {
		t.Error("Run(empty) succeeded, want a refusal")
	}
}

// TestCodeModeRunsOnlyThroughTheSandbox proves the only execution path is the
// injected sandbox: the program reaches it exactly once and comes back tagged.
func TestCodeModeRunsOnlyThroughTheSandbox(t *testing.T) {
	sandbox := &fakeSandbox{}
	live, err := NewCodeMode(true, sandbox)
	if err != nil {
		t.Fatalf("NewCodeMode: %v", err)
	}
	out, err := live.Run("tools.listleads({})")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sandbox.programs) != 1 || sandbox.programs[0] != "tools.listleads({})" {
		t.Fatalf("sandbox received %v, want exactly one program", sandbox.programs)
	}
	if out != "sandboxed:tools.listleads({})" {
		t.Errorf("Run output = %q, want the sandbox's answer", out)
	}
}

// TestNoDirectExecutionPath is the static half of the Code Mode criterion: the
// package must not link a process, plugin or syscall execution facility, so no
// code path can run agent-authored code without going through a Sandbox.
func TestNoDirectExecutionPath(t *testing.T) {
	forbidden := []string{`"os/exec"`, `"plugin"`, "syscall.Exec", "syscall.ForkExec", "exec.Command", "exec.CommandContext"}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	scanned := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		scanned++
		for _, needle := range forbidden {
			if strings.Contains(string(raw), needle) {
				t.Errorf("%s references %s; code mode must run only through a Sandbox", name, needle)
			}
		}
	}
	if scanned < 3 {
		t.Fatalf("scanned %d files, expected the package sources", scanned)
	}
}
