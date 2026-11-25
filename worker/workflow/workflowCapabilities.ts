/**
 * P7-RUN-001c — the capability advertisement of the deployed workflow runtime.
 *
 * The execution-provider contract (docs/architecture/adr-execution-provider-contract.md)
 * says a provider must *advertise* what it keeps, and a plan that needs an
 * unadvertised guarantee must be refused at admission — never silently
 * downgraded. This module is the runtime's side of that rule.
 *
 * Why this is not the same answer as `execution.CloudflareCapabilities`
 * (backend/pkg/execution): that one describes the REST adapter talking to the
 * Workflows API from outside (it cannot inject a sleep into a running workflow
 * and cannot read step history over the API), while this one describes the
 * deployed workflow itself, whose DAG has `sleep` nodes (step.sleep) and whose
 * step logs land in workflow_step_logs. Two layers, two honest answers.
 */

/** The guarantees a runtime advertises; `false` means "refuse, do not degrade". */
export interface CapabilityAdvertisement {
  cancel: boolean;
  pause: boolean;
  longSleep: boolean;
  history: boolean;
}

/** The guarantees the deployed runtime keeps. */
export const WORKFLOW_CAPABILITIES: CapabilityAdvertisement = {
  /** A run can be terminated; the state becomes `cancelled` and stays final. */
  cancel: true,
  /** A running run can be held at its next step boundary and resumed. */
  pause: true,
  /** `sleep` nodes map to step.sleep: no worker is held for the duration. */
  longSleep: true,
  /** Per-step rows are written to workflow_step_logs (best effort). */
  history: true,
};

export type WorkflowCapability = keyof typeof WORKFLOW_CAPABILITIES;

/** Run states that never transition again (contract rule 5). */
export const TERMINAL_STATES = ['succeeded', 'failed', 'cancelled'] as const;

/** True when a state is final: no cancel/pause/resume/overwrite may follow. */
export function isTerminalState(status: string | null | undefined): boolean {
  const state = (status ?? '').trim().toLowerCase();
  return (TERMINAL_STATES as readonly string[]).includes(state);
}

/**
 * True when a run was cancelled. A cancelled run is final in the strictest
 * sense: the runtime must not rewrite it as `failed` or `succeeded`, which is
 * exactly what happens today when Cloudflare terminates an instance and the run
 * loop throws.
 */
export function isCancelled(status: string | null | undefined): boolean {
  return (status ?? '').trim().toLowerCase() === 'cancelled';
}

/**
 * Returns the capabilities a run needs that the runtime does not advertise.
 * Admission refuses the run when the list is non-empty: a missing guarantee is
 * a refusal, not a degraded run.
 *
 * The advertisement is a parameter so the rule can be proven against a runtime
 * that keeps less than the deployed one — and so the refuser and the advertised
 * answer are always the same object (never two copies that drift).
 */
export function missingCapabilities(
  required: readonly WorkflowCapability[],
  advertisement: CapabilityAdvertisement = WORKFLOW_CAPABILITIES,
): WorkflowCapability[] {
  return required.filter((capability) => advertisement[capability] !== true);
}

/**
 * Derives the capabilities one DAG document depends on. Adding a node kind that
 * needs a new guarantee means adding it here, so the refusal stays honest
 * instead of silently assuming support.
 */
export function dagRequires(dag: { nodes?: ReadonlyArray<{ type?: string }> | null } | null): WorkflowCapability[] {
  const required = new Set<WorkflowCapability>();
  for (const node of dag?.nodes ?? []) {
    if (node?.type === 'sleep') required.add('longSleep');
  }
  return [...required];
}
