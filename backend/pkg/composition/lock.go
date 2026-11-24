package composition

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"backend/pkg/ir"
)

// LockVersion is the only accepted shape version (docs/architecture/composition-lock.md §4).
// It is a format version, never a second counter for the System.
const LockVersion = "1.0.0"

// DigestPrefix names the algorithm inside every digest string, so an algorithm
// change is a visible prefix change instead of a silent reinterpretation (O26).
const DigestPrefix = "sha256:"

// ClosureEntry is one resolved dependency.
type ClosureEntry struct {
	Ref         string   `json:"ref"`
	Kind        string   `json:"kind"`
	Publisher   string   `json:"publisher"`
	Digest      string   `json:"digest"`
	RequestedBy []string `json:"requestedBy"`
}

// Lock is the immutable artifact: what a System actually resolved to.
type Lock struct {
	LockVersion     string         `json:"lockVersion"`
	SystemRef       string         `json:"systemRef"`
	SystemDigest    string         `json:"systemDigest"`
	CompilerVersion string         `json:"compilerVersion"`
	ResolutionMode  string         `json:"resolutionMode"`
	ResolvedClosure []ClosureEntry `json:"resolvedClosure"`
	ModelPolicyRefs []string       `json:"modelPolicyRefs"`
	EvaluationRefs  []string       `json:"evaluationRefs"`
	GeneratedAt     string         `json:"generatedAt"`
}

// Canonical renders the lock in the repository's canonical form: sorted keys,
// 2-space indent, exactly one trailing newline (ir.Canonicalize). Reusing the
// IR serializer is deliberate — one canonical form for every artifact.
func (l *Lock) Canonical() ([]byte, error) {
	normalized := *l
	normalize(&normalized)
	raw, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("lock: marshal: %w", err)
	}
	canonical, err := ir.Canonicalize(raw)
	if err != nil {
		return nil, fmt.Errorf("lock: canonicalize: %w", err)
	}
	return canonical, nil
}

// Digest is the content address of this lock. `generatedAt` is excluded so that
// re-resolving an unchanged closure yields the same digest; everything else
// (including compilerVersion) is covered.
func (l *Lock) Digest() (string, error) {
	normalized := *l
	normalized.GeneratedAt = ""
	normalize(&normalized)
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("lock: marshal: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", fmt.Errorf("lock: unmarshal: %w", err)
	}
	delete(fields, "generatedAt")
	stripped, err := json.Marshal(fields)
	if err != nil {
		return "", fmt.Errorf("lock: marshal stripped: %w", err)
	}
	canonical, err := ir.Canonicalize(stripped)
	if err != nil {
		return "", fmt.Errorf("lock: canonicalize stripped: %w", err)
	}
	return DigestBytes(canonical), nil
}

// DigestBytes is `sha256:<lowercase hex>` over the given bytes.
func DigestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return DigestPrefix + hex.EncodeToString(sum[:])
}

// normalize keeps empty collections as [] rather than null so a lock with no
// closure and a lock with a closure diff structurally, not accidentally.
func normalize(l *Lock) {
	if l.ResolvedClosure == nil {
		l.ResolvedClosure = []ClosureEntry{}
	}
	for i := range l.ResolvedClosure {
		if l.ResolvedClosure[i].RequestedBy == nil {
			l.ResolvedClosure[i].RequestedBy = []string{}
		}
	}
	if l.ModelPolicyRefs == nil {
		l.ModelPolicyRefs = []string{}
	}
	if l.EvaluationRefs == nil {
		l.EvaluationRefs = []string{}
	}
}
