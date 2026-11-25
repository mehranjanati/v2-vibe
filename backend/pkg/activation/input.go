package activation

// Match is the closed selector set. Every field present must match (AND); lists
// are OR within themselves. Run-time state (cost, step count, model output, the
// task text) is deliberately not selectable: that would make eligibility
// non-deterministic and un-reviewable.
type Match struct {
	Kinds           []Kind           `json:"kinds,omitempty"`
	Refs            []string         `json:"refs,omitempty"` // exact ref or `kind/name@*`
	Publishers      []string         `json:"publishers,omitempty"`
	TrustLevels     []TrustLevel     `json:"trustLevels,omitempty"`
	SideEffects     []SideEffect     `json:"sideEffects,omitempty"`
	Permissions     []string         `json:"permissions,omitempty"` // subset of the grant's
	Approvals       []ApprovalClass  `json:"approvals,omitempty"`
	RequiresHuman   *bool            `json:"requiresHuman,omitempty"`
	Scopes          []Scope          `json:"scopes,omitempty"`
	ResolutionModes []ResolutionMode `json:"resolutionModes,omitempty"`
}

// Rule is one policy rule. A deny always carries a reason: a refusal nobody can
// explain is not a policy.
type Rule struct {
	ID     string `json:"id"`
	Effect Effect `json:"effect"`
	Reason string `json:"reason,omitempty"`
	Match  Match  `json:"match"`
}

// Policy is one pinned policy version. `MaxCandidates` is the policy's own
// budget (0 = none) and is applied before the envelope's candidate bound, so a
// `budget` skip and a `bound` skip stay distinguishable in the record.
type Policy struct {
	Ref             string `json:"ref"` // activation-policy/name@MAJOR.MINOR.PATCH
	MinCapabilities int    `json:"minCapabilities,omitempty"`
	MaxCandidates   int    `json:"maxCandidates,omitempty"`
	Rules           []Rule `json:"rules"`
}

// Facts are the effective, reviewed facts of one candidate (from the snapshot).
type Facts struct {
	Kind           Kind            `json:"kind"`
	Ref            string          `json:"ref"`
	Publisher      string          `json:"publisher,omitempty"`
	TrustLevel     TrustLevel      `json:"trustLevel,omitempty"`
	SideEffect     SideEffect      `json:"sideEffect,omitempty"`
	Permissions    []string        `json:"permissions,omitempty"`
	Approvals      []ApprovalClass `json:"approvals,omitempty"`
	RequiresHuman  bool            `json:"requiresHuman,omitempty"`
	Scope          Scope           `json:"scope,omitempty"`
	ResolutionMode ResolutionMode  `json:"resolutionMode,omitempty"`
}

// Grant is one active tenant grant, as a snapshot. The caller decides which
// grants are active (revoked/expired ones must not be passed); this package
// never reads a store, so it cannot widen a tenant's authority on its own.
type Grant struct {
	Ref         string   `json:"ref"`
	Permissions []string `json:"permissions,omitempty"`
}

// Context is the subset of the context envelope activation consumes. The
// envelope itself (assembly) is not re-implemented here.
type Context struct {
	ContextID      string `json:"contextId"`
	SnapshotID     string `json:"snapshotId"`
	TenantKey      string `json:"tenantKey"`
	CandidateLimit int    `json:"candidateLimit"`
}

// Ranking is stage B's input. It may order and trim the eligible set; it may
// never introduce a ref (the engine intersects it with eligibility).
type Ranking struct {
	Source  string   `json:"source"`
	Version string   `json:"version,omitempty"`
	Order   []string `json:"order,omitempty"`
}

// Decision is one entry of the record: always attributable — an activated
// entry carries a rule id, and a skipped/denied one carries a reason.
type Decision struct {
	Candidate        string          `json:"candidate"`
	Decision         DecisionKind    `json:"decision"`
	RuleID           string          `json:"ruleId,omitempty"`
	Reason           string          `json:"reason,omitempty"`
	Stage            Stage           `json:"stage"`
	ApprovalRequired []ApprovalClass `json:"approvalRequired,omitempty"`
}

// Record is the artifact that makes "why did it not use the CRM tool?"
// answerable. Activations require, never satisfy, approvals: this type has no
// field that could claim an approval was given.
type Record struct {
	ActivationPolicyRef string     `json:"activationPolicyRef"`
	CompositionDigest   string     `json:"compositionDigest"`
	ContextID           string     `json:"contextId"`
	DecidedAt           string     `json:"decidedAt,omitempty"` // injected clock; excluded from Canonical
	Decisions           []Decision `json:"decisions"`
	Ranking             Ranking    `json:"ranking"`
	RunID               string     `json:"runId"`
	TenantKey           string     `json:"tenantKey"`
}
