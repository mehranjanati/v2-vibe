package gateway

import "sync"

// Event is the concrete usage record. Attribution fields are filled from the
// caller's identity, never from model output.
type Event struct {
	// PolicyRef is the policy the caller asked for.
	PolicyRef string
	// ModelID and ProviderID are what actually ran.
	ModelID    string
	ProviderID string
	Usage      Usage
	CostUSD    float64
	// Agent, Tenant and RunID attribute the spend.
	Agent  string
	Tenant string
	RunID  string
	// FallbacksUsed counts candidates skipped before this one succeeded.
	FallbacksUsed int
	// Attempts lists every candidate tried, in order, with its error class.
	Attempts []Attempt
}

// Attempt is one candidate dispatch inside a routed call.
type Attempt struct {
	ModelID    string
	ProviderID string
	ErrorClass ErrorClass
	Error      string
}

// UsageSink records one Event per successful call. A sink error must not fail
// the call — accounting is important, but not more important than the answer.
type UsageSink interface {
	Record(Event)
}

// MemoryUsage is the in-process sink used by tests and the trust slice.
type MemoryUsage struct {
	Events []Event
}

// Record implements UsageSink.
func (m *MemoryUsage) Record(e Event) { m.Events = append(m.Events, e) }

// Health tracks consecutive failures per provider so routing can deprioritize
// a sick provider instead of paying a timeout on every call.
type Health struct {
	mu       sync.Mutex
	failures map[string]int
	// Threshold is the consecutive-failure count that marks a provider
	// unhealthy for the current call. Zero means 3.
	Threshold int
}

// NewHealth returns an empty health tracker.
func NewHealth() *Health { return &Health{failures: make(map[string]int)} }

// Healthy reports whether a provider may be tried.
func (h *Health) Healthy(providerID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	limit := h.Threshold
	if limit <= 0 {
		limit = 3
	}
	return h.failures[providerID] < limit
}

// Failed records a provider failure.
func (h *Health) Failed(providerID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failures[providerID]++
}

// Succeeded clears a provider's failure count.
func (h *Health) Succeeded(providerID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.failures, providerID)
}

// FailureCount reports the consecutive failure count (tests/inspection).
func (h *Health) FailureCount(providerID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.failures[providerID]
}
