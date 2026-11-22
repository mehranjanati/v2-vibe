package agentskills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SkillFile is the document name the agentskills spec fixes.
const SkillFile = "SKILL.md"

// resourceNameRe keeps a resource reference to one safe path element. No
// separators, no dots-only names: a resource is a file inside a known
// directory of the skill, never a path.
var resourceNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Loader reads skills from a directory of skill folders:
//
//	<root>/<skill-name>/SKILL.md
//	<root>/<skill-name>/scripts/...
//	<root>/<skill-name>/references/...
//	<root>/<skill-name>/assets/...
type Loader struct {
	root string
}

// Open pins a loader to a skills root. The root must exist: a typo must fail
// here, not produce an empty resolve later.
func Open(root string) (*Loader, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("agentskills: skills root is required")
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("agentskills: skills root %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("agentskills: skills root %q is not a directory", root)
	}
	return &Loader{root: root}, nil
}

// Root returns the directory the loader reads from.
func (l *Loader) Root() string { return l.root }

// Load parses one skill by folder name. Trust is supplied by the caller
// (marketplace decision), never read from the document.
func (l *Loader) Load(name string, trust Trust) (*Skill, error) {
	if !resourceNameRe.MatchString(name) || strings.Contains(name, "..") {
		return nil, &ValidationError{Field: "name", Reason: fmt.Sprintf("%q is not a single folder name", name)}
	}
	path := filepath.Join(l.root, name, SkillFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("agentskills: read %s: %w", path, err)
	}
	s, err := parseSkill(string(raw), name, trust)
	if err != nil {
		return nil, err
	}
	// dir is the absolute folder the resources are read from; the name
	// check above used the folder name, not this path.
	s.dir = filepath.Join(l.root, name)
	return s, nil
}

// All loads every skill folder under the root, sorted by name. A folder
// without a SKILL.md is ignored (it may be a fixture or a scratch dir); a
// SKILL.md that fails validation fails the whole call, because a partially
// loaded set would hide a broken skill behind a working one.
func (l *Loader) All(trust Trust) ([]*Skill, error) {
	entries, err := os.ReadDir(l.root)
	if err != nil {
		return nil, fmt.Errorf("agentskills: read skills root %q: %w", l.root, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(l.root, e.Name(), SkillFile)); err != nil {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	out := make([]*Skill, 0, len(names))
	for _, name := range names {
		s, err := l.Load(name, trust)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// Cards is the resolve view: name + description only, nothing activated.
func (l *Loader) Cards(trust Trust) ([]Card, error) {
	skills, err := l.All(trust)
	if err != nil {
		return nil, err
	}
	cards := make([]Card, 0, len(skills))
	for _, s := range skills {
		cards = append(cards, s.Card())
	}
	return cards, nil
}

// Activate returns the skill body — the first thing that may enter an agent
// context beyond the card. It refuses a body over BodyTokenBudget instead of
// injecting it, and it never returns the frontmatter.
func (s *Skill) Activate() (string, error) {
	if strings.TrimSpace(s.body) == "" {
		return "", &ValidationError{Field: "body", Reason: "skill body is empty"}
	}
	if tokens := estimateTokens(s.body); tokens > BodyTokenBudget {
		return "", fmt.Errorf("agentskills: %s: body is ~%d tokens, over the %d budget; split it into references/", s.Name, tokens, BodyTokenBudget)
	}
	return s.body, nil
}

// Resource reads one file from scripts/, references/ or assets/ on demand.
// Community skills cannot read scripts/: unsigned code is read-only until a
// Verified review (D18 §4), and that refusal is the loader's half of the
// trust boundary — the Gate is the other half.
func (s *Skill) Resource(kind Kind, name string) ([]byte, error) {
	if !kind.Valid() {
		return nil, &ValidationError{Field: "resource", Reason: fmt.Sprintf("%q must be scripts, references or assets", string(kind))}
	}
	if kind == KindScripts && s.Trust == TrustCommunity {
		return nil, &DeniedError{Reason: fmt.Sprintf("skill %s is community-trusted: scripts stay read-only until a Verified review", s.Name)}
	}
	if !resourceNameRe.MatchString(name) || strings.Contains(name, "..") {
		return nil, &ValidationError{Field: "resource", Reason: fmt.Sprintf("%q must be a single file name inside %s/", name, kind)}
	}
	base := filepath.Join(s.dir, string(kind))
	path := filepath.Join(base, name)
	// Defence in depth: even with a validated name, the resolved path must
	// stay inside the skill's own resource directory.
	if rel, err := filepath.Rel(base, path); err != nil || rel != name {
		return nil, &DeniedError{Reason: fmt.Sprintf("resource %q escapes %s/", name, kind)}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("agentskills: read %s/%s: %w", kind, name, err)
	}
	return raw, nil
}

// ResourceNames lists the files a kind directory holds, sorted. A missing
// directory is an empty list, not an error: all three directories are
// optional in the spec.
func (s *Skill) ResourceNames(kind Kind) ([]string, error) {
	if !kind.Valid() {
		return nil, &ValidationError{Field: "resource", Reason: fmt.Sprintf("%q must be scripts, references or assets", string(kind))}
	}
	entries, err := os.ReadDir(filepath.Join(s.dir, string(kind)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("agentskills: list %s/: %w", kind, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}
