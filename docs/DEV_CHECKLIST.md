# DEV CHECKLIST — V2 Vibe (Multi-Agent + Node Catalog + Git-Base)

> مرجع زنده توسعه. تسک انجام‌شده: `- [ ]` → `- [x]`.
> ترتیب اجرا: P0 → P1 (شامل P1-git، lineage و P1.10) → P5 (پیش‌نیاز موج ۱ فروش، بعد از P1-git و P1.10، قبل یا موازی P2) → P2 → P6 (ادیتور ورکفلو n8n-like) → P3 → P4.
> مشخصات اجرایی + تست هر تسک: `docs/DEV_TASKS_SPEC.md` (ایندکس) ← `docs/DEV_SPEC_P1A.md` · `docs/DEV_SPEC_P1B.md` · `docs/DEV_SPEC_P2.md` · `docs/DEV_SPEC_P3P4.md` · `docs/DEV_SPEC_P5.md` · `docs/DEV_SPEC_P6.md`.
>
> **Backlog اصلاح مستندات:** `docs/DOCS_AUDIT_BACKLOG.md` (۱۶ تسک با اولویت P0–P3؛ ✅ همه انجام شد — وضعیت زنده و معیار پذیرش هر تسک همان‌جاست).


## P0 — تثبیت شده ✅

- [x] P0.1 — تیم Multi-Agent (Coordinator/Coder/Reviewer با AgentAsTool)
  - [x] P0.1.1 — `backend/skills/00_coordinator.md` + `03_reviewer.md`
  - [x] P0.1.2 — `backend/pkg/engine/team.go`
  - [x] P0.1.3 — رجیستری skills + env override
  - [x] P0.1.4 — سیم‌کشی hub/room/cmd + fallback تک-coder
  - [x] P0.1.5 — ایونت‌های WS (team_started/activity/completed)
  - [x] P0.1.6 — فرانت handler + typecheck/lint سبز
  - [x] P0.1.7 — تست‌ها سبز + `go vet/build` سبز

## P1 — قدم A: Branch-per-Generation + Diff قابل Review

- [ ] P1.0 — تمیزکاری ریپوی پلتفرم (اندازه‌گیری ۲۰۲۶-۰۹-۲۸ روی `main`: ۵۷۲ ورودی `git status --porcelain` = ۳۸۲ حذف + ۶۶ اصلاح + ۹۳ افزودن + ۳۱ untracked)
  - [x] P1.0.0 — رفع ابزار D1 ✅ (۲۰۲۶-۰۹-۲۳): اسکریپت‌ها به `bun --bun wrangler d1 migrations apply v2-vibe --{local,remote} --config wrangler.v2.jsonc` اصلاح شد؛ `db:generate` بدون drift؛ D1 تولید هر ۱۱ مایگریشن + ۲۹ جدول دارد. ⚠️ مسیر `--local` روی macOS <۱۳.۵ اجرا نمی‌شود (workerd) → در CI/DevContainer اجرا شود. جزئیات: `docs/DEV_SPEC_P1A.md`
  - [x] P1.0.1 — ✅ (۲۰۲۶-۰۹-۲۸) روی برنچ `chore/p1.0-repo-cleanup` در ۳ کامیت: (۱) `feat(phase-1): commit multi-agent engine, workflow API and dual-plane wiring`، (۲) `chore: remove retired worker/agents, space and container surfaces` = ۳۷۲ حذف خالص (worker/agents ۱۴۵ + worker/services ۷۳ + worker/api ۵۸ + worker/utils ۲۵ + worker/database ۱۲ + worker/middleware ۴ + worker/logger ۳ + worker/config ۲ + worker/types ۱ + worker/observability ۱ + `worker/app.ts` + `space/` ۳۴ + `container/` ۱۱ + `SandboxDockerfile` + `scripts/deploy.ts`) به‌علاوه‌ٔ ۱۰ ماژول تایپ که با rename به `worker/types/` رفتند، (۳) `docs: track the spec set, CF limits, audit backlog and archive` = کل `docs/**`
  - [x] P1.0.1b — ✅ (۲۰۲۶-۰۹-۲۸) `scripts/validate-spec-refs.mjs` + `scripts/validate-postman.mjs` + `scripts/postman-route-contract.json` + `scripts/run-tests.mjs` در کامیت اول همان PR کامیت شدند و `docs/DOCS_AUDIT_BACKLOG.md` + `docs/archive/**` در کامیت مستندات آمدند (کلون/CI تازه `docs:check` و `test` را دارد)
  - [x] P1.0.2 — ✅ (۲۰۲۶-۰۹-۲۸) PR #1 به `github/main` (https://github.com/mehranjanati/v2-vibe/pull/1) پس از سبز شدن هر دو job (`ci` + `go-test`) با مرج تمیز شد — merge commit `22a5cb3`
  - [x] P1.0.3 — ✅ (۲۰۲۶-۰۹-۲۸) CI روی `main` پس از مرج سبز شد (jobهای `ci` و `go-test` روی `22a5cb3`)؛ تأیید CI + بیلد سبز پس از مرج (`go vet ./... && go test ./...` + `typecheck/lint/build`) — یادداشت: jobهای گیت `lint`/`typecheck`/`test-build`/`go-test` از T15 وجود دارند (در `ci.yml` و در هر دو ورک‌فلوی دیپلوی که `deploy` به آن‌ها وابسته است)؛ (`docs/DOCS_AUDIT_BACKLOG.md` ← T15) + گیت‌های لوکال ۲۰۲۶-۰۹-۲۸: `typecheck`/`lint` (۰ error، ۳ warning)/`build`/`go vet ./...`+`go test ./...`/`docs:check` سبز؛ `bun run test` روی macOS ۱۲.۷.۶ اجرا نمی‌شود (workerd ≥ ۱۳.۵) ⇒ فقط در CI تأیید شود
  - [ ] P1.0.4 — هیچ‌وقت `.dev.vars*`/`.prod.vars`/`.env*`/`.wrangler/`/`dist/` را کامیت نکن
  - [x] P1.0.5 — `CLOUDFLARE_API_TOKEN` ✅ (۲۰۲۶-۰۹-۲۳): با `bun run d1:token` (اسکریپت جدید `scripts/sync-d1-token.ts`) توکن OAuth تازه از wrangler خوانده، با `SELECT 1` روی D1 اعتبارسنجی و در `.env` ریشه نوشته می‌شود؛ سپس کانتینر `vibesdk-backend` بازسازی شد و env آن تأیید شد (`len=93`) — قبلاً `len=0` بود. همچنین `backend/.env`/`.env.example` از placeholderها پاک شد و `backend/cmd/main.go` حالا `.env` ریشه را هم می‌خواند. برای توکن ماندگار: یک بار Custom token با `Account → D1 → Edit` بساز و دستی جای OAuth بگذار.

- [ ] P1.1 — D1: جدول‌های lineage (+ ستون Git)
  - [ ] P1.1.1 — `generation`/`generation_files`/`generation_audit` در `worker/database/schema.ts`
  - [ ] P1.1.1b — ستون‌های `commit_sha` + `branch` + `fork` در `generation` (اتصال lineage↔Git، بدون این دوطرفه نمی‌شود)
  - [ ] P1.1.2 — `migrations/0011_generation_lineage.sql` via `bun run db:generate`
  - [ ] P1.1.3 — `bun run db:migrate:local` + تست سبز
- [ ] P1.2 — Go: `backend/pkg/engine/generation.go`
  - [ ] P1.2.1 — `StartGenerationRecord` (snapshot `vfs:snap:{genID}` + parent)
  - [ ] P1.2.2 — `FinishGenerationRecord` (diff + verdict + audit، best-effort)
  - [ ] P1.2.3 — هش sha256 + گارد nil-Redis + TTL ۷ روز
  - [ ] P1.2.4 — `generation_test.go` (parent، diff، nil-Redis)
- [ ] P1.3 — قلاب اجرا (+ Git داخلی)
  - [ ] P1.3.0 — تصمیم persistence گیتی (پیش‌نیاز P1.3.4): volume ماندگار vs R2-remote per-app vs D1-CAS — بدون این، bare repo روی محیط ephemeral یتیم می‌ماند
  - [ ] P1.3.1 — قلاب در `runTeam` (`team.go`)
  - [ ] P1.3.2 — قلاب در `runDualModelPipeline` (`dual_model.go`)
  - [ ] P1.3.3 — `author_agent` در `notifyWriteTool`
  - [ ] P1.3.4 — Git داخلی Go: `go-git` در `go.mod` + `backend/pkg/engine/gitrepo.go` (init/open/commit/log/diff/revert per-appId، bare repo در `/data/git/{appId}.git`)
  - [ ] P1.3.5 — قلاب Git در `finalizeGeneration` (کامیت batch یکتا روی `gen/{genId}`) + پارامتر `author` در `UpsertFile/DeleteFile` برای intake (ثبت، نه کامیت جدا — وگرنه انفجار کامیت روی چانک‌ها)
  - [ ] P1.3.6 — pre-commit secret-scan (token|secret|api_key) + سقف حجم per-file (۱MB، skip لگسی) + `author` اجباری
- [ ] P1.4 — API تاریخچه + diff + rollback (+ mirror به GitHub)
  - [ ] P1.4.1 — تایپ‌ها `src/api-types.ts` (+ `commit_sha`/`branch` در مدل generation)
  - [ ] P1.4.2 — متدها `src/lib/api-client.ts` (+ push/PR/import)
  - [ ] P1.4.3 — هندلر Go: `backend/pkg/api/generations.go` + دسترسی D1 از `backend/pkg/cloudflare/d1.go` (نه `worker/database/services/` که وجود ندارد)
  - [ ] P1.4.4 — ثبت روت در `backend/pkg/api/routes.go` (+ تایپ/متد در `src/api-types.ts` و `src/lib/api-client.ts`، از طریق `controlPlane.baseUrl`)
  - [ ] P1.4.5 — mirror اتمی: جایگزینی `pushFiles` تکی با Git Data API (tree→commit→ref) — یک generation = یک کامیت اتمی؛ ذخیره `last_pushed_sha`
  - [ ] P1.4.6 — بنر «Push / ساخت PR» با diff summary بعد از `team_completed` (base=main، head=gen/{id}، body خودکار)؛ اسکوپ توکن `repo` برای PR
  - [ ] P1.4.7 — endpoint import (GitHub → VFS): diff با HEAD داخلی → fast-forward تمیز / fork + بنر تعارض؛ `author=import`
- [ ] P1.5 — فرانت History (+ SHA و بنر پوش)
  - [ ] P1.5.1 — تب History با verdict badge + نمایش `commit_sha`/branch
  - [ ] P1.5.2 — نمای diff summary (op + size + hash)
  - [ ] P1.5.3 — Rollback با confirm + refresh بعد از `team_completed` (revert-commit جدید، نه rewrite تاریخ)
  - [ ] P1.5.4 — حالت mirror pending: قطع اینترنت/توکن → generation داخلی موفق + بنر «mirror pending»
- [ ] P1.6 — اتصال reviewer به baseline + hub-awareness (Co-Coder)
  - [ ] P1.6.1 — `03_reviewer.md` (diff نسبت به نسل قبلی)
  - [ ] P1.6.2 — تزریق `changed_files` در `team.go`
  - [ ] P1.6.3 — hub-awareness: شناسایی hub-fileها (پرریفرنس‌ترین فایل‌ها از روی plan/VFS) + اعلام blast-radius در task reviewer
- [ ] P1.7 — اعتبارسنجی P1
  - [ ] P1.7.1 — `go vet/test` سبز (شامل `gitrepo_test.go`: generation → SHA، rollback → revert-commit)
  - [ ] P1.7.2 — `bun run typecheck/lint/build` سبز
  - [ ] P1.7.3 — E2E: دو generation → دو SHA روی دو برانچ با parent درست → diff → rollback
  - [ ] P1.7.4 — E2E پوش: یک کامیت اتمی در ریپوی کاربر + PR با body خودکار؛ round-trip (VFS → Git → VFS یکسان؟)
- [ ] P1.8 — difficulty gate برای `canRunTeam` (DATS)
  - [ ] P1.8.1 — classifier سبک: تعداد steps + چگالی وابستگی از روی VFS/plan (≤۲ فایل و بدون وابستگی → تک-coder، وگرنه تیم)
  - [ ] P1.8.2 — تست gate (آسان→تک، سخت→تیم) + لاگ تصمیم در audit
- [ ] P1.9 — ابزار `vfs_claim` (AgentRoom: claim/status/release)
  - [ ] P1.9.1 — `claim`/`release`/`status` در `teamTools` (فقط سیگنال مالکیت، نه اشتراک history)
  - [ ] P1.9.2 — coordinator: قبل از delegate به coder، claim بگیرد؛ بعد از اتمام release
  - [ ] P1.9.3 — تست برخورد (دو claim همزمان روی یک path → دومی صف/خطا)
- [ ] P1.10 — قرارداد «تولید شناسنامه‌دار» + خط لوله write دستی (۶ دروازه)
  - [ ] P1.10.1 — قرارداد coder: `data-vibe-block/section/id/slots` در `02_coder.md` + ولیدیتور Go (Suspense ترمیم: id یکتا، block از plan)
  - [ ] P1.10.2 — استخراج `vibe.meta.json` موقع finalize (لیست id/block/section/slots، بدون پارس HTML در فرانت)
  - [ ] P1.10.3 — G0 intake: پارامتر `author` در `UpsertFile/DeleteFile` (با P1.3.5 یکی شود، جدا نزن)
  - [ ] P1.10.4 — G1 syntax: pass جدید با acorn در `preview-normalize` (امروز فقط regex است) + htmlparser2/jsonc + گسترش `SanitizeJS` (status=broken + خط دقیق، ولی ذخیره کن؛ با P3.1 یکی شود، جدا نزن)
  - [ ] P1.10.5 — G2 identity: `pkg/design/identity.go` + چک reviewer (نباشد → unmanaged، نه خطا)
  - [ ] P1.10.6 — G3 wiring: missing-ref از `resolve()` موجود + بنر فرانت + micro-fix صفر-توکن
  - [ ] P1.10.7 — G4 conflict: فلگ `fork=true` در lineage (با P1.1.1b یکی است) + بنر «مال من/مال ایجنت»
  - [ ] P1.10.8 — G5 preview: banner قرمز/زرد/خاکستری + «برگرد به آخرین سالم» + «با AI درست کن»
  - [ ] P1.10.9 — Monaco editable + Save صریح (readOnly برداشته شود، lazy بماند؛ نه autosave)

## P2 — قدم ۳: کاتالوگ «نود-به‌عنوان-پکیج»

- [ ] P2.1 — قرارداد + ذخیره‌سازی
  - [ ] P2.1.1 — manifest نود (`name@version` + `kind` + `paramsSchema` + `credentials`)
  - [ ] P2.1.2 — جدول `node_packages` + `0012_node_packages.sql`
  - [ ] P2.1.3 — کش KV (`nodepkg:{name}@{version}`)
- [ ] P2.2 — Go: ولیدیشن رجیستری-آگاه
  - [ ] P2.2.1 — `backend/pkg/engine/nodepkg.go`
  - [ ] P2.2.2 — `Validate(wf, registry)` در `workflowschema.go`
  - [ ] P2.2.3 — تست‌ها سبز
- [ ] P2.3 — TS: dispatch جدول‌محور
  - [ ] P2.3.1 — جدول `kind → handler` در `VibeWorkflow.ts`
  - [ ] P2.3.2 — resolve manifest → validate → اجرا
  - [ ] P2.3.3 — `NonRetryableError` برای نود بد + تست
- [ ] P2.4 — API کاتالوگ
  - [ ] P2.4.1 — `GET /api/nodes` + `GET /api/nodes/:name@:version`
  - [ ] P2.4.2 — `POST /api/workflows/validate`
- [ ] P2.5 — اتصال تیم ایجنتیک
  - [ ] P2.5.1 — `02_coder.md`: تولید `node.pkg@version`
  - [ ] P2.5.2 — `03_reviewer.md`: چک کاتالوگ + credential ارجاعی
- [ ] P2.6 — ۲۰ نود curated
  - [ ] P2.6.1 — notify: email/slack/telegram
  - [ ] P2.6.2 — http: request/webhook-trigger/webhook-call
  - [ ] P2.6.3 — data: db.query/kv/queue
  - [ ] P2.6.4 — time: cron/sleep + ai: prompt/classify/extract
  - [ ] P2.6.5 — logic: branch/loop/map + premium: stripe/sheets
  - [ ] P2.6.6 — مثال درست/غلط در manifest هر نود (دو نمونه، برای coder)
- [ ] P2.7 — ایزولاسیون + امنیت نود + کوتای per-app (user_id + ماسک لاگ + MCP/ETDI)
  - [ ] P2.7.1 — اسکوپ `user_id` در کوئری‌های `workflow_*` + چک مالکیت در trigger
  - [ ] P2.7.2 — ماسک credential در لاگ (فقط ارجاع، هرگز plaintext) + Dual-LLM برای دیتای نامطمئن
  - [ ] P2.7.3 — پین ورژن + checksum manifest (ETDI) + مرز cross-server dataflow
  - [ ] P2.7.4 — کوتای per-app (سقف generation/کامیت/ران‌تایم) + تصمیم secret-storage (توکن GitHub: KV یا D1 رمزنگاری‌شده + چرخه revoke) — با P6.4 یکی شود، جدا نزن
- [ ] P2.8 — اعتبارسنجی + exit path (E2E نمونه + سایز باندل در بودجه + بیلد سبز + export باز)
  - [ ] P2.8.1 — E2E: `webhook.trigger → ai.extract → slack.postMessage` روی CF
  - [ ] P2.8.2 — سایز باندل ورکر: `wrangler deploy --outdir bundled/ --dry-run` + لاگ `Total Upload` (سقف پلتفرم ۶۴ MiB فشرده‌نشده؛ سقف ۳MiB در ۲۰۲۶-۰۹-۰۴ حذف شد — `docs/CF_LIMITS.md` §۴)
  - [ ] P2.8.3 — export کامل DAG + تاریخچه به فرمت باز (exit path کاربر)

## P3 — Suspense + AutoFix (از v0) + Circuit Breaker

- [ ] P3.1 — Suspense صفر-توکن در `notifyWriteTool` (id/src/token/tag؛ با P1.10.4 یکی شود، جدا نزن)
- [ ] P3.2 — prompt-cache-aware + RAG کاتالوگ (فقط سکشن مرتبط)
- [ ] P3.3 — مثال درست/غلط در manifest + لاگ REQUEST_CHANGES برای RFT آینده (مثال‌ها با P2.6.6 یکی است — فقط لاگ اینجا)
- [ ] P3.4 — Circuit Breaker قطعی (ZeroLabs)
  - [ ] P3.4.1 — هوک pre-persist: گارد حذف فاجعه‌بار + سقف حجم + bailout بعد از ۳ تلاش ناموفق
  - [ ] P3.4.2 — micro-pass با temperature 0 برای fix موضعی (نه rewrite کل فایل، نه patch یکپارچه)
  - [ ] P3.4.3 — تصمیم reflect درونی (ReflexiCoder) vs reviewer بیرونی: هر دو — reflect برای خطای سطحی، reviewer برای verdict
- [ ] P3.5 — کالبدشکافی‌های باز (Temporal durable execution / Dify Human-Input / Mastra runners)

## P4 — فاز ۲: Sandbox واقعی (خارج از light worker — دلیل: CPU ۱۰ms در پلن Free و نبود sandbox/container binding؛ سقف ۳MiB دیگر وجود ندارد، `docs/CF_LIMITS.md` §۴)

- [ ] P4.1 — Sandbox per-chat + Bash در همان FS (در کنترل‌پلین/کانتینر جدا، نه ورکر)
- [ ] P4.2 — Verifier واقعی + Dual-key

## P5 — App Runtime (بک‌اند per-app: جدول + API + نقش‌ها)

> چرا: بدون این، انبار/حسابداری نمایشی می‌ماند (localStorage). ERP واقعی =
> تراکنش + نقش + audit. پیش‌نیاز موج ۱ فروش.

- [ ] P5.1 — مدل داده per-app (جدول‌های ثابت + `data_json`، نه DDL پویا)
  - [ ] P5.1.1 — جدول `app_records` (`app_id`, `table_name`, `row_id`, `data_json`) + ایندکس‌ها — DDL به‌ازای هر اپ ممنوع (محدودیت D1)
  - [ ] P5.1.2 — جدول‌های واقعی جدا برای موجودی (`stock`: `app_id`+`sku` یکتا — برای decrement اتمی) + template مرجع: `products`/`movements`/`invoices` (نه hardcode)
  - [ ] P5.1.3 — مایگریشن + ایندکس (`app_id`, `sku`, `created_at`)
- [ ] P5.2 — API تراکنشی
  - [ ] P5.2.1 — CRUD تولیدشده per-table با اسکوپ `app_id + user_id`
  - [ ] P5.2.2 — decrement اتمی موجودی (`UPDATE ... WHERE stock >= qty`، نه read-then-write)
  - [ ] P5.2.3 — audit خودکار هر write (کی، چه ردیفی، قبل/بعد) در `generation_audit` یا جدول جدا
- [ ] P5.3 — نقش‌ها و دسترسی
  - [ ] P5.3.1 — نقش‌ها: `admin`/`storekeeper`/`accountant` + جدول `app_members`
  - [ ] P5.3.2 — policy per-table per-role (انباردار: انبار RW، مالی RO و بالعکس)
  - [ ] P5.3.3 — اتصال به auth موجود (JWT/session فعلی) + تست نفوذ پایه
- [ ] P5.4 — اتصال فرانت و ایجنت
  - [ ] P5.4.1 — داشبورد از API واقعی (جایگزین localStorage) + حالت آفلاین-خوانا
  - [ ] P5.4.2 — coder: تولید فرم/گزارش روی API واقعی (نه mock data)
  - [ ] P5.4.3 — reviewer: چک «دیتا از API می‌آید؟» به چک‌لیست `03_reviewer.md`
- [ ] P5.5 — ابزار مهاجرت (import اکسل/CSV با گزارش سطر خراب، نه fail کل فایل)
- [ ] P5.6 — اعتبارسنجی: سناریو فروش همزمان (دو ثبت همزمان → موجودی منفی نشود) + E2E موج ۱

## P6 — ادیتور ورکفلو n8n-like (ساخت و تغییر فلو توسط کاربر)

> چرا: زنجیره تولید → نمایش → اجرا آماده است (`workflow.json` + `WorkflowVisualizer` + `VibeWorkflow`) ولی حلقه ویرایش باز است.
> کاربر فلو را می‌بیند ولی نمی‌تواند دست بزند. ترتیب ساخت: L1 → L3 → L2.
> پیش‌نیازها: موج ۱ بدون P2 شروع می‌شود (روی ۷ تایپ فعلی)؛ موج ۲+ به P2 نیاز دارد.

- [ ] P6.1 — موج ۱: Canvas زنده روی ۷ تایپ فعلی (بدون P2)
  - [ ] P6.1.1 — `WorkflowVisualizer`: `showInteractive` روشن + `onNodeClick` → انتخاب نود + `onConnect` (چک cycle قبل از قبول) + delete (نود + edgeهای متصل + هشدار orphan)
  - [ ] P6.1.2 — پالت ۷ تایپ (`trigger/http/db/ai/email/condition/sleep`) + drag نود جدید با position + params پیش‌فرض
  - [ ] P6.1.3 — ذخیره به VFS → generation جدید (`author=user`) + lineage + کامیت Git داخلی (P1-git)
- [ ] P6.2 — L1: پنل params (فرم هر نود)
  - [ ] P6.2.1 — فرم داینامیک از روی `validateNodeParams` (بعداً: از manifest نود P2، نه hardcode)
  - [ ] P6.2.2 — ولیدیشن فرانت با همان `validateWorkflowV2` موجود در `VibeWorkflow.ts` قبل از ذخیره
- [ ] P6.3 — L3: ویرایش JSON با گارد (موازی P6.2)
  - [ ] P6.3.1 — Monaco روی `workflow.json` + schema v2 (autocomplete/error زنده)
  - [ ] P6.3.2 — ذخیره → خط لوله G1 (jsonc-parser) → invalid = draft + بنر خط دقیق (نه reject)
- [ ] P6.4 — Credential vault جنریک (پیش‌نیاز موج ۲، با P2.7 هم‌زمان)
  - [ ] P6.4.1 — جدول credentials (ارجاع، هرگز plaintext) + UI «اتصال جدید» + اسکوپ `repo` برای PR
  - [ ] P6.4.2 — ماسک در لاگ + Dual-LLM برای دیتای نامطمئن (با P2.7.2 یکی شود، جدا نزن)
- [ ] P6.5 — Test-run (اجرای آزمایشی بدون side-effect واقعی)
  - [ ] P6.5.1 — جدول سطح side-effect در manifest هر نود: `safe` (واقعاً اجرا) / `mocked` (فقط لاگ «می‌فرستادم به X») / `blocked` (در test ممنوع)
  - [ ] P6.5.2 — حالت `dryRun` در `POST /api/workflows/trigger` (validate + شبیه‌سازی، بدون commit بیرونی)
  - [ ] P6.5.3 — UI: دکمه Test با ورودی نمونه + لیست اجراها + لاگ per-step (بک `workflow_instances/step_logs` آماده است)
- [ ] P6.6 — Active toggle + trigger واقعی (موج ۴)
  - [ ] P6.6.1 — webhook URL per-workflow (`/wh/:workflowId` روی ورکر) — اول webhook، بعد cron
  - [ ] P6.6.2 — Cron Triggers ورکر + Workflows (دقت دقیقه‌ای + کوتای CF مستند شود؛ اعداد فقط از `docs/CF_LIMITS.md` §۱/§۲ — سقف طول cron expression در داک رسمی تأیید نشده و `⚠️ unverified` است، به آن عدد استناد نکن)
  - [ ] P6.6.3 — toggle Active/Inactive + گارد W3: نودهای `email/stripe/http-POST` بنر تأیید جدا («ایمیل واقعی می‌رود — مطمئنی؟»)
- [ ] P6.7 — اعتبارسنجی: سناریو «فلو را با موس عوض کن → Test → Active → webhook واقعی» + سایز باندل در بودجهٔ خودمان (`Total Upload` از `--dry-run`؛ سقف پلتفرم ۶۴ MiB فشرده‌نشده — `docs/CF_LIMITS.md` §۴)
- [ ] P6.8 — Observability سرتاسری: trace یک generation از plan تا push (اتصال `debuglog.go` + `workflow_step_logs` + lineage + SHA در یک trace-id)

## قوانین

1. تیک فقط وقتی تست/بیلد سبز شده.
2. هر P کامل شد تاریخ بزن.

| P | وضعیت | تاریخ |
|---|-------|-------|
| P0 | ✅ done | 2026-09-22 |
| P1 | ⬜ todo | — |
| P2 | ⬜ todo | — |
| P3 | ⬜ todo | — |
| P4 | ⬜ todo | — |
| P5 | ⬜ todo | — |
| P6 | ⬜ todo | — |

