import { useEffect, useState } from 'react';

/**
 * Returns `value` delayed by `delay` ms. While new values keep arriving the
 * timer restarts, so only the last value within a quiet window is emitted.
 *
 * Used to throttle expensive derived renders — e.g. rebuilding the static
 * preview srcdoc (O(total file size)) while `file_chunk_generated` events
 * stream in over the WebSocket.
 */
export function useDebouncedValue<T>(value: T, delay: number): T {
	const [debounced, setDebounced] = useState(value);

	useEffect(() => {
		const timer = setTimeout(() => setDebounced(value), Math.max(delay, 0));
		return () => clearTimeout(timer);
	}, [value, delay]);

	return debounced;
}