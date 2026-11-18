# Trade Memory Security — Provenance, Evidence Guard, Retention

> **Scope:** the three defenses that make a trader agent's cross-run memory
> trustworthy: provenance on every journal write, a runner-side guard on market
> evidence, and enforced retention. These are the mitigations for the attack
> classes documented in the research literature (information injection against
> news-reading agents, memory injection against agent histories).
>
> **Status:** `implemented_unverified` — code and unit tests are in place and
> green (`cd backend && go test ./pkg/trader/ ./pkg/engine/`); the tools are
> wired into the plan-execute tool set but no live exchange feed exercises them
> yet. Phase A2 (hash chain + Git anchor) landed on top of Phase A.
> **Implements:** the Phase A / A2 slice of the trader-agent plan; seeds
> checklist task **P7.6** (scoped memory).
> **Companion specs:** [`system-ir.md`](system-ir.md) (memory is a runtime
> concern, never an IR field), [`mcp-trust-slice.md`](mcp-trust-slice.md)
> (untrusted boundaries, identity from the caller),
> [`../07-multitenancy-security.md`](../07-multitenancy-security.md).

## Contents

| Section | Anchor |
|---|---|
| Why memory is the attack surface | [`#why`](#why) |
| Defense 1 — provenance on every write | [`#provenance`](#provenance) |
| Defense 2 — market evidence guard | [`#guard`](#guard) |
| Defense 2b — hash chain + Git anchor | [`#chain`](#chain) |
| Defense 3 — risk gate on the order path | [`#gate`](#gate) |
| Defense 4 — enforced retention | [`#retention`](#retention) |
| What is still missing | [`#gaps`](#gaps) |

<a id="why"></a>
## Why memory is the attack surface

Two published results drive this design:

1. **Fabricated news beats prompt injection.** An information-injection
   benchmark over ~10,000 decisions reports 14% attack success for prompt
   injection but 62% for fabricated news, with single templates reaching 100%
   across every tested instrument — and only hierarchical aggregation reduced
   it.
2. **Memory injection beats prompt injection.** On a Web3 agent benchmark with
   150+ tasks and 500+ attack cases, injections written into the agent's own
   history succeeded far more often than prompt injections, because stored
   context is trusted by construction. Standard detectors missed roughly half.

Both attacks land in the same place: a row in the agent's history that looks
<a id="provenance"></a>
## Defense 1 — provenance on every write

`TradeProvenance` (`backend/pkg/trader/provenance.go:56`) pins four
things per event: the agent ref, the skill ref, the tool ref and the config
version — all as `kind/name@MAJOR.MINOR.PATCH` refs, the same grammar the
manifest gate pins. `Validate` rejects:

- a `Source` outside the closed allowlist (`tradeSourceAllowlist`,
  `backend/pkg/trader/provenance.go:43`) — an agent cannot name a new
  feed into existence;
- a missing `ConfigVersion` — a stale or unknown risk configuration must
  reject, never fall back to a permissive default;
- a floating or malformed ref (`@latest` is not a pin).

`AppendTradeEventWithProvenance` (`backend/pkg/trader/provenance.go:108`)
is the only append path for decisions and fills. It deletes any
model-supplied `provenance` key before merging: the engine's block always wins,
so a model cannot forge its own source.

The tool that agents actually call — `trade_checkpoint`
(`backend/pkg/trader/tools.go:119`) — requires `source` and
`config_version` as input and fails the tool call when provenance is invalid.

<a id="guard"></a>
## Defense 2 — market evidence guard

`GuardConfig.Check` (`backend/pkg/trader/guard.go:79`) classifies
one observation and returns an explicit verdict plus a machine-readable reason
(`source-not-allowlisted`, `symbol-not-allowlisted`, `timestamp-missing`,
`latency-exceeded:<lag>`, `stale-data:<age>`). A rejection becomes `flat` with
a reason — never a silently ignored signal.

`GuardConfig.CheckCandles` (`backend/pkg/trader/guard.go:133`)
applies series semantics rather than per-row semantics:

| Rule | Why |
|---|---|
| Latency and age judged on the **newest** candle only | older candles in a valid series are naturally older; judging each by fetch lag rejects every real series |
| Timestamps must be **strictly ascending** | a reordered or gapped series is not the one the venue produced |
| A **future** timestamp rejects the batch | look-ahead bias, the survey's most-cited methodological failure |
| Any violation rejects the batch **whole** | one fabricated candle moves RSI, so a partially trusted series is not a safer series |

`DefaultGuardConfig()` (`backend/pkg/trader/guard.go:57`) is the
paper-trading policy for 1h candles (2h max age, 1h max lag, BTCUSDT/ETHUSDT).
It carries a `Version` string so every accepted decision records which policy
admitted the evidence. Live execution must tighten both windows to seconds —
a new config version, not an edit to this one.

exactly like a legitimately recorded observation. A journal without provenance
is therefore an assertion, not evidence.

<a id="chain"></a>
## Defense 2b — hash chain + Git anchor (tamper-EVIDENCE)

Provenance is an assertion: a writer who can reach D1 can fabricate the whole
`provenance` block. The hash chain answers the other question — *did anything
change after it was written?*

`row_hash = sha256(prev_hash ‖ id ‖ tenant ‖ agent ‖ run ‖ event ‖ detail)`
(`backend/pkg/trader/chain.go:61`). Every field is in the digest, so editing any
field of any row breaks that row and every later row. `appendChained`
(`backend/pkg/trader/chain.go:106`) is the single write primitive: it holds a
per-`(tenant, agent)` lock, reads the head, hashes and inserts. The lock exists
because D1 is single-threaded but read-then-write is not atomic across
statements — without it two appends would fork the chain.

What it catches (each pinned by a test in `backend/pkg/trader/chain_test.go`):

| Attack | Detected as | Why |
|---|---|---|
| Edit a `detail_json` value | `row-hash-mismatch` | the payload is inside the digest |
| Delete a middle row | `link-broken` | survivors no longer link to their predecessor |
| Insert a row bypassing the writer | `row-hash-missing` | an unchained row is never trusted |
| Rewrite another tenant's chain | not seen by this tenant | the WHERE clause pins both identity columns |

`VerifyChain` (`backend/pkg/trader/chain_verify.go:38`) fails closed, and
`trade_resume` refuses to return history when it fails — resuming from tampered
memory is exactly the memory-injection failure mode.

### Anchoring in Git

A chain alone is not enough: an attacker who can rewrite the *entire* table can
recompute every hash. `AnchorHead` (`backend/pkg/trader/chain_verify.go:99`)
therefore seals the verified head outside D1 through the `Anchor` interface
(`backend/pkg/trader/chain_verify.go:91`).

The control plane implements it over the internal Git repo
(`backend/pkg/engine/trade_anchor.go:49`): one commit per anchor, one repo per
`(tenant, agent)` (`AnchorAppID`, `backend/pkg/engine/trade_anchor.go:38`),
payload `journal.anchor.json` = `{head_hash, rows, anchored_at, chain_format}`.
Commit objects are content-addressed and the repo never rewrites history
(`Revert` appends), so a table rewrite cannot reproduce the commit chain.

Two deliberate behaviours:

- **A broken chain is never anchored** — sealing corrupt history would bless it.
- **A failed anchor never loses the row.** The anchor is best-effort; the
  journal row already exists and the next successful anchor seals the longer
  chain.

### Threat model, stated honestly

| Attacker | Chain | Anchor | Notes |
|---|---|---|---|
| Model writes misleading rows through the tools | ✗ not caught | ✗ not caught | By design: this is what `guard` + the risk gate are for. The chain proves *no later edit*, not *the content was right*. |
| Model/DBA edits a row afterwards | ✅ caught | ✅ | the row no longer matches its digest |
| Full table rewrite with recomputed hashes | ✗ caught only as a fork | ✅ detected | the anchored head no longer exists in the chain |
| Anchor store also compromised | — | ✗ | out of scope; would need external timestamping |

<a id="gate"></a>
## Defense 3 — risk gate on the order path (Phase B)

The literature's bluntest finding is the one this section implements: *a prompt
is a suggestion; a runner-side check is a rejection.* `risk-manager.skill`
states the limits; `RiskPolicy.CheckOrder`
(`backend/pkg/trader/gate_checks.go:16`) is what makes them true.

`DefaultRiskPolicy()` (`backend/pkg/trader/gate.go:48`) mirrors the skill text
value for value — 1% risk to stop, 20% position cap, 60% gross exposure, 3 open
positions, −3% daily stop, 3-loss streak halt, 2h stale quote, BTCUSDT/ETHUSDT
allowlist — so the prose and the enforcement cannot drift apart silently.

Fourteen controls run in a fixed order and the **first failure wins**, each with
its own reason (`config-version-mismatch`, `symbol-not-allowlisted`,
`kill-switch-active`, `loss-streak-halt`, `daily-loss-stop`, `stale-quote`,
`position-exists`, `max-open-positions`, `position-cap`, `gross-exposure`,
`risk-per-trade-exceeded`, `stop-loss-required`, `stop-loss-above-entry`,
`policy-invalid`). A rejection always names the control, because a generic
"denied" hides which guard fired and makes incident review guesswork.

The gate **rejects; it never clamps.** A clamped order still trades a size the
model did not reason about, which is how "1% risk" quietly becomes "whatever the
gate allowed".

### The approval token is the enforcement seam

`trade_risk_check` returns a token; `trade_order` will not fill without one.
The token is an HMAC-SHA256 over *exactly* the intent — policy version, symbol,
side, quantity, price, stop and client order id — plus a 5-minute expiry
(`issueToken`/`VerifyToken`, `backend/pkg/trader/gate_token.go:52`). Consequences
that matter:

- widening an approval (0.01 → 0.5) produces a different MAC and is refused;
- swapping the symbol or the stop is refused;
- an approval expires before the market state it was granted against can drift;
- the key is per-process and never leaves it, so a model cannot mint its own
  token; a restart invalidates outstanding approvals, which is the safe
  direction.

`ExecutePaperOrder` (`backend/pkg/trader/order.go:39`) verifies the token
**before** touching state, so a refused order leaves no half-applied position.
Quotes are deliberately *not* re-priced by the gate: the model must pass
`quote_age_seconds` honestly, and a stale reference is rejected rather than
silently refreshed to whatever is convenient.

<a id="retention"></a>
## Defense 4 — enforced retention

`SweepTradeMemory` (`backend/pkg/trader/retention.go:39`) deletes
journal rows and a stale state row beyond the agent's pinned
`memory.retentionDays` (90 for `spot-trader`). It is:

- **tenant-scoped** — both statements pin `tenant_id` and `agent_name` as bind
  parameters; an empty identity is a no-op, never a global delete;
- **bounded** — a `LIMIT`-bearing subquery keeps one call from holding D1's
  single-threaded database for a whole backlog (`docs/CF_LIMITS.md` §8);
- **explicit about opt-out** — `retentionDays <= 0` disables the sweep and
  logs it, so "unlimited" is a decision, not a default.

<a id="gaps"></a>
## What is still missing

| Gap | Why it matters | Owner |
|---|---|---|
| Live feed round-trip through the guard | The guard is unit-tested against synthetic evidence; no exchange adapter calls it yet | Phase B follow-up |
| Venue reconciliation + restart drill | Idempotency and restart safety are proven by unit tests, not against a venue | Phase B follow-up |
| Scheduled sweep wiring | `SweepTradeMemory` exists; no cron/Workflow calls it | Phase B follow-up |
| Anchor cadence | Anchoring happens per checkpoint only; a scheduled seal of idle chains would shorten the unanchored window | Phase B follow-up |
| Monitoring / heartbeat | No alert path; today the operator is the monitor (see `RUNBOOK.md` §5) | Phase B follow-up |
| Persistent config-version registry | `RiskPolicy.Version` is a constant string; resolution against a real registry is the compiler's job | P7.3/P7.4 |
| Live order executor | Paper fills at the reference price; a live executor needs vault-held credentials and the same token contract | not scheduled |
| Memory consolidation and self-reflection | The survey's "adaptation" layer; no implementation | P7.7 |

_Last reviewed: 2026-11-18._
