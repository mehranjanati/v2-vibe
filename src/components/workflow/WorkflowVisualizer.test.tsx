import { describe, expect, it } from 'vitest';
import { parseWorkflow, WORKFLOW_FILE_PATH } from './WorkflowVisualizer';

const VALID = JSON.stringify({
	nodes: [
		{ id: 'n1', type: 'trigger', label: 'User submits', params: {}, position: { x: 0, y: 0 } },
		{ id: 'n2', type: 'db', label: 'Save booking', params: { table: 'bookings' }, position: { x: 300, y: 0 } },
	],
	edges: [{ id: 'e1', source: 'n1', target: 'n2', label: 'on submit' }],
});

describe('parseWorkflow', () => {
	it('returns null for missing/invalid input', () => {
		expect(parseWorkflow(undefined)).toBeNull();
		expect(parseWorkflow('')).toBeNull();
		expect(parseWorkflow('not json {')).toBeNull();
		expect(parseWorkflow('[]')).toBeNull();
		expect(parseWorkflow(JSON.stringify({ nodes: [] }))).toBeNull();
	});

	describe('parses a valid DAG', () => {
		const wf = parseWorkflow(VALID)!;
		it('parses nodes', () => {
			expect(wf).not.toBeNull();
			expect(wf.nodes).toHaveLength(2);
			expect(wf.nodes[0].id).toBe('n1');
			expect(wf.nodes[0].type).toBe('trigger');
			expect(wf.nodes[0].data.label).toBe('User submits');
		});
		it('parses edges and drops dangling ones', () => {
			expect(wf.edges).toHaveLength(1);
			expect(wf.edges[0].source).toBe('n1');
		});
	});

	it('falls back unknown node types to function', () => {
		const wf = parseWorkflow(
			JSON.stringify({ nodes: [{ id: 'a', type: 'weird', label: 'X' }], edges: [] }),
		)!;
		expect(wf.nodes[0].type).toBe('function');
	});

	it('repairs the "position: { x" string-key malformation emitted by small models', () => {
		const broken = `{
			"nodes": [
				{ "id": "n1", "type": "trigger", "label": "User opens website", "params": {}, "position: { x": 0, "y": 0 } },
				{ "id": "n2", "type": "function", "label": "Render grid", "params": {}, "position: { x": 300, "y": 0 } }
			],
			"edges": [{ "id": "e1", "source": "n1", "target": "n2" }]
		}`;
		const wf = parseWorkflow(broken)!;
		expect(wf).not.toBeNull();
		expect(wf.nodes).toHaveLength(2);
		expect(wf.nodes[0].position).toEqual({ x: 0, y: 0 });
		expect(wf.nodes[1].position).toEqual({ x: 300, y: 0 });
	});

	it('auto-positions nodes with missing positions', () => {
		const wf = parseWorkflow(
			JSON.stringify({
				nodes: [
					{ id: 'a', type: 'trigger', label: 'A' },
					{ id: 'b', type: 'db', label: 'B' },
				],
				edges: [],
			}),
		)!;
		expect(wf.nodes[0].position).toEqual({ x: 0, y: 0 });
		expect(wf.nodes[1].position).toEqual({ x: 280, y: 0 });
	});

	it('uses expected VFS path', () => {
		expect(WORKFLOW_FILE_PATH).toBe('workflow.json');
	});
});
