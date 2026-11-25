;; ARCH-WASM-001 — canonical WAT for worker/wasm/tool-sandbox.ts
;; The embedded bytes in tool-sandbox.ts are authoritative; this file is the
;; human-readable source. Module: (func $add (param i32 i32) (result i32)
;; local.get 0; local.get 1; i32.add), exported as "add". No imports.
(module
  (func $add (param i32 i32) (result i32)
    local.get 0
    local.get 1
    i32.add)
  (export "add" (func $add)))
