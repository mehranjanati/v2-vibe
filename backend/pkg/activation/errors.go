package activation

import "fmt"

// Code is the closed failure vocabulary of the contract (§8).
type Code string

const (
	// CodeContext means the envelope is missing or unusable.
	CodeContext Code = "E_ACTIVATION_CONTEXT"
	// CodePolicy means the policy is unreadable, invalid, or would grant what
	// the composition never declared.
	CodePolicy Code = "E_ACTIVATION_POLICY"
	// CodeEscalation means a candidate is outside the snapshot closure. This is
	// a security event, not a bug report: it is refused and audited.
	CodeEscalation Code = "E_ACTIVATION_ESCALATION"
	// CodeBudget means eligibility left nothing while the policy required at
	// least one capability.
	CodeBudget Code = "E_ACTIVATION_BUDGET"
)

// AuditEvent is what a caller must append to the platform's existing audit
// sink. This package defines no sink of its own (one audit path, not two).
type AuditEvent struct {
	Code      Code   `json:"code"`
	TenantKey string `json:"tenantKey"`
	RunID     string `json:"runId"`
	Ref       string `json:"ref,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// Error is a refusal. It carries the machine code, the human detail, any events
// the caller must audit, and the record built so far (so a refusal is still
// attributable: "why did it stop" deserves the same trace as "why was it
// skipped").
type Error struct {
	Code   Code
	Detail string
	Events []AuditEvent
	Record *Record
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return "activation: " + string(e.Code)
	}
	return "activation: " + string(e.Code) + ": " + e.Detail
}

// Is lets callers branch on the code with errors.Is.
func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && other.Code == e.Code
}

// Result is a successful decision.
type Result struct {
	Record Record
	// Activated is the ordered set the run may use (already trimmed).
	Activated []string
	// Eligible is the pre-trim set, sorted, for tracing and tests.
	Eligible []string
}

func refusal(code Code, detail string, events []AuditEvent) *Error {
	return &Error{Code: code, Detail: detail, Events: events}
}

func policyErrorf(format string, args ...interface{}) *Error {
	return refusal(CodePolicy, fmt.Sprintf(format, args...), nil)
}
