// Package memory is the ADI-MEM-001a memory contract: scoped records behind a
// three-operation provider interface.
//
// Contract: docs/architecture/memory-provider.md. The shape is deliberately
// small — Remember, Recall, Forget — because a correction is forget-plus-remember
// (a record that changed its content is a different record with a different
// source), and because every extra method is another thing a provider can get
// subtly wrong about tenancy.
//
// What this package is NOT: a knowledge index (that is the retrieval side of
// context assembly), an embedding model, or an authorization service. It
// enforces the tenant wall, the scope ladder and the secret rule; it grants
// nothing.
package memory
