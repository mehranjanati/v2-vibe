package mcp

import (
	"sort"
	"time"
)

// sortedStrings returns a sorted copy, used for order-independent policy
// comparison (permission and secret sets are sets, not sequences).
func sortedStrings(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	sort.Strings(out)
	return out
}

// sortedApprovals returns the approval classes in a stable order.
func sortedApprovals(in []ApprovalClass) []string {
	out := make([]string, 0, len(in))
	for _, a := range in {
		out = append(out, string(a))
	}
	sort.Strings(out)
	return out
}

// nowFunc is the clock seam shared by grant issuance and execution.
type nowFunc func() time.Time
