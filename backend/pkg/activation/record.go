package activation

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"backend/pkg/composition"
	"backend/pkg/ir"
)

// formatDecidedAt renders the injected clock. A zero clock is a caller error,
// not "now": guessing would break replay.
func formatDecidedAt(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339)
}

// orderByRanking applies stage B: only eligible refs survive, ranking order
// first, then the rest by ref ascending. Ties break on ref, so the total order
// never depends on map iteration.
func orderByRanking(eligible []string, ranking *Ranking) []string {
	allowed := make(map[string]bool, len(eligible))
	for _, ref := range eligible {
		allowed[ref] = true
	}
	ordered := make([]string, 0, len(eligible))
	used := map[string]bool{}
	if ranking != nil {
		for _, ref := range ranking.Order {
			if allowed[ref] && !used[ref] {
				ordered = append(ordered, ref)
				used[ref] = true
			}
			// An ineligible or unknown ref is ignored, never added: the
			// provider may order the eligible set, not extend it.
		}
	}
	rest := make([]string, 0, len(eligible))
	for _, ref := range eligible {
		if !used[ref] {
			rest = append(rest, ref)
		}
	}
	sort.Strings(rest)
	return append(ordered, rest...)
}

func rankingInfo(ranking *Ranking) Ranking {
	if ranking == nil || strings.TrimSpace(ranking.Source) == "" {
		return Ranking{Source: "none"}
	}
	return Ranking{Source: ranking.Source, Version: ranking.Version}
}

func sortDecisions(decisions []Decision) []Decision {
	sort.Slice(decisions, func(i, j int) bool { return decisions[i].Candidate < decisions[j].Candidate })
	return decisions
}

func uniqueApprovals(classes []ApprovalClass) []ApprovalClass {
	seen := map[ApprovalClass]bool{}
	out := make([]ApprovalClass, 0, len(classes))
	for _, class := range classes {
		if seen[class] {
			continue
		}
		seen[class] = true
		out = append(out, class)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Canonical renders the record deterministically: `decidedAt` is excluded (the
// same rule the composition lock applies to `generatedAt`), so a replay of the
// same inputs is byte-identical.
func (r Record) Canonical() ([]byte, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("activation: marshal record: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("activation: unmarshal record: %w", err)
	}
	delete(fields, "decidedAt")
	stripped, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("activation: marshal stripped record: %w", err)
	}
	canonical, err := ir.Canonicalize(stripped)
	if err != nil {
		return nil, fmt.Errorf("activation: canonicalize record: %w", err)
	}
	return canonical, nil
}

// Digest is the content address of the decision (clock excluded).
func (r Record) Digest() (string, error) {
	canonical, err := r.Canonical()
	if err != nil {
		return "", err
	}
	return composition.DigestBytes(canonical), nil
}
