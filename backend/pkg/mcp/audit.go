package mcp

import (
	"sync"
	"time"
)

// Decision records whether an execution attempt was allowed.
type Decision string

const (
	// DecisionAllowed means every trust check passed and the call ran.
	DecisionAllowed Decision = "allowed"
	// DecisionDenied means a trust check failed and nothing ran.
	DecisionDenied Decision = "denied"
)

// AuditEvent is one execution-attempt record. The sink is append-only; the
// trust slice never mutates or deletes events.
type AuditEvent struct {
	// At is the attempt timestamp (UTC).
	At time.Time
	// Tenant, User, Agent carry the caller identity (never model text).
	Tenant string
	User   string
	Agent  string
	// Server, Tool, Version name the invoked MCP tool.
	Server  string
	Tool    string
	Version string
	// GrantID is the grant that authorized the call, empty when denied
	// before grant resolution.
	GrantID string
	// Decision is allowed or denied; Verdict explains which check decided.
	Decision Decision
	Verdict  string
	// ApprovalRef records the human/policy approval when one was required.
	ApprovalRef string
	// IdempotencyKey is the deduplication key the caller supplied.
	IdempotencyKey string
}

// AuditSink appends execution-attempt records. A production sink persists to
// the lineage/audit tables; this package ships an in-memory sink the trust
// gate tests against (see audit_test.go conventions in execution_test.go).
type AuditSink interface {
	// Append records one event. Implementations must not fail closed on
	// logging: a sink error must not grant authority it did not have.
	Append(AuditEvent)
	// Events returns a copy of all recorded events, oldest first.
	Events() []AuditEvent
}

// MemorySink is an in-memory AuditSink for the trust slice and its tests.
type MemorySink struct {
	mu     sync.RWMutex
	events []AuditEvent
}

// NewMemorySink returns an empty sink.
func NewMemorySink() *MemorySink { return &MemorySink{} }

// Append records one event.
func (s *MemorySink) Append(e AuditEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

// Events returns a copy of all recorded events, oldest first.
func (s *MemorySink) Events() []AuditEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AuditEvent, len(s.events))
	copy(out, s.events)
	return out
}

// ByTenant filters a copy of the events to one tenant.
func (s *MemorySink) ByTenant(tenant string) []AuditEvent {
	all := s.Events()
	kept := make([]AuditEvent, 0, len(all))
	for _, e := range all {
		if e.Tenant == tenant {
			kept = append(kept, e)
		}
	}
	return kept
}
