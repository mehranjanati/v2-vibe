// Package bundle is the control plane's bundled composition source
// (ADI-FOUND-004c): the repository's vetted example manifests, embedded so
// that producing a snapshot never depends on the repository layout at
// runtime (a Docker image carries no docs/ directory).
//
// What this honestly is: bundled resolution mode with the example catalog as
// its asset set. A System that references anything outside these files
// resolves with E_MISSING until a real registry exists (ADI-ECO-002, O27) —
// that is the documented first slice, not a silent fallback. The parity test
// keeps the embedded copy byte-identical to the docs originals, so the docs
// gate and the runtime always agree on what "bundled" contains.
package bundle

import (
	"embed"

	"backend/pkg/composition"
)

// assets is the embedded example catalog. A System manifest is a composition
// root, not a resolvable asset, so LoadBundledSource skips it here exactly as
// it does for the docs directory.
//
//go:embed assets/*.json
var assets embed.FS

// New returns the bundled AssetSource the snapshot producer resolves against.
// The error is a build defect (a malformed embedded manifest) — callers log
// it and leave the source unset, which makes the produce path answer 503
// instead of silently resolving against nothing.
func New() (*composition.BundledSource, error) {
	return composition.LoadBundledSource(assets, "assets")
}
