// Package activation is the deterministic eligibility core of the activation
// engine (ADI-CTX-003), implementing docs/architecture/activation-policy.md.
//
// It answers one question: given the candidates carried by a context envelope,
// the tenant's pinned policy, the composition closure and the tenant's active
// grants, which capabilities may this run use — and why not the others?
//
// What it deliberately is not:
//
//   - Not an authorizer. `mcp.Gate` remains the only place a call executes, at
//     call time, per grant. A bug here can only make a run *less* capable.
//   - Not a store reader. Grants arrive as an input snapshot; this package has
//     no import of the MCP stores, mints no grant, installs nothing and mutates
//     no manifest (a static scan and a reflect test enforce that).
//   - Not time-dependent. The clock is injected, so two runs with the same
//     inputs produce byte-identical records (minus `decidedAt`).
//
// Vocabulary is mirrored from backend/pkg/mcp rather than imported, so the
// "no grant store" rule is a compile-time property; a parity test keeps the two
// copies from drifting.
package activation

// Kind is a capability namespace, exactly the reference grammar's first segment.
type Kind string

const (
	KindAgent       Kind = "agent"
	KindSkill       Kind = "skill"
	KindTool        Kind = "tool"
	KindKnowledge   Kind = "knowledge"
	KindModelPolicy Kind = "model-policy"
	KindEval        Kind = "eval"
)

// TrustLevel is provenance, never authority (mirrors the marketplace vocabulary).
type TrustLevel string

const (
	TrustPrivate   TrustLevel = "private"
	TrustCommunity TrustLevel = "community"
	TrustVerified  TrustLevel = "verified"
)

// SideEffect mirrors mcp.SideEffect: the reviewed Tool Manifest value.
type SideEffect string

const (
	SideEffectSafe         SideEffect = "safe"
	SideEffectExternal     SideEffect = "external"
	SideEffectIrreversible SideEffect = "irreversible"
)

// ApprovalClass mirrors mcp.ApprovalClass: the four manifest classes.
type ApprovalClass string

const (
	ApprovalExternalMessage ApprovalClass = "external-message"
	ApprovalExternalWrite   ApprovalClass = "external-write"
	ApprovalPayment         ApprovalClass = "payment"
	ApprovalHumanReview     ApprovalClass = "human-review"
)

// Scope is the context scope vocabulary the envelope already uses.
type Scope string

const (
	ScopeRun          Scope = "run"
	ScopeConversation Scope = "conversation"
	ScopeAgent        Scope = "agent"
	ScopeTeam         Scope = "team"
	ScopeTenant       Scope = "tenant"
)

// ResolutionMode records how the snapshot resolved a ref, so a tenant can say
// "bundled-only snapshots are not production".
type ResolutionMode string

const (
	ModeRegistry     ResolutionMode = "registry"
	ModeBundled      ResolutionMode = "bundled"
	ModeOfflineCache ResolutionMode = "offline-cache"
)

// Effect is the only thing a rule may do to a candidate.
type Effect string

const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
)

// DecisionKind is the closed decision vocabulary of the record.
type DecisionKind string

const (
	DecisionActivated DecisionKind = "activated"
	DecisionDenied    DecisionKind = "denied"
	DecisionSkipped   DecisionKind = "skipped"
)

// Stage names where a decision was made.
type Stage string

const (
	StageEligibility Stage = "eligibility"
	StageBudget      Stage = "budget"
	StageBound       Stage = "bound"
)

// Skipped reasons. "No allow" is default-deny: silence is never permission.
const (
	ReasonNoAllow = "no-allow"
	ReasonBudget  = "budget"
	ReasonBound   = "bound"
)
