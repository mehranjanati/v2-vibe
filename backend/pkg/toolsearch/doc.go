// Package toolsearch is the P7-SCH-001 dynamic tool search: `search_tools`
// beside the static manifests, not instead of them.
//
// The rule from the research log (proposal 5) is the whole design: search
// **augments** the Gate and never replaces it. A surfaced candidate is only a
// pointer to a pinned capability. To call it the caller still needs the
// manifest, the policy, a live tenant grant, an idempotency key and an audit
// row — this package gives the caller the pinned ref and the grant id, nothing
// more. Nothing here executes a tool.
//
// Three properties the tests hold:
//
//   - A fixed bound. A query returns at most MaxCandidates entries and at most
//     ContextTokenBudget estimated tokens, so tool search can never flood a
//     context window (the failure mode it exists to prevent).
//   - Deterministic ranking. Score, then name, then pinned ref — the same
//     catalog and query always produce the same order, so a golden test can
//     assert behaviour instead of averages.
//   - An honest miss. When nothing scores above zero the result is empty and
//     says so; it never degrades into "return everything".
//
// Code Mode lives here only as a gated spike: the type cannot be constructed
// without (a) an explicit enabled flag and (b) a Sandbox implementation. The
// package imports no process or plugin API, so there is no code path that
// executes agent-authored code unsandboxed — the only executor takes a
// Sandbox, and the zero value refuses.
//
// Not wired to any agent or route: same proving-ground status as the loaders
// and the stdio transport (D8).
package toolsearch
