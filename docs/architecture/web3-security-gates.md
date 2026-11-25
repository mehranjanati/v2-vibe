# Web3 — Validation and Security Pipeline Gates (PROPOSED, DESIGN ONLY)

> **Status:** `proposed` 2026-11-25. Design only: **no gate is implemented**, no
> scanner is chosen, no CI job exists.
> Task: `W2-WEB3-01` (acceptance: gate list with pass/fail criteria; no gate claims
> audit status; evidence artifacts per gate defined).
> Companions: `web3-compilation-and-artifacts.md` (W1-WEB3-02 — build evidence G1
> consumes), `web3-solidity-generation-scope.md` (W1-WEB3-01 — scope and the module
> pin G0/G8 check), `web3-security-signing.md` (normative invariants),
> `web3-smart-contract-ir.md` (the IR whose `tests` and `security` fields drive the
> gates), `web3-toolchain-pipeline.md` (stage order).
> Labels: FACT (read from the repo's schema or primary docs on 2026-11-25),
> ASSUMPTION, OPEN.

## The line this document draws first

Every document in this track repeats one sentence, so it is the first thing a gate
implements: **passing a gate is not an audit.**

That is not a disclaimer bolted on at the end — it is a structural rule with three
consequences, and each one is enforceable:

1. **A gate cannot write an audit status.** Contract IR already has
   `spec.abstract.security.auditStatus` with the closed set
   `none | reviewed | audited` (FACT: `contract-ir/contract.schema.json`). The
   pipeline may read it and may refuse it; it may never set it. `reviewed` and
   `audited` require an external record that the pipeline did not produce.
2. **A gate verdict has its own vocabulary** (`not_run | failed | passed | waived |
   not_applicable`), distinct from any claim about the contract's safety. There is
   deliberately no `safe` value.
3. **Every verdict names its bound.** A fuzz verdict is "no counterexample in N runs
   with these seeds", never "no bugs". An invariant verdict is "these properties held
   under this harness", never "the contract is correct".

The gates therefore answer one narrow question: *was the work this design requires
actually done, on this exact artifact?* — not *is this contract safe to deploy with
real money?*

## Gate list (acceptance criterion 1)

Gates run in order; a later gate may consume an earlier gate's evidence but never
substitutes for it. Where the **source** column names an IR field, the gate's
required set comes from the contract document itself — so it is machine-readable
rather than negotiated per project.

| ID | Gate | Source | Standard | High | Pass criterion (summary) |
|---|---|---|---|---|---|
| G0 | Scope admission | IR kind + policies + upgradeability | required | required | Kind is in the W1-WEB3-01 in-scope table; every `payable` function names one of the four authority shapes; `proxy` has `risksAcknowledged: true` |
| G1 | Build reproducibility | `provenance` + W1-WEB3-02 manifest | required | required | Double build in two independent instances produces identical `abi`/bytecode/metadata digests; compiler and tarball digests match the pins; import closure inside `vendor/` |
| G2 | Unit tests | `tests[kind=unit]` | required | required | Every `required: true` unit entry maps to an existing test that ran and passed, with zero skips |
| G3 | Integration tests | `tests[kind=integration]` | required | required | Every required entry maps to an end-to-end scenario that ran and passed |
| G4 | Fuzz | `tests[kind=fuzz]` | optional | required | Profile-defined run count met, seeds recorded, zero counterexamples |
| G5 | Invariant | `tests[kind=invariant]` | optional | required | Every `spec.abstract.invariants[]` entry mapped to a passing invariant test; no counterexample |
| G6 | Static analysis | `tests[kind=static-analysis]` | required | required | Zero high-severity findings; every medium finding fixed or dispositioned in writing with an owner |
| G7 | Simulation preview | `tests[kind=simulation]` | optional | required | Deploy plus scripted calls succeed on a pinned fork; every asserted state transition observed; no unexpected revert |
| G8 | Provenance and traceability | `provenance` + manifest | required | required | `irDigest` matches the IR compiled, `templateRef` is pinned, source header carries IR digest and toolchain, metadata IPFS CID matches the manifest, `auditStatus` claim has a resolvable external record |
| G9 | Deployment policy and approvals | risk class + policy version | required | required | Required approvals for the risk class exist; mainnet never auto-approves; high-risk has simulation evidence plus a rollback or pause plan |
| G10 | Secret and key hygiene | invariants 1–2, 6 | required | required | No key, seed, or token material in sources, NatSpec, artifacts, or logs; `sandbox.keysPresent: false`; no secret in the IR |

`Standard` means testnet or low value; `High` means mainnet, value movement,
upgrades, or cross-chain enablement — the same two classes
`web3-security-signing.md` already defines. A project may add gates; it may not drop
a required one without a recorded owner waiver (see "Verdict model").

## Gate detail — pass/fail, evidence, and what each gate does *not* prove

Objective criteria only: a gate that needs a human to interpret its output is not a
gate, it is a review step (and reviews are recorded separately).

### G0 — Scope admission (pre-build, no compiler)

**Pass:** the contract's kind maps onto an in-scope row of
`web3-solidity-generation-scope.md`; every `payable` function declares a `policy`
that resolves to one of the four authority shapes; `upgradeability.strategy: proxy`
carries `risksAcknowledged: true`; every `dependencies[]` ref is pinned and
resolvable.
**Fail:** an out-of-scope kind (AMM math, governance, bridge, diamonds, 4626, 4337,
oracle consumer); an unresolvable policy; `proxy` without acknowledgement.
**Evidence:** `admission.json` — `{irDigest, kind, checks:[{id, result}], verdict}`.
**Does not prove:** that the *design* is sound. A well-formed escrow can still be a
bad escrow.

### G1 — Build reproducibility

**Pass:** the W1-WEB3-02 manifest carries `reproducibility.verified: true` from two
independent instances; compiler digest and tarball digest match their pins; every
resolved import path is inside `vendor/`; `settings.libraries` is empty.
**Fail:** a single build, a digest mismatch, an import outside the closure, or any
non-empty `libraries`.
**Evidence:** the manifest itself plus both build ids.
**Does not prove:** that the *source* is safe. It proves that the artifact is the
one this source produces.

### G2 — Unit tests

**Pass:** for every `tests[]` entry with `kind: unit` and `required: true`, a test
exists, ran, and passed; the IR-entry → test-name mapping is recorded; the suite
reported zero skips and zero disabled tests; each negative case asserts a specific
revert rather than "any revert".
**Fail:** a required entry with no mapped test; a skipped, filtered, or
zero-assertion test; a negative case that only expects failure.
**Evidence:** `test-report.json` — per-test `{irTest, testName, status, duration}`
plus the artifact digest under test.
**Does not prove:** coverage of untested behaviour; the IR's own completeness.

### G3 — Integration tests

**Pass:** every required `integration` entry maps to a scenario that deploys the
artifact and drives the cross-contract flow it names, then asserts final state.
**Fail:** a scenario that only constructs objects; a missing scenario; a scenario run
against a different artifact digest.
**Evidence:** `integration-report.json` with the artifact digest, scenario names, and
final-state assertions.
**Does not prove:** behaviour against real external protocols — the scenarios use
locally pinned fixtures.

### G4 — Fuzz

**Pass:** the profile's run count is met, the seed set is recorded, and no
counterexample was found. A counterexample is a **fail**, and its replay command is
part of the evidence.
**Fail:** an unrecorded seed set (unreproducible run); runs below the profile
minimum; any counterexample not fixed.
**Evidence:** `fuzz-report.json` — `{runs, seeds, counterexamples, profileVersion}`.
**Does not prove:** absence of bugs outside the property's input domain; a fuzzer
cannot explore what the property does not state.

### G5 — Invariant tests

**Pass:** every `spec.abstract.invariants[]` string maps to an invariant test (or to
an explicit `not_applicable` with a reason); no counterexample; depth and run counts
recorded.
**Fail:** an invariant with no mapping and no reason; a counterexample.
**Evidence:** `invariant-report.json` — the invariant text, the handler set, runs,
depth, seeds.
**Does not prove:** that the listed invariants are the *right* ones. The IR author
chose them; the gate only proves they held.

### G6 — Static analysis

**Pass:** zero findings at the analyzer's highest severity; every medium finding is
either fixed or carries a written disposition (why accepted, by whom); the analyzer
name, version, and config digest are recorded; suppressions have an owner and an
expiry.
**Fail:** any unresolved high finding; a suppression without an expiry; an analyzer
version not recorded (an unrecorded run is not reproducible).
**Evidence:** `static-analysis.sarif` (or canonical JSON) plus `dispositions.json`.
**Does not prove:** absence of exploitable logic errors — analyzers find patterns,
not intent.

### G7 — Simulation preview

**Pass:** deploy plus the scripted call sequence succeed on the pinned fork
(chain id and block recorded); every asserted state transition is observed; no
unexpected revert or gas spike beyond the profile budget.
**Fail:** an unrecorded fork block; an unasserted step; a revert on the happy path.
**Evidence:** `simulation.json` — fork reference, transactions, assertions, gas used.
**Does not prove:** mainnet behaviour, because the fork is a snapshot and the mempool
is not simulated.


### G8 — Provenance and traceability

**Pass:** the manifest's `contract.irDigest` equals the digest of the IR the source
was generated from; `templateRef` is an exact pinned version; the generated source
header carries the IR digest and the toolchain triple; the metadata IPFS CID in the
manifest matches the metadata output; and if `security.auditStatus` is anything but
`none`, an external record resolves (auditor identity, scope digest, date, findings
digest).
**Fail:** a digest mismatch anywhere in that chain; an unset `templateRef`; or
`auditStatus: reviewed|audited` with no resolvable external record — the pipeline
**cannot** satisfy this itself, so the honest verdict is `failed`.
**Evidence:** `provenance.json` — the resolution results plus the external audit
reference when present.
**Does not prove:** that the audit was good, only that one exists and covers this
digest.

### G9 — Deployment policy and approvals

**Pass:** the deployment profile's risk class is recorded; the required approvals for
that class exist with approver references (never secrets) and a policy version; a
mainnet deployment has no auto-approval; a high-risk deployment has simulation
evidence (G7) plus a rollback or pause plan.
**Fail:** a missing approval; a mainnet auto-approval; a high-risk deployment without
simulation evidence or without a rollback/pause plan; an unrecorded policy version.
**Evidence:** `policy-decision.json` — `{riskClass, policyVersion, requiredApprovals,
approvals[], decision, rollbackPlanRef}`.
**Does not prove:** that the approver understood the risk, only that the approval
exists under the recorded policy.

### G10 — Secret and key hygiene

**Pass:** no private key, mnemonic, seed, or provider token appears in the sources,
NatSpec, artifacts, IR, or logs; `sandbox.keysPresent` is `false`; no secret-shaped
string is present in the manifest.
**Fail:** a pattern match of key material anywhere in the build inputs or outputs, or
a `keysPresent: true` attestation.
**Evidence:** `secret-scan.json` — the patterns used, the scanned digest set, zero
matches.
**Does not prove:** that no *future* secret will leak; it is a point-in-time check,
which is why the signing boundary (not this gate) is what actually keeps keys out.

## Verdict model (acceptance criterion 2 in practice)

A verdict is a function of evidence digests plus a policy version — never of
opinion. Recording the policy version means a verdict can be re-evaluated later
without re-reading the run.

| Field | Values | Rule |
|---|---|---|
| Per-gate `status` | `not_run`, `failed`, `passed`, `waived`, `not_applicable` | `waived` and `not_applicable` require a reason and an owner; both are visible in the summary, never silent |
| Per-gate `bound` | Free text, required when `passed` | States exactly what was observed ("256 runs, seeds 1…8", "invariants I1–I4 under handler H") |
| Aggregate `gates_passed` | `true` only when every gate required for the risk class is `passed` | `waived` does not count as passed |
| `audit_status` | Always `none` unless an external record exists | The pipeline never sets it; G8 verifies it |
| `production_ready` | **No such field** | Readiness is a human decision recorded outside the gates |

**Waivers** are owner decisions with an expiry, recorded as
`{gate, reason, owner, expiresAt}` and referenced by the verdict. An expired waiver
turns the gate back into `failed` on the next evaluation. A waiver may not be issued
by the agent that requested the deployment.

**Forbidden labels** in any UI, artifact, manifest, or generated document:
"audited", "secure", "safe", "hack-proof", "guaranteed", "production-ready", and
"verified" used as a synonym for audited. `source_verified` is allowed only for
on-chain source verification (Sourcify/Blockscan-style), and it must be rendered with
its own label and a link — it says the published source compiles to the deployed
bytecode, nothing more.


## Evidence artifacts (acceptance criterion 3)

One artifact per gate, digest-addressed, referenced from the verdict. Nothing in this
table is a screenshot, a chat message, or "the CI tab was green".

| Gate | Artifact | Format | Key contents | Digest scope |
|---|---|---|---|---|
| G0 | `admission.json` | JSON | irDigest, kind, per-check result | canonical JSON |
| G1 | artifact manifest (W1-WEB3-02) | JSON | compiler, settings, pins, output digests, `reproducibility` | manifest bytes |
| G2 | `test-report.json` | JSON | IR entry → test name, status, durations | report bytes |
| G3 | `integration-report.json` | JSON | scenarios, artifact digest, final state | report bytes |
| G4 | `fuzz-report.json` | JSON | runs, seeds, counterexamples, profile version | report bytes |
| G5 | `invariant-report.json` | JSON | invariants, handler set, runs, depth, seeds | report bytes |
| G6 | `static-analysis.sarif` + `dispositions.json` | SARIF + JSON | findings by severity, dispositions, suppressions | both files |
| G7 | `simulation.json` | JSON | fork block, transactions, assertions, gas | report bytes |
| G8 | `provenance.json` | JSON | digest chain results, audit reference (if any) | report bytes |
| G9 | `policy-decision.json` | JSON | risk class, policy version, approvals, rollback ref | report bytes |
| G10 | `secret-scan.json` | JSON | patterns, scanned set, zero matches | report bytes |

Rules for the catalogue:

- **Every artifact carries the artifact digest it describes.** A test report that
  does not name the bytecode it tested is not evidence.
- **Canonical serialization.** Sorted keys, 2-space indent, one trailing newline —
  the same rule the repo already applies to manifests and IR, so a digest is stable.
- **Retention.** Evidence is retained at least as long as the deployment it licensed,
  and it is never overwritten: a re-run produces a new verdict, not an edited one.
- **Storage location is OPEN** (O27 family). The shape is storage-agnostic on purpose.
- **The verdict references artifacts by digest**, so tampering with an artifact
  breaks the reference instead of changing the story.

## Acceptance criteria mapping

| Criterion (backlog) | Where it is answered |
|---|---|
| Gate list with pass/fail criteria | "Gate list" table (11 gates, required per risk class) plus one detail block per gate with explicit **Pass** and **Fail** lists |
| No gate claims audit status | "The line this document draws first" plus "Verdict model": no `safe`/`production_ready` value, `audit_status` never set by the pipeline, G8 fails an unbacked `audited` claim, and the forbidden-label list |
| Evidence artifacts per gate defined | "Evidence artifacts": one artifact per gate with format, contents, digest scope, and retention rules |

Verification for this task is documentation-only: `bun run docs:check` must stay green
(six validators), and no code, scanner, CI job, route, or dependency may appear.

## Relationship to the rest of the Web3 track

| Task | Consumes from here | Adds |
|---|---|---|
| `W3-WEB3-01` wallet/signing/deploy | the verdict and G9's approval record — signing refuses a deployment whose verdict is not `gates_passed` | signing authority, broadcast, confirmation policy |
| `W4-WEB3-01` interop | G0's refusal of bridge/OApp kinds in v1 | LayerZero adapter implementation |
| `W1-WEB3-01` scope | — | supplies the in-scope table G0 enforces and the allowlist G1/G8 check |
| `W1-WEB3-02` compilation | — | supplies the manifest G1 and G8 read |
| Future validator work | the `auditStatus` rule | a semantic check in `scripts/validate-contract-ir.mjs`: `reviewed`/`audited` without an external record is rejected. Not implemented here — this task is design-only |

## What this does not do

No scanner, fuzzer, simulator, or CI job is added; no gate is implemented in code;
no framework is chosen (`W3-TOOL` stays PROPOSED); no policy schema is finalised
(G9's fields are the input to the signing ADR); and no claim is made that any
existing contract has passed these gates — none has been run. `go.mod`,
`package.json`, and `bun.lock` are untouched by design.

## Open items

- **Scanner choice.** The shortlist and evaluation criteria stay OPEN in
  `web3-toolchain-pipeline.md`; G6 is written tool-agnostically so the choice cannot
  change the pass criteria.
- **Fuzz/invariant budgets** (`runs`, `depth`) per risk class: the profile owns the
  numbers; this document only requires that they exist and are recorded.
- **Waiver policy.** Who may waive a high-risk gate, and for how long, is a product
  and governance decision; the mechanism (owner, reason, expiry) is defined here.
- **Policy schema.** `policy-decision.json` fields are the input to
  `adr-web3-signing-authority.md`; finalising them there avoids two competing
  schemas.
- **Signed verdicts.** Whether the aggregate verdict is signed by the control plane
  (and with which key) is a follow-up; the digest references already make tampering
  detectable, signing would make it attributable.

