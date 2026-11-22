package openapiimport

import (
	"fmt"
	"sort"
)

// Merge folds a new import into an existing set, so importing the same spec
// twice is idempotent:
//
//   - an identical manifest (same ref, same canonical content) is kept once;
//   - the same ref with different content is a conflict, because silently
//     replacing a reviewed capability under an unchanged version is exactly the
//     "policy drift" the Gate refuses — the fix is a version bump in Options;
//   - anything new is appended.
//
// The result is sorted by ref, so callers can compare byte-for-byte across runs.
func Merge(existing, incoming []Imported) ([]Imported, error) {
	byRef := make(map[string]Imported, len(existing)+len(incoming))
	for _, imp := range existing {
		if err := imp.Validate(); err != nil {
			return nil, fmt.Errorf("openapiimport: merge: existing manifest is invalid: %w", err)
		}
		if prev, dup := byRef[imp.Ref()]; dup && !canonicalEqual(prev.Manifest, imp.Manifest) {
			return nil, fmt.Errorf("openapiimport: merge: existing set already holds two different manifests for %s", imp.Ref())
		}
		byRef[imp.Ref()] = imp
	}
	for _, imp := range incoming {
		if err := imp.Validate(); err != nil {
			return nil, fmt.Errorf("openapiimport: merge: incoming manifest is invalid: %w", err)
		}
		if prev, exists := byRef[imp.Ref()]; exists {
			if !canonicalEqual(prev.Manifest, imp.Manifest) {
				return nil, fmt.Errorf("openapiimport: merge: %s already exists with different content; bump the version instead of overwriting a reviewed tool", imp.Ref())
			}
			continue
		}
		byRef[imp.Ref()] = imp
	}
	out := make([]Imported, 0, len(byRef))
	for _, imp := range byRef {
		out = append(out, imp)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Ref() < out[b].Ref() })
	return out, nil
}
