// Package evaluation is the ADI-EVAL-002a benchmark runner.
//
// It turns a gate definition into a result artifact for one composition snapshot
// (docs/architecture/benchmark-runner.md). Two layers are executable today — L1
// structural and L2 capability — because the evidence they need already exists
// (`backend/pkg/ir` and `backend/pkg/composition`). L3 behavioral, L4 operational
// and L5 safety report `not_evaluated` with a precise reason: no dataset, no
// provider and no asset source exist yet, and saying so is the contract, not a
// gap to paper over.
//
// The runner is a batch function, not a service: no route, no queue, no store and
// no model call. Its output is a canonical document whose digest excludes the
// clock, so two runs of the same request are comparable.
package evaluation
