/**
 * ARCH-WASM-001 — WASM tool-sandbox spike test (WASM-06 shape, minimal).
 *
 * Maps to `docs/architecture/wasm-poc-spec.md` WASM-06 (tool calling) at the
 * smallest possible scope: one pure function call through WebAssembly,
 * no host imports, no Gate, no network. Factors recorded here:
 * reproducible command (`bunx vitest run worker/wasm/tool-sandbox.test.ts`),
 * no toolchain dependency (bytes are hand-assembled, `add.wat` is the
 * readable source), verdict PASS only when the runtime actually instantiates
 * and computes.
 *
 * Out of scope (explicit): WASI, WIT, threading/concurrency, streaming,
 * cancellation, perf numbers, Model B (Eino-in-WASM).
 */

import { describe, it, expect } from 'vitest';
import { runWasmAdd } from './tool-sandbox';

describe('wasm tool-sandbox spike — instantiate + call add()', () => {
  it('computes 2 + 3 = 5 inside WebAssembly', async () => {
    await expect(runWasmAdd(2, 3)).resolves.toEqual({ tool: 'add', args: [2, 3], result: 5 });
  });

  it('wraps i32 overflow exactly like the WASM spec', async () => {
    // 2^31 wraps to -2^31 under i32.add — proves we ran real WASM
    // semantics, not a JS addition.
    const { result } = await runWasmAdd(2147483647, 1);
    expect(result).toBe(-2147483648);
  });

  it('imports nothing: the module has zero host surface to review', async () => {
    // Re-instantiate from the same bytes and assert no import object is
    // needed — WebAssembly.instantiate(bytes) without a second argument
    // throws when the module declares any import.
    const mod = await WebAssembly.compile(
      new Uint8Array([
        0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x07, 0x01, 0x60,
        0x02, 0x7f, 0x7f, 0x01, 0x7f, 0x03, 0x02, 0x01, 0x00, 0x07, 0x07, 0x01,
        0x03, 0x61, 0x64, 0x64, 0x00, 0x00, 0x0a, 0x09, 0x01, 0x07, 0x00, 0x20,
        0x00, 0x20, 0x01, 0x6a, 0x0b,
      ]),
    );
    expect(WebAssembly.Module.imports(mod)).toEqual([]);
  });
});
