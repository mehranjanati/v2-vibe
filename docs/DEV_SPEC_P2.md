# SPEC P2 — Node-as-Package Catalog (first capability-registry content)

## P2.1 — Manifest contract

- اسکیمای هر نود (`NodeManifest`): `{name, version, kind, paramsSchema (JSON-Schema), credentials: [{key, label, required}], sideEffect: safe|mocked|blocked (برای P6.5), examples: [{title, params, ok: true|false}]}`.
- P2.1.2: جدول `node_packages` (`name, version, manifest_json, checksum, created_at`؛ کلید یکتا `(name, version)`) + `migrations/0012_node_packages.sql` via `db:generate` + migrate لوکال.
- P2.1.3: کش KV `nodepkg:{name}@{version}` با TTL ۲۴h؛ miss → D1؛ invalidation موقع publish نسخه جدید.
- تست: insert دو نسخه یک نود → resolve نسخه درست؛ checksum ناسازگار → reject.

## P2.2 — Registry-aware Go validation

- `backend/pkg/engine/nodepkg.go`: `LoadRegistry(ctx)`, `ResolveNode(name@version)`, `ValidateWithRegistry(wf, registry) error`.
- P2.2.2: در `backend/pkg/engine/workflowschema.go`: متد موجود `wf.Validate()` (`backend/pkg/engine/workflowschema.go:296`) **بدون تغییر بماند** — Go overload ندارد و چهار فراخوان دارد (`backend/agent/planner.go:115`، `backend/pkg/engine/room.go:822,840,1143`). تابع جدا اضافه کن: `ValidateWithRegistry(wf, registry) error` (هم‌نام P2.2.1) که `type` ناشناس یا `pkg@version` ناموجود → خطای salvage (preview/viz زنده، runtime بلاک).
- تست: (۱) فلو با نود موجود → ok؛ (۲) `pkg` ناموجود → خطای مشخص با نام نود؛ (۳) `params` خلاف schema → خطا با path فیلد.

## P2.3 — Table-driven TS dispatch

- `worker/workflow/VibeWorkflow.ts`: جدول `HANDLERS: Record<kind, (node, ctx) => Promise<...>>` به‌جای switch بلند؛ P2.3.2: قبل از handler → resolve manifest از KV/D1 → validate params با schema → بعد اجرا.
- P2.3.3: نود بد (manifest ناموجود/params نامعتبر) → `NonRetryableError` (retry بیهوده ممنوع) + لاگ در `step_logs`.
- تست: unit dispatch (mock handler)؛ نود بد → NonRetryable؛ سایز باندل چک شود (مقدمه P2.8.2).

## P2.4 — Catalog API

- لایهٔ اجرا: این اندپوینت‌ها روی **Go control plane** می‌نشینند (`backend/pkg/api/nodes.go` + ثبت در `backend/pkg/api/routes.go`) — کنار `/api/workflows/*` موجود (`backend/pkg/api/routes.go:314`). روی Worker نگذار.
- `GET /api/nodes` (لیست name+version+kind+توضیح، pagination)؛ `GET /api/nodes/:name@:version` (manifest کامل)؛ `POST /api/workflows/validate` (body=DAG → `{ok, errors[]}` با همان منطق P2.2، آینه TS).
- ⚠️ اگر روزی این را روی light Worker هم سرو کردی: fallback `app.all('*')` در `worker/light/lightApp.ts:920` همان HTML اپلیکیشن (۲۰۰) را برای مسیرهای غیر-`/api/*` برمی‌گرداند؛ برای `/api/*` ناشناس از قبل catch-all موجود در `worker/light/lightApp.ts:906` فعال است که JSON ۴۰۴ می‌دهد (اصلاح T1 در `docs/DOCS_AUDIT_BACKLOG.md`). مسیر جدید را **قبل از** `worker/light/lightApp.ts:920` ثبت کن.
- تست: typecheck + unit با D1 لوکال؛ validate فلو خراب → errors با path دقیق.

## P2.5 — Agentic-team wiring

- P2.5.1: در `02_coder.md`: «نود را فقط به‌صورت `node.pkg@version` تولید کن + params طبق schema + مثال manifest را ببین».
- P2.5.2: در `03_reviewer.md`: چک‌لیست «pkg در کاتالوگ هست؟ params با schema می‌خواند؟ credential ارجاعی است نه plaintext؟».
- تست: دستی — تولید نمونه → reviewer نود خیالی را بگیرد.

## P2.6 — 20 curated nodes (5 groups)

- ترتیب پیشنهادی: notify (email/slack/telegram) → http (request/webhook-trigger/webhook-call) → data (db.query/kv/queue) → time+ai (cron/sleep/prompt/classify/extract) → logic+premium (branch/loop/map/stripe/sheets).
- هر نود: manifest کامل + handler واقعی + `examples` (P2.6.6: دقیقاً دو نمونه درست/غلط).
- تست هر نود: handler با mock provider + نمونه غلط → خطای validation.

## P2.7 — Isolation + security + quota

- P2.7.1: همه کوئری‌های `workflow_*` اسکوپ `user_id`؛ trigger نود غریبه → 403 + audit.
- P2.7.2: ماسک credential در هر لاگ (`****` + key-name)؛ دیتای نامطمئن فقط به مدل قرنطینه (Dual-LLM).
- P2.7.3: پین ورژن در DAG ذخیره‌شده + checksum موقع resolve؛ ناسازگار → بلاک اجرا.
- P2.7.4: کوتای per-app (عددها در config، نه hardcode): سقف generation روزانه/کامیت/ران‌تایم workflow؛ secret-storage: توکن GitHub در KV رمزنگاری‌شده یا D1 + revoke endpoint. با P6.4 یکی شود.
- تست: (۱) کاربر B → trigger فلو کاربر A → 403؛ (۲) لاگ اجرا → هیچ secret plaintext؛ (۳) سقف کوتا → خطای مشخص + بنر فرانت؛ (۴) تست نفوذ پایه P5.3.3 این را هم پوشش دهد.

## P2.8 — Validation + exit path

- P2.8.1: E2E روی CF واقعی: `webhook.trigger → ai.extract → slack.postMessage` (با Slack mock در test-run، واقعی فقط دستی).
- P2.8.2: `wrangler deploy --outdir bundled/ --dry-run` و لاگ عدد `Total Upload` در CI. ⚠️ سقف «۳MiB فشرده» در ۴ سپتامبر ۲۰۲۶ حذف شده و سقف پلتفرم امروز **۶۴ MiB فشرده‌نشده** است (`docs/CF_LIMITS.md` §۴) ⇒ gate مرج را روی **بودجهٔ خودمان** بگذار (مثلاً gzip <۵MiB برای latency) و دلیلش را در PR بنویس، نه روی عدد حذف‌شده.
- P2.8.3: export: `GET /api/workflows/:id/export` → JSON باز (DAG + تاریخچه اجراها + manifestهای استفاده‌شده) قابل import مجدد.
