/**
 * useControlPlane — React hook that wires the frontend to the Go control
 * plane via `controlPlaneClient`. It manages the room WebSocket connection,
 * generation streaming, VFS state, and Cloudflare Pages deployment status.
 *
 * This is additive: it does not replace the existing `useChat` flow, but
 * provides a decoupled surface for the control-plane backend. All state is
 * local to the hook, so adopting it never breaks existing components.
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import {
	connectToAgent,
	createAgentSession,
	deployProject,
	openControlPlaneSocket,
} from '@/services/controlPlaneClient';

/** A single file in the control-plane VFS. */
export interface ControlPlaneFile {
	filePath: string;
	fileContents: string;
}

/** Options for the hook. */
export interface UseControlPlaneOptions {
	/** Existing agent/room id to connect to. */
	agentId?: string;
	/** Auto-connect on mount when agentId is provided. */
	autoConnect?: boolean;
}

/** Inbound WebSocket message types the hook understands. */
type ControlPlaneMessage = {
	type: string;
	filePath?: string;
	chunk?: string;
	file?: { filePath: string; fileContents: string };
	previewURL?: string;
	error?: string;
	progress?: number;
};

function upsertChunk(
	files: ControlPlaneFile[],
	filePath: string,
	chunk: string,
): ControlPlaneFile[] {
	const idx = files.findIndex((f) => f.filePath === filePath);
	if (idx === -1) {
		return [...files, { filePath, fileContents: chunk }];
	}
	const next = [...files];
	next[idx] = { ...next[idx], fileContents: next[idx].fileContents + chunk };
	return next;
}

function upsertFile(
	files: ControlPlaneFile[],
	filePath: string,
	contents: string,
): ControlPlaneFile[] {
	const idx = files.findIndex((f) => f.filePath === filePath);
	if (idx === -1) {
		return [...files, { filePath, fileContents: contents }];
	}
	const next = [...files];
	next[idx] = { ...next[idx], fileContents: contents };
	return next;
}

export function useControlPlane({
	agentId,
	autoConnect = true,
}: UseControlPlaneOptions = {}) {
	const [socket, setSocket] = useState<WebSocket | null>(null);
	const [connected, setConnected] = useState(false);
	const [files, setFiles] = useState<ControlPlaneFile[]>([]);
	const [isGenerating, setIsGenerating] = useState(false);
	const [isDeploying, setIsDeploying] = useState(false);
	const [deploymentUrl, setDeploymentUrl] = useState<string>();
	const [deploymentError, setDeploymentError] = useState<string>();
	const [deployProgress, setDeployProgress] = useState(0);
	const [error, setError] = useState<string>();

	const socketRef = useRef<WebSocket | null>(null);

	const handleMessage = useCallback((raw: string) => {
		let msg: ControlPlaneMessage;
		try {
			msg = JSON.parse(raw) as ControlPlaneMessage;
		} catch {
			return; // ignore non-JSON frames
		}

		switch (msg.type) {
			case 'file_chunk_generated':
				if (msg.filePath && msg.chunk !== undefined) {
					setFiles((prev) => upsertChunk(prev, msg.filePath!, msg.chunk!));
				}
				break;
			case 'file_generated':
				if (msg.file) {
					setFiles((prev) =>
						upsertFile(prev, msg.file!.filePath, msg.file!.fileContents),
					);
				}
				break;
			case 'generation_started':
				setIsGenerating(true);
				break;
			case 'generation_complete':
				setIsGenerating(false);
				break;
			case 'deployment_started':
				setIsDeploying(true);
				setDeploymentError(undefined);
				setDeployProgress(0);
				break;
			case 'deploy_progress':
				setDeployProgress(msg.progress ?? 0);
				break;
			case 'deployment_completed':
				setIsDeploying(false);
				setDeploymentUrl(msg.previewURL);
				setDeployProgress(100);
				break;
			case 'deployment_failed':
				setIsDeploying(false);
				setDeploymentError(msg.error || 'Deployment failed');
				break;
			case 'error':
				setError(msg.error || 'Control plane error');
				break;
			default:
				// Unknown types are ignored (backwards compatible).
				break;
		}
	}, []);

	const connect = useCallback(
		(id: string) => {
			socketRef.current?.close();
			const ws = openControlPlaneSocket(id);
			socketRef.current = ws;
			setSocket(ws);
			setConnected(false);

			ws.onopen = () => setConnected(true);
			ws.onclose = () => setConnected(false);
			ws.onerror = () => setError('Control plane WebSocket error');
			ws.onmessage = (event) => handleMessage(String(event.data));
		},
		[handleMessage],
	);

	/** Create a new session and connect to its room. */
	const createSession = useCallback(
		async (query: string, projectType = 'app') => {
			const res = await createAgentSession(query, projectType);
			if (!res.success || !res.data) {
				setError(res.error || 'Failed to create session');
				return null;
			}
			connect(res.data.agentId);
			return res.data;
		},
		[connect],
	);

	/** Connect to an existing session. */
	const connectExisting = useCallback(
		async (id: string) => {
			const res = await connectToAgent(id);
			if (!res.success || !res.data) {
				setError(res.error || 'Failed to connect to session');
				return false;
			}
			connect(id);
			return true;
		},
		[connect],
	);

	/** Send a generation prompt over the room WebSocket. */
	const sendPrompt = useCallback((prompt: string): boolean => {
		const ws = socketRef.current;
		if (!ws || ws.readyState !== WebSocket.OPEN) {
			setError('Control plane WebSocket not connected');
			return false;
		}
		ws.send(JSON.stringify({ type: 'generate_all', message: prompt }));
		return true;
	}, []);

	/** Trigger a Cloudflare Pages deployment of the project VFS. */
	const deploy = useCallback(async (projectId: string): Promise<boolean> => {
		const res = await deployProject(projectId);
		if (!res.success) {
			setDeploymentError(res.error || 'Deploy failed');
			return false;
		}
		return true;
	}, []);

	useEffect(() => {
		if (autoConnect && agentId) {
			connect(agentId);
		}
		return () => socketRef.current?.close();
	}, [autoConnect, agentId, connect]);

	return {
		socket,
		connected,
		files,
		isGenerating,
		isDeploying,
		deploymentUrl,
		deploymentError,
		deployProgress,
		error,
		connect,
		createSession,
		connectExisting,
		sendPrompt,
		deploy,
	};
}