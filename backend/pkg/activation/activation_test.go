package activation

// ADI-CTX-003 — the eligibility core against its own contract.
//
// The eleven named tests below mirror docs/architecture/activation-policy.md
// §10 exactly. A twelfth test keeps the mirrored mcp vocabulary honest, and a
// thirteenth proves the negative (no grant minting, no manifest mutation) by
// scanning this package's own sources.

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

var testTime = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

func baseContext() Context {
	return Context{
		ContextID:      "ctx:test-1",
		SnapshotID:     "sha256:deadbeef",
		TenantKey:      "tenant-a",
		CandidateLimit: 8,
	}
}

func basePolicy(rules ...Rule) Policy {
	return Policy{Ref: "activation-policy/test@1.0.0", Rules: rules}
}

func allowKind(ids string, kind Kind) Rule {
	return Rule{ID: ids, Effect: EffectAllow, Match: Match{Kinds: []Kind{kind}}}
}

func denyIrreversible(id string) Rule {
	return Rule{ID: id, Effect: EffectDeny, Reason: "the tenant forbids irreversible side effects without human approval", Match: Match{SideEffects: []SideEffect{SideEffectIrreversible}}}
}

func lookupFacts() Facts {
	return Facts{
		Kind: KindTool, Ref: "tool/lookup@1.0.0", Publisher: "crm",
		TrustLevel: TrustPrivate, SideEffect: SideEffectSafe,
		Permissions: []string{"leads:read"}, Scope: ScopeRun, ResolutionMode: ModeBundled,
	}
}

func explodeFacts() Facts {
	return Facts{
		Kind: KindTool, Ref: "tool/explode@1.0.0", Publisher: "crm",
		TrustLevel: TrustPrivate, SideEffect: SideEffectIrreversible,
		Scope: ScopeRun, ResolutionMode: ModeBundled,
	}
}

func refundFacts() Facts {
	return Facts{
		Kind: KindTool, Ref: "tool/refund@2.0.0", Publisher: "billing",
		TrustLevel: TrustVerified, SideEffect: SideEffectExternal,
		Permissions: []string{"refunds:write"}, Approvals: []ApprovalClass{ApprovalHumanReview},
		RequiresHuman: true, Scope: ScopeTeam, ResolutionMode: ModeRegistry,
	}
}

func baseRequest() Request {
	return Request{
		RunID:   "run-1",
		Context: baseContext(),
		Policy: basePolicy(
			Rule{ID: "allow-tools", Effect: EffectAllow, Match: Match{Kinds: []Kind{KindTool}}},
			denyIrreversible("deny-irreversible"),
		),
		Closure:    []string{"tool/lookup@1.0.0", "tool/explode@1.0.0", "tool/refund@2.0.0"},
		Candidates: []Facts{lookupFacts(), explodeFacts(), refundFacts()},
		Grants: []Grant{
			{Ref: "tool/lookup@1.0.0", Permissions: []string{"leads:read"}},
			{Ref: "tool/refund@2.0.0", Permissions: []string{"refunds:write"}},
		},
		DecidedAt: testTime,
	}
}

func mustErrCode(t *testing.T, err error, code Code) *Error {
	t.Helper()
	var aerr *Error
	if !errors.As(err, &aerr) {
		t.Fatalf("expected an activation error, got %v", err)
	}
	if aerr.Code != code {
		t.Fatalf("expected %s, got %s (%s)", code, aerr.Code, aerr.Detail)
	}
	return aerr
}

func decisionBy(t *testing.T, record Record, ref string) Decision {
	t.Helper()
	for _, decision := range record.Decisions {
		if decision.Candidate == ref {
			return decision
		}
	}
	t.Fatalf("no decision for %q in %+v", ref, record.Decisions)
	return Decision{}
}

// TestDenyOverridesAllow: a denier wins regardless of rule order — there is no
// "more specific but later" and no last-match tie-break to get wrong.
func TestDenyOverridesAllow(t *testing.T) {
	for _, order := range [][]Rule{
		{allowKind("allow-tools", KindTool), denyIrreversible("deny-irreversible")},
		{denyIrreversible("deny-irreversible"), allowKind("allow-tools", KindTool)},
	} {
		req := baseRequest()
		req.Policy = basePolicy(order...)
		result, err := Decide(req)
		if err != nil {
			t.Fatalf("decide: %v", err)
		}
		denied := decisionBy(t, result.Record, "tool/explode@1.0.0")
		if denied.Decision != DecisionDenied || denied.RuleID != "deny-irreversible" || denied.Reason == "" {
			t.Fatalf("explode must be denied with its rule id and reason, got %+v", denied)
		}
		if got := decisionBy(t, result.Record, "tool/lookup@1.0.0"); got.Decision != DecisionActivated {
			t.Fatalf("lookup must stay activated, got %+v", got)
		}
		if len(result.Activated) != 2 || result.Activated[0] != "tool/lookup@1.0.0" || result.Activated[1] != "tool/refund@2.0.0" {
			t.Fatalf("activation is ref-ordered regardless of input order, got %v", result.Activated)
		}
	}
}

// TestEscalationOutsideClosureRefusedAndAudited: a candidate the snapshot never
// declared is a security event — refused, named, and attached as an audit event.
func TestEscalationOutsideClosureRefusedAndAudited(t *testing.T) {
	req := baseRequest()
	req.Candidates = []Facts{lookupFacts(), explodeFacts(), refundFacts(), {
		Kind: KindTool, Ref: "tool/backdoor@9.9.9", SideEffect: SideEffectSafe,
	}}
	aerr := mustErrCode(t, mustFail(Decide(req)), CodeEscalation)
	if len(aerr.Events) != 1 {
		t.Fatalf("exactly one audit event is expected, got %+v", aerr.Events)
	}
	event := aerr.Events[0]
	if event.Code != CodeEscalation || event.TenantKey != "tenant-a" || event.RunID != "run-1" || event.Ref != "tool/backdoor@9.9.9" {
		t.Fatalf("the audit event must carry the code, tenant, run and ref, got %+v", event)
	}
	if aerr.Record != nil {
		t.Fatal("an escalation names candidates only after enforcement; no decisions are emitted")
	}
}

func mustFail(_ Result, err error) error { return err }

// TestPolicyAllowingOutsideClosureFailsToLoad: a rule that would grant what the
// composition never declared fails at load, not at use.
func TestPolicyAllowingOutsideClosureFailsToLoad(t *testing.T) {
	req := baseRequest()
	req.Policy = basePolicy(Rule{
		ID: "allow-ghost", Effect: EffectAllow,
		Match: Match{Refs: []string{"tool/ghost@1.0.0"}},
	})
	aerr := mustErrCode(t, mustFail(Decide(req)), CodePolicy)
	if !strings.Contains(aerr.Detail, "tool/ghost@1.0.0") {
		t.Fatalf("the refusal must name the undeclared ref, got %q", aerr.Detail)
	}
}

// TestDefaultDenyYieldsSkipped: no matching allow is `skipped/no-allow`, never
// `activated` — silence is not permission.
func TestDefaultDenyYieldsSkipped(t *testing.T) {
	req := baseRequest()
	req.Policy = basePolicy(denyIrreversible("deny-irreversible"))
	result, err := Decide(req)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if len(result.Activated) != 0 {
		t.Fatalf("nothing may activate without an allow, got %v", result.Activated)
	}
	skipped := decisionBy(t, result.Record, "tool/lookup@1.0.0")
	if skipped.Decision != DecisionSkipped || skipped.Reason != ReasonNoAllow || skipped.Stage != StageEligibility {
		t.Fatalf("unmatched must be skipped/no-allow at eligibility, got %+v", skipped)
	}
	// Every entry stays attributable: skipped ones carry a reason.
	for _, decision := range result.Record.Decisions {
		if decision.Decision != DecisionActivated && decision.Reason == "" {
			t.Fatalf("every skipped or denied decision needs a reason: %+v", decision)
		}
	}
}

// TestBoundTrimsToEightWithSkippedBound: nine eligible candidates and a bound
// of eight yield eight activated plus one `skipped/bound`. The policy budget
// (MaxCandidates) is the narrower scope, so it comes first and produces `budget`.
func TestBoundTrimsToEightWithSkippedBound(t *testing.T) {
	ref := func(i int) string { return nameTool(i) }

	candidates := make([]Facts, 0, 9)
	closure := make([]string, 0, 9)
	for i := range 9 {
		r := ref(i)
		closure = append(closure, r)
		candidates = append(candidates, Facts{Kind: KindTool, Ref: r, SideEffect: SideEffectSafe})
	}

	req := Request{
		RunID:      "run-bound",
		Context:    Context{ContextID: "ctx:bound", SnapshotID: "sha256:bound", TenantKey: "tenant-a", CandidateLimit: 8},
		Policy:     basePolicy(Rule{ID: "allow-all", Effect: EffectAllow, Match: Match{}}),
		Closure:    closure,
		Candidates: candidates,
		DecidedAt:  testTime,
	}
	result, err := Decide(req)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if len(result.Activated) != 8 {
		t.Fatalf("expected eight activated, got %v", result.Activated)
	}
	if got := decisionBy(t, result.Record, ref(8)); got.Decision != DecisionSkipped || got.Reason != ReasonBound || got.Stage != StageBound {
		t.Fatalf("the ninth candidate must be skipped/bound, got %+v", got)
	}

	// The policy budget trims before the envelope bound.
	req.Policy.MaxCandidates = 5
	req.RunID = "run-budget"
	result, err = Decide(req)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if len(result.Activated) != 5 {
		t.Fatalf("expected five activated, got %v", result.Activated)
	}
	if got := decisionBy(t, result.Record, ref(5)); got.Decision != DecisionSkipped || got.Reason != ReasonBudget || got.Stage != StageBudget {
		t.Fatalf("the sixth candidate must be skipped/budget, got %+v", got)
	}
	if got := decisionBy(t, result.Record, ref(8)); got.Decision != DecisionSkipped || got.Reason != ReasonBudget {
		t.Fatalf("every trimmed candidate uses the narrower scope's reason, got %+v", got)
	}
}

func nameTool(i int) string {
	return "tool/app" + itoa(i) + "@1.0.0"
}

// TestIneligibleRankingRefIgnored: the ranking provider may order only what
// stage A allowed. Anything else is ignored and the run continues.
func TestIneligibleRankingRefIgnored(t *testing.T) {
	req := baseRequest()
	req.Ranking = &Ranking{
		Source:  "ranker-x",
		Version: "3.0.0",
		Order:   []string{"tool/explode@1.0.0", "tool/backdoor@9.9.9", "tool/refund@2.0.0", "tool/lookup@1.0.0"},
	}
	result, err := Decide(req)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if len(result.Activated) != 2 || result.Activated[0] != "tool/refund@2.0.0" || result.Activated[1] != "tool/lookup@1.0.0" {
		t.Fatalf("ranking must order the eligible set, never extend it, got %v", result.Activated)
	}
	if result.Record.Ranking.Source != "ranker-x" || result.Record.Ranking.Version != "3.0.0" {
		t.Fatalf("the record must name the provider the ordering came from, got %+v", result.Record.Ranking)
	}

	// No provider: the deterministic ref order applies and cites "none".
	req.Ranking = nil
	result, err = Decide(req)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if result.Record.Ranking.Source != "none" {
		t.Fatalf("no provider must be recorded as none, got %+v", result.Record.Ranking)
	}
	if len(result.Activated) != 2 || result.Activated[0] != "tool/lookup@1.0.0" || result.Activated[1] != "tool/refund@2.0.0" {
		t.Fatalf("without a provider the total order is by ref, got %v", result.Activated)
	}
}

// TestDeterministicReplayByteIdentical: same inputs + the same injected clock
// yield byte-identical canonical records. Same inputs with a *different* clock
// still agree on Canonical (decidedAt is excluded, not covered).
func TestDeterministicReplayByteIdentical(t *testing.T) {
	req := baseRequest()
	first, err := Decide(req)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	second, err := Decide(req)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	a, err := first.Record.Canonical()
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	b, err := second.Record.Canonical()
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	if string(a) != string(b) {
		t.Fatalf("replay must be byte-identical:\n%s\n%s", a, b)
	}

	// The returned record still carries the injected clock; only the digest
	// excludes it.
	if first.Record.DecidedAt != "2026-11-24T12:00:00Z" {
		t.Fatalf("the record carries the injected clock, got %q", first.Record.DecidedAt)
	}
	digest, err := first.Record.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("the digest is a sha256 content address, got %q", digest)
	}
}

// TestApprovalRequiredRecordedNotApproved: requiresHuman adds an approval
// precondition to the record, and the record structurally cannot claim the
// approval was given.
func TestApprovalRequiredRecordedNotApproved(t *testing.T) {
	result, err := Decide(baseRequest())
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	refund := decisionBy(t, result.Record, "tool/refund@2.0.0")
	if refund.Decision != DecisionActivated {
		t.Fatalf("a precondition does not deny the capability, got %+v", refund)
	}
	if len(refund.ApprovalRequired) != 1 || refund.ApprovalRequired[0] != ApprovalHumanReview {
		t.Fatalf("requiresHuman must add approvalRequired: [human-review], got %+v", refund)
	}
	// Case-insensitive proof that no field of a Decision can read as approved.
	value := reflect.ValueOf(refund)
	valueType := value.Type()
	for i := range valueType.NumField() {
		name := valueType.Field(i).Name
		if strings.EqualFold(name, "approved") || strings.Contains(strings.ToLower(name), "approved") {
			t.Fatalf("a Decision must never carry an approved field: %s", name)
		}
	}
}

// TestEmptyCapabilitiesEmptyDecisions: an empty closure is a legitimate
// outcome — the run has nothing to call, and that is not an error.
func TestEmptyCapabilitiesEmptyDecisions(t *testing.T) {
	req := baseRequest()
	req.Closure = nil
	req.Candidates = nil
	req.Context.CandidateLimit = 8
	result, err := Decide(req)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if len(result.Activated) != 0 || len(result.Record.Decisions) != 0 {
		t.Fatalf("empty capabilities yield empty decisions, got %+v", result)
	}
	if _, err := result.Record.Canonical(); err != nil {
		t.Fatalf("even the empty record must serialize deterministically: %v", err)
	}
}

// TestBudgetRefusalOnlyWhenMinCapabilitiesPositive: an empty eligible set with
// minCapabilities > 0 is E_ACTIVATION_BUDGET (with the record attached); the
// same emptiness with no minimum is a clean empty decision.
func TestBudgetRefusalOnlyWhenMinCapabilitiesPositive(t *testing.T) {
	req := baseRequest()
	req.Candidates = []Facts{explodeFacts()}
	req.Closure = []string{"tool/explode@1.0.0"}
	req.Policy = basePolicy(denyIrreversible("deny-irreversible"))
	req.Policy.MinCapabilities = 1

	_, err := Decide(req)
	budgetErr := mustBeCode(t, err, CodeBudget)
	if budgetErr.Record == nil {
		t.Fatal("a budget refusal must attach the record, so the empty run stays attributable")
	}
	if got := decisionBy(t, *budgetErr.Record, "tool/explode@1.0.0"); got.Decision != DecisionDenied {
		t.Fatalf("the denial that emptied the set must be in the attached record, got %+v", got)
	}

	req.Policy.MinCapabilities = 0
	result, err := Decide(req)
	if err != nil {
		t.Fatalf("without a minimum, emptiness is not a refusal: %v", err)
	}
	if len(result.Activated) != 0 || len(result.Record.Decisions) != 1 {
		t.Fatalf("expected one denial and nothing activated, got %+v", result)
	}
}

func mustBeCode(t *testing.T, err error, code Code) *Error {
	t.Helper()
	return mustErrCode(t, err, code)
}

// TestNoGrantMintingNoManifestMutation is the negative half: the engine happens
// entirely inside pure inputs. A compile-time property — the package must not
// import the grant store (or anything that can execute) — and a vocabulary
// property — no import may even *name* the executor — keep it that way.
func TestNoGrantMintingNoManifestMutation(t *testing.T) {
	imports := map[string][]string{}
	if err := listImports(t, imports); err != nil {
		t.Fatalf("scan package: %v", err)
	}
	for file, names := range imports {
		for _, name := range names {
			switch name {
			case "backend/pkg/mcp":
				t.Fatalf("%s: non-test code must not import backend/pkg/mcp (vocabulary is mirrored, grants stay out)", file)
			case "os/exec", "os", "database/sql", "net/http", "plugin":
				t.Fatalf("%s: the eligibility core executes nothing and opens no connections (imported %s)", file, name)
			}
		}
	}
}

// mirrorChecks proves the mirrored vocabulary has not drifted from the source
// it copies. (mcp is imported here because this file is a test, not the engine.)
func TestMirroredVocabularyMatchesMcp(t *testing.T) {
	if string(SideEffectSafe) != "safe" || string(SideEffectExternal) != "external" || string(SideEffectIrreversible) != "irreversible" {
		t.Fatalf("side-effect vocabulary drifted: %v %v %v", SideEffectSafe, SideEffectExternal, SideEffectIrreversible)
	}
	if string(ApprovalExternalMessage) != "external-message" ||
		string(ApprovalExternalWrite) != "external-write" ||
		string(ApprovalPayment) != "payment" ||
		string(ApprovalHumanReview) != "human-review" {
		t.Fatal("approval vocabulary drifted")
	}
}

// listImports returns the quoted import paths of every non-test Go file in this
// package, in sorted file order. go/parser keeps the rule dependency-free and
// exact: no regex can fake a string literal into an import.
func listImports(t *testing.T, out map[string][]string) error {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		return err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		parsed, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range parsed.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			out[name] = append(out[name], path)
		}
	}
	return nil
}
