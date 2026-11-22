// Package openapiimport is the P7-OPI-001 loader for the second integration
// family: one OpenAPI operation becomes one pinned Tool Manifest.
//
// It follows decision D18 ("one contract, many loaders"): the importer does
// not invent a runner, an auth plane or a registry. It produces the same Tool
// Manifest vocabulary every other family uses and a mapping onto the existing
// mcp.ToolPolicy, so an imported surface is enforced by the same Gate.
//
// Rules the importer enforces (research log, proposal 3):
//
//   - An import exposes exactly the audited surface. There is no "import the
//     whole spec" default: the caller names the operations (or sets AllowAll
//     explicitly, which is documented as a review shortcut, not a good idea).
//   - Every manifest is pinned: MAJOR.MINOR.PATCH from options or the spec's
//     info.version, never a range and never "latest".
//   - Credential-shaped material is never imported. Security schemes and
//     credential headers become `vault:` references and are removed from the
//     input schema, so a model can neither see nor supply a secret.
//   - Import is deterministic: same spec + same options produce byte-identical
//     canonical manifests, and re-importing into an existing set adds nothing
//     (Merge), so a repeated import cannot duplicate a capability.
//
// What it is not: an HTTP transport (the Gate still owns execution), a
// manifest registry, or an OpenAPI validator — unsupported constructs are
// rejected or ignored explicitly, never guessed.
package openapiimport
