// Package plan is the ADI-FOUND-005a compiler: it turns a verified composition
// snapshot plus the System document it describes into an executable plan.
//
// Contract: docs/architecture/ir-to-run-binding.md. Only the PROCEDURAL System
// shape compiles here, and that is a property of the platform rather than a
// shortcut: the IR's node kinds are the same seven the DAG interpreter already
// runs, so the mapping is 1:1. An agentic System is refused with E_BIND_SHAPE,
// because no runtime node type runs an agent and lowering one into a chain would
// silently discard delegation, topology order and per-agent policy.
//
// A plan is a pure function of (snapshot, compiler version), so it is never
// stored: the run row keeps a digest and the plan is recomputed.
package plan
