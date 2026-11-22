# System Architecture — Ecosystem Target (Canonical)

> **Status:** `adopted` 2026-11-21. Live: `architecture-diagrams.md`, `llm.md`.
> History: `04-system-architecture.md` (preserved).

```mermaid
flowchart TB
    subgraph Products
        AST[Vibe Assistant]
        RES[Vibe Research]
        COD[Vibe Coder]
        COM[Vibe Commerce]
        STU[Vibe Studio]
        CLD[Vibe Cloud]
        NET[Vibe Network]
    end
    subgraph Shared
        IDN[Identity + Policy]
        IR[Canonical System IR]
        MOD[Model Gateway + Router]
        CAP[Tool / Skill Registry]
        MEM[Memory + Knowledge]
        RUN[Vibe Runtime]
        EVT[Events + Workflows]
        MET[Metering + Audit]
    end
    subgraph Providers
        CF[Cloudflare provider]
        LOCAL[Self-hosted / local]
        WASM[WASM sandbox PoC]
        CTR[Containers]
        GPU[GPU inference]
    end
    AST --> IDN
    COD --> IR
    COM --> IR
    STU --> IR
    IR --> RUN
    CAP --> RUN
    MOD --> RUN
    MEM --> RUN
    IDN --> RUN
    RUN --> EVT
    RUN --> MET
    RUN --> CF
    RUN --> LOCAL
    RUN --> WASM
    RUN --> CTR
    GPU --> MOD
    CLD --> RUN
    NET --> RUN
```

Outcome flows into validated System IR, then policy check, capability
resolution, durable execution with approvals and idempotency, tenant-scoped
state plus lineage plus audit, traces and evaluation, reported outcome.
Control never executes untrusted code; execution never owns identity truth.
Cloudflare is one provider, not the platform identity.
