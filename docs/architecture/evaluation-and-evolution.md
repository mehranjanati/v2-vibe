# Evaluation and Controlled Evolution (PROPOSED)

> **Status:** `proposed` 2026-11-23, mission VIBE-ARCH-CONSOLIDATION-ADI-001. Awaiting owner acceptance. Companion: `adaptive-intelligence-architecture.md`, `composition-lock.md`, `context-activation-and-memory.md`, `adr-adaptive-intelligence-boundaries.md`. Base: System IR invariants (`system-ir.md`), manifest refs (`manifest-schemas.md`), execution contract C1-C18 (`../ecosystem/06-runtime-and-execution-architecture.md`), lineage D1 tables (`generations`, `generation_files`, `generation_audits`), metering guard (`backend/pkg/metering` reads durable results only).

> Authoritative evaluation contract: [`evaluation-contract.md`](evaluation-contract.md) (ADI-EVAL-001). Summary, so the two cannot disagree:

## 1. Evaluation layers

Five closed layers — **L1 structural** (IR invariants), **L2 capability** (closure resolves), **L3 behavioral** (dataset), **L4 operational** (C1-C18), **L5 safety** (denials hold, isolation, no secret leaks, approvals plus idempotency). Two are automated today (L1 via `backend/pkg/ir`, L2 via `backend/pkg/composition`), L4 is partly automated (18/18 on the reference `Fake`; the Cloudflare adapter is unverified live), L3 and the end-to-end half of L5 have **no evidence pipeline yet**. The subject is always a **snapshot**, so an asset update creates a new subject and old results do not carry over.

## 2. Verdicts, datasets and gates

Verdicts are `pass` / `fail` / `not_evaluated` / `blocked`, and **`not_evaluated` is never `pass`** — a required layer without evidence blocks promotion exactly like a failure, with a different operator action. Datasets are pinned `eval/name@X.Y.Z` artifacts with a recorded digest and a stated sample size; there is **no composite score** (a blended number lets a failing safety metric be averaged away). A gate names layers, datasets and thresholds, and its failure blocks promotion; waiving a layer requires a named, expiring approver decision. An LLM judge may inform L3 only — never L1/L2/L4 and never L5, where a denial that executed is a fact rather than an opinion. O20 datasets stay open and are not invented here.

> Authoritative lifecycle contract: [`candidate-lifecycle.md`](candidate-lifecycle.md) (ADI-EVAL-003). Summary:

## 3. Candidate lifecycle

**A candidate is a snapshot; a stable composition is a pointer.** There is no editable stable artifact and no candidate file format, so the acceptance rule — candidates are created without changing stable compositions — holds structurally rather than by discipline. States are closed (`proposed → evaluating → evaluated → approved → promoted`, plus `rejected`, `expired`, `superseded`) and every transition has a guard: no promotion without a passing gate, none without a named approver. Promotion is a **compare-and-set** on `(tenantKey, systemRef, environment)` with an `expectedCurrentSnapshotId`, so two simultaneous promotions produce a visible `E_CAND_CONFLICT` instead of a silent loss. Evidence is valid for `(snapshotId, dataset digest, evaluatorVersion)` and un-promoted candidates expire, because stale evidence wearing the look of current evidence is worse than no evidence. New runs get the promoted default; in-flight and completed runs are untouched because they are already snapshot-bound.

> Authoritative contract: [`promotion-and-rollback.md`](promotion-and-rollback.md) (ADI-EVAL-004). Summary: promotion is a conjunction (passing gate for the target environment **and** the required approvals **and** an unexpired candidate **and** a successful compare-and-set) — never a score, never one-of-four. An approval binds to the evaluation artifact digest and expires with the candidate; the proposal author cannot be the sole production approver unless the self-approval exception is recorded. Rollout happens on environment pointers (`development → staging → production`), each its own promotion with its own evidence and audit row, because no traffic-splitting layer exists. **Rollback** targets a snapshot that was previously stable at that pointer and needs no new evaluation, but is refused if the target closure contains a retired asset; **emergency pin** targets any verified snapshot with a named decision and marks the pointer until a normal promotion clears it. Every pointer movement writes exactly one `audit_logs` row, and a movement that cannot be audited does not happen.

## 4. Approval, promotion, rollback rules

No unrestricted self-modification; no automatic publication of unverified extensions; no silent changes to active snapshots; historical versions stay reproducible; every evolution operation auditable. Promotion needs evaluation evidence plus approval; failed evaluation blocks promotion; emergency retirement follows the tombstone semantics (existing pins resolve, new pins/runs refused, successor declared not inferred).

## 5. Run lineage

Every run binds to its composition snapshot digest (see composition-lock.md) plus activation decision trace (see context-activation-and-memory.md) plus execution history (provider history surface) plus evaluation outcome. Lineage rows live in the data layer (D1/Redis), never in manifests.

## 6. Minimum vertical slice: Single-Agent Adaptive Composition Proof

Purpose: prove one existing Agent uses independently versioned capabilities through a deterministic composition plus activation lifecycle. Scope: one System Manifest, one existing Agent, a small set of existing Skills/Tools, exact pins, Composition Lock proposal, one immutable Run Snapshot, deterministic Activation, policy validation, run lineage, basic evaluation. Example: Adaptive Sales Qualification (qualify an incoming lead; capabilities: Lead Qualification Skill, Customer Knowledge, CRM Read Tool, Model Policy). Non-goals: no new multi-agent behavior, no unrestricted MCP execution, no package self-installation during activation, no self-modifying production system, no mandatory fine-tuning, no new database or broker, no new product surface. Success: composition validates and resolves; exact closure recorded; execution tied to immutable snapshot; activation selects allowed capabilities only; denied/skipped explainable; later versions do not alter history; evaluation compares outcomes. Gated by D8/D9: executable only as a bounded M0-consistent single-agent proof.

## 7. Open items

Dataset catalog per product (O20 open); benchmark harness owner; canary policy per environment (P5 Cloud ops); emergency-retirement authority; marketplace certification bar before Verified evaluation claims.
