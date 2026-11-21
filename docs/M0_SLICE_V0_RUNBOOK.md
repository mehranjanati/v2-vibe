# M0 Vertical Slice v0 — Execution Runbook (D9)

> **Status:** `adopted` scope (D9) / **executed 2026-11-21 on PRODUCTION** — 6 of the
> 7 acceptance criteria carry live evidence (recorded under *Implemented But
> Unverified* in [`DEV_STATUS.md`](DEV_STATUS.md)); criterion 2 is partial (the
> cross-tenant 403 leg needs a throwaway second account) and criterion 4 is
> API-level only (the History UI has not been exercised in a browser).
>
> Owner docs: slice definition + acceptance criteria in
> [`02-mvp.md`](02-mvp.md); scope freeze [`DEV_CHECKLIST.md`](DEV_CHECKLIST.md);
> decisions [`10-decisions-and-open-questions.md`](10-decisions-and-open-questions.md)
> (D8, D9); rollout prerequisites in
> [`DEPLOY_RENDER_CLOUDFLARE.md`](DEPLOY_RENDER_CLOUDFLARE.md) (P0 rollout
> runbook).

## What this proves

One loop, end to end, for a real user under real governance: **NL request →
single agent → governed run → lineage row + one Git revision → History/diff →
rollback**. It exercises only code that already exists on `main`; the script adds
no feature, only the proof.

## Preconditions (all must be true before Step 1)

| # | Condition | Why |
|---|---|---|
| 1 | A deployed control plane with `CONTROL_PLANE_REQUIRE_SESSION=true` and `VIBECODER_STORE_KV_ID=56d249930a4447ee92493a7ff061a3ae` (the namespace the Edge mints sessions into — a staging-specific namespace returns 401 for every login, see the deploy runbook correction). The 2026-11-21 run used **production** | acceptance criterion 2 cannot be met boundary-off |
| 2 | `bun run d1:token` succeeds locally and `.env` holds `CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_API_TOKEN`, `D1_DATABASE_ID` | the evidence read-backs use D1 directly |
| 3 | A **throwaway** second account on staging (never production) | the 403 proof needs a second identity |
| 4 | `jq` available locally (or substitute `python3 -m json.tool`) | readable evidence |

Shell setup for every step below:

```bash
cd '/Users/elbaan/V2 vibe'
API='https://<staging-control-plane-host>'      # no trailing slash
SESSION='<Edge session token of the OWNER>'     # localStorage: session token
CHAT='<chat id created in Step 1>'
auth=(-H "X-Session-Token: $SESSION")
```

## Step 0 — preflight (read-only, ~1 min)

```bash
bun run d1:token                                  # validates the stored OAuth token, rewrites .env
node --version                                    # wrangler needs >= 22 for the read-backs below
curl -sS -o /dev/null -w 'staging /api/status: %{http_code}\n' "$API/api/status"
curl -sS "${auth[@]}" "$API/api/limits/usage" | jq -r '.success'   # expect true
```

Then confirm the staging namespace really is the boundary's store — this is the
same proof as the P0 runbook, and it must stay green. **Also verify the
identity-store pairing**, because a mismatch here looks exactly like a broken
boundary: the `VIBECODER_STORE_KV_ID` the service verifies against must be the
namespace the Edge mints `session:token:*` into — if the service's namespace
shows zero sessions while the Edge's shows your live logins, that is the 401
cause:

```bash
bun run check:cp-auth --url "$API"
```

The script counts live `session:token:*` keys per namespace (Edge's store vs the
service's), probes the control plane anonymously (must be 401 with the boundary
on — anything else means the boundary is off and the runbook stops), then probes
with one live Edge token (PASS = 404/200, MISMATCH = 401, MISCONFIGURED = 503).
It never prints a session token. Manual equivalent of the counting step:

```bash
ACC=$(grep -m1 '^CLOUDFLARE_ACCOUNT_ID=' .env | cut -d= -f2-)
TOK=$(grep -m1 '^CLOUDFLARE_API_TOKEN=' .env | cut -d= -f2-)
for NS in 56d249930a4447ee92493a7ff061a3ae 4748f35d77274731a17fa4f32c8c3e00; do
  echo -n "session count in $NS: "
  curl -s -H "Authorization: Bearer $TOK" \
    "https://api.cloudflare.com/client/v4/accounts/$ACC/storage/kv/namespaces/$NS/keys?prefix=session%3Atoken%3A&limit=100" \
    | python3 -c 'import sys,json; print(len(json.load(sys.stdin).get("result") or []))'
done
```

```bash
cd backend && VIBE_P03_KV=real \
  CLOUDFLARE_ACCOUNT_ID=$(grep -m1 '^CLOUDFLARE_ACCOUNT_ID=' ../.env | cut -d= -f2-) \
  CLOUDFLARE_API_TOKEN=$(grep -m1 '^CLOUDFLARE_API_TOKEN=' ../.env | cut -d= -f2-) \
  VIBECODER_STORE_KV_ID=56d249930a4447ee92493a7ff061a3ae \
  go test -tags e2e ./e2e/ -run P03 -v
```

## Step 1 — run 1: create the app (criterion 1)

In the SPA (signed in as the owner):

1. New chat → prompt: *"A one-page todo list: add, complete and delete items,
   saved in memory; no server code."* Keep it small on purpose — the slice must
   route to the **single-coder** path.
2. Approve the proposed plan when it appears.
3. Wait for `generation_complete`.

Capture the identifiers:

```bash
curl -sS "${auth[@]}" "$API/api/generations/$CHAT" | jq '.data.generations[0] | {id,status,verdict,commitSha,parent,createdAt}'
```

**Expected:** one row, `status: "succeeded"`, `verdict` present
(`done`/`approve`), **`commitSha` non-empty**, `parent: null`.
**Gate check (anti-M1 drift):** this demo plan must have been executed by the
single-coder loop. Confirm from the control-plane log line the gate writes:
`team unavailable` or `small independent plan` — if the log says
`executing plan via multi-agent team`, the slice's premise is violated; change
the prompt to a smaller one and re-run.

## Step 2 — run 2: change one file (criteria 1 + 3)

In the **same** chat: *"Change the header text to 'Today' and keep everything
else as it is."* Approve, wait for completion.

```bash
curl -sS "${auth[@]}" "$API/api/generations/$CHAT" \
  | jq '.data.generations | map({id,parent,verdict,commitSha})'
```

**Expected:** the newest row's `parent` is run 1's id, and its `commitSha`
differs from run 1's.

## Step 3 — the diff is the change, not the world (criterion 3)

```bash
GEN2=$(curl -sS "${auth[@]}" "$API/api/generations/$CHAT" | jq -r '.data.generations[0].id')
curl -sS "${auth[@]}" "$API/api/generations/$CHAT/$GEN2/diff" \
  | jq '.data | {commitSha, files: (.files | map({path,op,beforeSize,afterSize,author}))}'
```

**Expected:** only the files run 2 actually touched; each row carries
`op` (`create`/`modify`/`delete`), both sizes and a non-null `author`.
**No `contents` key may appear** — the contract is metadata only. A diff that
lists every file means the baseline was lost (see the failure playbook).

Back the same claim with D1 (author attribution, criterion 3):

```bash
NODE22="$HOME/.nvm/versions/node/v22.23.1/bin/node"
"$NODE22" node_modules/wrangler/bin/wrangler.js d1 execute v2-vibe --remote \
  --config wrangler.v2.jsonc --json -y --command \
  "SELECT path, op, author_agent FROM generation_files WHERE generation_id = '$GEN2' ORDER BY path"
"$NODE22" node_modules/wrangler/bin/wrangler.js d1 execute v2-vibe --remote \
  --config wrangler.v2.jsonc --json -y --command \
  "SELECT actor, action FROM generation_audits WHERE generation_id = '$GEN2' ORDER BY rowid"
```

**Expected:** every file row has an `author_agent`; the audit trail contains
`file_written` entries and `generation_finished`.

## Step 4 — History is readable from the same rows (criterion 4: inspect)

Open the **History** tab in the SPA and confirm by eye: both runs listed newest
first, verdict badge, short `commit_sha`, branch, and the parent link on run 2.
Select run 2 → the diff table shows the same paths/sizes the API returned.
Open the rollback dialog on **run 1** and confirm its text says *"creates a NEW
revert commit … History is never rewritten"* — then press **Cancel**. The actual
rollback happens once, in Step 5.

## Step 5 — rollback through the product path (criteria 4 + 5)

Press **Roll back to this** on **run 1** in the History tab and confirm. Then
capture what the product did:

```bash
GEN1='<run 1 id>'
HIST=$(curl -sS "${auth[@]}" "$API/api/generations/$CHAT")
echo "$HIST" | jq '.data.generations | length'        # still 2: a revert adds no generation row
echo "$HIST" | jq -r '.data | .generations[0].commitSha, .generations[1].commitSha'
curl -sS "${auth[@]}" "$API/api/generations/$CHAT/$GEN2/diff" | jq '.data.files | length'
```

**Expected:** the row count stays at 2; both original `commitSha` values are
still returned (a revert never rewrites history); the old diff still loads.
The revert commit itself is the one the UI reported — capture it from the
success banner, or from HEAD after the rollback:

```bash
"$NODE22" node_modules/wrangler/bin/wrangler.js d1 execute v2-vibe --remote \
  --config wrangler.v2.jsonc --json -y --command \
  "SELECT action, actor, detail_json FROM generation_audits WHERE generation_id = '$GEN1' AND action = 'rollback'"
```

**Expected:** one `rollback` row whose `detail_json` carries the revert commit,
`restored_files` and `applied: true` (the room was live in the browser).
Also confirm the working set really went back: the preview must show run 1's
content, not run 2's header.

## Step 6 — governance (criterion 2, the reason this runs on staging)


```bash
# anonymous: no session token at all
curl -sS -o /dev/null -w 'anonymous list: %{http_code}\n' "$API/api/generations/$CHAT"

# the SECOND (throwaway) account must not read, diff or roll back
curl -sS -o /dev/null -w 'other list:   %{http_code}\n' -H "X-Session-Token: $OTHER" "$API/api/generations/$CHAT"
curl -sS -o /dev/null -w 'other diff:   %{http_code}\n' -H "X-Session-Token: $OTHER" "$API/api/generations/$CHAT/$GEN1/diff"
curl -sS -o /dev/null -w 'other rollback:%{http_code}\n' -X POST -H "X-Session-Token: $OTHER" "$API/api/generations/$CHAT/$GEN1/rollback"

# the owner still works
curl -sS -o /dev/null -w 'owner list:   %{http_code}\n' "${auth[@]}" "$API/api/generations/$CHAT"
```

**Expected:** `401` anonymous, `403` (owned by someone else) or `404` (unknown to
this caller) for the second account on all three routes, `200` for the owner.
If the owner gets `401`, the SPA session token is stale — re-sign in and repeat;
if the second account gets `200`, **stop**: the boundary is not enforcing and
every later step is meaningless.

## Step 7 — failure is loud, never a blank success (criterion 6)

In the SPA, start a third run and press **Stop** while it is streaming. Then:

```bash
curl -sS "${auth[@]}" "$API/api/generations/$CHAT" \
  | jq '.data.generations[0] | {id,status,verdict,commitSha}'
```

**Expected:** `status: "cancelled"` (a provider error would be `"failed"`), a
`verdict` that is `null` or the run's real verdict, **`commitSha: null`** — a
cancelled run produces no revision — and the SPA showed a cancellation event, not
a success banner with an empty preview. If the row says `succeeded`, the failure
path is lying: stop and investigate before recording anything.

## Step 8 — record the evidence (criterion 7)

The 2026-11-21 run against production is recorded in
[`DEV_STATUS.md`](DEV_STATUS.md) under *Implemented But Unverified* with its ids
and SHAs. Re-running the slice fills this table while the state is live, then
replaces that entry. Numbers, not adjectives.

| # | Criterion | Evidence to paste | 2026-11-21 |
|---|---|---|---|
| 1 | Loop | run id + `status` + `commitSha` | ✅ `2dc6aa64…` succeeded · `eaf402b5…` |
| 2 | Governance | the four HTTP codes (anonymous / other / owner) | ⚠️ anon=401 · owner=200 · **other not run** |
| 3 | Attribution | the `generation_files` rows with `author_agent` + audit actions | ✅ 3/3 (`coder`/`gapfill`/`coder`) |
| 4 | Visibility | History screenshot description + diff row count | ⚠️ API only (`1` row, `3` diff entries) |
| 5 | Rollback safety | revert commit + "list still N rows" + both SHAs | ✅ `4904bc2d…` · list still `1` row |
| 6 | Loud failure | cancelled row (`status`, `commitSha: null`) | ✅ `972856df…` cancelled · null |
| 7 | Reproducible | this table itself, dated | ✅ dated 2026-11-21 |

Recorded `DEV_STATUS` line (already applied):

```text
- **M0 slice v0 (D9) — executed 2026-11-21 on PRODUCTION.** run `2dc6aa64…`
  succeeded (commit `eaf402b5…`), 3 files; revert `4904bc2d…`; attribution 3/3
  file rows carry author_agent; rollback left the list at 1 row; cancelled run
  `972856df…` → status=cancelled, commitSha=null. Boundary ON,
  VIBECODER_STORE_KV_ID=56d249930a4447ee92493a7ff061a3ae (the Edge's store).
  Criterion 2 partial (no second account); criterion 4 API-level only.
```

## Failure playbook

| Symptom | Likely cause | Action |
|---|---|---|
| owner gets `401` | SPA session token stale after the boundary came on | re-sign in, copy the fresh token into `SESSION`, repeat Step 6 |
| the second account gets `200` | boundary not enforcing | check `CONTROL_PLANE_REQUIRE_SESSION=true` and `VIBECODER_STORE_KV_ID` on the staging service, restart it, repeat §0 — do not continue |
| a succeeded run has `commitSha: null` | no D1 client or no baseline (`commitGeneration` is a no-op) | check `D1_DATABASE_ID` in `.env` (run `bun run d1:token`) and the control-plane log line `git: generation commit failed` |
| the diff lists every file | baseline lost (Redis-less run, snapshot evicted) | note it as a finding — the commit still carries the full tree, so rollback works, but the diff is not a diff |
| rollback answers `409` | that generation has no internal revision | pick a run whose row shows a `commitSha` |
| rollback returns `applied: false` | no live room (browser tab closed) | reopen the chat, wait for the room, retry — the working set is restored then |
| rollback answers `502` | repo unusable (D1 credentials/tables) | check the control-plane log and the D1 read-back from §0 |

## What a green run does NOT prove

Restating the slice's own boundary so a green table is not oversold: this proves
the **platform loop** under governance for one real user. It does **not** prove
product-market fit (O1), does not exercise agent **authoring** (the next slice),
does not touch multi-agent composition (frozen by D8), and says nothing about
cost or quality metrics.


