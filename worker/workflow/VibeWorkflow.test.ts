/**
 * VibeWorkflow — Phase-2 DAG interpreter tests.
 *
 * Runs against the vitest pool-workers runtime (so `cloudflare:workers` /
 * `cloudflare:workflows` modules resolve) but drives `VibeWorkflow.run()`
 * directly with a fake `WorkflowStep` and a mocked `env.DB.prepare` chain,
 * asserting the orchestration contract:
 *   - topological execution order, 1 node = 1 step.do, name = node id
 *   - edge conditions evaluated between steps (pruned branches are skipped)
 *   - sleep nodes map to step.sleep(nodeId, duration)
 *   - retry/timeout propagate 1:1 into step.do's WorkflowStepConfig
 *   - invalid/missing DAGs mark the instance failed + throw NonRetryableError
 *   - P7.12 run control: a cancelled run is final, and a DAG needing an
 *     unadvertised guarantee is refused before any step
 */

import { describe, it, expect } from 'vitest';
import { createExecutionContext, waitOnExecutionContext } from 'cloudflare:test';
import { NonRetryableError } from 'cloudflare:workflows';
import type { WorkflowStep } from 'cloudflare:workers';
import {
  VibeWorkflow,
  type WorkflowInput,
  type WorkflowRunResult,
  type WorkflowV2Document,
} from './VibeWorkflow';
import {
  WORKFLOW_CAPABILITIES,
  type CapabilityAdvertisement,
} from './workflowCapabilities';

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

interface FakeInstanceRow {
  workflow_id: string;
  status: string;
  input: string | null;
  user_id: string | null;
}

interface StepCall {
  kind: 'do' | 'sleep';
  name: string;
  config?: unknown;
  duration?: unknown;
}

function makeStep(opts: {
  results?: Record<string, unknown>;
  invokeCallbacks?: boolean;
  /** P7.12: fires when a step starts, so a test can act mid-run. */
  onStep?: (name: string) => void;
  /** P7.12: makes one step throw, emulating a terminated instance. */
  throwOn?: string;
}) {
  const calls: StepCall[] = [];
  return {
    calls,
    async do(name: string, arg2: unknown, arg3?: unknown): Promise<unknown> {
      const config = typeof arg2 === 'function' ? undefined : arg2;
      const callback = typeof arg2 === 'function' ? arg2 : arg3;
      calls.push({ kind: 'do', name, config });
      opts.onStep?.(name);
      if (opts.throwOn === name) {
        // What Cloudflare does to a terminated instance: the in-flight step
        // throws, and the run's catch block runs.
        throw new Error('vibe-workflow: instance terminated');
      }
      if (opts.invokeCallbacks && callback) {
        return (callback as () => Promise<unknown>)();
      }
      return opts.results?.[name];
    },
    async sleep(name: string, duration: unknown): Promise<void> {
      calls.push({ kind: 'sleep', name, duration });
    },
  };
}

interface FakeWrite {
  sql: string;
  values: unknown[];
}

function makeFakeDb(opts: { instance: FakeInstanceRow | null; dag: Record<string, unknown> | null }) {
  const writes: FakeWrite[] = [];
  // P7.12: writes the `status <> 'cancelled'` guard refused, kept apart so a
  // test can prove both "the runtime tried" and "the row did not move".
  const refusedWrites: FakeWrite[] = [];

  class FakeStatement {
    private values: unknown[] = [];
    constructor(private readonly sql: string) {}

    bind(...values: unknown[]): FakeStatement {
      this.values = values;
      return this;
    }

    async first(): Promise<Record<string, unknown> | null> {
      if (this.sql.includes('FROM workflow_instances')) {
        return opts.instance as Record<string, unknown> | null;
      }
      if (this.sql.includes('FROM workflow_dags')) return opts.dag;
      return null;
    }

    async run(): Promise<{ success: boolean; meta: { changes: number; last_row_id: number } }> {
      const write = { sql: this.sql, values: [...this.values] };
      // Emulate the WHERE clause the production SQL carries: a guarded instance
      // write cannot touch a row that an operator already cancelled.
      const guarded = this.sql.includes('UPDATE workflow_instances') && this.sql.includes("status <> 'cancelled'");
      if (guarded && opts.instance && opts.instance.status.trim().toLowerCase() === 'cancelled') {
        refusedWrites.push(write);
        return { success: true, meta: { changes: 0, last_row_id: 0 } };
      }
      writes.push(write);
      return { success: true, meta: { changes: 1, last_row_id: 1 } };
    }

    async all(): Promise<{ results: Record<string, unknown>[]; success: boolean; meta: { changes: number; last_row_id: number } }> {
      return { results: [], success: true, meta: { changes: 0, last_row_id: 0 } };
    }
  }

  return {
    db: {
      prepare(sql: string): FakeStatement {
        return new FakeStatement(sql);
      },
    },
    writes,
    refusedWrites,
    /** P7.12: the operator's cancel lands while the run is in flight. */
    cancelInstance(): void {
      if (opts.instance) opts.instance.status = 'cancelled';
    },
    /** The durable row as it stands after the run. */
    instanceRow(): FakeInstanceRow | null {
      return opts.instance;
    },
  };
}

/**
 * `WorkflowEntrypoint` is a workerd-native class: its constructor only accepts a
 * native `ExecutionContext`, while the pool-workers runtime can only hand out a
 * JS-shaped one (`createExecutionContext()` returns a plain class instance), so
 * `new VibeWorkflow(...)` always throws
 * "Failed to construct 'WorkflowEntrypoint': constructor parameter 1 is not of
 * type 'ExecutionContext'". Build the instance from the prototype and inject the
 * two fields `run()` reads instead; `ctx.waitUntil` still lands in
 * `waitOnExecutionContext`.
 */
function makeWorkflow(
  ctx: ExecutionContext,
  env: Env,
  workflowClass: typeof VibeWorkflow = VibeWorkflow,
): VibeWorkflow {
  const instance = Object.create(workflowClass.prototype) as VibeWorkflow;
  for (const [key, value] of Object.entries({ ctx, env })) {
    Object.defineProperty(instance, key, {
      value,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  }
  return instance;
}

interface RunResult {
  success: boolean;
  result?: WorkflowRunResult;
  error?: Error;
}

async function runWorkflow(opts: {
  dag: WorkflowV2Document | null;
  instance?: FakeInstanceRow | null;
  payload?: WorkflowInput;
  results?: Record<string, unknown>;
  invokeCallbacks?: boolean;
  /** Owning tenant recorded on the DAG row (P0.7); null = unowned. */
  dagTenant?: string | null;
  /** P7.12: run under a runtime whose advertisement differs from the deployed one. */
  workflowClass?: typeof VibeWorkflow;
  /** P7.12: cancel the durable row when this step starts (a mid-run cancel). */
  cancelAtStep?: string;
  /** P7.12: make this step throw (a terminated instance). */
  throwOn?: string;
}): Promise<{
  outcome: RunResult;
  callNames: string[];
  stepDoConfigs: Record<string, unknown>;
  writes: FakeWrite[];
  refusedWrites: FakeWrite[];
  instanceRow: FakeInstanceRow | null;
}> {
  const fakeDb = makeFakeDb({
    instance:
      opts.instance === undefined
        ? { workflow_id: 'wf-1', status: 'pending', input: null, user_id: null }
        : opts.instance,
    dag: opts.dag
      ? {
          schema_version: opts.dag.schemaVersion,
          dag_json: JSON.stringify(opts.dag),
          user_id: opts.dagTenant ?? null,
        }
      : null,
  });
  const step = makeStep({
    results: opts.results,
    invokeCallbacks: opts.invokeCallbacks,
    throwOn: opts.throwOn,
    onStep: opts.cancelAtStep
      ? (name) => {
          if (name === opts.cancelAtStep) fakeDb.cancelInstance();
        }
      : undefined,
  });
  const ctx = createExecutionContext();

  const workflow = makeWorkflow(
    ctx as unknown as ExecutionContext,
    { DB: fakeDb.db } as unknown as Env,
    opts.workflowClass,
  );

  let outcome: RunResult;
  try {
    const result = await workflow.run(
      { instanceId: 'inst-1', payload: opts.payload ?? {}, timestamp: new Date() },
      step as unknown as WorkflowStep,
    );
    outcome = { success: true, result };
  } catch (error) {
    outcome = { success: false, error: error as Error };
  }
  await waitOnExecutionContext(ctx);

  const stepDoConfigs: Record<string, unknown> = {};
  for (const call of step.calls) {
    if (call.kind === 'do' && call.config !== undefined) {
      stepDoConfigs[call.name] = call.config;
    }
  }
  return {
    outcome,
    callNames: step.calls.map((call) => call.name),
    stepDoConfigs,
    writes: fakeDb.writes,
    refusedWrites: fakeDb.refusedWrites,
    instanceRow: fakeDb.instanceRow(),
  };
}

function instanceStatus(writes: FakeWrite[], wanted: string): boolean {
  return writes.some((write) => {
    const trimmed = write.sql.trimStart();
    return trimmed.startsWith('UPDATE workflow_instances') && write.values[0] === wanted;
  });
}

function skippedStepLogs(writes: FakeWrite[]): FakeWrite[] {
  return writes.filter((write) => {
    const trimmed = write.sql.trimStart();
    return trimmed.startsWith('INSERT INTO workflow_step_logs') && write.values.includes('skipped');
  });
}

const VALID_AI = { model: 'm', prompt: 'p' };
const VALID_EMAIL = { to: 'a@example.com', subject: 's' };

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('VibeWorkflow — topological execution', () => {
  it('runs nodes in topological order with step names equal to node ids', async () => {
    const dag: WorkflowV2Document = {
      schemaVersion: 2,
      nodes: [
        { id: 'trigger', type: 'trigger' },
        { id: 'step-a', type: 'ai', params: VALID_AI },
        { id: 'step-b', type: 'ai', params: VALID_AI },
        { id: 'step-c', type: 'ai', params: VALID_AI },
      ],
      edges: [
        { source: 'trigger', target: 'step-a' },
        { source: 'step-a', target: 'step-b' },
        { source: 'step-b', target: 'step-c' },
      ],
    };

    const { outcome, callNames, writes } = await runWorkflow({
      dag,
      results: {
        trigger: { hello: 'world' },
        'step-a': { done: true },
        'step-b': { done: true },
        'step-c': { done: true },
      },
    });

    expect(outcome.success).toBe(true);
    expect(callNames).toEqual(['trigger', 'step-a', 'step-b', 'step-c']);
    expect(instanceStatus(writes, 'running')).toBe(true);
    expect(instanceStatus(writes, 'succeeded')).toBe(true);
    expect(outcome.result?.payload).toEqual({});
    expect(Object.keys(outcome.result?.steps ?? {})).toEqual([
      'trigger',
      'step-a',
      'step-b',
      'step-c',
    ]);
  });
});

describe('VibeWorkflow — condition branching', () => {
  const truthyBranchDag = (): WorkflowV2Document => ({
    schemaVersion: 2,
    nodes: [
      { id: 'trigger', type: 'trigger' },
      { id: 'gate', type: 'condition', params: { condOp: 'truthy', lhs: '${var.amount}' } },
      { id: 'yes', type: 'email', params: VALID_EMAIL },
      { id: 'no', type: 'email', params: VALID_EMAIL },
    ],
    edges: [
      { source: 'trigger', target: 'gate' },
      { source: 'gate', target: 'yes', condition: { op: 'eq', lhs: '${step.gate}', rhs: 'true' } },
      { source: 'gate', target: 'no', condition: { op: 'eq', lhs: '${step.gate}', rhs: 'false' } },
    ],
  });

  it('executes the true branch and prunes the false branch (skipped log)', async () => {
    const { outcome, callNames, writes } = await runWorkflow({
      dag: truthyBranchDag(),
      payload: { amount: 42 }, // truthy(${var.amount}) -> true
      invokeCallbacks: true,
    });

    expect(outcome.success).toBe(true);
    expect(callNames).toEqual(['trigger', 'gate', 'yes']);
    expect(callNames).not.toContain('no');

    const skipped = skippedStepLogs(writes);
    expect(skipped).toHaveLength(1);
    expect(skipped[0].values).toContain('no');
  });

  it('evaluates a numeric edge condition against the event payload', async () => {
    const { outcome, callNames, writes } = await runWorkflow({
      dag: {
        schemaVersion: 2,
        nodes: [
          { id: 'trigger', type: 'trigger' },
          { id: 'window', type: 'sleep', params: { duration: '1 second' } },
          { id: 'smoke', type: 'email', params: VALID_EMAIL },
        ],
        edges: [
          { source: 'trigger', target: 'window' },
          {
            source: 'window',
            target: 'smoke',
            condition: { op: 'gt', lhs: '${var.amount}', rhs: '100' },
          },
        ],
      },
      payload: { amount: 250 },
    });

    expect(outcome.success).toBe(true);
    expect(callNames).toEqual(['trigger', 'window', 'smoke']);
    expect(skippedStepLogs(writes)).toHaveLength(0);
  });
});

describe('VibeWorkflow — sleep nodes', () => {
  it('maps a sleep node to step.sleep with the parsed duration', async () => {
    const dag: WorkflowV2Document = {
      schemaVersion: 2,
      nodes: [
        { id: 'trigger', type: 'trigger' },
        { id: 'wait', type: 'sleep', params: { duration: '5 minutes' } },
        { id: 'after', type: 'ai', params: VALID_AI },
      ],
      edges: [
        { source: 'trigger', target: 'wait' },
        { source: 'wait', target: 'after' },
      ],
    };

    const { outcome, callNames, writes } = await runWorkflow({
      dag,
      results: { trigger: {}, after: { done: true } },
    });

    expect(outcome.success).toBe(true);
    expect(callNames).toEqual(['trigger', 'wait', 'after']);
    expect(instanceStatus(writes, 'succeeded')).toBe(true);
  });

  it('rejects a sleep node with an invalid duration as NonRetryableError', async () => {
    const dag: WorkflowV2Document = {
      schemaVersion: 2,
      nodes: [
        { id: 'trigger', type: 'trigger' },
        { id: 'wait', type: 'sleep', params: { duration: 'soon' } },
      ],
      edges: [{ source: 'trigger', target: 'wait' }],
    };

    const { outcome, writes } = await runWorkflow({ dag });
    expect(outcome.success).toBe(false);
    expect(outcome.error).toBeInstanceOf(NonRetryableError);
    expect(instanceStatus(writes, 'failed')).toBe(true);
  });
});

describe('VibeWorkflow — step config propagation', () => {
  it('maps retry and timeout 1:1 onto step.do config', async () => {
    const dag: WorkflowV2Document = {
      schemaVersion: 2,
      nodes: [
        { id: 'trigger', type: 'trigger' },
        {
          id: 'flaky',
          type: 'ai',
          params: VALID_AI,
          retry: { limit: 3, delay: '5 seconds', backoff: 'constant' },
          timeout: '1 minute',
        },
      ],
      edges: [{ source: 'trigger', target: 'flaky' }],
    };

    const { outcome, stepDoConfigs } = await runWorkflow({
      dag,
      results: { trigger: {}, flaky: { ok: true } },
    });

    expect(outcome.success).toBe(true);
    expect(stepDoConfigs['flaky']).toEqual({
      retries: { limit: 3, delay: '5 seconds', backoff: 'constant' },
      timeout: '1 minute',
    });
    expect(stepDoConfigs['trigger']).toBeUndefined();
  });

  it('leaves config undefined when the node has no retry or timeout', async () => {
    const dag: WorkflowV2Document = {
      schemaVersion: 2,
      nodes: [
        { id: 'trigger', type: 'trigger' },
        { id: 'plain', type: 'ai', params: VALID_AI },
      ],
      edges: [{ source: 'trigger', target: 'plain' }],
    };

    const { outcome, stepDoConfigs } = await runWorkflow({
      dag,
      results: { trigger: {}, plain: { ok: true } },
    });

    expect(outcome.success).toBe(true);
    expect(stepDoConfigs['plain']).toBeUndefined();
  });
});

describe('VibeWorkflow — failure policies', () => {
  it('marks the instance failed and throws NonRetryableError on a cycle', async () => {
    const dag: WorkflowV2Document = {
      schemaVersion: 2,
      nodes: [
        { id: 'trigger', type: 'trigger' },
        { id: 'a', type: 'ai', params: VALID_AI },
        { id: 'b', type: 'ai', params: VALID_AI },
      ],
      edges: [
        { source: 'a', target: 'b' },
        { source: 'b', target: 'a' },
      ],
    };

    const { outcome, writes } = await runWorkflow({ dag });
    expect(outcome.success).toBe(false);
    expect(outcome.error).toBeInstanceOf(NonRetryableError);
    expect(outcome.error?.message).toContain('cycle');
    expect(instanceStatus(writes, 'failed')).toBe(true);
  });

  it('rejects a wrong schemaVersion before any step executes', async () => {
    const dag: WorkflowV2Document = {
      schemaVersion: 1,
      nodes: [{ id: 'trigger', type: 'trigger' }],
      edges: [],
    };

    const { outcome, callNames, writes } = await runWorkflow({ dag });
    expect(outcome.success).toBe(false);
    expect(outcome.error).toBeInstanceOf(NonRetryableError);
    expect(outcome.error?.message).toContain('schemaVersion');
    expect(callNames).toEqual([]);
    expect(instanceStatus(writes, 'failed')).toBe(true);
  });

  it('marks the instance failed when the DAG row is missing', async () => {
    const { outcome, writes } = await runWorkflow({ dag: null });
    expect(outcome.success).toBe(false);
    expect(outcome.error).toBeInstanceOf(NonRetryableError);
    expect(outcome.error?.message).toContain('not found');
    expect(instanceStatus(writes, 'failed')).toBe(true);
  });

  it('throws NonRetryableError when the instance row is missing', async () => {
    const { outcome, writes } = await runWorkflow({ dag: null, instance: null });
    expect(outcome.success).toBe(false);
    expect(outcome.error).toBeInstanceOf(NonRetryableError);
    expect(outcome.error?.message).toContain('instance');
    // No DB writes: there is no instance row to update.
    expect(writes).toEqual([]);
  });
});

describe('VibeWorkflow — tenant isolation (P0.7)', () => {
  const singleStepDag = (): WorkflowV2Document => ({
    schemaVersion: 2,
    nodes: [{ id: 'trigger', type: 'trigger' }],
    edges: [],
  });

  it('refuses to execute a DAG owned by another tenant', async () => {
    const { outcome, callNames, writes } = await runWorkflow({
      dag: singleStepDag(),
      dagTenant: 'user-a',
      instance: { workflow_id: 'wf-1', status: 'pending', input: null, user_id: 'user-b' },
    });

    expect(outcome.success).toBe(false);
    expect(outcome.error).toBeInstanceOf(NonRetryableError);
    expect(outcome.error?.message).toContain('tenant mismatch');
    // No step runs and the instance is marked failed, never running.
    expect(callNames).toEqual([]);
    expect(instanceStatus(writes, 'failed')).toBe(true);
    expect(instanceStatus(writes, 'running')).toBe(false);
  });

  it('refuses an owned DAG when the run records no tenant', async () => {
    const { outcome, callNames } = await runWorkflow({
      dag: singleStepDag(),
      dagTenant: 'user-a',
      instance: { workflow_id: 'wf-1', status: 'pending', input: null, user_id: null },
    });

    expect(outcome.success).toBe(false);
    expect(outcome.error?.message).toContain('tenant mismatch');
    expect(callNames).toEqual([]);
  });

  it('executes when the run tenant matches the DAG owner', async () => {
    const { outcome, callNames } = await runWorkflow({
      dag: singleStepDag(),
      dagTenant: 'user-a',
      instance: { workflow_id: 'wf-1', status: 'pending', input: null, user_id: 'user-a' },
      results: { trigger: { ok: true } },
    });

    expect(outcome.success).toBe(true);
    expect(callNames).toEqual(['trigger']);
  });

  it('executes an unowned DAG with an unowned run (pre-P0.7 rows)', async () => {
    const { outcome, callNames } = await runWorkflow({
      dag: singleStepDag(),
      dagTenant: null,
      instance: { workflow_id: 'wf-1', status: 'pending', input: null, user_id: null },
      results: { trigger: { ok: true } },
    });

    expect(outcome.success).toBe(true);
    expect(callNames).toEqual(['trigger']);
  });
});

describe('VibeWorkflow — run control (P7.12)', () => {
  /** A runtime that keeps only three of the four advertised guarantees. */
  class NoLongSleepWorkflow extends VibeWorkflow {
    static override readonly advertisedCapabilities: CapabilityAdvertisement = {
      ...WORKFLOW_CAPABILITIES,
      longSleep: false,
    };
  }

  const sleepDag = (): WorkflowV2Document => ({
    schemaVersion: 2,
    nodes: [
      { id: 'trigger', type: 'trigger' },
      { id: 'wait', type: 'sleep', params: { duration: '1 second' } },
    ],
    edges: [{ source: 'trigger', target: 'wait' }],
  });

  it('advertises exactly the object it enforces', () => {
    // One object, not two copies that can drift: the caller reads this and
    // `run()` refuses against this.
    expect(VibeWorkflow.advertisedCapabilities).toBe(WORKFLOW_CAPABILITIES);
  });

  it('writes nothing when the instance was already cancelled', async () => {
    const { outcome, callNames, writes, refusedWrites } = await runWorkflow({
      dag: sleepDag(),
      instance: { workflow_id: 'wf-1', status: 'cancelled', input: null, user_id: null },
    });

    // A re-entered instance is a no-op: no step, no state write and no refusal.
    // A final state must not be re-decided by the runtime.
    expect(outcome.success).toBe(true);
    expect(outcome.result).toEqual({ payload: {}, steps: {} });
    expect(callNames).toEqual([]);
    expect(writes).toEqual([]);
    expect(refusedWrites).toEqual([]);
  });

  it('keeps a cancel that lands mid-run final: no succeeded rewrite', async () => {
    const { outcome, callNames, writes, refusedWrites, instanceRow } = await runWorkflow({
      dag: {
        schemaVersion: 2,
        nodes: [
          { id: 'trigger', type: 'trigger' },
          { id: 'gate', type: 'condition', params: { condOp: 'truthy', lhs: '${var.ok}' } },
        ],
        edges: [{ source: 'trigger', target: 'gate' }],
      },
      payload: { ok: true },
      invokeCallbacks: true,
      cancelAtStep: 'gate',
    });

    expect(outcome.success).toBe(true);
    expect(callNames).toEqual(['trigger', 'gate']);
    // `running` was written before the cancel landed; `succeeded` was refused
    // by the `status <> 'cancelled'` guard — the operator's decision wins.
    expect(instanceStatus(writes, 'running')).toBe(true);
    expect(instanceStatus(writes, 'succeeded')).toBe(false);
    expect(refusedWrites.some((write) => write.values[0] === 'succeeded')).toBe(true);
    expect(instanceRow?.status).toBe('cancelled');
  });

  it('never rewrites a cancelled run as failed when the instance is terminated', async () => {
    const { outcome, writes, refusedWrites, instanceRow } = await runWorkflow({
      dag: {
        schemaVersion: 2,
        nodes: [
          { id: 'trigger', type: 'trigger' },
          { id: 'gate', type: 'condition', params: { condOp: 'truthy', lhs: '${var.ok}' } },
        ],
        edges: [{ source: 'trigger', target: 'gate' }],
      },
      payload: { ok: true },
      invokeCallbacks: true,
      cancelAtStep: 'gate',
      // What Cloudflare does to a terminated instance: the in-flight step throws.
      throwOn: 'gate',
    });

    expect(outcome.success).toBe(false);
    expect(outcome.error?.message).toContain('terminated');
    // The catch block tried to mark the run failed; the guard refused it, so
    // the row still says `cancelled` and the refused-vs-failed distinction holds.
    expect(instanceStatus(writes, 'failed')).toBe(false);
    expect(refusedWrites.some((write) => write.values[0] === 'failed')).toBe(true);
    expect(instanceRow?.status).toBe('cancelled');
  });

  it('refuses a DAG that needs an unadvertised guarantee, before any step', async () => {
    const { outcome, callNames, writes } = await runWorkflow({
      dag: sleepDag(),
      workflowClass: NoLongSleepWorkflow,
    });

    expect(outcome.success).toBe(false);
    expect(outcome.error).toBeInstanceOf(NonRetryableError);
    expect(outcome.error?.message).toContain('longSleep');
    expect(outcome.error?.message).toContain('does not advertise');
    // Refusal, not degradation: no step ran, the run never went `running`, and
    // the only write is the honest failure that says why.
    expect(callNames).toEqual([]);
    expect(instanceStatus(writes, 'running')).toBe(false);
    expect(instanceStatus(writes, 'failed')).toBe(true);
    expect(writes).toHaveLength(1);
  });

  it('admits the same DAG when the runtime advertises the guarantee', async () => {
    const { outcome, callNames } = await runWorkflow({ dag: sleepDag() });

    expect(outcome.success).toBe(true);
    expect(callNames).toEqual(['trigger', 'wait']);
  });
});