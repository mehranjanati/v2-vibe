# SPEC P6 — Workflow Canvas (inspection surface) + Runtime Pieces

> ترتیب L1 → L3 → L2. موج ۱ بدون P2 (روی ۷ تایپ فعلی + `validateNodeParams`/`validateWorkflowV2` آماده).

## P6.1 — Live canvas (wave 1)

> ⚠️ تصحیح پیش‌فرض: `showInteractive` در `src/components/workflow/WorkflowVisualizer.tsx:254` prop مخصوص `<Controls>` در `@xyflow/react` است (قفل کنترل‌های zoom) و **حالت ویرایش را روشن نمی‌کند**. امروز کانواس کاملاً readonly است: `ReactFlow` بدون `onNodesChange`/`onConnect`/`onNodeClick`/`nodesDraggable` و بدون `useNodesState` (`src/components/workflow/WorkflowVisualizer.tsx:245-256`). پس P6.1 = state-محور کردن کامپوننت، نه روشن‌کردن یک flag.

- P6.1.1: `WorkflowVisualizer` را به state مدیریت‌شده ببر (`useNodesState`/`useEdgesState` + sync با `files[WORKFLOW_FILE_PATH]`)؛ سپس `onNodeClick` → انتخاب؛ `onConnect` → چک cycle (DFS روی DAG؛ cycle → reject + toast علت)؛ Delete → حذف نود + edgeها + هشدار orphan (نود بی‌ورودی/خروجی).
- P6.1.1b: هم‌ترازی تایپ‌ها قبل از ساخت پالت: `nodeTypes` فعلی فقط `trigger/function/http/db/condition` است (`src/components/workflow/WorkflowVisualizer.tsx:206-212`) — `ai`، `email`، `sleep` رندرر ندارند و `function` در مجموعهٔ بستهٔ `backend/pkg/engine/workflowschema.go:24-30` نیست. رندرر ۷ تایپ مجاز را کامل کن و `function` را حذف کن (وگرنه پالت ۷ تایپ روی کانواس ناقص می‌افتد).
- P6.1.2: پالت ۷ تایپ با drag → نود جدید `{id: uuid, position: drop-point, params: defaults[type]}` (defaults در یک فایل `src/components/workflow/node-defaults.ts`، هم‌راستا با `backend/pkg/engine/workflowschema.go` و `validateNodeParams` در `worker/workflow/VibeWorkflow.ts:312`).
- P6.1.3: ذخیره → `workflow.json` در VFS + generation جدید `author=user` + lineage (P1.1) + کامیت Git (P1.3.5).
- تست: (۱) unit cycle-check (DAG خطی ok؛ back-edge reject؛ self-loop reject)؛ (۲) unit: هر ۷ تایپ مجاز رندرر دارد و `function` دیگر نودی نمی‌سازد؛ (۳) دستی: drag→connect→delete→save→History ردیف جدید.

## P6.2 — Params panel (L1)

- P6.2.1: فرم داینامیک از `validateNodeParams` (`worker/workflow/VibeWorkflow.ts:312`) هر تایپ (فیلدها: text/number/select/secret-ref)؛ بعد از P2: از manifest نود (بدون تغییر کامپوننت — فقط datasource عوض شود).
- P6.2.2: قبل از ذخیره `validateWorkflowV2` (`worker/workflow/VibeWorkflow.ts:402`) اجرا شود؛ خطا → فیلد قرمز + پیام، ذخیره بلاک (نه silent).
- تست: typecheck + دستی هر ۷ تایپ (فرم باز → ذخیره → validate سبز).

## P6.3 — JSON editing (L3, parallel)

- P6.3.1: Monaco روی `workflow.json` + JSON-schema v2 (از روی `backend/pkg/engine/workflowschema.go` آینه شود) برای autocomplete/error.
- P6.3.2: ذخیره → `jsonc-parser`؛ invalid → ذخیره به‌عنوان draft (`workflow.draft.json`) + بنر با `{line, msg}`؛ valid → جایگزین اصلی + generation جدید.
- تست: JSON خراب → draft + بنر دقیق؛ JSON سالم → اعمال + lineage.

## P6.4 — Credential vault (with P2.7)

- P6.4.1: جدول `credentials(id, user_id, provider, ref_encrypted, created_at)` — فقط ارجاع رمزنگاری‌شده، هرگز plaintext؛ UI «اتصال جدید» (Slack/Telegram/Stripe/GitHub) + اسکوپ `repo` برای PR.
- P6.4.2: با P2.7.2 یکی شود (جدا نزن).
- تست: ساخت credential → در D1 فقط ciphertext؛ resolve در runtime → مقدار درست؛ revoke → استفاده بعدی fail.

## P6.5 — Test-run

- P6.5.1: فیلد `sideEffect` در manifest (P2.1): `safe` اجرا واقعی / `mocked` فقط لاگ `«[dry-run] می‌فرستادم به X با {...}»` / `blocked` در test خطا.
- P6.5.2: `POST /api/workflows/trigger` **از قبل وجود دارد** (`backend/pkg/api/routes.go:314` → `backend/pkg/api/workflows.go`, `handleWorkflowTrigger`) — همان هندلر را با `{dryRun: true, sampleInput}` گسترش بده، روت جدید نساز. رفتار: validate + اجرای شبیه‌سازی بدون commit بیرونی؛ نتیجه `{steps: [{node, status, mocked, output}]}`.
- P6.5.3: UI: دکمه Test + فرم ورودی نمونه + لیست اجراها + لاگ per-step (خواندن از `workflow_instances/step_logs` موجود — بک جدید نزن).
- تست: (۱) فلو با email-mocked در dryRun → صفر ایمیل واقعی + لاگ mocked؛ (۲) نود blocked در dryRun → خطای مشخص؛ (۳) دستی: Test → Active.

## P6.6 — Active + real triggers (wave 4)

- P6.6.1: `POST /wh/:workflowId` روی ورکر (Hono route جدید در `worker/light/lightApp.ts` داخل `buildLightApp()` — **قبل از** fallback `app.all('*')` در `worker/light/lightApp.ts:920` ثبت شود؛ بودجهٔ سایز را از `docs/CF_LIMITS.md` §۴ بردار — سقف پلتفرم امروز **۶۴ MiB فشرده‌نشده** است، نه ۳MiB؛ برای `/api/*` ناشناس هم catch-all موجود در `worker/light/lightApp.ts:906` از قبل JSON ۴۰۴ می‌دهد، نه HTML اپلیکیشن — اصلاح T1 در `docs/DOCS_AUDIT_BACKLOG.md`): احراز (secret per-workflow در header) → اعتبارسنجی بدنه (**≤ ۱ MiB** طبق CF_LIMITS §۳؛ بزرگ‌تر → ۴۱۳) → ساخت instance → اجرا. اول webhook، بعد cron.
- P6.6.2: Cron Triggers + Workflows — اعداد را از **`docs/CF_LIMITS.md` §۱ و §۲** بردار (نه از حافظه): تایم‌زون UTC؛ کوچک‌ترین بازه **۱ دقیقه**؛ سقف **۵ per account (Free) / ۲۵۰ (Paid)**؛ انتشار تغییرات تا **۱۵ دقیقه**؛ wall time **۱۵ دقیقه**؛ CPU **۱۰ms (Free)**. مسیر ترجیحی برای فلوهای زمان‌بندی‌شده `schedules` خودِ Workflow است (**۱۰۰ schedule/account**، بودجهٔ ۱ ساعته در Paid؛ سقف طول expression در داک رسمی تأیید نشده و در `CF_LIMITS.md §۲` علامت `⚠️ unverified` خورده است — به سقف فرضی استناد نکن). دقت اجرا تضمین‌شده نیست ⇒ **idempotency + پنجرهٔ تحمل** الزامی است.
- P6.6.3: toggle Active/Inactive per-workflow (غیرفعال → webhook 410) + گارد W3: ذخیره/فعال‌سازی فلو با نود `email/stripe/http-POST` → modal تأیید جدا («ایمیل واقعی می‌رود — مطمئنی؟» + نام مقصد).
- تست: (۱) webhook با secret درست → instance ساخته شد؛ secret غلط → 401؛ غیرفعال → 410؛ (۲) دستی: فلو واقعی end-to-end.

## P6.7 + P6.8 — Validation + observability

- P6.7: سناریوی کامل «drag نود → connect → Test با نمونه → Active → webhook واقعی → لاگ per-step» + چک `wrangler deploy --outdir bundled/ --dry-run` و مقایسهٔ `Total Upload` با **بودجهٔ خودمان** (سقف پلتفرم ۶۴ MiB فشرده‌نشده است — `docs/CF_LIMITS.md` §۴).
- P6.8: `trace-id` واحد از plan تا push: در `debuglog.go` + `workflow_step_logs` + ردیف lineage + SHA کامیت — یک query بتواند کل مسیر را نشان دهد. تست: یک generation نمونه → هر ۴ جا همان trace-id.
