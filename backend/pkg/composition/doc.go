// Package composition is the ADI-FOUND-003a resolver: it turns a validated
// System manifest plus an asset source into an immutable Composition Lock.
//
// Design contract: docs/architecture/composition-resolver.md. The lock shape,
// failure codes and test cases are normative there; this package is the first
// implementation of it, in bundled mode only.
//
// Shape of the thing:
//   - Resolve is a pure function over (manifest bytes, AssetSource, mode,
//     compiler version, injected clock). It does no network I/O, opens no
//     store and never calls time.Now().
//   - The only outside world is the AssetSource interface. A registry, a cache
//     and a bundled directory are all adapters over it.
//   - Findings are codes plus messages. A non-empty finding list means no lock:
//     the resolver never returns a partial composition with warnings, because
//     partial compositions fail mysteriously at run time.
//
// What this package deliberately is NOT: an authorizer (that is mcp.Gate), a
// trust evaluator (marketplace provenance), a store (ADI-FOUND-004) or an
// execution path. It resolves references and digests bytes.
package composition
