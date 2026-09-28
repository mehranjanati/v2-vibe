import { useMemo } from 'react';
import {
	ReactFlow,
	Background,
	Controls,
	MiniMap,
	type Edge,
	type Node,
	type NodeProps,
	type NodeTypes,
	Handle,
	Position,
	MarkerType,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';

/**
 * WorkflowVisualizer — parses `workflow.json` from the Virtual File System
 * (accumulated over the WebSocket stream by the Go generation engine) and
 * renders the app's backend logic as an interactive DAG via ReactFlow.
 *
 * Expected schema (emitted by the generation system prompt):
 * {
 *   "nodes": [{ "id", "type": "trigger|function|http|db|condition",
 *               "label", "params": {}, "position": { "x", "y" } }],
 *   "edges": [{ "id", "source", "target", "label"? }]
 * }
 *
 * Invalid or missing workflow.json renders a friendly empty state — never
 * a crash.
 */

export const WORKFLOW_FILE_PATH = 'workflow.json';

interface RawWorkflowNode {
	id?: unknown;
	type?: unknown;
	label?: unknown;
	params?: unknown;
	position?: unknown;
}

interface RawWorkflow {
	nodes?: unknown;
	edges?: unknown;
}

export interface WorkflowNodeData extends Record<string, unknown> {
	label: string;
	nodeType: string;
	params: Record<string, unknown>;
}

export interface ParsedWorkflow {
	nodes: Node<WorkflowNodeData>[];
	edges: Edge[];
}

/** Node-type accent colors (dark-theme friendly). */
const NODE_COLORS: Record<string, { bg: string; border: string; icon: string }> = {
	trigger: { bg: '#1d2b1a', border: '#4ade80', icon: '⚡' },
	function: { bg: '#1a2333', border: '#60a5fa', icon: 'ƒ' },
	http: { bg: '#2b1f2e', border: '#c084fc', icon: '↔' },
	db: { bg: '#332a17', border: '#facc15', icon: '🗄' },
	condition: { bg: '#331a1a', border: '#f87171', icon: '⎇' },
};

function isRecord(v: unknown): v is Record<string, unknown> {
	return typeof v === 'object' && v !== null && !Array.isArray(v);
}

/**
 * Repair the most common malformations small models emit in workflow.json:
 *  1. `"position: { x": 0, "y": 0 }`  →  `"position": { "x": 0, "y": 0 }`
 *     (the whole position object collapsed into a single string key)
 *  2. `": ,` / `": }` empty values     →  `": null,` / `": null}`
 * Returns the repaired string; the caller retries JSON.parse on it.
 */
function repairWorkflowJson(raw: string): string {
	let out = raw.replace(
		/"position:\s*\{\s*x"\s*:\s*([^,}]*)\s*,\s*"y"\s*:\s*([^,}]*)\s*\}/g,
		(_m, x: string, y: string) =>
			`"position": {"x": ${x.trim() || '0'}, "y": ${y.trim() || '0'}}`,
	);
	// Empty values after a colon → null.
	out = out.replace(/:\s*,/g, ': null,').replace(/:\s*\}/g, ': null}');
	return out;
}

/** Grid fallback for nodes with missing/invalid positions. */
function autoPosition(index: number): { x: number; y: number } {
	const col = index % 4;
	const row = Math.floor(index / 4);
	return { x: col * 280, y: row * 180 };
}

/** Defensive parse of the raw workflow.json string. Never throws. */
export function parseWorkflow(raw: string | undefined): ParsedWorkflow | null {
	if (!raw) return null;
	let data: RawWorkflow;
	try {
		data = JSON.parse(raw) as RawWorkflow;
	} catch {
		// Second chance: repair common LLM malformations, then re-parse.
		try {
			data = JSON.parse(repairWorkflowJson(raw)) as RawWorkflow;
		} catch {
			return null;
		}
	}
	if (!isRecord(data) || !Array.isArray(data.nodes)) return null;

	const rawNodes = data.nodes as RawWorkflowNode[];
	if (rawNodes.length === 0) return null;

	const validIds = new Set<string>();
	for (const n of rawNodes) {
		if (typeof n?.id === 'string') validIds.add(n.id);
	}
	if (validIds.size === 0) return null;

	const nodes: Node<WorkflowNodeData>[] = [];
	for (const n of rawNodes) {
		if (typeof n?.id !== 'string' || !validIds.has(n.id)) continue;
		const nodeType =
			typeof n.type === 'string' && NODE_COLORS[n.type] ? n.type : 'function';
		const pos = isRecord(n.position)
			? {
					x: typeof n.position.x === 'number' ? n.position.x : -1,
					y: typeof n.position.y === 'number' ? n.position.y : -1,
				}
			: { x: -1, y: -1 };
		nodes.push({
			id: n.id,
			type: nodeType,
			position:
				pos.x >= 0 && pos.y >= 0 ? pos : autoPosition(nodes.length),
			data: {
				label: typeof n.label === 'string' && n.label ? n.label : n.id,
				nodeType,
				params: isRecord(n.params) ? n.params : {},
			},
		});
	}

	const edges: Edge[] = [];
	if (Array.isArray(data.edges)) {
		for (const e of data.edges) {
			if (!isRecord(e)) continue;
			if (
				typeof e.id !== 'string' ||
				typeof e.source !== 'string' ||
				typeof e.target !== 'string' ||
				!validIds.has(e.source) ||
				!validIds.has(e.target)
			) {
				continue;
			}
			edges.push({
				id: e.id,
				source: e.source,
				target: e.target,
				label: typeof e.label === 'string' ? e.label : undefined,
				animated: true,
				style: { stroke: '#71717a' },
				markerEnd: { type: MarkerType.ArrowClosed, color: '#71717a' },
			});
		}
	}

	return { nodes, edges };
}

/** Custom DAG node: colored header by type + params summary. */
function WorkflowNode({ data }: NodeProps<Node<WorkflowNodeData>>) {
	const colors = NODE_COLORS[data.nodeType] ?? NODE_COLORS.function;
	const paramEntries = Object.entries(data.params).slice(0, 4);
	return (
		<div
			className="rounded-lg border shadow-md min-w-44 max-w-60 overflow-hidden"
			style={{ background: colors.bg, borderColor: colors.border }}
		>
			<Handle type="target" position={Position.Left} style={{ background: colors.border }} />
			<div
				className="px-3 py-1.5 text-xs font-semibold flex items-center gap-2"
				style={{ color: colors.border, borderBottom: `1px solid ${colors.border}44` }}
			>
				<span aria-hidden>{colors.icon}</span>
				<span className="uppercase tracking-wide">{data.nodeType}</span>
			</div>
			<div className="px-3 py-2 text-sm font-medium text-zinc-100">{data.label}</div>
			{paramEntries.length > 0 && (
				<div className="px-3 pb-2 space-y-0.5">
					{paramEntries.map(([k, v]) => (
						<div key={k} className="text-[10px] font-mono text-zinc-400 truncate">
							{k}: {typeof v === 'string' ? v : JSON.stringify(v)}
						</div>
					))}
				</div>
			)}
			<Handle type="source" position={Position.Right} style={{ background: colors.border }} />
		</div>
	);
}

const nodeTypes: NodeTypes = {
	trigger: WorkflowNode,
	function: WorkflowNode,
	http: WorkflowNode,
	db: WorkflowNode,
	condition: WorkflowNode,
};

export function WorkflowVisualizer({
	files,
	className,
}: {
	/** Client-side VFS map (path -> contents) accumulated from the WebSocket. */
	files: Record<string, string>;
	className?: string;
}) {
	const workflow = useMemo(() => parseWorkflow(files[WORKFLOW_FILE_PATH]), [files]);

	if (!workflow) {
		return (
			<div
				className={
					className ??
					'h-full w-full flex items-center justify-center bg-bg-1 text-text-50/60 text-sm'
				}
			>
				<div className="text-center space-y-1">
					<p className="font-medium text-text-primary">No workflow yet</p>
					<p className="text-xs">
						The generated app does not include a valid{' '}
						<code className="font-mono text-text-50">{WORKFLOW_FILE_PATH}</code> DAG.
					</p>
				</div>
			</div>
		);
	}

	return (
		<div className={className ?? 'h-full w-full'}>
			<ReactFlow
				nodes={workflow.nodes}
				edges={workflow.edges}
				nodeTypes={nodeTypes}
				fitView
				minZoom={0.2}
				proOptions={{ hideAttribution: true }}
			>
				<Background color="#27272a" gap={16} />
				<Controls showInteractive={false} />
				<MiniMap pannable zoomable maskColor="#18181b99" />
			</ReactFlow>
		</div>
	);
}

export default WorkflowVisualizer;
