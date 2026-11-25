/**
 * ARCH-WASM-001 — WASM tool-sandbox spike (Model C), Step 1.
 *
 * What this is: the smallest possible proof that the Workers runtime can
 * instantiate a WebAssembly module and call an exported function — the
 * mechanical precondition for "Native Eino orchestrator + WASM tool
 * components" (docs/architecture/eino-wasm-compatibility.md, Model C).
 *
 * What this is NOT:
 * - Not a tool runtime: no WIT, no capability interface, no host imports.
 * - Not Eino-in-WASM (Model B): nothing here compiles Go or Eino.
 * - Not production code: nothing imports this module. It is imported only
 *   by its own test file, which the root suite excludes or runs in CI.
 * - Not a performance claim: no binary-size / cold-start numbers here.
 *
 * Binary provenance: `wasm/add.wasm` is 41 bytes hand-assembled from the
 * WASM 1.0 spec (magic + version + type/func/export/code sections),
 * exporting `add(i32, i32) -> i32`. No toolchain needed, so the PoC runs
 * on any host without wat2wasm/wasm-tools. The canonical WAT is kept in
 * `wasm/add.wat` for readability; the bytes are authoritative.
 */

const ADD_WASM_BYTES = new Uint8Array([
  0x00, 0x61, 0x73, 0x6d, // magic "\0asm"
  0x01, 0x00, 0x00, 0x00, // version 1
  // type section: one type (i32, i32) -> i32
  0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f,
  // function section: one function, type 0
  0x03, 0x02, 0x01, 0x00,
  // export section: export func 0 as "add"
  0x07, 0x07, 0x01, 0x03, 0x61, 0x64, 0x64, 0x00, 0x00,
  // code section: one body: local.get 0, local.get 1, i32.add
  0x0a, 0x09, 0x01, 0x07, 0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b,
]);

/** What a WASM tool call resolves to: name, args, and the i32 result. */
export interface WasmAddResult {
  tool: 'add';
  args: readonly [number, number];
  result: number;
}

/**
 * Instantiate the embedded module and call `add(a, b)`.
 * Pure computation only — the module imports nothing, so there is no
 * host surface to review (no WASI, no network, no capability).
 */
export async function runWasmAdd(a: number, b: number): Promise<WasmAddResult> {
  const instantiated = (await WebAssembly.instantiate(ADD_WASM_BYTES)) as unknown as {
    instance: { exports: { add?: unknown } };
  };
  const add = instantiated.instance.exports.add;
  if (typeof add !== 'function') {
    throw new Error('wasm-tool-sandbox: embedded module does not export add()');
  }
  const result = (add as (x: number, y: number) => number)(a | 0, b | 0);
  return { tool: 'add', args: [a | 0, b | 0], result };
}
