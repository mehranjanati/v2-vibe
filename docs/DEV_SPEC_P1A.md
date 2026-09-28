# SPEC P1A — P1.0 تا P1.5 (lineage + Git داخلی + mirror + History)

## P1.0 — تمیزکاری ریپو (پیش‌نیاز همه)

- هدف: ۵۴۳ تغییر روی `main` به PR تمیز تبدیل شود.
- مراحل: `git fetch github origin` → برانچ `chore/phase1-cleanup` → کامیت منطقی چندتایی (team.go/skills/WS/checklist جدا) → push → PR به `github/main` → rebase روی آخرین main → مرج.
- تست: P1.0.3: پس از مرج `cd backend && go vet ./... && go test ./...` + روت `bun run typecheck && bun run lint && bun run build` — هر ۵ سبز، وگرنه revert.
- P1.0.4: در تمام کامیت‌ها `git add -A` روی ریشه نزن؛ `.dev.vars*`، `.prod.vars`، `.env*`، `.wrangler/`، `dist/` و `backend/dist/` باید کامیت‌نشده بمانند (در `.gitignore` هستند ✅).

## P1.0.0 — رفع ابزار D1 (پیش‌نیاز P1.1، P2.1، P5.1) — ✅ انجام شد (۲۰۲۶-۰۹-۲۳)

- مشکل اولیه: `bun run db:migrate:local` fail می‌شد — ۱) wrangler به Node ≥۲۲ نیاز دارد و shell روی `v20.19.0` است؛ ۲) اسکریپت‌ها `--config` نداشتند در حالی که فقط `wrangler.v2.jsonc` وجود دارد؛ ۳) نام DB در اسکریپت `vibesdk-db` بود ولی نام واقعی `v2-vibe` است.
- **رفع انجام‌شده** (فقط `package.json`):
  - `db:migrate:local` → `bun --bun wrangler d1 migrations apply v2-vibe --local --config wrangler.v2.jsonc`
  - `db:migrate:remote` → `CI=true bun --bun wrangler d1 migrations apply v2-vibe --remote --config wrangler.v2.jsonc`
  - نکته: `bun --bun` نسخهٔ Node را `v24` گزارش می‌کند ⇒ چک engines رد می‌شود (همان الگویی که `cf-typegen` از قبل استفاده می‌کرد). نیازی به نصب Node ۲۲ نیست.
- **اعتبارسنجی‌شده**: `bun run db:generate` → «No schema changes, nothing to migrate» (بدون drift). `D1_DATABASE_ID` در `.env` با `database_id` در `wrangler.v2.jsonc` **یکسان** است ✅. `vite.config.ts` هم `configPath: 'wrangler.v2.jsonc'` می‌دهد، پس dev server و migration روی یک DB لوکال می‌نشینند.
- **وضعیت D1 تولید (کلودفلر) — checked**: `d1_migrations` شامل **هر ۱۱ مایگریشن** است (۰۰۰۰ تا ۰۰۱۰، هر دو `0006`) و DB **۲۹ جدول** دارد (شامل `workflow_dags`/`workflow_instances`/`workflow_step_logs`/`github_tokens`). پس روی production چیزی برای apply نمانده.
- ⚠️ **محدودیت محیطی (مهم):** مسیر `--local` به **workerd** نیاز دارد و workerd روی macOS < ۱۳.۵ **hard-fail** می‌دهد (`Unsupported macOS version … 12.6.0`). یعنی روی این ماشین اجرای local migrate ممکن نیست؛ باید در Linux/CI/DevContainer اجرا شود. به همین دلیل `vite.dev.config.ts` (بدون `@cloudflare/vite-plugin`) وجود دارد.
- ⚠️ **گپ P1.1/P1.2 — تصمیم (الف) و اجرا ✅ (۲۰۲۶-۰۹-۲۳):** توکن D1 کنترل‌پلین با `bun run d1:token` (`scripts/sync-d1-token.ts`) تأمین می‌شود — توکن تازهٔ OAuth از `wrangler login` خوانده، با `SELECT 1` روی D1 اعتبارسنجی و در `.env` ریشه نوشته می‌شود (بدون هیچ کلیک در داشبورد). تأیید سرتاسری: `docker compose up -d backend` و env کانتینر از `len=0` به `len=93` رسید و D1 از همان توکن جواب می‌دهد. `backend/.env` و `.env.example` از placeholderهای `your-cloudflare-*` پاک شدند و `backend/cmd/main.go` حالا `.env` ریشه را هم می‌خواند (`envFiles := {".env", "../.env", "backend/.env"}`). توکن `AI_GATEWAY_API_KEY` موجود (`cfut_…`) تست شد و به D1 دسترسی ندارد (خطای 7403).
  - توکن OAuth عمر کوتاه دارد (expiry در output اسکریپت) ⇒ قبل از هر اجرای طولانی کنترل‌پلین یک بار `bun run d1:token` بزن. برای ماندگاری: یک بار Custom token با `Account → D1 → Edit` بساز و دستی در `.env` بگذار (اسکریپت با `--force` فقط در صورت نیاز جایگزین می‌کند).
  - اثر جانبی آگاهانه: وقتی مقدار `CLOUDFLARE_API_TOKEN` در `.env` ست باشد، Wrangler هم به‌جای لاگین مرورگری/OAuth از همان توکن استفاده می‌کند — این برای `db:migrate:remote` کاملاً کافی است، ولی `wrangler deploy` علاوه بر D1 به دسترسی‌های Workers Scripts:Edit + KV + R2 + Workers AI + Workflows هم نیاز دارد.
  - `CLOUDFLARE_API_TOKEN=` خالی = رفتار قبلی (OAuth برای wrangler، خطای صریح `d1: accountID, apiToken and databaseID are required` برای Go)؛ پس تا قبل از جای‌گذاری، هیچ‌چیز نمی‌شکند.
  - تست پذیرش بعد از جای‌گذاری (فقط-خواندنی — توکن را چاپ نمی‌کند):
    `curl -s -X POST "https://api.cloudflare.com/client/v4/accounts/$CLOUDFLARE_ACCOUNT_ID/d1/database/$D1_DATABASE_ID/query" -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" -H 'Content-Type: application/json' --data '{"sql":"SELECT 1 AS ok"}'` → باید `{"success":true,...}` بدهد.
- تست پذیرش:
  - روی محیط پشتیبانی‌شده: `bun run db:migrate:local` دو بار پشت‌سرهم بدون خطا (idempotent) + `SELECT name FROM sqlite_master WHERE type='table'`.
  - روی macOS قدیمی (این ماشین) معادل فقط-خواندنی: `bun --bun wrangler d1 migrations list v2-vibe --remote --config wrangler.v2.jsonc` → «No migrations to apply!» و `bun --bun wrangler d1 execute v2-vibe --remote --config wrangler.v2.jsonc --json -y --command "SELECT name FROM sqlite_master WHERE type='table'"`.

## P1.1 — جدول‌های lineage در D1

- هدف: سه جدول `generations` (`id/chat_id/parent/commit_sha/branch/fork/verdict/status/created_at`)، `generation_files` (`generation_id/path/op/before_hash/after_hash/before_size/after_size/author_agent`)، `generation_audits` (`generation_id/actor/action/detail_json/created_at`) + ایندکس `(chat_id, created_at)`.
  - نام‌گذاری جمع است تا با کنوانسیون فعلی `worker/database/schema.ts` یکسان بماند (`workflow_dags`، `workflow_instances`، `apps`، `audit_logs`). اگر ترجیح می‌دهی audit نسل‌ها در `audit_logs` موجود (`worker/database/schema.ts:475`) بنشیند، همان را در P1.2/P1.4 جایگزین کن و جدول سوم را نساز.
- فایل‌ها: ویرایش `worker/database/schema.ts` → تولید مایگریشن با `bun run db:generate` و **نام فایل را از خروجی همان دستور بردار** (شمارهٔ `0011` را حدس نزن: `migrations/meta/_journal.json` دو ورودی `0006` تکراری دارد و `0009` ندارد).
- مراحل: **P1.0.0 کامل شود** → تعریف در schema → generate → بازبینی SQL دستی (down-migration مخرب نباشد) → `bun run db:migrate:local` (نسخهٔ اصلاح‌شدهٔ P1.0.0).
- تست: migrate روی لوکال بدون خطا؛ `SELECT` از هر سه جدول؛ migrate دوباره idempotent باشد (دو بار اجرا = بدون خطا).

## P1.2 — `backend/pkg/engine/generation.go`

- پیش‌نیاز مسیر D1 — تصمیم (الف) اعمال شد: توکن در `.env` (`CLOUDFLARE_API_TOKEN`) می‌نشیند و `docker-compose.yml` همان را به بک‌اند می‌رساند (اجرای مستقیم باینری هم از `godotenv` می‌خواند). بدون مقدار، `D1Client` با خطای صریح `d1: accountID, apiToken and databaseID are required` fail می‌کند (تست curl پذیرش در P1.0.0). پس acceptance اول P1.2 = پس از جای‌گذاری توکن، این خطا دیگر دیده نشود.
- هدف: رکورد lineage بدون شکستن generation (best-effort: خطای lineage هرگز run را fail نکند).
- API: `StartGenerationRecord(ctx, chatID) (genID string, err error)` — snapshot کل VFS به Redis `vfs:snap:{genID}` + ردیف running + parent = آخرین succeeded همین چت. `FinishGenerationRecord(ctx, genID, verdict)` — diff snapshot↔VFS فعلی → ردیف‌های `generation_files` + بستن status + ردیف‌های audit.
- جزئیات: هش sha256 (کتابخانه استاندارد)؛ گارد nil-Redis (مثل `loadVFS`)؛ TTL ۷ روز روی `vfs:snap:*`؛ audit در goroutine detached.
- تست (`generation_test.go`): (۱) دو record پشت‌هم → parent دومی = اولی؛ (۲) create/modify/delete درست تشخیص داده شود؛ (۳) Redis=nil → هر دو تابع بدون خطا و generation زنده.

## P1.3 — قلاب اجرا + Git داخلی

- P1.3.0: تصمیم کتبی persistence (volume/R2-remote/D1-CAS) در همین فایل اسپک ثبت شود؛ بدون تصمیم، P1.3.4 شروع نشود.
- P1.3.1/3.2: اول `runTeam` و `runDualModelPipeline` → `StartGenerationRecord`؛ آخر (قبل از finalize) → `FinishGenerationRecord` با verdict واقعی.
- P1.3.3: `author` باید آرگومان **صریح** نوشتن باشد، نه چیزی که داخل wrapper استنتاج شود: امضای `UpsertFile/DeleteFile` (`backend/pkg/engine/room.go:606,623`) پارامتر `author` بگیرد و همهٔ سایت‌های نوشتن پرش کنند — `(*notifyWriteTool).InvokableRun` (`backend/pkg/engine/plan_execute.go:68`؛ توجه: `notifyWriteTool` یک type است، `backend/pkg/engine/plan_execute.go:63`) برای `coder`، `roomVFSStore.VFSWrite/VFSDelete` (`backend/pkg/engine/plan_execute.go:28,43`)، `backend/pkg/engine/gapfill.go:64,236,387` → `gapfill`، `backend/pkg/engine/dual_model.go:229,278,330` و `backend/pkg/engine/room.go:1078` → سازندهٔ همان مسیر. دلیل: gapfill هرگز از `notifyWriteTool` عبور نمی‌کند، پس انتساب درون wrapper نمی‌تواند `gapfill` را تشخیص دهد.
- P1.3.4: `go-git` به `go.mod` + `backend/pkg/engine/gitrepo.go`: `Init/Open/Commit(msg, author)/Log/Diff/Revert` per-appId روی bare repo مسیر تصمیم P1.3.0. پیام کامیت: `[gen:{id}] {goal} | verdict:{v} | author:{a}`.
- P1.3.5: **فقط** `finalizeGeneration` یک کامیت batch روی `gen/{genId}` می‌زند؛ `UpsertFile/DeleteFile` فقط `author` را برای intake ثبت می‌کنند (کامیت جدا ممنوع — تست انفجار کامیت پایین).
- P1.3.6: pre-commit: regex `(?i)(token|secret|api_key|password)\s*[:=]` → بلاک + خطا؛ فایل >۱MB → skip با warning + ردیف audit؛ `author` خالی → خطا.
- تست (`gitrepo_test.go`): (۱) دو generation → دو SHA متفاوت با parent درست (`git log` برانچ)؛ (۲) ۱۰ write در یک generation → دقیقاً ۱ کامیت؛ (۳) محتوای secret → کامیت بلاک؛ (۴) فایل ۲MB → skip؛ (۵) revert → کامیت جدید، تاریخ قبلی سر جاش.

## P1.4 — API تاریخچه + mirror

- مسیر Change Path (معماری **dual-plane** — مرجع: `docs/llm.md:5-28` (`#current-architecture`)): این اندپوینت‌ها به **Go control plane** تعلق دارند، چون generation و VFS همان‌جا تولید می‌شود.
  1. `src/api-types.ts` (مدل `Generation` + `commit_sha`/`branch`/`fork`)
  2. `src/lib/api-client.ts` — از طریق `controlPlane.baseUrl` (`src/config/api.ts`)، نه فرض same-origin
  3. هندلرها: `backend/pkg/api/generations.go`
  4. ثبت روت در `backend/pkg/api/routes.go` (`app.Get` / `app.Post`)
  5. D1 از طریق `backend/pkg/cloudflare/d1.go` (`Query` / `Exec`)
  - ⛔ مسیرهای `worker/database/services/`، `worker/api/routes/` و `worker/app.ts` **در این repo وجود ندارند** و `worker/api/controllers/**` هم در این بیلد import نمی‌شود. تنها اگر اندپوینتی باید روی لبه باشد، فایل `worker/light/lightApp.ts` داخل `buildLightApp()` است (قبل از fallback SPA).
- قانون نویسندهٔ واحد D1: writer همین مسیر باشد؛ light Worker هم binding `DB` دارد (`worker/types/bindings.ts`) — دو نویسنده روی یک جدول نگذار.
- اندپوینت‌ها: `GET /api/generations/:chatId` (لیست)؛ `GET /api/generations/:chatId/:genId/diff` (op+hash+size، نه محتوا)؛ `POST /api/generations/:chatId/:genId/rollback` (revert-commit + ردیف rollback)؛ P1.4.5 mirror با Git Data API (tree→commit→ref، یک کامیت اتمی، ذخیره `last_pushed_sha`)؛ P1.4.7 import (diff با HEAD → fast-forward یا fork).
- تست: (۱) typecheck سبز؛ (۲) unit سرویس با D1 لوکال: لیست/diff/rollback؛ (۳) E2E P1.7.4: push → دقیقاً ۱ کامیت در ریپوی تست؛ PR → body شامل goal+verdict+فایل‌ها؛ (۴) round-trip: VFS→Git→VFS بایت‌به‌بایت یکسان؛ (۵) توکن نامعتبر → generation داخلی موفق + فلگ mirror-pending.

## P1.5 — فرانت History

- P1.5.1: تب History در چت: لیست generationها با badge (approve/request_changes/done/error) + `commit_sha` کوتاه + branch.
- P1.5.2: نمای diff: هر فایل `path + op + before→after size + hash کوتاه`.
- P1.5.3: Rollback با modal confirm → بعدش refresh لیست + بنر «برگشته به {genId}».
- P1.5.4: اگر `mirror_pending` → بنر زرد «mirror pending» + دکمه Retry-push.
- تست: (۱) `bun run typecheck && bun run lint`؛ (۲) تست دستی سناریو: دو generation → دو ردیف با parent → diff دومی فقط فایل تغییریافته → rollback → VFS = نسل اول + ردیف سوم rollback.
