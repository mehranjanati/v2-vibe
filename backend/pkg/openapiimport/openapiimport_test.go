package openapiimport

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"backend/pkg/mcp"
)

func loadFixture(t *testing.T) *Spec {
	t.Helper()
	raw, err := os.ReadFile("testdata/leads-api.yaml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	spec, err := Load(raw)
	if err != nil {
		t.Fatalf("Load(YAML fixture): %v", err)
	}
	return spec
}

func testOptions() Options {
	return Options{
		Publisher:        "Acme",
		License:          "Apache-2.0",
		Owner:            "acme/leads-api",
		Commit:           "sha:abc1234",
		Allow:            []string{"listLeads", "createLead", "getLead", "updateLead", "deleteLead"},
		TenantContextKey: "tenant_id",
	}
}

// TestLoadSpec proves both formats parse and that unsupported documents fail
// with a reason instead of an empty spec.
func TestLoadSpec(t *testing.T) {
	spec := loadFixture(t)
	if spec.OpenAPI != "3.0.3" {
		t.Errorf("OpenAPI = %q, want 3.0.3", spec.OpenAPI)
	}
	if len(spec.Paths) != 2 {
		t.Errorf("paths = %d, want 2", len(spec.Paths))
	}
	jsonDoc := `{"openapi":"3.1.0","info":{"title":"t","version":"1.0.0"},"paths":{"/x":{"get":{"operationId":"getX","responses":{"200":{"description":"ok"}}}}}}`
	fromJSON, err := Load([]byte(jsonDoc))
	if err != nil {
		t.Fatalf("Load(JSON): %v", err)
	}
	if fromJSON.Paths["/x"].Get == nil {
		t.Error("JSON document lost its /x GET operation")
	}

	cases := map[string]struct{ doc, want string }{
		"empty":    {"   ", "document is empty"},
		"swagger2": {`{"swagger":"2.0","paths":{}}`, "is not 3.x"},
		"no-paths": {`{"openapi":"3.0.0","info":{"version":"1.0.0"},"paths":{}}`, "declares no paths"},
		"garbage":  {"{{{{not yaml", "neither JSON nor YAML"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load([]byte(tc.doc))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestImportAllowedSurface is the main contract test: five operations become
// five pinned manifests whose authority fields follow the method.
func TestImportAllowedSurface(t *testing.T) {
	imported, err := Import(loadFixture(t), testOptions())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(imported) != 5 {
		t.Fatalf("imported %d manifests, want 5", len(imported))
	}
	// Sorted by tool name: create, delete, get, list, update.
	wantOrder := []string{"createlead", "deletelead", "getlead", "listleads", "updatelead"}
	for i, want := range wantOrder {
		if got := imported[i].Name(); got != want {
			t.Errorf("imported[%d].Name = %q, want %q", i, got, want)
		}
		if err := imported[i].Validate(); err != nil {
			t.Errorf("%s failed self-validation: %v", imported[i].Ref(), err)
		}
	}

	byName := map[string]Imported{}
	for _, imp := range imported {
		byName[imp.Name()] = imp
	}

	list := byName["listleads"]
	listSpec := list.Manifest["spec"].(map[string]interface{})
	if got := listSpec["sideEffect"]; got != "safe" {
		t.Errorf("GET sideEffect = %v, want safe", got)
	}
	if got := listSpec["permissions"].([]interface{})[0]; got != "leads:read" {
		t.Errorf("GET permission = %v, want leads:read", got)
	}
	if _, has := listSpec["idempotency"]; has {
		t.Error("GET must not declare an idempotency key")
	}

	create := byName["createlead"].Manifest["spec"].(map[string]interface{})
	if got := create["sideEffect"]; got != "external" {
		t.Errorf("POST sideEffect = %v, want external", got)
	}
	if got := create["approvals"].([]interface{}); len(got) != 1 || got[0] != "external-write" {
		t.Errorf("POST approvals = %v, want [external-write]", got)
	}
	if got := create["permissions"].([]interface{})[0]; got != "leads:create" {
		t.Errorf("POST permission = %v, want leads:create", got)
	}
	idem, ok := create["idempotency"].(map[string]interface{})
	if !ok || idem["key"] != "idempotencyKey" {
		t.Errorf("POST idempotency = %v, want the body idempotencyKey field", create["idempotency"])
	}

	del := byName["deletelead"].Manifest["spec"].(map[string]interface{})
	if got := del["sideEffect"]; got != "irreversible" {
		t.Errorf("DELETE sideEffect = %v, want irreversible", got)
	}
	if got := del["approvals"].([]interface{}); len(got) != 1 || got[0] != "human-review" {
		t.Errorf("DELETE approvals = %v, want [human-review]", got)
	}
	if idem, ok := del["idempotency"].(map[string]interface{}); !ok || idem["key"] != "id" {
		t.Errorf("DELETE idempotency = %v, want the id path parameter", del["idempotency"])
	}

	update := byName["updatelead"].Manifest["spec"].(map[string]interface{})
	if got := update["sideEffect"]; got != "external" {
		t.Errorf("PATCH sideEffect = %v, want external", got)
	}
	if got := update["permissions"].([]interface{})[0]; got != "leads:update" {
		t.Errorf("PATCH permission = %v, want leads:update", got)
	}

	meta := create["description"].(string)
	if !strings.Contains(meta, "Create a lead") {
		t.Errorf("description = %q, want the operation summary", meta)
	}
	if got := create["timeoutSeconds"]; got != 30 {
		t.Errorf("timeoutSeconds = %v, want the 30s default", got)
	}
	tenant := create["tenant"].(map[string]interface{})
	if tenant["isolation"] != "required" || tenant["contextKey"] != "tenant_id" {
		t.Errorf("tenant = %v, want required/tenant_id", tenant)
	}
	adapter := create["adapter"].(map[string]interface{})
	if adapter["type"] != "http" || adapter["toolName"] != "POST /leads" {
		t.Errorf("adapter = %v, want http POST /leads", adapter)
	}
}

// TestWholeSpecIsNotTheDefault proves the audited-surface rule: with no
// allow-list the import refuses, and a typo in the list fails instead of
// silently importing nothing.
func TestWholeSpecIsNotTheDefault(t *testing.T) {
	spec := loadFixture(t)
	opts := testOptions()
	opts.Allow = nil
	if _, err := Import(spec, opts); err == nil || !strings.Contains(err.Error(), "must name the surface") {
		t.Fatalf("Import(no allow-list) err = %v, want a refusal", err)
	}

	opts.Allow = []string{"NOSUCHOPERATION", "GET /nope"}
	for _, bad := range opts.Allow {
		opts.Allow = []string{bad}
		if _, err := Import(spec, opts); err == nil || !strings.Contains(err.Error(), "present in the spec") {
			t.Errorf("Import(allow=%q) err = %v, want a mismatch refusal", bad, err)
		}
	}

	// AllowAll is the explicit escape hatch, and it imports every operation.
	opts.Allow = nil
	opts.AllowAll = true
	all, err := Import(spec, opts)
	if err != nil {
		t.Fatalf("Import(AllowAll): %v", err)
	}
	if len(all) != 5 {
		t.Errorf("AllowAll imported %d, want all 5", len(all))
	}
}

// TestPathAllowEntryImportsEveryMethodOnIt proves the path form of the
// allow-list.
func TestPathAllowEntryImportsEveryMethodOnIt(t *testing.T) {
	opts := testOptions()
	opts.Allow = []string{"/leads/{id}"}
	imported, err := Import(loadFixture(t), opts)
	if err != nil {
		t.Fatalf("Import(/leads/{id}): %v", err)
	}
	if len(imported) != 3 {
		t.Fatalf("imported %d, want GET+PATCH+DELETE of /leads/{id}", len(imported))
	}
	for _, imp := range imported {
		if imp.Path != "/leads/{id}" {
			t.Errorf("%s imported from %q, want only /leads/{id}", imp.Name(), imp.Path)
		}
	}
}

// TestUnpinnedImportRejected proves the version pin is mandatory even when the
// spec declares a non-semver version.
func TestUnpinnedImportRejected(t *testing.T) {
	doc := `{"openapi":"3.0.0","info":{"title":"t","version":"v2-beta"},"paths":{"/x":{"get":{"operationId":"getX","responses":{"200":{"description":"ok"}}}}}}`
	spec, err := Load([]byte(doc))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	opts := testOptions()
	opts.Allow = []string{"getX"}
	if _, err := Import(spec, opts); err == nil || !strings.Contains(err.Error(), "must be pinned MAJOR.MINOR.PATCH") {
		t.Fatalf("err = %v, want a version-pin refusal", err)
	}
	opts.Version = "1.0.0"
	if _, err := Import(spec, opts); err != nil {
		t.Fatalf("Import with an explicit pin: %v", err)
	}

	opts.Publisher = ""
	if _, err := Import(spec, opts); err == nil || !strings.Contains(err.Error(), "publisher is required") {
		t.Errorf("err = %v, want a publisher refusal", err)
	}
}

// TestCredentialsBecomeVaultRefs is the security criterion: a credential never
// reaches a manifest, and the secret is a vault: reference.
func TestCredentialsBecomeVaultRefs(t *testing.T) {
	imported, err := Import(loadFixture(t), testOptions())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	for _, imp := range imported {
		raw, err := imp.Bytes()
		if err != nil {
			t.Fatalf("Bytes(%s): %v", imp.Name(), err)
		}
		for _, forbidden := range []string{"X-Api-Key", "Authorization", "bearerAuth", "apiKeyAuth"} {
			if bytes.Contains(raw, []byte(forbidden)) {
				t.Errorf("%s manifest mentions %q; credentials must be vault references only", imp.Name(), forbidden)
			}
		}
		spec := imp.Manifest["spec"].(map[string]interface{})
		secrets := spec["secrets"].([]interface{})
		if len(secrets) == 0 {
			t.Fatalf("%s has no secret reference although the document requires bearer auth", imp.Name())
		}
		for _, raw := range secrets {
			s, _ := raw.(string)
			if !strings.HasPrefix(s, "vault:") {
				t.Errorf("%s secret %q is not a vault: reference", imp.Name(), s)
			}
		}
		if !containsString(secrets, "vault:acme/bearerauth") {
			t.Errorf("%s secrets = %v, want the bearerAuth reference", imp.Name(), secrets)
		}
	}

	// The PATCH operation declares an Authorization header parameter; it must
	// add its own vault reference and stay out of the input schema.
	update := findByName(t, imported, "updatelead")
	spec := update.Manifest["spec"].(map[string]interface{})
	if !containsString(spec["secrets"].([]interface{}), "vault:acme/authorization") {
		t.Errorf("secrets = %v, want the Authorization reference", spec["secrets"])
	}
	input := spec["inputSchema"].(map[string]interface{})
	props := input["properties"].(map[string]interface{})
	if _, has := props["Authorization"]; has {
		t.Error("inputSchema still exposes the Authorization header")
	}
	if _, has := props["id"]; !has {
		t.Error("inputSchema lost the id path parameter")
	}

	// A non-credential header stays a real argument.
	create := findByName(t, imported, "createlead")
	createProps := create.Manifest["spec"].(map[string]interface{})["inputSchema"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, has := createProps["X-Request-Id"]; !has {
		t.Error("createLead lost the non-credential X-Request-Id header")
	}
	if _, has := createProps["email"]; !has {
		t.Error("createLead did not inline its body fields")
	}
}

// TestReimportIsIdempotent proves the acceptance criterion: importing twice
// adds nothing, and a changed manifest under the same version is refused.
func TestReimportIsIdempotent(t *testing.T) {
	spec := loadFixture(t)
	first, err := Import(spec, testOptions())
	if err != nil {
		t.Fatalf("Import #1: %v", err)
	}
	second, err := Import(spec, testOptions())
	if err != nil {
		t.Fatalf("Import #2: %v", err)
	}
	for i := range first {
		a, _ := first[i].Bytes()
		b, _ := second[i].Bytes()
		if !bytes.Equal(a, b) {
			t.Fatalf("%s is not deterministic across imports", first[i].Name())
		}
	}
	merged, err := Merge(first, second)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(merged) != len(first) {
		t.Errorf("Merge produced %d manifests, want the original %d (no duplicates)", len(merged), len(first))
	}

	// Same ref, different content: refused, because overwriting a reviewed
	// capability under an unchanged version is the drift the Gate rejects.
	changedOpts := testOptions()
	changedOpts.TimeoutSeconds = 45
	changed, err := Import(spec, changedOpts)
	if err != nil {
		t.Fatalf("Import(changed timeout): %v", err)
	}
	if _, err := Merge(first, changed); err == nil || !strings.Contains(err.Error(), "bump the version") {
		t.Fatalf("Merge(conflicting content) err = %v, want a version-bump refusal", err)
	}

	// Bumping the version is the sanctioned path and yields a different ref.
	bumped := testOptions()
	bumped.Version = "2.1.1"
	next, err := Import(spec, bumped)
	if err != nil {
		t.Fatalf("Import(bumped version): %v", err)
	}
	both, err := Merge(first, next)
	if err != nil {
		t.Fatalf("Merge(bumped): %v", err)
	}
	if len(both) != len(first)+len(next) {
		t.Errorf("Merge(bumped) = %d manifests, want %d", len(both), len(first)+len(next))
	}
}

// TestCanonicalBytesMatchTheDocsSerializer proves the output is in the canonical
// form the docs validator enforces: sorted keys, 2-space indent, trailing
// newline (scripts/lib/schema-subset.mjs serialize()).
func TestCanonicalBytesMatchTheDocsSerializer(t *testing.T) {
	imported, err := Import(loadFixture(t), testOptions())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	raw, err := imported[0].Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte("{\n  \"apiVersion\": \"vibesdk/v1\",\n  \"kind\": \"Tool\",\n")) {
		t.Errorf("canonical prefix is wrong:\n%s", string(raw[:min(len(raw), 120)]))
	}
	if !bytes.HasSuffix(raw, []byte("}\n")) {
		t.Error("canonical output must end with a newline after the closing brace")
	}
	// "metadata" sorts before "spec", and "apiVersion" before both.
	if !strings.Contains(string(raw), "\"metadata\"") || strings.Index(string(raw), "\"metadata\"") > strings.Index(string(raw), "\"spec\"") {
		t.Error("keys are not in sorted order")
	}
	// Re-serializing must be a no-op (stable canonical form).
	again, err := Imported{Manifest: imported[0].Manifest}.Bytes()
	if err != nil {
		t.Fatalf("Bytes (again): %v", err)
	}
	if !bytes.Equal(raw, again) {
		t.Error("serialization is not idempotent")
	}
}

// TestValidateRejectsBadManifest proves the self-check catches the mistakes an
// importer bug would produce before a reviewer ever sees them.
func TestValidateRejectsBadManifest(t *testing.T) {
	imported, err := Import(loadFixture(t), testOptions())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(Manifest)
		want   string
	}{
		{"wrong-kind", func(m Manifest) { m["kind"] = "Skill" }, "must be Tool"},
		{"unpinned-version", func(m Manifest) {
			m["metadata"].(map[string]interface{})["version"] = "latest"
		}, "must be pinned"},
		{"no-permissions", func(m Manifest) {
			m["spec"].(map[string]interface{})["permissions"] = []interface{}{}
		}, "at least one entry"},
		{"literal-secret", func(m Manifest) {
			m["spec"].(map[string]interface{})["secrets"] = []interface{}{"sk-live-123"}
		}, "vault: reference"},
		{"irreversible-without-approval", func(m Manifest) {
			m["spec"].(map[string]interface{})["approvals"] = []interface{}{}
		}, "approval class"},
		{"missing-tenant", func(m Manifest) {
			delete(m["spec"].(map[string]interface{}), "tenant")
		}, "tenant is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			copy := deepCopy(imported[0].Manifest)
			// The irreversible-without-approval case needs an irreversible
			// manifest, so force the side effect for every case.
			copy["spec"].(map[string]interface{})["sideEffect"] = "irreversible"
			tc.mutate(copy)
			err := Imported{Manifest: copy}.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// countingTransport stands in for the HTTP adapter: it records invocations, so a
// test can prove a denial happened before any HTTP call was made.
type countingTransport struct{ calls []string }

func (c *countingTransport) DiscoverTools(mcp.ServerRegistration) ([]mcp.DiscoveredTool, error) {
	return nil, nil
}

func (c *countingTransport) InvokeTool(_ mcp.ServerRegistration, tool, _ string) (string, error) {
	c.calls = append(c.calls, tool)
	return `{"ok":true}`, nil
}

type gateHarness struct {
	gate     *mcp.Gate
	grants   *mcp.Store
	policies *mcp.PolicyStore
	audit    *mcp.MemorySink
	tr       *countingTransport
	id       mcp.Identity
	server   string
}

func newGateHarness(t *testing.T, server string) *gateHarness {
	t.Helper()
	h := &gateHarness{
		grants: mcp.NewStore(), policies: mcp.NewPolicyStore(), audit: mcp.NewMemorySink(),
		tr: &countingTransport{}, id: mcp.Identity{Tenant: "tenant-a", User: "user-1", Agent: "reporter"},
		server: server,
	}
	reg := mcp.NewRegistry()
	if err := reg.Register(mcp.ServerRegistration{
		Name: server, Endpoint: "https://api.leads.example.com/v2", Tools: []string{"listleads", "deletelead"},
		RegisteredBy: "user-1", RegisteredAt: testNow(),
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	gate, err := mcp.NewGate(mcp.GateConfig{Registry: reg, Policies: h.policies, Grants: h.grants, Audit: h.audit, Now: testNow})
	if err != nil {
		t.Fatalf("NewGate: %v", err)
	}
	h.gate = gate
	return h
}

func testNow() time.Time { return time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC) }

// TestImportedToolConductsThroughTheGate is the second acceptance criterion:
// every imported tool runs only through mcp.Gate, so an ungranted call is
// denied before any HTTP request exists.
func TestImportedToolConductsThroughTheGate(t *testing.T) {
	imported, err := Import(loadFixture(t), testOptions())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	list := findByName(t, imported, "listleads")
	policy, err := list.Policy()
	if err != nil {
		t.Fatalf("Policy(listleads): %v", err)
	}
	h := newGateHarness(t, list.Server)
	if err := h.policies.Put(policy); err != nil {
		t.Fatalf("Put(policy): %v", err)
	}
	ref := mcp.ServerRef{Server: policy.Server, Tool: policy.Tool, Version: policy.ToolManifestVersion}

	// No grant: the Gate must refuse and the transport must stay untouched.
	if _, err := h.gate.Call(mcp.CallRequest{Identity: h.id, Ref: ref, ArgsJSON: `{}`}, h.tr); err == nil {
		t.Fatal("gate.Call succeeded without a grant")
	} else {
		var denied *mcp.DeniedError
		if !errors.As(err, &denied) || !strings.Contains(denied.Verdict, "no grant") {
			t.Fatalf("err = %v, want the no-grant denial", err)
		}
	}
	if len(h.tr.calls) != 0 {
		t.Errorf("transport ran %v, want nothing before authorization", h.tr.calls)
	}

	// With a grant the same call executes through the Gate exactly once.
	grant, err := h.grants.Issue(h.id, ref, policy, "user-1", false, testNow().Add(time.Hour), testNow())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := h.gate.Call(mcp.CallRequest{Identity: h.id, Ref: ref, GrantID: grant.ID, ArgsJSON: `{}`}, h.tr); err != nil {
		t.Fatalf("gate.Call with a grant: %v", err)
	}
	if len(h.tr.calls) != 1 {
		t.Errorf("transport calls = %v, want exactly one", h.tr.calls)
	}

	// An irreversible import (DELETE) carries human review all the way to the
	// Gate: the policy needs a live human approval, not a standing one.
	del := findByName(t, imported, "deletelead")
	delPolicy, err := del.Policy()
	if err != nil {
		t.Fatalf("Policy(deletelead): %v", err)
	}
	if !delPolicy.RequiresHuman {
		t.Fatal("DELETE policy should require human approval")
	}
	if _, err := h.grants.Issue(h.id, mcp.ServerRef{Server: delPolicy.Server, Tool: delPolicy.Tool, Version: delPolicy.ToolManifestVersion},
		delPolicy, "user-1", false, time.Time{}, testNow()); err == nil {
		t.Error("Issue without human approval succeeded for an irreversible import")
	}
}

// TestPolicyMapsAuthorityFields proves the manifest→policy mapping keeps every
// authority-bearing field (and adds nothing).
func TestPolicyMapsAuthorityFields(t *testing.T) {
	imported, err := Import(loadFixture(t), testOptions())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	update := findByName(t, imported, "updatelead")
	policy, err := update.Policy()
	if err != nil {
		t.Fatalf("Policy(updatelead): %v", err)
	}
	if policy.Server != "acme-api" || policy.Tool != "updatelead" || policy.ToolManifestVersion != "2.1.0" {
		t.Errorf("policy identity = %s/%s@%s, want acme-api/updatelead@2.1.0", policy.Server, policy.Tool, policy.ToolManifestVersion)
	}
	if policy.SideEffect != mcp.SideEffectExternal {
		t.Errorf("SideEffect = %q, want external", policy.SideEffect)
	}
	if len(policy.Approvals) != 1 || policy.Approvals[0] != mcp.ApprovalExternalWrite {
		t.Errorf("Approvals = %v, want [external-write]", policy.Approvals)
	}
	if !containsString(anyToStr(policy.Secrets), "vault:acme/bearerauth") {
		t.Errorf("Secrets = %v, want the bearer reference", policy.Secrets)
	}
	if policy.IdempotencyKey != "id" {
		t.Errorf("IdempotencyKey = %q, want id", policy.IdempotencyKey)
	}
	if policy.TimeoutSeconds != 30 {
		t.Errorf("TimeoutSeconds = %d, want 30", policy.TimeoutSeconds)
	}
}

// --- helpers ----------------------------------------------------------------

func findByName(t *testing.T, imported []Imported, name string) Imported {
	t.Helper()
	for _, imp := range imported {
		if imp.Name() == name {
			return imp
		}
	}
	t.Fatalf("imported set has no manifest named %q", name)
	return Imported{}
}

func containsString(set []interface{}, want string) bool {
	for _, raw := range set {
		if s, ok := raw.(string); ok && s == want {
			return true
		}
	}
	return false
}

func anyToStr(in []string) []interface{} {
	out := make([]interface{}, 0, len(in))
	for _, s := range in {
		out = append(out, s)
	}
	return out
}

// deepCopy clones a manifest through JSON so a mutating case cannot corrupt the
// fixture that later cases reuse.
func deepCopy(m Manifest) Manifest {
	raw, err := json.Marshal(map[string]interface{}(m))
	if err != nil {
		return nil
	}
	var out Manifest
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}
