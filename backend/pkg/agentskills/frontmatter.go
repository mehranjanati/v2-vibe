package agentskills

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"backend/pkg/mcp"
	"gopkg.in/yaml.v3"
)

// Budgets are the progressive-loading economics from the research log
// (proposal 2). They bound what may enter an agent context, not what the
// upstream spec allows: a spec-valid document can still be too expensive.
const (
	// ResolveTokenBudget is the target cost of resolving one skill
	// (name + description). ~100 tokens per skill per turn.
	ResolveTokenBudget = 100
	// BodyTokenBudget is the hard ceiling for an activated skill body. A
	// longer body is refused rather than injected.
	BodyTokenBudget = 5000
	// NameMaxLen, DescriptionMaxLen and LicenseMaxLen mirror both the
	// agentskills frontmatter rules and the Skill manifest schema
	// (docs/architecture/manifest-schemas/skill.schema.json).
	NameMaxLen        = 64
	DescriptionMaxLen = 1024
	LicenseMaxLen     = 80
)

var (
	// nameRe is the agentskills.io slug rule: lowercase alphanumerics
	// separated by single hyphens, no leading/trailing hyphen.
	nameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	// licenseRe is a *shape* check for an SPDX tag (MIT, Apache-2.0,
	// GPL-3.0-or-later). It is deliberately not the SPDX license list: a
	// new identifier must not need a code change, and the manifest schema
	// checks the same shape.
	licenseRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+-]*$`)
)

// Trust is a marketplace trust level (D18 §4). It is never read from the
// document: the caller that publishes the skill decides it.
type Trust string

const (
	// TrustPrivate is a skill authored inside the tenant. Everything is
	// readable, including scripts, because the tenant already runs that code.
	TrustPrivate Trust = "private"
	// TrustVerified passed the same review an MCP server needs (D18 §4).
	TrustVerified Trust = "verified"
	// TrustCommunity is unsigned third-party content: read-only. Its
	// scripts stay denied until a Verified review promotes it.
	TrustCommunity Trust = "community"
)

// Valid reports whether the trust level is one the loader understands.
func (t Trust) Valid() bool {
	switch t {
	case TrustPrivate, TrustVerified, TrustCommunity:
		return true
	default:
		return false
	}
}

// ValidationError names the offending frontmatter field and why it is
// rejected, so a test or a UI can point at it without re-parsing.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return "agentskills: " + e.Field + ": " + e.Reason
}

// DeniedError is a trust-boundary refusal (for example a Community skill
// asking for its scripts). It is not a validation error: the document is
// fine, the caller is not allowed to use it that way.
type DeniedError struct {
	Reason string
}

func (e *DeniedError) Error() string { return "agentskills: denied: " + e.Reason }

// Card is everything a resolve may put in context: the name and the
// description, nothing else. The body and resources are not reachable from
// here on purpose.
type Card struct {
	Name        string
	Description string
	Trust       Trust
	// Tokens is the estimated resolve cost (name + description).
	Tokens int
}

// WithinBudget reports whether the card fits ResolveTokenBudget. A card that
// does not fit is still loadable — the platform reports the cost instead of
// silently truncating a spec-valid description — but resolvers should skip it.
func (c Card) WithinBudget() bool { return c.Tokens <= ResolveTokenBudget }

// Kind is one of the three optional resource directories in a skill.
type Kind string

const (
	KindScripts    Kind = "scripts"
	KindReferences Kind = "references"
	KindAssets     Kind = "assets"
)

// Valid reports whether the kind is one of the three spec directories.
func (k Kind) Valid() bool {
	switch k {
	case KindScripts, KindReferences, KindAssets:
		return true
	default:
		return false
	}
}

// Skill is a parsed and validated SKILL.md. The body is held but only
// returned through Activate, so no code path accidentally injects it while
// resolving.
type Skill struct {
	Name          string
	Description   string
	License       string
	Compatibility []string
	Metadata      map[string]string
	// Extensions keeps frontmatter keys this loader does not interpret.
	// They are preserved (never silently dropped) and carry no authority.
	Extensions map[string]any
	// AllowedTools are the pinned `mcp/<server>/<tool>@<x.y.z>` refs the
	// skill asks for. Advisory: Bind still requires a live tenant grant.
	AllowedTools []mcp.ServerRef
	// UnpinnedTools are `allowed-tools` entries that are not pinned refs
	// (upstream allows bare tool names). They are advisory notes only and
	// authorize nothing.
	UnpinnedTools []string
	// Trust is the marketplace level the loader was called with.
	Trust Trust

	dir  string
	body string
}

// Card returns the resolve-time payload.
func (s *Skill) Card() Card {
	return Card{
		Name:        s.Name,
		Description: s.Description,
		Trust:       s.Trust,
		Tokens:      estimateTokens(s.Name) + estimateTokens(s.Description),
	}
}

// frontmatter is the decoded YAML block. `inline` collects unknown keys so a
// newer upstream field is preserved in Extensions instead of failing the load
// (or being dropped without a trace).
type frontmatter struct {
	Name          string            `yaml:"name"`
	Description   string            `yaml:"description"`
	License       string            `yaml:"license"`
	Compatibility []string          `yaml:"compatibility"`
	Metadata      map[string]string `yaml:"metadata"`
	AllowedTools  []string          `yaml:"allowed-tools"`
	Extensions    map[string]any    `yaml:",inline"`
}

// parseFrontmatter splits a SKILL.md document into its YAML block and body.
// The delimiters must be on their own lines; CRLF is tolerated.
func parseFrontmatter(doc string) (yamlBlock, body string, err error) {
	doc = strings.TrimPrefix(doc, "\ufeff")
	lines := strings.Split(doc, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return "", "", &ValidationError{Field: "frontmatter", Reason: "document must start with a '---' delimiter line"}
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") != "---" {
			continue
		}
		yamlBlock = strings.Join(trimCR(lines[1:i]), "\n")
		body = strings.Join(lines[i+1:], "\n")
		return yamlBlock, body, nil
	}
	return "", "", &ValidationError{Field: "frontmatter", Reason: "missing closing '---' delimiter"}
}

func trimCR(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimSuffix(l, "\r")
	}
	return out
}

// parseSkill validates a SKILL.md document. dirName is the folder the
// document lives in ("" skips the folder/name agreement check).
func parseSkill(doc, dirName string, trust Trust) (*Skill, error) {
	if !trust.Valid() {
		return nil, &ValidationError{Field: "trust", Reason: fmt.Sprintf("%q must be private, verified or community", string(trust))}
	}
	yamlBlock, body, err := parseFrontmatter(doc)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(yamlBlock) == "" {
		return nil, &ValidationError{Field: "frontmatter", Reason: "frontmatter block is empty"}
	}
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return nil, &ValidationError{Field: "frontmatter", Reason: "not valid YAML: " + err.Error()}
	}

	s := &Skill{
		Name:        strings.TrimSpace(fm.Name),
		Description: strings.TrimSpace(fm.Description),
		License:     strings.TrimSpace(fm.License),
		Metadata:    fm.Metadata,
		Extensions:  fm.Extensions,
		Trust:       trust,
		body:        strings.TrimSpace(body),
	}
	for _, c := range fm.Compatibility {
		if v := strings.TrimSpace(c); v != "" {
			s.Compatibility = append(s.Compatibility, v)
		}
	}
	if err := s.validate(dirName); err != nil {
		return nil, err
	}
	for _, raw := range fm.AllowedTools {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			return nil, &ValidationError{Field: "allowed-tools", Reason: "empty entry"}
		}
		ref, ok := parseRef(entry)
		if !ok {
			// Upstream allows bare tool names; those stay advisory notes
			// and never become authority (see Bind). An `mcp/` entry is
			// an explicit pin request, so a malformed one is a rejection,
			// not a silent downgrade to advisory.
			if strings.HasPrefix(entry, "mcp/") {
				return nil, &ValidationError{Field: "allowed-tools", Reason: fmt.Sprintf("%q must be pinned mcp/<server>/<tool>@MAJOR.MINOR.PATCH", entry)}
			}
			s.UnpinnedTools = append(s.UnpinnedTools, entry)
			continue
		}
		s.AllowedTools = append(s.AllowedTools, ref)
	}
	sort.Slice(s.AllowedTools, func(i, j int) bool {
		return s.AllowedTools[i].String() < s.AllowedTools[j].String()
	})
	sort.Strings(s.UnpinnedTools)
	return s, nil
}

func (s *Skill) validate(dirName string) error {
	if s.Name == "" {
		return &ValidationError{Field: "name", Reason: "required"}
	}
	if n := utf8.RuneCountInString(s.Name); n > NameMaxLen {
		return &ValidationError{Field: "name", Reason: fmt.Sprintf("%d characters exceeds the %d limit", n, NameMaxLen)}
	}
	if !nameRe.MatchString(s.Name) {
		return &ValidationError{Field: "name", Reason: "must be lowercase alphanumerics separated by single hyphens"}
	}
	if dirName != "" && dirName != s.Name {
		return &ValidationError{Field: "name", Reason: fmt.Sprintf("%q must equal its directory name %q", s.Name, dirName)}
	}
	if s.Description == "" {
		return &ValidationError{Field: "description", Reason: "required"}
	}
	if n := utf8.RuneCountInString(s.Description); n > DescriptionMaxLen {
		return &ValidationError{Field: "description", Reason: fmt.Sprintf("%d characters exceeds the %d limit", n, DescriptionMaxLen)}
	}
	if s.License != "" {
		if n := utf8.RuneCountInString(s.License); n > LicenseMaxLen {
			return &ValidationError{Field: "license", Reason: fmt.Sprintf("%d characters exceeds the %d limit", n, LicenseMaxLen)}
		}
		if !licenseRe.MatchString(s.License) {
			return &ValidationError{Field: "license", Reason: "must be an SPDX tag such as MIT or Apache-2.0"}
		}
	}
	if !utf8.ValidString(s.body) {
		return &ValidationError{Field: "body", Reason: "not valid UTF-8"}
	}
	return nil
}

// parseRef parses the pinned form `mcp/<server>/<tool>@<MAJOR.MINOR.PATCH>`.
// Unpinned or bare entries return false and stay advisory.
func parseRef(entry string) (mcp.ServerRef, bool) {
	rest, ok := strings.CutPrefix(entry, "mcp/")
	if !ok {
		return mcp.ServerRef{}, false
	}
	at := strings.LastIndex(rest, "@")
	if at <= 0 || at == len(rest)-1 {
		return mcp.ServerRef{}, false
	}
	path, version := rest[:at], rest[at+1:]
	slash := strings.Index(path, "/")
	if slash <= 0 || slash == len(path)-1 {
		return mcp.ServerRef{}, false
	}
	ref := mcp.ServerRef{Server: path[:slash], Tool: path[slash+1:], Version: version}
	if !ref.Valid() {
		return mcp.ServerRef{}, false
	}
	return ref, true
}

// estimateTokens approximates token count as 4 runes per token. It is an
// estimate for budgeting, never a billing figure, and it is documented as
// such wherever a limit is reported.
func estimateTokens(s string) int {
	return (utf8.RuneCountInString(s) + 3) / 4
}
