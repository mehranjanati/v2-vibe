# Capability ↔ Tool Mapping (Eino Tool · Manifest · WIT · Gate)

> **Status:** `accepted` (2026-11-24, documentation only) — names the
> vocabulary the D18 loaders already obey; adds no loader.
> **Related:** D18 (`architecture/adr-unified-integrations.md`), D21
> (`architecture/adr-wasm-component-strategy.md`).

## Canonical flow (exactly this order)

```text
Eino Tool interface
  → Vibe Capability adapter
    → Tool Manifest (identity/version/schema/permissions/provenance/policy)
      → Vibe Gate authorization (`mcp.Gate`: registry → policy → grant → execution → audit)
        → WIT component interface (ABI only)
          → wasmCloud host capability (sandbox + granted host functions)
            → external service / isolated compute
              → audit + result propagation
```

## Separation of concerns

| Layer | Owns | Never owns |
|---|---|---|
| Tool Manifest | capability identity, version, I/O schema, permissions, provenance, policy | execution, authorization decisions |
| WIT interface | Component Model ABI, typed interaction | Vibe permissions, tenant decisions |
| `mcp.Gate` | sole authorization + policy enforcement | registries of convenience, execution |
| wasmCloud host | sandboxing, explicit host capabilities | grants, tenant data decisions |
| Eino Tool | orchestration-facing tool shape | governance, durability |

## Rules

1. A WIT import never grants a Vibe permission.
2. A component never self-authorizes tenant data access.
3. Every capability is versioned and resolved through composition
   (`composition-lock.md`); Manifest ↔ WIT refs are traceably associated.
4. The same capability identity + audit context survives native and WASM
   execution; durable state and secrets never enter the component.
