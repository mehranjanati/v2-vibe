package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"backend/pkg/composition"
	"backend/pkg/composition/bundle"
	"backend/pkg/plan"
)

// ---------- ADI-FOUND-004c: the snapshot producer on the live trigger path ----------
//
// An inline System document is resolved against the bundled source, persisted
// and compiled before anything durable is written. What these tests pin: a
// produced run starts bound and carries its plan identity; every refusal
// leaves zero instance rows and zero runtime instances; content addressing
// dedupes; first-writer-wins keeps tenants apart; and
// CONTROL_PLANE_REQUIRE_SNAPSHOT is finally usable with an inline document.

// visualWorkflowFixture is the repository's real procedural System: the same
// bytes the IR gate and the plan compiler tests use, so the route is proven
// against reviewed documents rather than invented ones.
const visualWorkflowFixture = "../../../docs/architecture/system-ir/examples/visual-workflow.system.json"

func systemFixture(t *testing.T, path string) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read system fixture %s: %v", path, err)
	}
	return json.RawMessage(data)
}

// mutateSystem rewrites a fixture document the way the resolver tests do.
func mutateSystem(t *testing.T, raw json.RawMessage, apply func(doc map[string]any)) json.RawMessage {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal system fixture: %v", err)
	}
	apply(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal system fixture: %v", err)
	}
	return out
}

// stubAsset is a catalog asset the bundle does not carry, supplied the way
// the resolver tests supply reserved namespaces (no manifest schema in v1).
func stubAsset(t *testing.T, ref, publisher string) composition.Asset {
	t.Helper()
	parts := strings.SplitN(ref, "@", 2)
	name := strings.SplitN(parts[0], "/", 2)[1]
	body, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"name": name, "publisher": publisher, "version": parts[1]},
	})
	if err != nil {
		t.Fatalf("marshal stub asset: %v", err)
	}
	return composition.Asset{Ref: ref, Kind: "agent", Publisher: publisher, Bytes: body}
}

// newProduceEnv wires the store and the bundled source exactly like main.go
// does when D1 is configured.
func newProduceEnv(t *testing.T, required bool, dags map[string]map[string]any) (*workflowEnv, *snapshotStoreFake) {
	t.Helper()
	env := newWorkflowEnv(t, p07Tokens(), dags)
	store := newSnapshotStoreFake()
	env.hub.SetSnapshotBinding(store, required)
	source, err := bundle.New()
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	env.hub.SetCompositionSource(source)
	return env, store
}

func produceBody(t *testing.T, workflowID string, system json.RawMessage) map[string]any {
	t.Helper()
	return map[string]any{
		"workflowId": workflowID,
		"input":      map[string]any{"lead": "acme"},
		"system":     system,
	}
}

// 1. The happy path: resolve, persist, compile, record, start.
func TestProduceResolvesPersistsCompilesAndStarts(t *testing.T) {
	env, store := newProduceEnv(t, false, map[string]map[string]any{"wf-a": generatedDagRow("user-a")})

	status, body := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "token-a",
		produceBody(t, "wf-a", systemFixture(t, visualWorkflowFixture)))
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (%v)", status, body)
	}
	data, _ := body["data"].(map[string]any)
	snapshotID, _ := data["snapshotId"].(string)
	if !strings.HasPrefix(snapshotID, "sha256:") {
		t.Fatalf("the response must name the produced snapshot, got %v", body)
	}
	if data["planKind"] != "workflow" {
		t.Fatalf("the procedural System compiles to a workflow plan, got %v", data["planKind"])
	}
	planDigest, _ := data["planDigest"].(string)
	if !strings.HasPrefix(planDigest, "sha256:") {
		t.Fatalf("the response must carry the plan digest, got %v", body)
	}
	if data["compilerVersion"] != "vibesdk-plan/0.1.0" {
		t.Fatalf("the response must carry the plan compiler version, got %v", body)
	}

	// The snapshot is stored and attributed to the run's tenant.
	if len(store.rows) != 1 {
		t.Fatalf("expected exactly one stored snapshot, got %d", len(store.rows))
	}
	if row, ok := store.rows[snapshotID]; !ok || row.TenantKey != "user-a" {
		t.Fatalf("the snapshot must be stored for the run's tenant: %v", store.rows)
	}

	// The run row carries the binding and the plan identity.
	rows := env.d1.instanceRows()
	if len(rows) != 1 {
		t.Fatalf("expected one recorded run, got %d", len(rows))
	}
	if rows[0]["snapshot_id"] != snapshotID {
		t.Fatalf("the run must record its snapshot, got %v", rows[0]["snapshot_id"])
	}
	if rows[0]["plan_kind"] != "workflow" || rows[0]["plan_digest"] != planDigest || rows[0]["compiler_version"] != "vibesdk-plan/0.1.0" {
		t.Fatalf("the run must record its plan identity: %v", rows[0])
	}
	if runs := env.d1.cloudflareRuns(); len(runs) != 1 {
		t.Fatalf("a produced run must start exactly one runtime instance, got %d", len(runs))
	}
}

// 2. Content addressing: the same document produces the same snapshot twice,
// and the second run reuses the row instead of writing a duplicate.
func TestProduceIsContentAddressed(t *testing.T) {
	env, store := newProduceEnv(t, false, map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
	system := systemFixture(t, visualWorkflowFixture)

	first := produceBody(t, "wf-a", system)
	first["input"] = map[string]any{"lead": "one"}
	second := produceBody(t, "wf-a", system)
	second["input"] = map[string]any{"lead": "two"}

	statusA, bodyA := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "token-a", first)
	statusB, bodyB := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "token-a", second)
	if statusA != http.StatusOK || statusB != http.StatusOK {
		t.Fatalf("both runs must start, got %d and %d (%v / %v)", statusA, statusB, bodyA, bodyB)
	}
	idA, _ := bodyA["data"].(map[string]any)["snapshotId"].(string)
	idB, _ := bodyB["data"].(map[string]any)["snapshotId"].(string)
	if idA == "" || idA != idB {
		t.Fatalf("the same document must produce the same snapshot, got %q and %q", idA, idB)
	}
	if len(store.rows) != 1 {
		t.Fatalf("content addressing must dedupe the snapshot, got %d rows", len(store.rows))
	}
	if rows := env.d1.instanceRows(); len(rows) != 2 {
		t.Fatalf("both runs must be recorded, got %d", len(rows))
	}
}

// 3. An agentic System resolves but is refused with E_BIND_SHAPE: no runtime
// node type runs an agent. Nothing references the produced snapshot, and the
// snapshot itself stays - it is valid content a future runtime may run.
func TestProduceRefusesAgenticShapeButKeepsTheSnapshot(t *testing.T) {
	env, store := newProduceEnv(t, false, map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
	base, err := bundle.New()
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	assets := append(base.Assets(), stubAsset(t, "agent/probe@1.0.0", "acme"))
	source, err := composition.NewBundledSource(assets...)
	if err != nil {
		t.Fatalf("compose source: %v", err)
	}
	env.hub.SetCompositionSource(source)

	agentic, err := json.Marshal(map[string]any{
		"apiVersion": "vibesdk/v1",
		"kind":       "System",
		"metadata":   map[string]any{"name": "agentic-probe", "publisher": "acme", "version": "1.0.0", "license": "Apache-2.0"},
		"spec": map[string]any{
			"outcome": "probe the agentic refusal",
			"agents":  map[string]any{"a": map[string]any{"ref": "agent/probe@1.0.0"}},
			"topology": []any{
				map[string]any{"on": "ingress.lead", "run": "a"},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal agentic system: %v", err)
	}

	status, body := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "token-a",
		produceBody(t, "wf-a", agentic))
	if status != http.StatusConflict || triggerCode(t, body) != string(plan.CodeShape) {
		t.Fatalf("expected 409 E_BIND_SHAPE, got %d (%v)", status, body)
	}
	if rows := env.d1.instanceRows(); len(rows) != 0 {
		t.Fatalf("a refused shape must not record a run: %v", rows)
	}
	if runs := env.d1.cloudflareRuns(); len(runs) != 0 {
		t.Fatalf("a refused shape must not start a runtime instance: %v", runs)
	}
	if len(store.rows) != 1 {
		t.Fatalf("the resolved composition stays stored for a future runtime, got %d rows", len(store.rows))
	}
}

// 4. A document that is not a valid System is a caller error: 400 E_IR, and
// nothing at all is persisted.
func TestProduceRefusesInvalidSystem(t *testing.T) {
	env, store := newProduceEnv(t, false, map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
	broken := mutateSystem(t, systemFixture(t, visualWorkflowFixture), func(doc map[string]any) {
		doc["metadata"].(map[string]any)["version"] = "1.0"
	})

	status, body := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "token-a",
		produceBody(t, "wf-a", broken))
	if status != http.StatusBadRequest || triggerCode(t, body) != string(composition.CodeIR) {
		t.Fatalf("expected 400 E_IR, got %d (%v)", status, body)
	}
	if len(store.rows) != 0 || len(env.d1.instanceRows()) != 0 {
		t.Fatalf("an invalid document must leave nothing behind: %v / %v", store.rows, env.d1.instanceRows())
	}
}

// 5. A capability the bundle does not carry is a resolution failure, not a
// silent fallback: 409 E_MISSING, nothing persisted.
func TestProduceRefusesAssetOutsideTheBundle(t *testing.T) {
	env, store := newProduceEnv(t, false, map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
	unknown := mutateSystem(t, systemFixture(t, visualWorkflowFixture), func(doc map[string]any) {
		nodes := doc["spec"].(map[string]any)["workflow"].(map[string]any)["nodes"].([]any)
		nodes[1].(map[string]any)["capability"] = "tool/absent@1.0.0"
	})

	status, body := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "token-a",
		produceBody(t, "wf-a", unknown))
	if status != http.StatusConflict || triggerCode(t, body) != string(composition.CodeMissing) {
		t.Fatalf("expected 409 E_MISSING, got %d (%v)", status, body)
	}
	if len(store.rows) != 0 || len(env.d1.instanceRows()) != 0 {
		t.Fatalf("a failed resolution must leave nothing behind: %v / %v", store.rows, env.d1.instanceRows())
	}
}

// 6/7. Missing configuration fails closed: no source or no store answers 503
// instead of degrading into an unattributed or unbound run.
func TestProduceWithoutSourceOrStoreIs503(t *testing.T) {
	envNoSource := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
	envNoSource.hub.SetSnapshotBinding(newSnapshotStoreFake(), false)
	status, body := workflowRequest(t, envNoSource.app, http.MethodPost, "/api/workflows/trigger", "token-a",
		produceBody(t, "wf-a", systemFixture(t, visualWorkflowFixture)))
	if status != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without a composition source, got %d (%v)", status, body)
	}
	if len(envNoSource.d1.instanceRows()) != 0 {
		t.Fatalf("a 503 must not record a run")
	}

	envNoStore := newWorkflowEnv(t, p07Tokens(), map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
	source, err := bundle.New()
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	envNoStore.hub.SetCompositionSource(source)
	status, body = workflowRequest(t, envNoStore.app, http.MethodPost, "/api/workflows/trigger", "token-a",
		produceBody(t, "wf-a", systemFixture(t, visualWorkflowFixture)))
	if status != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without a snapshot store, got %d (%v)", status, body)
	}
	if len(envNoStore.d1.instanceRows()) != 0 {
		t.Fatalf("a 503 must not record a run")
	}
}

// 8. The point of the whole line: with CONTROL_PLANE_REQUIRE_SNAPSHOT on, a
// run carrying an inline System document is admitted, because producing the
// snapshot satisfies the binding requirement.
func TestProduceSatisfiesRequireSnapshot(t *testing.T) {
	env, _ := newProduceEnv(t, true, map[string]map[string]any{"wf-a": generatedDagRow("user-a")})

	status, body := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "token-a",
		produceBody(t, "wf-a", systemFixture(t, visualWorkflowFixture)))
	if status != http.StatusOK {
		t.Fatalf("an inline System document must satisfy the binding requirement, got %d (%v)", status, body)
	}
	rows := env.d1.instanceRows()
	if len(rows) != 1 || rows[0]["snapshot_id"] == "" {
		t.Fatalf("the run must be bound: %v", rows)
	}
}

// 9. A run whose tenant cannot be resolved is refused: a snapshot nobody can
// attribute could never be admitted later, so producing it would create
// unreadable content.
func TestProduceRefusesUnknownTenant(t *testing.T) {
	env := newWorkflowEnv(t, nil, map[string]map[string]any{"wf-a": generatedDagRow(nil)})
	store := newSnapshotStoreFake()
	env.hub.SetSnapshotBinding(store, false)
	source, err := bundle.New()
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	env.hub.SetCompositionSource(source)

	status, body := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "",
		produceBody(t, "wf-a", systemFixture(t, visualWorkflowFixture)))
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unattributable run, got %d (%v)", status, body)
	}
	if len(store.rows) != 0 || len(env.d1.instanceRows()) != 0 {
		t.Fatalf("an unattributable run must leave nothing behind: %v / %v", store.rows, env.d1.instanceRows())
	}
}

// 10. First-writer-wins: an identical composition produced by another tenant
// is refused at write time instead of failing later at admission.
func TestProduceCrossTenantReuseIsRefused(t *testing.T) {
	env, store := newProduceEnv(t, false, map[string]map[string]any{
		"wf-a": generatedDagRow("user-a"),
		"wf-b": generatedDagRow("user-b"),
	})
	system := systemFixture(t, visualWorkflowFixture)

	status, _ := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "token-a", produceBody(t, "wf-a", system))
	if status != http.StatusOK {
		t.Fatalf("the first producer must succeed, got %d", status)
	}
	status, body := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "token-b", produceBody(t, "wf-b", system))
	if status != http.StatusConflict || triggerCode(t, body) != string(composition.CodeTenant) {
		t.Fatalf("expected 409 E_TENANT, got %d (%v)", status, body)
	}
	if rows := env.d1.instanceRows(); len(rows) != 1 {
		t.Fatalf("only the first tenant's run may exist, got %v", rows)
	}
	if len(store.rows) != 1 {
		t.Fatalf("the snapshot must not be rewritten, got %d rows", len(store.rows))
	}
}

// 11. The 004b path is unchanged: a run bound by snapshotId alone carries no
// plan identity, because its plan was not compiled here.
func TestSnapshotIdRunKeepsPlanIdentityEmpty(t *testing.T) {
	env, store := newProduceEnv(t, false, map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
	snapshot := testSnapshot(t, "user-a")
	if err := store.Persist(context.Background(), snapshot); err != nil {
		t.Fatalf("persist: %v", err)
	}

	status, body := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "token-a",
		map[string]any{"workflowId": "wf-a", "snapshotId": snapshot.SnapshotID})
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (%v)", status, body)
	}
	rows := env.d1.instanceRows()
	if len(rows) != 1 || rows[0]["snapshot_id"] != snapshot.SnapshotID {
		t.Fatalf("the run must carry its binding: %v", rows)
	}
	if rows[0]["plan_kind"] != "" || rows[0]["plan_digest"] != "" {
		t.Fatalf("a run bound without an inline document has no plan identity: %v", rows[0])
	}
}

// 12. Binding and producing are different intents; sending both is refused
// instead of silently picking one.
func TestProduceAndSnapshotIdAreMutuallyExclusive(t *testing.T) {
	env, store := newProduceEnv(t, false, map[string]map[string]any{"wf-a": generatedDagRow("user-a")})
	snapshot := testSnapshot(t, "user-a")
	if err := store.Persist(context.Background(), snapshot); err != nil {
		t.Fatalf("persist: %v", err)
	}

	status, body := workflowRequest(t, env.app, http.MethodPost, "/api/workflows/trigger", "token-a",
		map[string]any{
			"workflowId": "wf-a",
			"snapshotId": snapshot.SnapshotID,
			"system":     systemFixture(t, visualWorkflowFixture),
			"input":      map[string]any{},
		})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for the ambiguous request, got %d (%v)", status, body)
	}
	if len(env.d1.instanceRows()) != 0 {
		t.Fatalf("an ambiguous request must not record a run")
	}
}
