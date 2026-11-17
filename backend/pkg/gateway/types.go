package gateway

import (
	"errors"
	"fmt"
)

// Message is one chat turn. The gateway stays provider-neutral: no provider
// fields, no tool-call encoding beyond plain text content in v1.
type Message struct {
	Role    string `json:"role"` // system | user | assistant
	Content string `json:"content"`
}

// ChatRequest is the caller's request. It never names a provider or a model —
// it names a policy; the gateway resolves the rest.
type ChatRequest struct {
	Messages []Message
	// PolicyRef is `model-policy/name@MAJOR.MINOR.PATCH`. Required.
	PolicyRef string
	// MaxTokens overrides the policy default for this call (never above the
	// policy ceiling). Zero means "use the policy".
	MaxTokens int
	// BudgetUSD caps the estimated cost of this single call. Zero means
	// "use the policy budget".
	BudgetUSD float64
}

// Usage is token accounting for one provider call.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// ChatResponse is the successful result plus the attribution the platform
// needs for traces and cost rollups (P7.7).
type ChatResponse struct {
	Content string
	// ModelID and ProviderID identify what actually ran, not what was asked.
	ModelID    string
	ProviderID string
	Usage      Usage
	// CostUSD is the estimated cost from the registry's price table.
	CostUSD float64
	// FallbacksUsed counts how many candidates were skipped before success.
	FallbacksUsed int
}

// ErrorClass classifies provider failures so routing can decide what is
// retryable instead of retrying everything blindly.
type ErrorClass string

const (
	// ErrRetryable: rate limit, timeout, 5xx, transient transport.
	ErrRetryable ErrorClass = "retryable"
	// ErrFatal: auth, bad request, unsupported model — try the next candidate.
	ErrFatal ErrorClass = "fatal"
	// ErrBudget: the call was refused before dispatch (cost/token/window).
	ErrBudget ErrorClass = "budget"
)

// ProviderError is what adapters return; routing reads the class, callers see
// the wrapped error.
type ProviderError struct {
	Provider string
	Class    ErrorClass
	Status   int
	Message  string
	Err      error
}

func (e *ProviderError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("gateway: provider %s (%s): %s: %v", e.Provider, e.Class, e.Message, e.Err)
	}
	return fmt.Sprintf("gateway: provider %s (%s): %s", e.Provider, e.Class, e.Message)
}

func (e *ProviderError) Unwrap() error { return e.Err }

// ClassOf reports the error class of err, defaulting to retryable for
// unknown errors (a transport blip is the common case).
func ClassOf(err error) ErrorClass {
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe.Class
	}
	return ErrRetryable
}
