import type { ImageAttachment } from '@/api-types';

/**
 * Intent routing for outbound chat messages.
 *
 * The app distinguishes two intents for a user prompt:
 *   - `'build'` → `generate_all`   : regenerate/derive the app code (may
 *     create/modify files and trigger the preview/Sandpack).
 *   - `'chat'`  → `user_suggestion`: stream a conversational markdown reply
 *     without touching files, `shouldBeGenerating`, or the preview.
 *
 * Kept as a small pure module so the routing decision is unit-testable without
 * rendering React or opening a WebSocket.
 */
export type ChatMode = 'build' | 'chat';

export interface OutboundChatMessage {
	type: 'generate_all' | 'user_suggestion';
	data: Record<string, unknown>;
}

/**
 * Decide which WebSocket message to send for a user prompt given the current
 * mode. Returns a plain payload object consumed by `sendWebSocketMessage`.
 */
export function buildOutboundChatMessage(
	mode: ChatMode,
	message: string,
	images: ImageAttachment[] = [],
): OutboundChatMessage {
	if (mode === 'chat') {
		return {
			type: 'user_suggestion',
			data: {
				message,
				images: images.length > 0 ? images : undefined,
			},
		};
	}
	return {
		type: 'generate_all',
		data: { message },
	};
}

/** Human-friendly placeholder text for a given mode. */
export function chatPlaceholder(mode: ChatMode, isDebugging: boolean): string {
	if (isDebugging) {
		return 'Deep debugging in progress... Please abort to continue';
	}
	return mode === 'chat'
		? 'Ask a question about the project...'
		: 'Describe what you want to build...';
}
