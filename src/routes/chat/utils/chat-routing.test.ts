import { describe, it, expect } from 'vitest';
import {
	buildOutboundChatMessage,
	chatPlaceholder,
	type ChatMode,
} from './chat-routing';
import type { ImageAttachment } from '@/api-types';

const img = (id: string): ImageAttachment => ({
	id,
	filename: `${id}.png`,
	mimeType: 'image/png',
	base64Data: 'aGVsbG8=',
	size: 5,
});

describe('buildOutboundChatMessage (intent routing)', () => {
	it('routes build mode to generate_all with the prompt and no images', () => {
		const msg = buildOutboundChatMessage('build', 'build me a todo app', [img('a')]);
		expect(msg.type).toBe('generate_all');
		expect(msg.data.message).toBe('build me a todo app');
		expect(msg.data.images).toBeUndefined();
	});

	it('routes chat mode to user_suggestion with the prompt and images omitted when empty', () => {
		const msg = buildOutboundChatMessage('chat', 'what does this code do?', []);
		expect(msg.type).toBe('user_suggestion');
		expect(msg.data.message).toBe('what does this code do?');
		expect(msg.data.images).toBeUndefined();
	});

	it('includes images on chat mode when attachments are present', () => {
		const images = [img('a'), img('b')];
		const msg = buildOutboundChatMessage('chat', 'fix the styling', images);
		expect(msg.type).toBe('user_suggestion');
		expect(msg.data.images).toEqual(images);
	});

	it('never leaks images into build mode even when provided', () => {
		const msg = buildOutboundChatMessage('build', 'new feature', [img('x')]);
		expect(msg.type).toBe('generate_all');
		expect(msg.data.images).toBeUndefined();
	});

	// Enumerate every mode so a new ChatMode value forces an explicit decision.
	it('routes every ChatMode to a supported outbound type', () => {
		const modes: ChatMode[] = ['build', 'chat'];
		for (const mode of modes) {
			const msg = buildOutboundChatMessage(mode, 'prompt');
			expect(['generate_all', 'user_suggestion']).toContain(msg.type);
		}
	});
});

describe('chatPlaceholder', () => {
	it('returns the conversational placeholder in chat mode', () => {
		expect(chatPlaceholder('chat', false)).toContain('question');
	});

	it('returns the build placeholder in build mode', () => {
		expect(chatPlaceholder('build', false)).toContain('build');
	});

	it('shows the debugging placeholder regardless of mode', () => {
		expect(chatPlaceholder('chat', true)).toContain('abort');
		expect(chatPlaceholder('build', true)).toContain('abort');
	});
});
