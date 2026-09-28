/**
 * VibeWorkflow — Phase-2 execution layer for DAG schema v2.
 *
 * Interprets a Cloudflare Workflows DAG persisted in D1 (`workflow_dags`) by
 * the Go control plane (see `backend/pkg/engine/workflowschema.go`) and
 * executes it as a Cloudflare Workflow:
 *
 *   - 1 DAG node === 1 `step.do` (step name = node id)
 *   - edge conditions are evaluated between steps, outside `step.do`
 *   - `sleep` nodes map to `step.sleep(nodeId, duration)`
 *   - per-node `retry` / `timeout` map 1:1 to `step.do`'s `WorkflowStepConfig`
 *   - the trigger node receives `event.payload` as its execution input
 *   - condition `LHS` values are references (`${step.<id>}` / `${var.<key>}`)
 *     resolved against accumulated step outputs + the event payload; the
 *     `RHS` is a literal (`true` / `false` / number / string)
 *
 * Failure policy: a missing/invalid DAG (wrong schema version, bad node
 * params, unknown edge references, cycles, ...) marks the instance `failed`
 * in D1 and throws `NonRetryableError` so the Cloudflare runtime does not
 * schedule spurious retries.
 *
 * Persistence: instance transitions (`running` / `succeeded` / `failed`) are
 * awaited so the terminal D1 state is guaranteed; per-step log rows are
 * written fire-and-forget via `ctx.waitUntil`.
 */

import {
  WorkflowEntrypoint,
  type WorkflowBackoff,
  type WorkflowDelayDuration,
  type WorkflowEvent,
  type WorkflowSleepDuration,
  type WorkflowStep,
  type WorkflowStepConfig,
  type WorkflowTimeoutDuration,
} from 'cloudflare:workers';
import { NonRetryableError } from 'cloudflare:workflows';

export const WORKFLOW_SCHEMA_VERSION = 2;
export const WORKFLOW_NODE_TYPES = [
  'trigger',
  'http',
  'db',
  'ai',
  'email',
  'condition',
  'sleep',
] as const;
export const WORKFLOW_CONDITION_OPS = [
  'eq',
  'neq',
  'gt',
  'gte',
  'lt',
  'lte',
  'contains',
  'truthy',
] as const;
export const WORKFLOW_DB_OPS = ['insert', 'upsert', 'select', 'update', 'delete'] as const;
export const WORKFLOW_HTTP_METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE'] as const;
export const WORKFLOW_BACKOFF_MODES = ['constant', 'linear', 'exponential'] as const;

// ---------------------------------------------------------------------------
// DAG document types — typed mirrors of backend/pkg/engine/workflowschema.go
// ---------------------------------------------------------------------------

export type WorkflowV2NodeType = (typeof WORKFLOW_NODE_TYPES)[number];
export type WorkflowV2ConditionOp = (typeof WORKFLOW_CONDITION_OPS)[number];
export type WorkflowV2DbOp = (typeof WORKFLOW_DB_OPS)[number];
export type WorkflowV2HttpMethod = (typeof WORKFLOW_HTTP_METHODS)[number];
export type WorkflowV2Backoff = (typeof WORKFLOW_BACKOFF_MODES)[number];

/** Visual node position (layout only; ignored by the runtime). */
export interface WorkflowV2Position {
  x: number;
  y: number;
}

/**
 * Per-node retry policy, translated 1:1 to `step.do()`'s `retries` option.
 */
export interface WorkflowV2Retry {
  limit?: number;
  delay?: string;
  backoff?: string;
}

/**
 * Option bag attached to a node. The executable subset depends on `type`;
 * `validateWorkflowV2` enforces the per-type required fields.
 */
export interface WorkflowV2Params {
  // http
  url?: string;
  method?: string;
  headers?: Record<string, string>;
  body?: unknown;
  // db
  table?: string;
  op?: string;
  where?: Record<string, unknown>;
  data?: Record<string, unknown>;
  // ai
  model?: string;
  prompt?: string;
  system?: string;
  // email
  to?: string;
  subject?: string;
  // condition (node-level branch; edge conditions use WorkflowV2Condition)
  condOp?: string;
  lhs?: string;
  rhs?: string;
  // sleep
  duration?: string;
}

export interface WorkflowV2Node {
  id: string;
  type: string;
  label?: string;
  params?: WorkflowV2Params;
  position?: WorkflowV2Position;
  retry?: WorkflowV2Retry;
  timeout?: string;
}

/** Optional edge guard evaluated at runtime between steps, outside step.do. */
export interface WorkflowV2Condition {
  op: string;
  lhs: string;
  rhs?: string;
}

export interface WorkflowV2Edge {
  id?: string;
  source: string;
  target: string;
  label?: string;
  condition?: WorkflowV2Condition;
}

/** Top-level DAG document (schema v2). */
export interface WorkflowV2Document {
  schemaVersion: number;
  nodes: WorkflowV2Node[];
  edges: WorkflowV2Edge[];
}

/** Instance payload = the `event.payload` handed to the trigger node. */
export type WorkflowInput = Record<string, unknown>;

/** Result written to `workflow_instances.output` and returned from `run()`. */
export interface WorkflowRunResult {
  payload: WorkflowInput;
  steps: Record<string, unknown>;
}

// ---------------------------------------------------------------------------
// Duration helpers (mirrors Go validWorkflowDuration's suffix set)
// ---------------------------------------------------------------------------

const DURATION_RE =
  /^\s*\d+\s*(?:seconds|second|minutes|minute|hours|hour|days|day|weeks|week)\s*$/;

export function isValidWorkflowDuration(value: string | undefined | null): boolean {
  return typeof value === 'string' && DURATION_RE.test(value);
}

export function parseWorkflowDuration(
  value: string | undefined | null,
): WorkflowSleepDuration | null {
  if (!isValidWorkflowDuration(value)) return null;
  return (value as string).trim() as WorkflowSleepDuration;
}

// ---------------------------------------------------------------------------
// Condition evaluation (small, safe, side-effect-free)
// ---------------------------------------------------------------------------

/** Parse a literal RHS: booleans, null, numbers, otherwise a string. */
export function parseLiteral(value: string): unknown {
  const t = value.trim();
  if (t === 'true') return true;
  if (t === 'false') return false;
  if (t === 'null') return null;
  if (/^-?\d+(?:\.\d+)?$/.test(t)) return Number(t);
  return t;
}

function toNumber(value: unknown): number | null {
  if (typeof value === 'number') return value;
  if (typeof value === 'string' && value.trim() !== '') {
    const n = Number(value.trim());
    return Number.isFinite(n) ? n : null;
  }
  return null;
}

function looseEquals(a: unknown, b: unknown): boolean {
  if (a === null || a === undefined) return b === null || b === undefined;
  if (b === null || b === undefined) return false;
  const na = toNumber(a);
  const nb = toNumber(b);
  if (na !== null && nb !== null) return na === nb;
  return String(a) === String(b);
}

function compare(a: unknown, b: unknown): number {
  const na = toNumber(a);
  const nb = toNumber(b);
  if (na !== null && nb !== null) return na === nb ? 0 : na > nb ? 1 : -1;
  const sa = String(a ?? '');
  const sb = String(b ?? '');
  return sa === sb ? 0 : sa > sb ? 1 : -1;
}

function contains(a: unknown, b: unknown): boolean {
  if (Array.isArray(a)) return a.some((item) => looseEquals(item, b));
  return String(a ?? '').includes(String(b ?? ''));
}

/**
 * Resolve a condition `LHS` reference against accumulated step outputs and
 * the event payload:
 *   - `${step.<id>}`  -> output of the step with that id (trigger = payload)
 *   - `${var.<key>}`  -> payload[key] (falling back to step outputs)
 *   - anything else   -> treated as a literal
 */
export function resolveReference(
  ref: string,
  outputs: ReadonlyMap<string, unknown>,
  payload: Readonly<WorkflowInput>,
): unknown {
  const t = ref.trim();
  if (t.startsWith('${') && t.endsWith('}')) {
    const inner = t.slice(2, -1).trim();
    if (inner.startsWith('step.')) {
      const id = inner.slice(5).trim();
      if (id === 'payload') return payload;
      return outputs.get(id);
    }
    if (inner.startsWith('var.')) {
      const key = inner.slice(4).trim();
      return Object.prototype.hasOwnProperty.call(payload, key)
        ? payload[key]
        : outputs.get(key);
    }
    return Object.prototype.hasOwnProperty.call(payload, inner)
      ? payload[inner]
      : outputs.get(inner);
  }
  return parseLiteral(t);
}

/**
 * Evaluate an edge/node condition. Only allowed operators run; everything
 * else (including a malformed op) evaluates to `false`.
 */
export function evaluateCondition(
  condition: WorkflowV2Condition,
  outputs: ReadonlyMap<string, unknown>,
  payload: Readonly<WorkflowInput>,
): boolean {
  const op = (condition.op ?? '').trim();
  const lhs = resolveReference(condition.lhs ?? '', outputs, payload);
  if (op === 'truthy') return Boolean(lhs);
  const rhs = parseLiteral(condition.rhs ?? '');
  switch (op) {
    case 'eq':
      return looseEquals(lhs, rhs);
    case 'neq':
      return !looseEquals(lhs, rhs);
    case 'gt':
      return compare(lhs, rhs) === 1;
    case 'gte':
      return compare(lhs, rhs) >= 0;
    case 'lt':
      return compare(lhs, rhs) === -1;
    case 'lte':
      return compare(lhs, rhs) <= 0;
    case 'contains':
      return contains(lhs, rhs);
    default:
      return false;
  }
}
// ---------------------------------------------------------------------------
// Validation + topological sorting (mirrors Go WorkflowV2.Validate)
// ---------------------------------------------------------------------------

const NODE_TYPE_SET = new Set<string>(WORKFLOW_NODE_TYPES);
const CONDITION_OP_SET = new Set<string>(WORKFLOW_CONDITION_OPS);
const DB_OP_SET = new Set<string>(WORKFLOW_DB_OPS);
const HTTP_METHOD_SET = new Set<string>(WORKFLOW_HTTP_METHODS);
const BACKOFF_SET = new Set<string>(WORKFLOW_BACKOFF_MODES);
const MAX_RETRY_LIMIT = 10_000;

function validateCondition(prefix: string, cond: WorkflowV2Condition): string[] {
  const problems: string[] = [];
  const op = (cond.op ?? '').trim();
  if (!CONDITION_OP_SET.has(op)) {
    problems.push(`${prefix}: op "${cond.op}" is not one of eq|neq|gt|gte|lt|lte|contains|truthy`);
  }
  if ((cond.lhs ?? '').trim() === '') problems.push(`${prefix}: lhs is empty`);
  if (op !== 'truthy' && (cond.rhs ?? '').trim() === '') {
    problems.push(`${prefix}: rhs is required for op "${op}"`);
  }
  return problems;
}

function validateNodeParams(index: number, node: WorkflowV2Node): string[] {
  const problems: string[] = [];
  const p = node.params ?? {};
  const ref = `node ${index} (${node.type} ${node.id})`;
  switch (node.type) {
    case 'http': {
      const url = (p.url ?? '').trim();
      if (url === '') {
        problems.push(`${ref}: params.url is required for node.type "http"`);
      } else if (!url.startsWith('https://') && !url.startsWith('http://')) {
        problems.push(`${ref}: params.url "${url}" must be an absolute http(s) URL`);
      }
      const method = (p.method ?? '').trim().toUpperCase();
      if (method === '') {
        problems.push(`${ref}: params.method is required for node.type "http"`);
      } else if (!HTTP_METHOD_SET.has(method)) {
        problems.push(`${ref}: params.method "${p.method}" is not one of GET|POST|PUT|PATCH|DELETE`);
      }
      break;
    }
    case 'db': {
      if ((p.table ?? '').trim() === '') {
        problems.push(`${ref}: params.table is required for node.type "db"`);
      }
      const op = (p.op ?? '').trim();
      if (op === '') {
        problems.push(`${ref}: params.op is required for node.type "db"`);
      } else if (!DB_OP_SET.has(op)) {
        problems.push(`${ref}: params.op "${op}" is not one of insert|upsert|select|update|delete`);
      }
      break;
    }
    case 'ai': {
      if ((p.model ?? '').trim() === '') {
        problems.push(`${ref}: params.model is required for node.type "ai"`);
      }
      if ((p.prompt ?? '').trim() === '') {
        problems.push(`${ref}: params.prompt is required for node.type "ai"`);
      }
      break;
    }
    case 'email': {
      if (!(p.to ?? '').includes('@')) {
        problems.push(`${ref}: params.to must be an email address for node.type "email"`);
      }
      if ((p.subject ?? '').trim() === '') {
        problems.push(`${ref}: params.subject is required for node.type "email"`);
      }
      break;
    }
    case 'condition': {
      const op = (p.condOp ?? '').trim();
      if (!CONDITION_OP_SET.has(op)) {
        problems.push(`${ref}: params.condOp "${op}" is not one of eq|neq|gt|gte|lt|lte|contains|truthy`);
      }
      if ((p.lhs ?? '').trim() === '') {
        problems.push(`${ref}: params.lhs is required for node.type "condition"`);
      }
      if (op !== 'truthy' && (p.rhs ?? '').trim() === '') {
        problems.push(`${ref}: params.rhs is required for condOp "${op}" (omit only for truthy)`);
      }
      break;
    }
    case 'sleep':
      if (!isValidWorkflowDuration(p.duration)) {
        problems.push(`${ref}: params.duration "${p.duration}" must be a duration like "5 minutes"`);
      }
      break;
    default:
      break;
  }
  if (node.retry) {
    if (node.retry.limit !== undefined && node.retry.limit > MAX_RETRY_LIMIT) {
      problems.push(`${ref}: retry.limit ${node.retry.limit} exceeds the Workflows maximum of ${MAX_RETRY_LIMIT}`);
    }
    if (node.retry.backoff && !BACKOFF_SET.has(node.retry.backoff)) {
      problems.push(`${ref}: retry.backoff "${node.retry.backoff}" is not one of constant|linear|exponential`);
    }
  }
  if (node.timeout && !isValidWorkflowDuration(node.timeout)) {
    problems.push(`${ref}: timeout "${node.timeout}" must be a duration like "30 seconds"`);
  }
  return problems;
}

/**
 * Validate a v2 DAG document, returning every contract violation it finds:
 * schema version, node ids/types/params, exactly one trigger, edge
 * references, self-loops, edge conditions, and cycles.
 */
export function validateWorkflowV2(dag: WorkflowV2Document): string[] {
  const problems: string[] = [];
  if (dag.schemaVersion !== WORKFLOW_SCHEMA_VERSION) {
    problems.push(`schemaVersion must be ${WORKFLOW_SCHEMA_VERSION} (got ${dag.schemaVersion})`);
  }
  if (dag.nodes.length === 0) problems.push('workflow has no nodes');

  const ids = new Map<string, number>();
  let triggers = 0;
  dag.nodes.forEach((node, i) => {
    if (node.id.trim() === '') {
      problems.push(`node ${i}: id is empty`);
    } else if (ids.has(node.id)) {
      problems.push(`node ${i}: id "${node.id}" is duplicated (first at node ${ids.get(node.id)})`);
    } else {
      ids.set(node.id, i);
    }
    if (!NODE_TYPE_SET.has(node.type)) {
      problems.push(`node ${i}: unknown node.type "${node.type}"`);
    } else if (node.type === 'trigger') {
      triggers += 1;
    }
    problems.push(...validateNodeParams(i, node));
  });

  if (triggers === 0) {
    problems.push('workflow must contain exactly one trigger node (found none)');
  } else if (triggers > 1) {
    problems.push(`workflow must contain exactly one trigger node (found ${triggers})`);
  }

  dag.edges.forEach((edge, i) => {
    if (!ids.has(edge.source)) {
      problems.push(`edge ${i}: source "${edge.source}" references an unknown node`);
    }
    if (!ids.has(edge.target)) {
      problems.push(`edge ${i}: target "${edge.target}" references an unknown node`);
    }
    if (edge.source !== '' && edge.source === edge.target) {
      problems.push(`edge ${i}: self-loop "${edge.source}" -> "${edge.target}"`);
    }
    if (edge.condition) {
      problems.push(...validateCondition(`edge ${i}.condition`, edge.condition));
    }
  });

  if (dag.edges.length > 0 && topoSortWorkflow(dag) === null) {
    problems.push('workflow contains a cycle; edges must form a DAG');
  }

  return problems;
}

/**
 * Kahn's algorithm over the DAG. Returns a stable topological ordering of
 * node ids (declaration order among equal ranks) or `null` when the graph
 * contains a cycle.
 */
export function topoSortWorkflow(dag: WorkflowV2Document): string[] | null {
  const indegree = new Map<string, number>();
  const successors = new Map<string, string[]>();
  for (const node of dag.nodes) {
    indegree.set(node.id, 0);
    successors.set(node.id, []);
  }
  for (const edge of dag.edges) {
    if (!indegree.has(edge.source) || !indegree.has(edge.target)) continue;
    indegree.set(edge.target, (indegree.get(edge.target) ?? 0) + 1);
    successors.get(edge.source)?.push(edge.target);
  }

  const queue = dag.nodes
    .filter((node) => (indegree.get(node.id) ?? 0) === 0)
    .map((node) => node.id);
  const order: string[] = [];
  for (let i = 0; i < queue.length; i += 1) {
    const id = queue[i];
    order.push(id);
    for (const next of successors.get(id) ?? []) {
      const remaining = (indegree.get(next) ?? 0) - 1;
      indegree.set(next, remaining);
      if (remaining === 0) queue.push(next);
    }
  }
  return order.length === dag.nodes.length ? order : null;
}

// ---------------------------------------------------------------------------
// Step config mapping (retry/timeout -> WorkflowStepConfig)
// ---------------------------------------------------------------------------

/**
 * Map a node's `retry`/`timeout` onto a Cloudflare Workflows step config:
 *   - `retry.limit`            -> `retries.limit` (1:1)
 *   - `retry.delay`            -> `retries.delay` (defaults to "30 seconds")
 *   - `retry.backoff`          -> `retries.backoff`
 *   - `timeout`                -> `timeout`
 * Returns `undefined` when the node has neither knob, in which case the
 * plain `step.do(name, callback)` form is used.
 */
export function buildStepConfig(node: WorkflowV2Node): WorkflowStepConfig | undefined {
  const config: WorkflowStepConfig = {};
  const retry = node.retry;
  if (retry && retry.limit !== undefined && retry.limit > 0) {
    config.retries = {
      limit: retry.limit,
      delay: (retry.delay?.trim() || '30 seconds') as WorkflowDelayDuration,
      ...(retry.backoff?.trim()
        ? { backoff: retry.backoff.trim() as WorkflowBackoff }
        : {}),
    };
  }
  const timeout = node.timeout?.trim();
  if (timeout) config.timeout = timeout as WorkflowTimeoutDuration;
  return Object.keys(config).length > 0 ? config : undefined;
}

// ---------------------------------------------------------------------------
// VibeWorkflow entrypoint
// ---------------------------------------------------------------------------

interface WorkflowInstanceRow {
  workflow_id: string;
  status: string;
  input: string | null;
}

interface WorkflowDagRow {
  schema_version: number;
  dag_json: string;
}

/** WorkflowStep.do is generic over the serializable result; narrow it here. */
type StepDo = (
  name: string,
  configOrCallback: WorkflowStepConfig | (() => Promise<unknown>),
  callback?: () => Promise<unknown>,
) => Promise<unknown>;

const SAFE_IDENTIFIER_RE = /^[A-Za-z_][A-Za-z0-9_]*$/;

function isSafeIdentifier(value: string): boolean {
  return SAFE_IDENTIFIER_RE.test(value);
}

/** Coerce arbitrary DAG data into a D1 bindable value (no booleans in SQLite). */
function bindValue(value: unknown): string | number | null {
  if (value === null || value === undefined) return null;
  if (typeof value === 'number' || typeof value === 'string') return value;
  if (typeof value === 'boolean') return value ? 1 : 0;
  return JSON.stringify(value);
}

/**
 * Phase-2 runtime for DAG schema v2. Instantiated by Cloudflare Workflows
 * via the `[[workflows]]` binding; `event.payload` is the instance input
 * handed to the trigger node.
 */
export class VibeWorkflow extends WorkflowEntrypoint<Env, WorkflowInput> {
  override async run(
    event: Readonly<WorkflowEvent<WorkflowInput>>,
    step: WorkflowStep,
  ): Promise<WorkflowRunResult> {
    const instanceId = event.instanceId;
    const payload: WorkflowInput = { ...(event.payload ?? {}) };

    try {
      const instance = await this.loadInstance(instanceId);
      if (!instance) {
        throw new NonRetryableError(`vibe-workflow: workflow instance "${instanceId}" not found`);
      }

      const dagRow = await this.loadDag(instance.workflow_id);
      if (!dagRow) {
        const message = `vibe-workflow: workflow DAG not found for workflow_id "${instance.workflow_id}"`;
        await this.markFailed(instanceId, message);
        throw new NonRetryableError(message);
      }

      let dag: WorkflowV2Document;
      try {
        dag = JSON.parse(dagRow.dag_json) as WorkflowV2Document;
      } catch {
        const message = `vibe-workflow: workflow DAG for "${instance.workflow_id}" is not valid JSON`;
        await this.markFailed(instanceId, message);
        throw new NonRetryableError(message);
      }

      const problems = validateWorkflowV2(dag);
      if (problems.length > 0) {
        const message = `vibe-workflow: invalid workflow DAG: ${problems.join('; ')}`;
        await this.markFailed(instanceId, message);
        throw new NonRetryableError(message);
      }

      await this.markRunning(instanceId, payload);
      const outputs = await this.walk(dag, step, payload, instanceId);
      const result: WorkflowRunResult = {
        payload,
        steps: Object.fromEntries(outputs),
      };
      await this.markSucceeded(instanceId, result);
      return result;
    } catch (error) {
      if (error instanceof NonRetryableError) throw error;
      const message = error instanceof Error ? error.message : String(error);
      await this.markFailed(instanceId, message).catch(() => undefined);
      throw error;
    }
  }

  /**
   * Walk the DAG in topological order, executing each non-pruned node as a
   * step and evaluating edge conditions between steps. A node runs only when
   * all of its incoming edges have been released (AND semantics); an edge
   * whose condition evaluates to `false` permanently prunes its target.
   */
  private async walk(
    dag: WorkflowV2Document,
    step: WorkflowStep,
    payload: WorkflowInput,
    instanceId: string,
  ): Promise<Map<string, unknown>> {
    const order = topoSortWorkflow(dag);
    if (order === null) {
      const message = 'vibe-workflow: workflow contains a cycle';
      await this.markFailed(instanceId, message);
      throw new NonRetryableError(message);
    }

    const nodesById = new Map<string, WorkflowV2Node>(
      dag.nodes.map((node) => [node.id, node]),
    );
    const outEdges = new Map<string, WorkflowV2Edge[]>();
    const indegree = new Map<string, number>();
    for (const node of dag.nodes) {
      outEdges.set(node.id, []);
      indegree.set(node.id, 0);
    }
    for (const edge of dag.edges) {
      if (!outEdges.has(edge.source)) continue;
      outEdges.get(edge.source)?.push(edge);
      indegree.set(edge.target, (indegree.get(edge.target) ?? 0) + 1);
    }

    const outputs = new Map<string, unknown>();
    const blocked = new Set<string>();

    for (const id of order) {
      const node = nodesById.get(id);
      if (!node) continue;
      if (blocked.has(id)) continue; // logged as skipped when it was pruned
      if ((indegree.get(id) ?? 0) > 0) continue; // a prerequisite was pruned

      if (node.type === 'sleep') {
        await this.runSleepNode(node, step, outputs, instanceId);
      } else if (node.type === 'condition') {
        await this.runConditionStep(node, step, payload, outputs, instanceId);
      } else {
        await this.runStep(node, step, payload, outputs, instanceId);
      }

      for (const edge of outEdges.get(id) ?? []) {
        if (edge.condition) {
          const passed = evaluateCondition(edge.condition, outputs, payload);
          if (!passed) {
            blocked.add(edge.target);
            this.logSkipped(instanceId, edge.target, nodesById.get(edge.target)?.type);
            continue;
          }
        }
        const remaining = (indegree.get(edge.target) ?? 0) - 1;
        indegree.set(edge.target, remaining);
      }
    }

    return outputs;
  }

  /** Run a regular node as `step.do(id, callback)` (with optional config). */
  private async runStep(
    node: WorkflowV2Node,
    step: WorkflowStep,
    payload: WorkflowInput,
    outputs: Map<string, unknown>,
    instanceId: string,
  ): Promise<void> {
    const logId = crypto.randomUUID();
    this.logStarted(instanceId, node, logId);
    const config = buildStepConfig(node);
    const doStep = step.do as unknown as StepDo;
    const runNode = () => this.executeNode(node, payload);
    try {
      const value = config
        ? await doStep(node.id, config, runNode)
        : await doStep(node.id, runNode);
      outputs.set(node.id, value);
      this.logSucceeded(logId, value);
    } catch (error) {
      this.logFailed(logId, error);
      throw error;
    }
  }

  /** Run a `sleep` node as `step.sleep(id, duration)` (no step.do involved). */
  private async runSleepNode(
    node: WorkflowV2Node,
    step: WorkflowStep,
    outputs: Map<string, unknown>,
    instanceId: string,
  ): Promise<void> {
    const logId = crypto.randomUUID();
    this.logStarted(instanceId, node, logId);
    const duration = parseWorkflowDuration(node.params?.duration);
    if (duration === null) {
      const message = `vibe-workflow: node "${node.id}" has no valid params.duration`;
      this.logFailed(logId, new Error(message));
      await this.markFailed(instanceId, message);
      throw new NonRetryableError(message);
    }
    await step.sleep(node.id, duration);
    const output = { slept: true, duration };
    outputs.set(node.id, output);
    this.logSucceeded(logId, output);
  }

  /** Run a `condition` node as a step returning the raw evaluated boolean. */
  private async runConditionStep(
    node: WorkflowV2Node,
    step: WorkflowStep,
    payload: WorkflowInput,
    outputs: Map<string, unknown>,
    instanceId: string,
  ): Promise<void> {
    const logId = crypto.randomUUID();
    this.logStarted(instanceId, node, logId);
    const doStep = step.do as unknown as StepDo;
    const params = node.params ?? {};
    const condition: WorkflowV2Condition = {
      op: params.condOp ?? 'eq',
      lhs: params.lhs ?? '',
      rhs: params.rhs ?? '',
    };
    try {
      const value = await doStep(node.id, () =>
        Promise.resolve(evaluateCondition(condition, outputs, payload)),
      );
      outputs.set(node.id, value);
      this.logSucceeded(logId, value);
    } catch (error) {
      this.logFailed(logId, error);
      throw error;
    }
  }

  /**
   * Execute a node's callback for `step.do`. Each case returns a
   * JSON-serializable value that becomes the step's recorded output (and can
   * be referenced by `${step.<id>}` in later edge conditions).
   */
  private async executeNode(
    node: WorkflowV2Node,
    payload: WorkflowInput,
  ): Promise<unknown> {
    const params = node.params ?? {};
    switch (node.type) {
      case 'trigger':
        return this.clone(payload);
      case 'http':
        return this.executeHttp(params);
      case 'db':
        return this.executeDb(params);
      case 'ai':
        return this.executeAi(params);
      case 'email':
        return this.executeEmail(params);
      default:
        // Unreachable after validation.
        throw new Error(`vibe-workflow: no executor for node.type "${node.type}"`);
    }
  }

  private async executeHttp(params: WorkflowV2Params): Promise<unknown> {
    const method = (params.method ?? 'GET').toUpperCase();
    const init: RequestInit = {
      method,
      headers: { ...(params.headers ?? {}) },
    };
    if (params.body !== undefined) {
      init.body = typeof params.body === 'string' ? params.body : JSON.stringify(params.body);
    }
    const response = await fetch(params.url ?? '', init);
    const text = await response.text();
    let body: unknown = text;
    try {
      body = JSON.parse(text);
    } catch {
      // keep the raw text body
    }
    return {
      status: response.status,
      ok: response.ok,
      url: response.url,
      body,
    };
  }

  private async executeDb(params: WorkflowV2Params): Promise<unknown> {
    const table = (params.table ?? '').trim();
    const op = (params.op ?? '').trim();
    if (!isSafeIdentifier(table)) {
      throw new Error(`vibe-workflow: invalid db table identifier "${table}"`);
    }
    const whereValues = [...(Object.entries(params.where ?? {}))];
    const dataValues = [...(Object.entries(params.data ?? {}))];
    for (const [key] of [...whereValues, ...dataValues]) {
      if (!isSafeIdentifier(key)) throw new Error(`vibe-workflow: invalid db column identifier "${key}"`);
    }

    if (op === 'select') {
      const clauses = whereValues.map(([key]) => `${key} = ?`);
      const sql = `SELECT * FROM ${table}${clauses.length > 0 ? ` WHERE ${clauses.join(' AND ')}` : ''}`;
      const statement = this.env.DB.prepare(sql);
      if (whereValues.length > 0) statement.bind(...whereValues.map(([, v]) => bindValue(v)));
      const result = await statement.all();
      return { rows: result.results ?? [] };
    }

    if (op === 'insert') {
      const columns = dataValues.map(([key]) => key);
      const placeholders = columns.map(() => '?').join(', ');
      const sql = `INSERT INTO ${table} (${columns.join(', ')}) VALUES (${placeholders})`;
      const result = await this.env.DB.prepare(sql)
        .bind(...dataValues.map(([, v]) => bindValue(v)))
        .run();
      return { changes: result.meta.changes, last_row_id: result.meta.last_row_id };
    }

    if (op === 'upsert') {
      const columns = dataValues.map(([key]) => key);
      const sets = columns.map((key) => `${key} = excluded.${key}`).join(', ');
      const sql = `INSERT INTO ${table} (${columns.join(', ')}) VALUES (${columns.map(() => '?').join(', ')})
        ON CONFLICT DO UPDATE SET ${sets}`;
      const result = await this.env.DB.prepare(sql)
        .bind(...dataValues.map(([, v]) => bindValue(v)))
        .run();
      return { changes: result.meta.changes, last_row_id: result.meta.last_row_id };
    }

    if (op === 'update' || op === 'delete') {
      if (whereValues.length === 0) {
        throw new Error(`vibe-workflow: params.where is required for db op "${op}" (refusing unbounded write)`);
      }
      const sets = op === 'update'
        ? `SET ${dataValues.map(([key]) => `${key} = ?`).join(', ')}`
        : '';
      const whereClause = whereValues.map(([key]) => `${key} = ?`).join(' AND ');
      const bound = [
        ...dataValues.map(([, v]) => bindValue(v)),
        ...whereValues.map(([, v]) => bindValue(v)),
      ];
      const sql = `UPDATE ${table} ${sets} WHERE ${whereClause}`;
      const result = await this.env.DB.prepare(sql).bind(...bound).run();
      return { changes: result.meta.changes };
    }

    // op is validated at DAG load time; this guard keeps TS exhaustive.
    throw new Error(`vibe-workflow: unsupported db op "${op}"`);
  }

  private async executeAi(params: WorkflowV2Params): Promise<unknown> {
    const model = (params.model ?? '').trim();
    const prompt = params.prompt ?? '';
    const system = (params.system ?? '').trim();
    const inputs: Record<string, unknown> = system
      ? {
          messages: [
            { role: 'system', content: system },
            { role: 'user', content: prompt },
          ],
        }
      : { prompt };
    const runModel = this.env.AI.run as (
      model: string,
      inputs: Record<string, unknown>,
    ) => Promise<Record<string, unknown>>;
    const result = await runModel(model, inputs);
    return { model, ...result };
  }

  private async executeEmail(params: WorkflowV2Params): Promise<unknown> {
    // Email delivery is out of scope for Phase-2 (no provider binding is
    // wired); record the intent deterministically so the DAG contract holds.
    return {
      queued: true,
      to: params.to ?? '',
      subject: params.subject ?? '',
      note: 'email dispatch not configured; message recorded',
    };
  }

  private clone<T>(value: T): T {
    try {
      return structuredClone(value);
    } catch {
      return JSON.parse(JSON.stringify(value)) as T;
    }
  }

  // --------------------------------------------------------------------
  // D1 persistence
  // --------------------------------------------------------------------

  private async loadInstance(instanceId: string): Promise<WorkflowInstanceRow | null> {
    const row = await this.env.DB.prepare(
      'SELECT workflow_id, status, input FROM workflow_instances WHERE id = ?',
    )
      .bind(instanceId)
      .first<WorkflowInstanceRow>();
    return row ?? null;
  }

  private async loadDag(workflowId: string): Promise<WorkflowDagRow | null> {
    const row = await this.env.DB.prepare(
      'SELECT schema_version, dag_json FROM workflow_dags WHERE workflow_id = ?',
    )
      .bind(workflowId)
      .first<WorkflowDagRow>();
    return row ?? null;
  }

  private async markRunning(instanceId: string, payload: WorkflowInput): Promise<void> {
    await this.env.DB.prepare(
      'UPDATE workflow_instances SET status = ?, started_at = ?, input = COALESCE(input, ?), updated_at = CURRENT_TIMESTAMP WHERE id = ?',
    )
      .bind('running', Date.now(), JSON.stringify(payload ?? {}), instanceId)
      .run();
  }

  private async markSucceeded(instanceId: string, result: WorkflowRunResult): Promise<void> {
    await this.env.DB.prepare(
      'UPDATE workflow_instances SET status = ?, output = ?, completed_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?',
    )
      .bind('succeeded', JSON.stringify(result), Date.now(), instanceId)
      .run();
  }

  private async markFailed(instanceId: string, error: string): Promise<void> {
    await this.env.DB.prepare(
      'UPDATE workflow_instances SET status = ?, error = ?, completed_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?',
    )
      .bind('failed', error, Date.now(), instanceId)
      .run();
  }

  private logStarted(instanceId: string, node: WorkflowV2Node, logId: string): void {
    this.fire(
      this.env.DB.prepare(
        'INSERT INTO workflow_step_logs (id, instance_id, step_name, node_type, status, attempt, started_at) VALUES (?, ?, ?, ?, ?, ?, ?)',
      )
        .bind(logId, instanceId, node.id, node.type, 'running', 0, Date.now())
        .run(),
    );
  }

  private logSucceeded(logId: string, output: unknown): void {
    this.fire(
      this.env.DB.prepare(
        'UPDATE workflow_step_logs SET status = ?, output = ?, completed_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?',
      )
        .bind('succeeded', JSON.stringify(output ?? null), Date.now(), logId)
        .run(),
    );
  }

  private logFailed(logId: string, error: unknown): void {
    this.fire(
      this.env.DB.prepare(
        'UPDATE workflow_step_logs SET status = ?, error = ?, completed_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?',
      )
        .bind('failed', error instanceof Error ? error.message : String(error), Date.now(), logId)
        .run(),
    );
  }

  private logSkipped(instanceId: string, stepName: string, nodeType: string | undefined): void {
    this.fire(
      this.env.DB.prepare(
        'INSERT INTO workflow_step_logs (id, instance_id, step_name, node_type, status, attempt, completed_at) VALUES (?, ?, ?, ?, ?, ?, ?)',
      )
        .bind(crypto.randomUUID(), instanceId, stepName, nodeType ?? null, 'skipped', 0, Date.now())
        .run(),
    );
  }

  /** Fire-and-forget background write; failures are logged, never thrown. */
  private fire(promise: Promise<unknown>): void {
    this.ctx.waitUntil(
      promise.catch((error) => console.error('vibe-workflow: background D1 write failed', error)),
    );
  }
}