# Glossary and Architecture Terms (Canonical)

> **Status:** `adopted` 2026-11-21.

- Product: independently releasable Vibe offering.
- Shared contract: versioned cross-product interface.
- Control plane: intent, sessions, lineage.
- Execution plane: durable runs of agents/tools/workflows.
- System IR: validated serializable system definition (`kind: System`).
- Manifest: versioned agent/skill/tool/system descriptor.
- Provider: pluggable backend (model, execution, memory, storage, event).
- Adapter: protocol bridge (MCP/A2A/UCP/AP2/WASM).
- Registry/marketplace: discovery plus distribution of versioned closures.
- Adaptive Intelligence: shared compose/activate/personalize/evaluate/evolve capability (`architecture/adaptive-intelligence-architecture.md`, PROPOSED).
- Composition: allowed capabilities with exact pins. Lock/snapshot: immutable resolved closure plus digests (`architecture/composition-lock.md`, PROPOSED).
- Activation: authorized-subset selection for one run with decision trace (`architecture/context-activation-and-memory.md`, PROPOSED).
- IMPLEMENTED: live plus green tests. PARTIAL: incomplete. DESIGNED: spec only.
  PLANNED: roadmap item. RESEARCH: open question.
