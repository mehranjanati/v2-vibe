package toolsearch

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// The bound is the point of search: a model that asks for tools must get a
// handful, not a catalogue.
const (
	// DefaultLimit is how many candidates a query returns when the caller
	// does not ask for a specific count.
	DefaultLimit = 5
	// MaxCandidates is the hard ceiling. A request for more is clamped, not
	// honoured: an unbounded slice defeats the purpose of search.
	MaxCandidates = 8
	// ContextTokenBudget bounds the estimated cost of one result slice.
	ContextTokenBudget = 600
	// DescriptionMaxRunes caps one candidate's rendered line so a single
	// verbose manifest cannot eat the whole budget.
	DescriptionMaxRunes = 220
)

// Score weights. They are constants, not configuration: a ranking nobody can
// reproduce is a ranking nobody can review.
const (
	scorePhraseInName         = 10
	scorePhraseInDescription  = 4
	scoreNameWord             = 6
	scoreNameSubstring        = 3
	scoreDescriptionWord      = 2
	scoreDescriptionSubstring = 1
)

// Match is one surfaced candidate with its ranking score.
type Match struct {
	Candidate Candidate
	Score     int
}

// Result is one search response. It is a closed, bounded slice: Matches never
// exceeds the requested limit or MaxCandidates, and Tokens is the estimated cost
// of the rendered slice.
type Result struct {
	// Query is the normalised query the ranking used.
	Query string
	// Matches are the candidate tools, best first.
	Matches []Match
	// Tokens estimates the context cost of the slice (4 runes per token).
	Tokens int
	// Truncated is true when candidates were dropped by the limit or budget.
	Truncated bool
	// Missed is true when nothing scored above zero. The result is then
	// empty on purpose: "no tool matched" is information, not a failure.
	Missed bool
	// Note explains a miss or a truncation in one line, for the model and
	// for the audit trail.
	Note string
}

// Search ranks the catalog against a task query. It is pure: no network, no
// grants, no execution — the caller verifies grants separately (see grants.go)
// and executes through the Gate.
func (c *Catalog) Search(query string, limit int) Result {
	normalized, tokens := normalize(query)
	if len(tokens) == 0 {
		return Result{Query: normalized, Missed: true, Note: "empty or stop-word-only query: name the capability you need or call a tool you already know"}
	}
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxCandidates {
		limit = MaxCandidates
	}

	var scored []Match
	for _, cand := range c.candidates {
		if score := scoreCandidate(cand, normalized, tokens); score > 0 {
			scored = append(scored, Match{Candidate: cand, Score: score})
		}
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		if scored[i].Candidate.Name() != scored[j].Candidate.Name() {
			return scored[i].Candidate.Name() < scored[j].Candidate.Name()
		}
		return scored[i].Candidate.Pinned() < scored[j].Candidate.Pinned()
	})

	if len(scored) == 0 {
		return Result{
			Query:  normalized,
			Missed: true,
			Note:   "no tool in the catalog matches this query; do not invent one — state the missing capability instead",
		}
	}

	total := len(scored)
	kept := make([]Match, 0, limit)
	tokensUsed := 0
	truncated := false
	for _, m := range scored {
		if len(kept) == limit {
			truncated = true
			break
		}
		cost := m.Tokens()
		if len(kept) > 0 && tokensUsed+cost > ContextTokenBudget {
			truncated = true
			break
		}
		kept = append(kept, m)
		tokensUsed += cost
	}

	result := Result{Query: normalized, Matches: kept, Tokens: tokensUsed, Truncated: truncated || len(kept) < total}
	if result.Truncated {
		result.Note = fmt.Sprintf("showing %d of %d matches (limit %d, budget %d tokens)", len(kept), total, limit, ContextTokenBudget)
	}
	return result
}

// Line renders one match as the single line a context slice shows: the pinned
// ref, the source and a capped description.
func (m Match) Line() string {
	desc := m.Candidate.Description
	if utf8.RuneCountInString(desc) > DescriptionMaxRunes {
		desc = string([]rune(desc)[:DescriptionMaxRunes-1]) + "…"
	}
	return fmt.Sprintf("%s [%s] %s", m.Candidate.Pinned(), m.Candidate.Source, desc)
}

// Tokens estimates this match's cost in the slice.
func (m Match) Tokens() int { return estimateTokens(m.Line()) }

// Lines renders the whole result as the lines a context slice would carry.
func (r Result) Lines() []string {
	out := make([]string, 0, len(r.Matches))
	for _, m := range r.Matches {
		out = append(out, m.Line())
	}
	return out
}

// scoreCandidate applies the documented weights. Name matches always beat
// description matches, so a query naming a tool ranks that tool first even when
// a chatty description repeats the words.
func scoreCandidate(cand Candidate, phrase string, tokens []string) int {
	name := strings.ToLower(strings.NewReplacer("-", " ", "_", " ").Replace(cand.Name()))
	desc := strings.ToLower(cand.Description)
	score := 0
	if phrase != "" && strings.Contains(name, phrase) {
		score += scorePhraseInName
	}
	if phrase != "" && strings.Contains(desc, phrase) {
		score += scorePhraseInDescription
	}
	for _, token := range tokens {
		switch {
		case containsWord(name, token):
			score += scoreNameWord
		case strings.Contains(name, token):
			score += scoreNameSubstring
		}
		if containsWord(desc, token) {
			score += scoreDescriptionWord
		} else if strings.Contains(desc, token) {
			score += scoreDescriptionSubstring
		}
	}
	return score
}

// stopWords are query words that carry no capability signal. Without them a
// description like "…report with a cover page…" matches any query containing
// "with", which is exactly the noise that makes search useless.
var stopWords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "by": true, "can": true, "for": true, "from": true, "i": true,
	"in": true, "into": true, "is": true, "it": true, "me": true, "my": true,
	"of": true, "on": true, "or": true, "our": true, "please": true, "that": true,
	"the": true, "then": true, "this": true, "to": true, "use": true,
	"want": true, "when": true, "with": true, "you": true,
}

// normalize lowercases a query and splits it into unique searchable tokens.
func normalize(query string) (string, []string) {
	lowered := strings.ToLower(strings.TrimSpace(query))
	fields := strings.FieldsFunc(lowered, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	seen := map[string]bool{}
	tokens := make([]string, 0, len(fields))
	for _, f := range fields {
		// One-character tokens match almost every name as a substring, and
		// stop words match every description: both flatten the ranking.
		if len(f) < 2 || stopWords[f] || seen[f] {
			continue
		}
		seen[f] = true
		tokens = append(tokens, f)
	}
	return strings.Join(fields, " "), tokens
}

// containsWord reports whether the haystack holds the needle as a whole word.
func containsWord(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	start := 0
	for {
		idx := strings.Index(haystack[start:], needle)
		if idx < 0 {
			return false
		}
		at := start + idx
		beforeOK := at == 0 || !isWordByte(haystack[at-1])
		after := at + len(needle)
		afterOK := after == len(haystack) || !isWordByte(haystack[after])
		if beforeOK && afterOK {
			return true
		}
		start = at + 1
	}
}

func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_'
}

// estimateTokens approximates 4 runes per token; the same estimate the skill
// loader uses, so both budgets are comparable.
func estimateTokens(s string) int { return (utf8.RuneCountInString(s) + 3) / 4 }
