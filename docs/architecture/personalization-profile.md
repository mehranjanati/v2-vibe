# Personalization Profile — Normative Contract (PROPOSED, ADI-MEM-003)

> **Status:** `proposed` (2026-11-23) — design only. No store, no inference engine, no code in this change.
> **Reuses:** the memory record shape ([`memory-provider.md`](memory-provider.md)) — a preference is a memory record with `kind: preference`. **Consumed by:** context assembly (`context-assembly.md`, `kind: profile`). **Umbrella:** [`context-activation-and-memory.md`](context-activation-and-memory.md) §7.

## 1. What a profile is — and the one thing it is not

A **profile** is the set of preferences and defaults that shape *how* a run behaves: tone, verbosity, language, preferred tools, default model policy, approval strictness the user has chosen within what policy allows.

**A profile is not authorization.** It can never widen permissions, relax an approval class, change a pin, or select a capability outside the closure. Those are decided by activation (`activation-policy.md`) and enforced by `mcp.Gate`. A profile entry that appears to grant something is invalid by construction, and the validator refuses it rather than "best-effort" ignoring it.

This distinction is the whole reason the contract exists: personalization is the feature most likely to be quietly overloaded into a permission system, and that would break the invariant chain `activated ⊆ … ⊆ closure` that the rest of ADI depends on.

## 2. Three key namespaces (the corrected precedence)

The umbrella document describes a single ladder (platform → tenant → team/agent → explicit user → inferred). A single ladder is wrong in one important place: an organisation's *default* tone should not beat a user's *explicit* choice, while an organisation's *constraint* always must. The contract therefore splits keys into namespaces, each with its own winner rule.

| Namespace | Who may set it | Winner | Example key |
|---|---|---|---|
| `constraint.*` | platform, tenant only | highest layer wins outright; a user write is `E_PROFILE_KEY` | `constraint.maxApprovalClass` |
| `default.*` | tenant, team, agent | the narrowest layer that set it, unless an explicit user entry exists | `default.tone`, `default.verbosity` |
| `preference.*` | user (explicit), inferred fallback | **explicit beats every layer below it and every inferred value** | `preference.language`, `preference.model` |

Rules that follow:

1. **A user cannot write `constraint.*`.** Attempting it is refused, not dropped — silent drops teach users that their setting "sometimes works".
2. **A `default.*` is a default, not a mandate.** An explicit user entry for the same key wins; if the tenant truly needs to force a value, it belongs in `constraint.*`, where it is labelled as such.
3. **`preference.*` is the only namespace where inference is allowed**, and even there only when no explicit entry exists (§4).
4. The namespace is part of the key: no key exists outside them, and an unknown namespace is `E_PROFILE_KEY`.

## 3. Entry shape

```go
type Entry struct {
    Key        string    // namespaced: constraint.* | default.* | preference.*
    Layer      Layer     // platform | tenant | team | agent | user
    TenantKey  string
    Subject    string    // user id for user layer; team/agent id otherwise
    Value      any       // typed by the key catalog; never free text for closed keys
    Source     Source    // explicit | inferred
    Evidence   string    // required when Source == inferred: the observation it came from
    Confidence float64   // 0..1; required for inferred, meaningless for explicit
    Producer   string    // provider/feature id
    Version    string    // producer version
    UpdatedAt  time.Time // injected clock
}
```

Two rules make this more than a key-value bag:

- **An inferred entry without `Evidence` is refused** (`E_PROFILE_EVIDENCE`). An inference with no observation behind it is a guess, and guesses must not be stored as facts a model will trust later.
- **`Value` is typed per key.** The key catalog declares the type and, for closed keys (enums), the allowed values. A profile that accepts arbitrary values is a prompt-injection surface.

## 4. Explicit vs inferred, and the no-silent-override rule

| Situation | Behaviour |
|---|---|
| Inference for a key with **no** explicit entry | Stored, `Source: inferred`, with evidence and confidence |
| Inference for a key with an **explicit** entry | **Refused** (`E_PROFILE_CONFLICT`) and reported as a proposal; the platform may surface it to the user, who promotes it explicitly or ignores it |
| Explicit write over an inferred entry | Allowed; the inferred entry is superseded and retained in history as shadowed evidence |
| Explicit write over an explicit entry | Allowed; `UpdatedAt` moves, the previous value is kept in history |

**Inference never silently overrides an explicit setting** — not at write time, not at read time, not "just for this run". This is the acceptance criterion, and it is enforced in two places: the store refuses the conflicting write, and assembly (which reads the profile) never merges two values for one key into a single blob.

## 5. Resolution: one winner per key, with the losers recorded

Resolution happens where the profile is read, not where it is written.

```text
for each key:
  candidates = entries for (tenant, subject, key)
  winner     = apply the namespace rule of §2 (explicit beats inferred; narrowest allowed layer)
  shadowed   = every other candidate, with its layer and source
```

Rules:

1. **Deterministic:** the resolver is a pure function of the entry set; ties cannot exist because exactly one layer wins per namespace, and explicit always beats inferred.
2. **Auditable:** the resolved view carries `shadowed[]` so "why did it use `formal` when I set `casual`?" has an answer (in that example: a `constraint.*` or a narrower layer won, and the trace says which).
3. **Bounded for the model:** the context envelope carries the winner plus its provenance; `shadowed[]` stays in the trace, not in the prompt. Sending both to a model invites it to pick the wrong one.
4. **`constraint.*` is not negotiable at read time either:** if a winner would contradict a constraint, that winner is the constraint (they cannot conflict by construction, since the constraint layer is the only writer of `constraint.*`).

## 6. Update, versioning and deletion

| Operation | Rule |
|---|---|
| Update (explicit) | Append a new entry version; `UpdatedAt` from the injected clock; the previous value is retained for audit |
| Supersede (explicit over inferred) | New explicit entry; the inferred one becomes shadowed evidence rather than being deleted, so the platform can explain what it used to assume |
| Delete by user | Tombstone the key; the resolved view stops returning it; history remains until retention expires |
| Erasure (tenant/user deletion) | Remove entries **and** evidence references; report the real count, exactly like `memory-provider.md` §7 |
| Inferred entry expiry | Allowed (`ExpiresAt`); a preference inferred from one conversation should not shape the next year of runs |

## 7. Failure modes

| Code | Condition | Behaviour |
|---|---|---|
| `E_PROFILE_KEY` | Unknown namespace, unknown key, or a user write to `constraint.*` | Refuse; never drop silently |
| `E_PROFILE_CONFLICT` | An inferred write targets a key with an explicit entry | Refuse and report as a proposal |
| `E_PROFILE_EVIDENCE` | An inferred entry has no evidence reference | Refuse the write |
| `E_PROFILE_TENANT` | Entry or read from another tenant | Refuse; a profile is tenant data |
| `E_PROFILE_BOUNDS` | The profile exceeds the entry/byte bound | Refuse the write; a profile is small by nature, a runaway one is a bug |
| `E_PROFILE_SCOPE` | A write claims a layer the caller does not administer (e.g. a user writing `default.*` at tenant layer) | Refuse |

## 8. What this contract does not do

- It does not store anything itself: a profile may be a projection over settings tables and `memory` records with `kind: preference`, and that choice is deliberately open (§10).
- It does not infer anything: inference is a producer, and this contract only defines what a valid inferred entry looks like.
- It does not authorize, rank capabilities, choose models or modify the closure.
- It does not add a table, a migration or a settings service in this task.

## 9. Tests this contract will need (ADI-MEM-003a, when an implementation exists)

1. An explicit `preference.*` beats an inferred entry for the same key, and the inferred one appears in `shadowed[]`.
2. An inferred write over an explicit entry is refused with `E_PROFILE_CONFLICT` and creates no entry.
3. An inferred entry without `Evidence` is refused with `E_PROFILE_EVIDENCE`.
4. A user write to `constraint.*` is refused with `E_PROFILE_KEY` (not silently dropped).
5. A `default.*` set by the team is beaten by an explicit user entry, and wins when the user has none.
6. Resolution is a pure function: the same entry set yields the same view twice, and the winner is stable when entries are inserted in a different order.
7. Erasure returns the real count and leaves no resolvable entry.
8. An expired inferred entry stops appearing in the resolved view without deleting history.
9. A profile entry can never change what activation allows: a run whose `preference.*` names a capability outside the closure is still refused at activation.
10. Unknown key or unknown namespace in the key catalog is refused, so a typo cannot create a silent no-op preference.

## 10. Open items

| Item | Why it is open |
|---|---|
| Key catalog ownership and initial contents | A small, versioned catalog (`preference.language`, `preference.tone`, `default.model-policy`, …) needs an owner; today nothing defines it. |
| Whether inferred preferences ship at all in the first slice | **Recommendation: no.** Explicit-only personalization is useful and cheap; inference needs an evaluation dataset to prove it helps rather than annoys, which belongs to `evaluation-and-evolution.md`. |
| Storage shape: projection over settings vs dedicated table | Same O27-adjacent decision as memory (`memory-provider.md` §10); picking two different stories would be the mistake. |
| Whether `constraint.*` should also be readable from the tenant policy document | Today it is described as a profile namespace; a tenant policy file may be the better home, and that is a policy-ownership decision. |
| Retention of shadowed history | Audit wants it kept; privacy wants it gone. Needs an explicit retention window. |
