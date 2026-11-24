package composition

import (
	"fmt"
	"regexp"
	"strings"

	"backend/pkg/ir"
)

// Kinds a reference may name. These are the four manifest kinds plus the three
// reserved catalogs the IR already recognises (docs/architecture/manifest-schemas.md).
const (
	KindAgent       = "agent"
	KindSkill       = "skill"
	KindTool        = "tool"
	KindModelPolicy = "model-policy"
	KindKnowledge   = "knowledge"
	KindEval        = "eval"
)

// ReservedKinds are catalogs with no manifest schema in v1. They are terminal
// dependencies: the resolver records them and never looks inside them for
// further edges, because there is no schema that would make such a walk
// meaningful.
var ReservedKinds = []string{KindModelPolicy, KindKnowledge, KindEval}

// refRe is the IR's own pinned-reference grammar, reused so the resolver and
// the IR can never disagree about what an exact reference is (ADI-FOUND-001a
// tightened ir.SemverPattern to reject leading zeros).
var refRe = regexp.MustCompile(`^(agent|skill|tool|model-policy|knowledge|eval)/[a-z0-9]([-a-z0-9]*[a-z0-9])?@` + ir.SemverPattern + `$`)

// Ref is one exact, pinned capability reference: kind/name@MAJOR.MINOR.PATCH.
// Ranges, `latest`, channels and git refs are not representable: ParseRef
// refuses them, which is how "exact versions only" is enforced before any
// fetch happens.
type Ref struct {
	Kind    string
	Name    string
	Version string
}

// ParseRef parses an exact reference. Anything else is an error, by design.
func ParseRef(raw string) (Ref, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed != raw {
		return Ref{}, fmt.Errorf("reference %q has surrounding whitespace", raw)
	}
	if !refRe.MatchString(raw) {
		return Ref{}, fmt.Errorf("reference %q is not an exact kind/name@MAJOR.MINOR.PATCH in a known namespace", raw)
	}
	at := strings.LastIndex(raw, "@")
	slashed := raw[:at]
	slash := strings.Index(slashed, "/")
	return Ref{
		Kind:    slashed[:slash],
		Name:    slashed[slash+1:],
		Version: raw[at+1:],
	}, nil
}

// String renders the canonical form of a reference.
func (r Ref) String() string {
	return r.Kind + "/" + r.Name + "@" + r.Version
}

// Identity is kind/name: the unit a closure may pin at exactly one version
// (D15 identity rule, and the E_INCOMPATIBLE_VERSION check).
func (r Ref) Identity() string {
	return r.Kind + "/" + r.Name
}

// IsReserved reports whether the kind is a terminal catalog rather than a
// manifest kind.
func (r Ref) IsReserved() bool {
	for _, kind := range ReservedKinds {
		if r.Kind == kind {
			return true
		}
	}
	return false
}

// SystemRef renders the composed root. The `system/` prefix is deliberately NOT
// a resolvable namespace: it identifies the composition, never an asset.
func SystemRef(name, version string) string {
	return "system/" + name + "@" + version
}
