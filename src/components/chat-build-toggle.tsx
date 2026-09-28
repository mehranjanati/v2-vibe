import { ChatCircleDots, Code } from '@phosphor-icons/react';
import type { ChatMode } from '@/routes/chat/utils/chat-routing';

/** Shared Build/Chat segmented toggle used on the home page and in the chat
 * input. Build 🔨 issues `generate_all` (produce/modify app code); Chat 💬
 * issues `user_suggestion` (conversational reply, no files). */
export function ChatBuildToggle({
	value,
	onChange,
	disabled = false,
}: {
	value: ChatMode;
	onChange: (mode: ChatMode) => void;
	disabled?: boolean;
}) {
	return (
		<div
			role="group"
			aria-label="Message mode"
			className="flex items-center bg-slate-100 dark:bg-slate-800/50 rounded-lg p-0.5 border border-slate-200 dark:border-slate-700 shrink-0"
		>
			<button
				type="button"
				disabled={disabled}
				onClick={() => onChange('build')}
				title="Build mode: generate or modify the app code"
				aria-pressed={value === 'build'}
				className={`flex items-center gap-1 px-2.5 py-1.5 text-xs font-medium rounded-md transition-all duration-200 ${
					value === 'build'
						? 'bg-white dark:bg-slate-700 shadow-sm text-emerald-700 dark:text-emerald-400 border border-slate-200 dark:border-slate-600'
						: 'text-slate-600 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200'
				} ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}`}
			>
				<Code className="size-3" />
				Build
			</button>
			<button
				type="button"
				disabled={disabled}
				onClick={() => onChange('chat')}
				title="Chat mode: ask a question, get a conversational reply (no files generated)"
				aria-pressed={value === 'chat'}
				className={`flex items-center gap-1 px-2.5 py-1.5 text-xs font-medium rounded-md transition-all duration-200 ${
					value === 'chat'
						? 'bg-white dark:bg-slate-700 shadow-sm text-violet-700 dark:text-violet-400 border border-slate-200 dark:border-slate-600'
						: 'text-slate-600 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200'
				} ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}`}
			>
				<ChatCircleDots className="size-3" />
				Chat
			</button>
		</div>
	);
}