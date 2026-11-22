package toolsearch

import (
	"fmt"
	"sort"
	"strings"

	"backend/pkg/agentskills"
	"backend/pkg/mcp"
	"backend/pkg/openapiimport"
)

// Source records which loader produced a candidate, so a reviewer can trace a
// surfaced tool back to the family it came from (D18: one vocabulary, many
// loaders).
type Source string

const (
	SourceOpenAPI Source = "openapi"
	SourceSkill   Source = "skill"
	SourceMCP     Source = "mcp"
)

// Candidate is one searchable capability. It carries the pinned ref and the text
// a ranking may read — never the manifest, never a credential, never a grant.
type Candidate struct {
	// Ref pins the tool: server, tool name and MAJOR.MINOR.PATCH.
	Ref mcp.ServerRef
	// Source names the loader family that produced it.
	Source Source
	// Description is the manifest description (or, for a skill tool, the
	// skill's own description with the skill name as context).
	Description string
	// SideEffect mirrors the manifest when the loader knows it ("", when a
	// skill only names a tool).
	SideEffect string
}

// Pinned returns the content address a reviewer quotes.
func (c Candidate) Pinned() string { return c.Ref.String() }

// Name returns the tool name.
func (c Candidate) Name() string { return c.Ref.Tool }

// Catalog is the searchable set. It is built once from the loaders' output and
// is immutable from the caller's point of view afterwards.
type Catalog struct {
	candidates []Candidate
}

// NewCatalog returns an empty catalog.
func NewCatalog() *Catalog { return &Catalog{} }

// Add records candidates, rejecting unpinned refs (an unpinned capability can
// never be granted, so surfacing it would be a dead end) and duplicates.
func (c *Catalog) Add(in ...Candidate) error {
	for _, cand := range in {
		if !cand.Ref.Valid() {
			return fmt.Errorf("toolsearch: candidate %q is not a pinned mcp/<server>/<tool>@<x.y.z> ref", cand.Ref.String())
		}
		if cand.Source == "" {
			return fmt.Errorf("toolsearch: candidate %s has no source", cand.Pinned())
		}
		for _, existing := range c.candidates {
			if existing.Pinned() == cand.Pinned() {
				return fmt.Errorf("toolsearch: %s is already in the catalog (source %s)", cand.Pinned(), existing.Source)
			}
		}
		cand.Description = strings.Join(strings.Fields(cand.Description), " ")
		c.candidates = append(c.candidates, cand)
	}
	return nil
}

// Len reports how many capabilities are searchable.
func (c *Catalog) Len() int { return len(c.candidates) }

// Candidates returns a copy of the catalog in stable (ref) order.
func (c *Catalog) Candidates() []Candidate {
	out := append([]Candidate(nil), c.candidates...)
	sort.Slice(out, func(i, j int) bool { return out[i].Pinned() < out[j].Pinned() })
	return out
}

// AddImported indexes the output of the OpenAPI importer: one candidate per
// imported operation, in its own server namespace.
func (c *Catalog) AddImported(imported []openapiimport.Imported) error {
	cands := make([]Candidate, 0, len(imported))
	for _, imp := range imported {
		spec, _ := imp.Manifest["spec"].(map[string]interface{})
		description, _ := spec["description"].(string)
		sideEffect, _ := spec["sideEffect"].(string)
		cands = append(cands, Candidate{
			Ref:         mcp.ServerRef{Server: imp.Server, Tool: imp.Name(), Version: imp.Version()},
			Source:      SourceOpenAPI,
			Description: description,
			SideEffect:  sideEffect,
		})
	}
	return c.Add(cands...)
}

// AddSkill indexes the tools an agentskills skill declares in `allowed-tools`.
// The skill's own text is not a tool: procedure stays with the skill loader's
// resolve path, action stays here. A skill with no pinned tools adds nothing.
func (c *Catalog) AddSkill(skill *agentskills.Skill) error {
	if skill == nil {
		return fmt.Errorf("toolsearch: skill is nil")
	}
	cands := make([]Candidate, 0, len(skill.AllowedTools))
	for _, ref := range skill.AllowedTools {
		cands = append(cands, Candidate{
			Ref:         ref,
			Source:      SourceSkill,
			Description: skill.Name + ": " + skill.Description,
		})
	}
	return c.Add(cands...)
}
