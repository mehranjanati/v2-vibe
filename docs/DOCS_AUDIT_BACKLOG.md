# DOCS AUDIT BACKLOG — تسک‌های اصلاح مستندات

> خروجی بررسی `docs/` در working tree فعلی (۲۰۲۶-۰۹-۲۴؛ پیگیری T5 → T10، T6، T7 و T8 و T9 در ۲۰۲۶-۰۹-۲۵). این backlog صرفاً **ترتیب اجرا، اولویت، وابستگی، فایل هدف، معیار پذیرش و تست** هر مورد را ثبت می‌کند.
>
> - مرجع گزارش تحلیلی: تاریخچهٔ گفتگوی Cline (بررسی `docs/`).
> - مرجع جریان توسعه: `DEV_CHECKLIST.md` و `docs/DEV_TASKS_SPEC.md`.
> - قانون: تا تست/بیلد سبز نشده، تیک نزن (قانون ۱ چک‌لیست).

## نمای کلی

| ID | اولویت | وضعیت | خلاصه | فایل هدف |
|---|---|---|---|---|
| T1 | P0 — بحرانی | ✅ | مسیرهای ناشناختهٔ `/api/*` در Light Worker ممکن است HTML برگردانند | `worker/light/lightApp.ts` |
| T2 | P0 — زیاد | ✅ | `LOCAL_DEV.md` fallback چت را فعال توصیف می‌کند، ولی Worker آن را 503 می‌زند | `docs/LOCAL_DEV.md` |
| T3 | P1 — زیاد | ✅ | مجموعهٔ Postman به two-plane بازسازی شد (۱۲ ریکوئست + ولیدیتور) | `docs/v1dev-api-collection.postman_collection.json`, `docs/v1dev-environment.postman_environment.json`, `docs/POSTMAN_COLLECTION_README.md`, `scripts/validate-postman.mjs`, `scripts/postman-route-contract.json` |
| T4 | P1 — زیاد | ✅ | جدول مالکیت routeها با routes.go/lightApp.ts هم‌خوان شد (§۷ README) | `docs/POSTMAN_COLLECTION_README.md` |
| T5 | P1 — زیاد | ✅ | `usage-limits-ui.md` رفتار deploy قدیمی را فعال توصیف می‌کرد؛ با کد فعلی هم‌خوان شد | `docs/usage-limits-ui.md` |
| T6 | P2 — متوسط | ✅ | نمودارهای معماری به بخش فعال (dual-plane) + آرشیو تاریخی جدا شدند | `docs/architecture-diagrams.md`, `docs/archive/architecture-legacy.md` |
| T7 | P2 — متوسط | ✅ | متن فعال/تاریخی `llm.md` جدا شد (۵۱۸۳ → ۴۸۲ خط + آرشیو ۵۳۶۴ خط) | `docs/llm.md`, `docs/archive/llm-legacy.md` |
| T8 | P3 — کم | ✅ | ارجاع‌های خطی specها مسیر کامل گرفتند + شماره‌های کهنه اصلاح شد + ولیدیتور `scripts/validate-spec-refs.mjs` (پیگیری: جدول §۷ POSTMAN README و اتصال `docs:check` به pre-commit هم انجام شد) | `docs/DEV_SPEC_P1A.md`, `docs/DEV_SPEC_P1B.md`, `docs/DEV_SPEC_P2.md`, `docs/DEV_SPEC_P3P4.md`, `docs/DEV_SPEC_P6.md`, `docs/POSTMAN_COLLECTION_README.md` |
| T9 | P3 — کم | ✅ | هر ادعای `CF_LIMITS.md` منبع + `Last verified` گرفت (`[S1]`–`[S6]` + علامت `⚠️ unverified` برای یک عدد تأییدنشده) | `docs/CF_LIMITS.md` |
| T10 | P2 — متوسط | ✅ | مستندات toggle رزرو `ENABLE_USER_ACCOUNT_DEPLOY` (اسکوپ فقط سند) | `docs/setup.md`, `README.md` |
| T11 | P3 — کم | ✅ | `README.md` بازگردانده و بازنویسی شد (Architecture + بقیهٔ بخش‌ها → dual-plane؛ بدون اشاره به اجزای حذف‌شده) | `README.md` |
| T12 | P2 — متوسط | ✅ | انطباق `docs/setup.md` با دو-پلنی: حذف ادعاهای SpaceDO/Worker Loader/Artifacts + علامت‌گذاری توگل‌های بی‌خواننده | `docs/setup.md` |
| T13 | P2 — متوسط | ✅ | بازنویسی کامل `docs/setup.md` برای دو-پلنی + آرشیو V1 + کشف اینکه `bun run setup` در این درخت اجرا نمی‌شود | `docs/setup.md`, `docs/archive/setup-legacy.md`, `README.md`, `AGENTS.md`, `.github/workflows/deploy-*.yml` |
| T14 | P2 — متوسط | ✅ | پورت‌کردن `scripts/setup.ts` به `wrangler.v2.jsonc` + رفع باگ گارد اجرا + حالت read-only `--check` | `scripts/setup.ts` (+ `docs/setup.md`, `README.md`, `AGENTS.md`) |
| T15 | P2 — متوسط | ✅ | پاک‌سازی بلوک env قدیمی دو ورک‌فلو + افزودن jobهای گیت `lint`/`typecheck`/`go-test` و وابسته‌کردن `deploy` به آن‌ها | `.github/workflows/deploy-staging.yml`, `.github/workflows/deploy-release-live.yml`, `.github/workflows/ci.yml` |

## ترتیب اجرا

1. ✅ **T1 → T2** (پیش‌نیاز UX: تا API قرارداد JSON نداشته باشد، عیب‌یابی مستندات بی‌فایده است).
2. **T3 → T4** (Postman و README باید با هم به‌روز شوند).
3. ✅ **T5** (usage limits مستقل است).
4. ✅ **T6** → ✅ **T7** (جداسازی معماری تاریخی از فعال؛ هر دو انجام شدند).
5. ✅ **T8 → ✅ T9** (بهبودهای نگهداری) — و ✅ **T11** (بازگردانی + بازنویسی `README.md`؛ مستقل، انجام شد ۲۰۲۶-۰۹-۲۷).
6. ✅ **T10** (پیگیری T5؛ فقط سند، بدون وابستگی — انجام شد ۲۰۲۶-۰۹-۲۵) → ✅ **T11** (ویرایش گم‌شدهٔ README همین تسک را بازاعمال کرد).
7. ✅ **T12** (پیگیری T11: پاک‌سازی ادعاهای پیش‌نمایش/توگل‌های حذف‌شده در `docs/setup.md`؛ انجام شد ۲۰۲۶-۰۹-۲۷).
8. ✅ **T13** (پیگیری T12: بازنویسی کامل `docs/setup.md` + آرشیو V1 + اصلاح ادعای `bun run setup` در README/AGENTS + پاک‌سازی ورک‌فلوها؛ انجام شد ۲۰۲۶-۰۹-۲۷).
9. ✅ **T14** (کد: پورت `scripts/setup.ts` به `wrangler.v2.jsonc` + رفع باگ گارد اجرا + `--check`؛ انجام شد ۲۰۲۶-۰۹-۲۷).
10. ✅ **T15** (پیگیری T14: پاک‌سازی بلوک env دو ورک‌فلو + jobهای گیت CI برای `lint`/`typecheck`/`go test ./...` و وابسته‌کردن `deploy` به آن‌ها؛ انجام شد ۲۰۲۶-۰۹-۲۷).

**پیشرفت:** ✅ ۱۵ از ۱۵ تسک انجام شده (T1 … T15) — backlog بسته است.

---

## T1 — مسیرهای ناشناختهٔ `/api/*` نباید HTML برگردانند ✅

- **اولویت:** P0 — بحرانی
- **وضعیت:** ✅ انجام شد (۲۰۲۶-۰۹-۲۴)
- **وابستگی:** ندارد
- **فایل هدف:** `worker/light/lightApp.ts` (خطوط ۸۷۷–۸۹۲)
- **شرح:** قبل از `app.all('*', ...)` SPA fallback، catch-all برای `/api/*` اضافه شد که پاسخ JSON `404` با envelope استاندارد برگرداند.
- **معیار پذیرش:** ✅
  - `GET /api/unknown-route` → `404` با `Content-Type: application/json`.
  - routeهای موجود بدون تغییر کار کنند.
  - `bun run typecheck && bun run lint && bun run build` سبز.
- **تست:** `worker/light/lightApp.test.ts` ایجاد شد — ۱۸۴ خط، شامل تست‌های:
  - مسیرهای ناشناخته `/api/*` → JSON 404
  - routeهای شناخته‌شده → 200
  - routeهای backend-only → 503
  - SPA fallback → HTML
  - **توجه:** تست‌ها روی macOS < 13.5 اجرا نمی‌شوند (workerd limitation)؛ در CI/DevContainer اجرا شوند.
- **یادداشت:** نقص کد بود که از طریق بررسی قرارداد مستندات آشکار شد.

## T2 — اصلاح `LOCAL_DEV.md` دربارهٔ fallback چت ✅

- **اولویت:** P0 — زیاد
- **وضعیت:** ✅ انجام شد (۲۰۲۶-۰۹-۲۴)
- **وابستگی:** T1 (انجام شد)
- **فایل هدف:** `docs/LOCAL_DEV.md` (خطوط ۸، ۱۰۵، ۱۴۷–۱۵۵ و ۱۸۱)
- **شرح:** چهار ناحیه بازنویسی شد:
  1. **سربرگ (خط ۸):** «fallback به Worker قدیمی» حذف شد؛ اکنون صریح می‌گوید control plane برای چت **اجباری** است و Worker برای `/api/agent*`, `/api/projects/*`, `/ws/*` عمداً `503 NOT_AVAILABLE` می‌دهد.
  2. **یادداشت `VITE_CONTROL_PLANE_URL` (خط ۱۰۵):** توضیح داده شد که `controlPlane.baseUrl` به same-origin برمی‌گردد؛ در `bun run dev` آن Vite است و backend ندارد. همچنین `VITE_EXECUTION_PLANE_URL` به‌عنوان plane اختیاری Edge تفکیک شد.
  3. **بخش ۳.۴ (خطوط ۱۴۷–۱۵۵):** از «Fallback check (optional)» به «Chat is not served by the edge Worker» تغییر کرد؛ مسیر واقعی retry مستند شد: `apiClient.createAgentSession` → `POST /api/agent` → `controlPlane.baseUrl` (`src/lib/api-client.ts:167-170, 347-351`) — یعنی همان Go که شکست خورده، یا same-origin Worker که 503 می‌دهد. صریحاً «compatibility hook، نه fallback کارآمد» علامت خورد.
  4. **جدول Troubleshooting (خط ۱۸۱):** ردیف «Frontend falls back to legacy» به پیام واقعی لاگ + علت + راه‌حل دقیق تغییر کرد.
- **معیار پذیرش:** ✅
  - هیچ اشاره‌ای به «legacy Worker path» به‌عنوان مسیر کارآمد باقی نمانده (تنها دو ارجاع باقی‌مانده، متن لاگ واقعی کد و ردیف troubleshooting هستند).
  - ادعاها با `src/config/api.ts:47-55`, `src/lib/api-client.ts:163-170`, `:347-351`, `src/routes/chat/hooks/use-chat.ts:593-596`, `worker/light/lightApp.ts:877-889` هم‌خوان شده‌اند.
- **تست:** بازبینی متنی + `grep -niE 'legacy|fallback' docs/LOCAL_DEV.md`.
- **یادداشت:** تغییرات ثبت‌نشدهٔ قبلی کاربر در همان فایل (توکن D1 و preview hydration) دست‌نخورده باقی ماند.
- **تست:** بازبینی متنی.

## T3 — تعیین تکلیف مجموعهٔ Postman ✅

- **اولویت:** P1 — زیاد
- **وضعیت:** ✅ انجام شد (۲۰۲۶-۰۹-۲۴)
- **وابستگی:** T1
- **فایل‌های هدف:** `docs/v1dev-api-collection.postman_collection.json` (v2.0.0، ۱۲ ریکوئست)، `docs/v1dev-environment.postman_environment.json` (`workerUrl`+`controlUrl`)، `docs/POSTMAN_COLLECTION_README.md`، `scripts/validate-postman.mjs`، `scripts/postman-route-contract.json`، `scripts/validate-postman-negative.mjs` + فیxtureها، `docs/archive/v1dev-*legacy*`
- **شرح:** کالکشن به دو پوشهٔ Worker Plane (۷) و Control Plane (۵) بازسازی شد؛ هر ریکوئست مارکر `plane: worker|control` + متغیر پایهٔ همان plane؛ `baseUrl`/`localUrl` حذف؛ `POST /api/agent` با NDJSON multi-line parsing؛ پاسخ R2 (سشن مشترک نیست) و ترتیب اجرا در README مستند شد.
- **معیار پذیرش:** ✅ `node scripts/validate-postman.mjs` سبز (۱۲ ریکوئست)؛ `node scripts/validate-postman-negative.mjs` سبز (ولیدیتور روی فیxture قرمز می‌شود)؛ بدون `{{baseUrl}}` فعال؛ ست فعال ۲۹→۱۲.
- **تست:** ولیدیتور + negative self-test (R5).

## T4 — جدول مالکیت routeها در README ✅

- **اولویت:** P1 — زیاد
- **وضعیت:** ✅ انجام شد (۲۰۲۶-۰۹-۲۴)
- **وابستگی:** T3
- **فایل هدف:** `docs/POSTMAN_COLLECTION_README.md` (§۷)
- **شرح:** جدول endpoint-by-endpoint با ستون Plane + Source (خطوط `routes.go`/`lightApp.ts`): auth/CSRF/OAuth/logout → Worker؛ `/health`، `/api/agent*`، `/api/agent/session|connect`، `/api/apps/:id` → Control/Go؛ deadها آرشیو.
- **معیار پذیرش:** ✅ جدول با `worker/light/lightApp.ts` و `backend/pkg/api/routes.go` هم‌خوان است.
- **تست:** بازبینی متنی + `node scripts/validate-postman.mjs`.

## T5 — اصلاح `usage-limits-ui.md` دربارهٔ deploy ✅

- **اولویت:** P1 — زیاد
- **وضعیت:** ✅ انجام شد (۲۰۲۶-۰۹-۲۵)
- **وابستگی:** ندارد
- **فایل هدف:** `docs/usage-limits-ui.md` — بخش «Deploy gate popup» (پاراگراف مقدمه + پاراگراف بند نهایی)
- **شرح:** پاراگراف قدیمی «flag فعال است» با وضعیت واقعی کد جایگزین شد:
  1. `userAccountDeploy` در **هر دو** پلن ثابت `false` است: `worker/light/lightApp.ts` (داخل `buildLightApp()`) و `backend/pkg/api/routes.go`؛ هیچ‌کدام `ENABLE_USER_ACCOUNT_DEPLOY` را نمی‌خوانند (flag فقط در `worker/types/env.d.ts` اعلان و در `docs/setup.md`/`README.md` مستند شده است).
  2. مسیر فعال deploy: `src/routes/chat/chat.tsx` (`capabilities?.userAccountDeploy ?? false` → `handleDeployToCloudflare(..., 'platform')`) → `deployProject()` در `src/services/controlPlaneClient.ts` → `POST /api/projects/:id/deploy` روی Go (`handleDeployProject` → `room.DeployRoomVFS` در `backend/pkg/engine/deploy.go`) با اعتبارنامهٔ **پلتفرم** (`CLOUDFLARE_ACCOUNT_ID`/`CLOUDFLARE_API_TOKEN`)؛ اتصال Cloudflare کاربر دخالتی ندارد.
  3. پاپ‌آپ deploy gate در معماری فعلی **خواب** است: هیچ پلنی `cloudflare_deployment_error` همراه `code` منتشر نمی‌کند. `CloudflareDeploymentErrorCode` (`worker/api/websocketTypes.ts`) فقط تایپ پروتکل و مصرف‌کنندهٔ فرانت است و شکست‌های Go به‌صورت `deployment_started`/`deploy_progress`/`deployment_completed`/`deployment_failed` می‌آیند.
  4. تنها مسیر باقی‌ماندهٔ `target: 'user'`، پیام WS لگسی `deploy` است که فقط fallback بعد از شکست REST محسوب می‌شود (`src/routes/chat/hooks/use-chat.ts`).
  - همچنین یک خط «Dormant in the current architecture» به پاراگراف مقدمهٔ بخش اضافه شد تا خواننده جدول را با وضعیت خواب اشتباه نگیرد.
- **معیار پذیرش:** ✅ پاراگراف‌ها با کد فعلی هم‌خوان‌اند (دو پلن، مسیر REST روی Go، عدم وجود منتشرکنندهٔ `cloudflare_deployment_error` و عدم خواندن flag).
- **تست:** بازبینی متنی + grepهای تأییدی:
  - `grep -rn "cloudflare_deployment" --include='*.go' backend/` → فقط کامنت/مدل `CloudflareDeploymentCompleted` در `backend/pkg/models/websocket.go`، بدون هیچ broadcaster.
  - `grep -rn "ENABLE_USER_ACCOUNT_DEPLOY" .` → فقط `worker/types/env.d.ts` + `docs/setup.md` + `README.md` (هیچ خوانندهٔ کد).
  - `grep -rn "not_connected\|not_configured" --include='*.go' backend/` → خالی.
- **یادداشت:** این تسک فقط مستندات را اصلاح کرد؛ نیازی به `typecheck/lint/build` نبود (کد تغییر نکرد). منبع اختلاف: `target: 'user'` در کد فرانت زنده است ولی پرچم آن در دو پلن hardcode شده.
- **پیگیری (T10 — اجرا شد ۲۰۲۶-۰۹-۲۵، اسکوپ فقط سند):** `docs/setup.md:214` و `README.md:116` (شمارهٔ خط در آن زمان؛ پس از بازگردانی T11 همان فایل، سطر ۱۵۷) اکنون `ENABLE_USER_ACCOUNT_DEPLOY` را **رزرو/غیرفعال** معرفی می‌کنند و به `docs/usage-limits-ui.md` ارجاع می‌دهند.


## T6 — جداسازی نمودارهای معماری تاریخی ✅

- **اولویت:** P2 — متوسط
- **وضعیت:** ✅ انجام شد (۲۰۲۶-۰۹-۲۵)
- **وابستگی:** T7 (هم‌زمان)
- **فایل‌های هدف:** `docs/architecture-diagrams.md` (بخش فعال — ۸۵۸ → ۹۵ خط)، `docs/archive/architecture-legacy.md` (فایل جدید — آرشیو ۱۱ نمودار تاریخی)
- **شرح:**
  1. **یافتهٔ اصلی:** بخشی که با عنوان `## Current architecture` منتشر می‌شد، خودش معماری **حذف‌شدهٔ** ThinkAgent/SpaceDO/Cloudflare Artifacts/Worker Loader/App Facet را توصیف می‌کرد — یعنی «بخش فعال» مستقیماً نقض معیار پذیرش بود، نه فقط «مخلوط‌شدن». تنها شرح معتبر dual-plane در `docs/llm.md:5-13` و `AGENTS.md` است (Go control plane + light Worker).
  2. کل محتوای تاریخی (۱۱ بلوک mermaid: دیاگرام mislabeled، Presentation-Ready و «Detailed System Diagrams» §۱ تا §۹) عیناً به `docs/archive/architecture-legacy.md` منتقل شد.
  3. سربرگ آرشیو banner صریح `Historical / removed in dual-plane migration` دارد و **هر ۱۱ نمودار** برچسب `> **Status:** legacy - removed in the dual-plane migration | historical only; ...` گرفته‌اند؛ عنوان بخش اول هم از `## Current architecture` به `## ThinkAgent / SpaceDO architecture (formerly mislabeled "Current architecture")` اصلاح شد.
  4. `docs/architecture-diagrams.md` بازنویسی شد و فقط `## Current architecture (dual-plane)` را نگه می‌دارد: نمودار جدید dual-plane (SPA → light Worker با `ASSETS`/`DB`/GitHub، و SPA → Go control plane → Redis / AI Gateway / Pages / Workflows)، برچسب `current` + تاریخ بازبینی، جدول مسئولیت پلن‌ها با ارجاع به `src/config/api.ts`، جدول bindingهای `wrangler.v2.jsonc` (و توضیح اینکه R2 عمداً bind نشده)، و ارجاع به `§۷` فایل `docs/POSTMAN_COLLECTION_README.md` برای مالکیت routeها (بدون تکرار جدول T4).
  5. فهرست حذف‌شده‌ها به بخش `## Historical diagrams (removed)` در **انتهای** سند منتقل شد تا هم reader از مسیرهای مرده آگاه شود و هم grep بخش فعال تمیز بماند.
  6. **رفع باگ جانبی (کشف‌شده در همین تسک):** در سند قدیمی بلوک `erDiagram` بخش ۵ بسته نشده بود (۲۱ fence برای ۱۱ نمودار) و در نتیجه از بخش ۶ به بعد به‌عنوان code رندر می‌شد. در آرشیو fence گم‌شده اضافه شد.
- **معیار پذیرش:** ✅
  - هر نمودار برچسب وضعیت دارد: ۱۱ برچسب در آرشیو + برچسب `current` روی نمودار فعال.
  - هیچ مسیر حذف‌شده‌ای در بخش فعال نمانده (تست grep زیر خالی است).
  - آرشیو و سند فعال دوطرفه لینک شده‌اند.
- **تست:**
  - بخش فعال تمیز است:
    ```bash
    awk '/^## Current architecture \(dual-plane\)/,/^## Historical diagrams \(removed\)/' docs/architecture-diagrams.md \
      | grep -niE 'space/|SpaceDO|ThinkAgent|worker/api/routes|Artifacts|Worker Loader|App Facet|Sandbox|CodeGen|phase'
    # خروجی: خالی
    ```
  - برچسب‌ها و توازن fence:
    ```bash
    grep -c '^> \*\*Status:\*\*' docs/archive/architecture-legacy.md   # ۱۱
    grep -c '^```'              docs/archive/architecture-legacy.md   # ۲۲ (۱۱ جفت متوازن)
    grep -c '^```'              docs/architecture-diagrams.md         # ۲ (۱ بلوک mermaid)
    ```
- **یادداشت:** این تسک فقط مستندات را تغییر داد؛ `typecheck/lint/build` لازم نبود (هیچ فایل کد تغییر نکرد).
- **پیگیری خارج از اسکوپ:** `README.md` (بخش «Architecture» سطر ۳۲–۷۰) و بدنهٔ تاریخی `docs/llm.md` همچنان ThinkAgent/SpaceDO/Artifacts را به‌عنوان معماری جاری توصیف می‌کنند. اصلاح `llm.md` اسکوپ T7 است؛ ناهم‌خوانی `README.md` باید به‌عنوان یک تسک مستقل در همین backlog ثبت شود.

## T7 — جداسازی متن فعال/تاریخی `llm.md` ✅

- **اولویت:** P2 — متوسط
- **وضعیت:** ✅ انجام شد (۲۰۲۶-۰۹-۲۵)
- **وابستگی:** T6 (انجام شد)
- **فایل‌های هدف:** `docs/llm.md` (۵۱۸۳ → ۴۸۲ خط)، `docs/archive/llm-legacy.md` (فایل جدید — ۵۳۶۴ خط)
- **شرح:**
  1. **یافتهٔ اصلی:** مشکل فقط «در‌هم‌آمیختگی» نبود؛ از سطر ۵۸ تا پایان سند (۵۱۲۶ خط) تماماً معماری **حذف‌شدهٔ** ThinkAgent/SpaceDO/Cloudflare Artifacts/Worker Loader/App Facet/CodeGen DO/Sandbox را به‌عنوان سیستم جاری توصیف می‌کرد و تنها ~۵۰ خط ابتدایی معتبر بود.
  2. کل بدنهٔ تاریخی (سطر ۵۸ تا EOF) **عیناً و بدون تغییر متن** به `docs/archive/llm-legacy.md` منتقل شد (`diff` روی متن سطر‌به‌سطر: فقط ۱۰۸ سطر خالیِ افزوده برای جدا کردن banner‌ها، بدون هیچ تغییر دیگری).
  3. هر سرفصل تاریخی (۱۰۸ سرفصل H1/H2 بیرون از code fence) برچسب صریح
     `> **Status:** legacy — removed in the dual-plane migration; historical only.` گرفته است، به‌علاوهٔ banner چندسطری بالای فایل با لینک دوطرفه به سند فعال.
  4. `docs/llm.md` بازنویسی شد و فقط معماری فعال (dual-plane + Go control plane) را مستند می‌کند:
     LLM inference (AI Gateway ↔ Workers AI، SSE، `ConnectionLostError`، truncation)،
     roles/skills (۴ نقش + مدل/دما/توکن پیش‌فرض + `SKILLS_DIR`/`<ROLE>_*`)،
     generation pipeline (سه مسیر: dual-model planner→coder، plan-execute-replan، fence-streaming + gap-fill/finalize)،
     tools (`vfs_*`، `rest_api`، middleware ترمیم JSON)، agents & team (eino ReAct + AgentAsTool با least privilege برای reviewer)،
     vector index/RAG (`idx:vfs`)، WS protocol، persistence (کلیدهای Redis + جداول D1)، API surface، testing.
  5. **anchor پایدار:** heading `## Current architecture` (anchor `#current-architecture`) دست‌نخورده ماند چون `docs/architecture-diagrams.md:6` و `docs/archive/architecture-legacy.md:12,42` به آن لینک می‌دهند؛ برای بقیهٔ بخش‌ها `<a id="…">` صریح + جدول Contents اضافه شد (۱۳ anchor، هر ۱۳ در TOC لینک شده است).
  6. **رفع ارجاع کهنهٔ ناشی از همین تغییر:** `docs/CF_LIMITS.md:5` که سند فعال را «۵۱۸۲ خط» معرفی می‌کرد، به مسیر جدید (سند فعال + آرشیو) اصلاح شد.
- **معیار پذیرش:** ✅
  - سند فعال ۴۸۲ سطر < ۱۰۰۰.
  - بخش تاریخی کاملاً مجزا در `docs/archive/llm-legacy.md` (۵۳۶۴ سطر) با banner.
- **تست:**
  ```bash
  wc -l docs/llm.md                                   # 482 (< 1000)
  grep -c '^```' docs/llm.md                          # 8  (۴ جفت متوازن)
  awk '/^## Current architecture$/,/^## Historical content/' docs/llm.md \
    | grep -niE 'thinkagent|spacedo|artifacts|worker loader|app facet|space/|worker/agents|worker/api/routes|worker/services|codegen|sandbox|phase'
  # خروجی: خالی
  grep -c 'Status:\*\* `legacy`' docs/archive/llm-legacy.md   # 109 (۱۰۸ سرفصل + banner فایل)
  grep -c '^```' docs/archive/llm-legacy.md                   # 278 (۱۳۹ جفت متوازن)

  # بدنهٔ آرشیو = کل بخش تاریخی سند قبلی (مقایسه با نسخهٔ committed)
  git show HEAD:docs/llm.md | awk '/^## .*Recent Changes/{f=1} f' | grep -v '^$' > /tmp/t7_head.txt
  sed -e '1,/^---$/d' docs/archive/llm-legacy.md \
    | grep -v 'Status:\*\* `legacy`' | grep -v '^$' > /tmp/t7_arch.txt
  diff /tmp/t7_head.txt /tmp/t7_arch.txt
  # تنها اختلاف مجاز: ۷ سطر یادداشت «Preview (dual-plane, option a)» که uncommitted بود
  ```
- **یادداشت:** فقط مستندات تغییر کرد؛ `typecheck/lint/build` لازم نبود (هیچ فایل کد تغییر نکرد).
- **بازبینی ادعاها هنگام نوشتن سند فعال:** دو ادعا که با کد تأیید نشد، تکرار نشد:
  (الف) فهرست exclusions در `vitest.config.ts` فقط `**/node_modules/**`, `**/dist/**`, `**/.git/**`, `**/test/worker-entry.ts`, `**/sdk/test/**` است — `worker/api/routes/**`/`cf-git/**` در آن نیستند (و این دو مسیر در repo وجود ندارند)، پس ادعای AGENTS.md عیناً در سند فعال تکرار نشد؛
  (ب) نام بردن از اجزای حذف‌شده در بخش فعال به فهرست «Historical content» منتقل شد تا grep بخش فعال تمیز بماند.
- **پیگیری خارج از اسکوپ:** (۱) ناهم‌خوانی `README.md` که T6 علامت زده بود، در working tree فعلی بی‌موضوع است چون خودِ فایل حذف شده است (`D README.md`) — به‌عنوان **T11** در همین backlog ثبت شد. (۲) ارجاع‌های خطی specها (مثلاً `docs/llm.md:5-35` در `docs/DEV_SPEC_P1A.md:59`) به **T8** منتقل شد و در همان ۲۰۲۶-۰۹-۲۵ انجام شد (اکنون `docs/llm.md:5-28` + مسیر کامل همهٔ ارجاع‌ها).

## T8 — کامل‌کردن مسیرهای ارجاع خطی specها ✅

- **اولویت:** P3 — کم
- **وضعیت:** ✅ انجام شد (۲۰۲۶-۰۹-۲۵)
- **وابستگی:** ندارد
- **فایل هدف:** `docs/DEV_SPEC_P1A.md`, `docs/DEV_SPEC_P1B.md`, `docs/DEV_SPEC_P2.md`, `docs/DEV_SPEC_P3P4.md`, `docs/DEV_SPEC_P6.md`, `docs/POSTMAN_COLLECTION_README.md`, `scripts/validate-spec-refs.mjs`, `package.json`, `.husky/pre-commit`, `AGENTS.md`
- **شرح (وضعیت قبل از T8):** ارجاع‌هایی مانند `schema.ts:475` مسیر پوشه نداشتند (`room.go:606,623`، `plan_execute.go:26,40`، `gapfill.go:64,236,387`، `dual_model.go:229,278,330`، `workflowschema.go:296`، `routes.go:314`، `WorkflowVisualizer.tsx:254`) و چند ارجاع خطی هم به‌شکل «خطوط ۲۰۷-۲۱۳»، «خطوط ۲۴۵-۲۵۸» و «خطوط ۲۷۷-۳۳۵» بدون نام فایل نوشته شده بود.
- **کار انجام‌شده:** همهٔ **۳۰** ارجاع `file:line` شش اسپک (و در پیگیری، **۱۵** ارجاع جدول §۷ `docs/POSTMAN_COLLECTION_README.md`) مسیر کامل repo-relative گرفتند و شماره‌های کهنه‌ای که با کد فعلی نمی‌خواندند هم اصلاح شد:
  1. `backend/pkg/engine/plan_execute.go`: «`:62` + خط ۵۶» → `:68` (`(*notifyWriteTool).InvokableRun`) و `:63` (type `notifyWriteTool`)؛ `:26,40` → `:28,43` (`VFSWrite`/`VFSDelete`)؛ `:87` → `:91` (`llm.SanitizeJS`؛ تعریف در `backend/pkg/llm/sanitize.go:33`).
  2. `backend/pkg/engine/workflowschema.go`: `23-30` → `24-30` (خط ۲۳ خالی است). `src/components/workflow/WorkflowVisualizer.tsx`: «۲۰۷-۲۱۳» → `206-212` (نودها) و «۲۴۵-۲۵۸» → `245-256` (بلوک `ReactFlow`) — پوشهٔ واقعی `src/components/workflow/` است، نه `workflows/`.
  3. `src/components/preview/preview-normalize.ts`: «۲۷۷-۳۳۵» → `274-288` (بدنهٔ فعلی `sanitizeJs`؛ فایل ۳۶۶ خط است).
  4. `docs/llm.md:5-35` → `docs/llm.md:5-28` + ارجاع به anchor پایدار `#current-architecture` (بازماندهٔ پیش از T7؛ از خط ۳۰ «Contents» شروع می‌شود).
  5. `worker/light/lightApp.ts`: «۸۹۱» (fallback قدیمی) → `:908` برای `app.all('*')` مربوط به SPA و `:894` برای catch-all فعلی `/api/*` که از T1 JSON ۴۰۴ می‌دهد؛ هشدارهای P2.4 و P6.6.1 به وضعیت واقعی بعد از T1 به‌روز شد.
  6. مسیرهای بدون خط که فقط با نام فایل آمده بودند: `backend/pkg/engine/team.go`، `backend/pkg/engine/hub.go`، `backend/pkg/engine/team_hub.go`، `backend/pkg/engine/gate.go`، `backend/skills/02_coder.md`، `backend/pkg/design/blocks.go`، `src/components/workflow/node-defaults.ts` و `worker/workflow/VibeWorkflow.ts:312` (`validateNodeParams`) / `:402` (`validateWorkflowV2`).
- **معیار پذیرش:** ✅ همهٔ ارجاع‌های `file:line` در `docs/DEV_SPEC_P*.md` مسیر کامل دارند (۳۰ ارجاع در ۶ فایل) و ولیدیتور در اسکوپ پیش‌فرض خود کل درخت زندهٔ مستندات را سبز پاس می‌کند (۴۶ ارجاع در ۱۵ فایل؛ `docs/archive/**` و همین backlog عمداً خارج‌اند).
- **تست:** `bun run docs:check` → سبز (`validate-spec-refs: OK (46 refs in 15 files)` + `Postman contract validation PASSED`)، یعنی همهٔ ارجاع‌های `file:line` در کل درخت زندهٔ `docs/` (به‌جز `docs/archive/**` و `docs/DOCS_AUDIT_BACKLOG.md`) هم مسیر کامل دارند و هم شمارهٔ خطشان داخل فایل موجود است. اجراهای مستقیم: `node scripts/validate-spec-refs.mjs` (اسکوپ پیش‌فرض: همهٔ `docs/**/*.md` زنده) و `node scripts/validate-spec-refs.mjs --paths <file…>` (فهرست صریح؛ مثلاً چهار سند مشخص). ارجاعِ بی‌مسیر → خطا + پیشنهاد کاندید از `git ls-files`؛ بلوک‌های کد fenced اسکن نمی‌شوند.
- **اتصال (بعد از تأیید سبز بودن):** `package.json` → `"docs:check": "node scripts/validate-spec-refs.mjs && node scripts/validate-postman.mjs"`؛ `.husky/pre-commit` → قدم ۱: اگر فایل استیج‌شدهٔ `docs/**` یا `scripts/validate-*` وجود داشته باشد `bun run docs:check` اجرا و در صورت شکست کامیت بلاک می‌شود (`SKIP_TESTS=1` مثل قبل همهٔ چک‌ها را دور می‌زند)؛ `AGENTS.md` هم در بخش Verification به‌روز شد. این تغییر فقط وقتی فعال می‌شود که خودِ مستندات/ولیدیتورها دست‌خورده باشند، پس روی کامیت‌های کد اثری ندارد.
- **پیگیری همان نقص در `docs/POSTMAN_COLLECTION_README.md` — ✅ انجام شد (۲۰۲۶-۰۹-۲۵):** §۷ (جدول مالکیت route، سطرهای ۷۲-۸۰) **۱۳ ارجاع بی‌مسیر** داشت (`lightApp.ts:188`، `routes.go:61`، `auth_pg.go:98,158`، …). بازبینی نشان داد **شماره‌ها درست بودند** (اصلاحات T1 در خطوط ۸۹۰+ است و این سطرها را جابه‌جا نکرده است) و فقط مسیر پوشه نداشتند؛ همه به `worker/light/lightApp.ts:…`، `backend/pkg/api/routes.go:…` و `backend/pkg/api/auth_pg.go:98,158` (تأییدشده: `handleRegisterPG`/`handleLoginPG`) تبدیل شدند. اکنون `docs/POSTMAN_COLLECTION_README.md` در اسکوپ پیش‌فرض ولیدیتور هم پوشش داده می‌شود و سبز است (`validate-spec-refs` → ۴۶ ارجاع در ۱۵ فایل).
- **آگاهانه انجام‌نشده:** ارجاع `README.md:116` در سطرهای T10/T11 همین backlog عمداً به فایل حذف‌شده اشاره می‌کرد (موضوع **T11**) و دست‌نخورده ماند — ولیدیتور هم بک‌لاگ را اسکن نمی‌کند. **(حل‌شده در T11، ۲۰۲۶-۰۹-۲۷:** فایل بازگردانده شد و همان جمله در `README.md:157` بازاعمال شد؛ یادداشت پیگیری در T10/T5 اضافه شد.**)**

## T9 — افزودن منبع و تاریخ به `CF_LIMITS.md` ✅

- **اولویت:** P3 — کم
- **وابستگی:** ندارد
- **فایل هدف:** `docs/CF_LIMITS.md`
- **شرح:** کنار هر claim زمان‌محور، لینک رسمی و تاریخ بازبینی اضافه کنید:
  ```text
  Source: https://developers.cloudflare.com/workers/platform/limits/
  Last verified: 2026-09-24
  ```
- **معیار پذیرش:** ✅ هر claim دارای `Source` + `Last verified` است (ستون `منبع` در §۱/§۲، بلوک `Source`/`Last verified` در §۳–§۶، و جدول شناسه→لینک→تاریخ در §۷).
- **تست:** بازبینی متنی + دو چک تکرارپذیر (سبز در ۲۰۲۶-۰۹-۲۵):
  - `bun run docs:check` → `validate-spec-refs: OK (46 refs in 15 files)` و `Postman contract validation PASSED`.
  - `grep -c 'Last verified' docs/CF_LIMITS.md` → ۱۰ (۶ بلوک سطح-بخش در §۱–§۶ + ۴ ارجاع دیگر در سربرگ/§۷).
- **وضعیت:** ✅ انجام شد (۲۰۲۶-۰۹-۲۵).
- **کار انجام‌شده:**
  1. سربرگ فایل: «قرارداد منبع» اضافه شد — شناسهٔ `[S1]`…`[S6]` برای هر ادعای زمان‌محور، `[derived]` برای تحلیل/اندازه‌گیری داخلی، `⚠️ unverified` برای مقدار تأییدنشده؛ تاریخ فایل به ۲۰۲۶-۰۹-۲۵ به‌روز شد.
  2. §۱ (Cron) و §۲ (Workflow `schedules`) ستون **`منبع`** به‌ازای هر ردیف گرفتند؛ §۳، §۴، §۵ و §۶ بلوک `Source`/`Last verified` سطح-بخش (و علامت‌های درون‌متنی `[S1]`…`[S6]` در پاراگراف‌های §۴).
  3. §۷ از فهرست ساده به جدول «شناسه → منبع → لینک رسمی → `Last updated` خودِ سند → `Last verified` ما» + بخش «روش بازبینی (تکرارپذیر)» تبدیل شد (مارک‌داون `index.md` هر صفحه، پیشوند `r.jina.ai` برای بخش‌های وسط صفحه، و دستور اندازه‌گیری بیلد SPA).
  4. منابع دوباره از داک رسمی خوانده و راستی‌آزمایی شدند (`[S1]` Cron Triggers، `[S2]` Scheduled Handler، `[S3]` Workers Limits، `[S4]` Workflows Limits، `[S5]` Trigger Workflows، `[S6]` changelog ۲۰۲۶-۰۹-۰۴) — همهٔ اعداد قبلی درست بودند؛ `[S5]` منبع جدید برای «۱۰۰ schedule per account» است.
  5. سه یافتهٔ نو هنگام تأیید: (الف) کلایم «بزرگ‌ترین فایل بیلد ~۲.۶۷MB» برای working tree فعلی درست نبود (`dist/client`: ۱۴۹ فایل، بزرگ‌ترین فایل `ts.worker-*.js` ≈ ۶٬۹۱۵٬۶۹۲ بایت ≈ ۶.۶ MiB؛ چانک خودِ اپ `index-*.js` همان ۲٬۶۷۰٬۶۱۳ بایت است) ⇒ به `[derived]` + اندازه‌گیری تاریخ‌دار اصلاح شد؛ (ب) ردیف «حداکثر طول cron expression = ۲۵۶ کاراکتر» در هیچ‌یک از S4/S5 (و نه در شمای wrangler) پیدا نشد ⇒ `⚠️ unverified` با توضیح کامل؛ (ج) محدودهٔ `Max request body` با مقادیر Enterprise (تا ۵ GB) تکمیل شد.
- **آگاهانه انجام‌نشده:** هیچ کد production تغییر نکرد (فقط سند)؛ مقدار `⚠️ unverified` هم عمداً حذف نشد تا ابهام مستند و قابل پیگیری بماند.

## T10 — علامت‌گذاری `ENABLE_USER_ACCOUNT_DEPLOY` به‌عنوان رزرو/غیرفعال ✅

- **اولویت:** P2 — متوسط
- **وضعیت:** ✅ انجام شد (۲۰۲۶-۰۹-۲۵)
- **وابستگی:** T5 (انجام شد)
- **فایل هدف:** `docs/setup.md` (سطر ۲۱۴)، `README.md` (بخش Feature toggles)
- **شرح:**
  1. `docs/setup.md:214` — ستون Effect و Notes: `(reserved — not read by any plane)` و صراحت «Setting `\"true\"` **currently has no effect**» + ارجاع به `docs/usage-limits-ui.md`.
  2. `README.md:116` — حذف نام flag از فهرست «Unset values default to off» و اضافه‌کردن جملهٔ «currently reserved and not read by any plane — both `GET /api/capabilities` implementations return `userAccountDeploy: false`, so Think deploys always use the platform path (see `docs/usage-limits-ui.md`)».
  3. هیچ کد production تغییر نکرد؛ فقط هم‌خوان‌سازی سند (اسکوپ انتخاب‌شده توسط کاربر).
- **معیار پذیرش:** ✅ هیچ جمله‌ای در `docs/setup.md`/`README.md` این flag را toggle کارآمد معرفی نمی‌کند و هر دو به وضعیت واقعی (`userAccountDeploy: false` در هر دو پلن) ارجاع می‌دهند.
- **تست:** بازبینی متنی + `git diff README.md docs/setup.md` (فقط دو ناحیهٔ هدف).
- **پیگیری (T11 — اجرا شد ۲۰۲۶-۰۹-۲۷):** ویرایش README این تسک در working tree گم شده بود، چون کل فایل بعد از T10 حذف شده بود. در T11 فایل بازگردانده و همان جمله در بخش «Feature toggles» بازاعمال شد — اکنون `README.md:157` (پاراگراف توگل‌ها در `README.md:155`) — و پرچم در همان‌جا **رزرو/غیرفعال** معرفی می‌شود. `docs/setup.md:214` بدون تغییر معتبر است.

## T11 — تعیین تکلیف `README.md` (ثبت‌شده در T7) ✅

- **اولویت:** P3 — کم
- **وضعیت:** ✅ انجام شد (۲۰۲۶-۰۹-۲۷) — **خروجی (ب)** انتخاب و اجرا شد: فایل بازگردانده + بازنویسی شد.
- **وابستگی:** T6 (این مورد در پیگیری خارج از اسکوپ T6 خواسته شده بود)، T7 (ثبت شد)، T10 (ویرایش گم‌شدهٔ همین فایل بازاعمال شد)
- **فایل هدف:** `README.md` (به‌همراه به‌روزرسانی وضعیت در همین backlog، `docs/DEV_TASKS_SPEC.md`, `docs/DEV_CHECKLIST.md` و توضیح `scripts/validate-spec-refs.mjs`)
- **شرح و تصمیم:**
  1. **یافته:** `README.md` در working tree کامیت‌نشده حذف شده بود (`git status` → `D README.md`؛ در `HEAD` نسخهٔ ۱۷۰ خطی موجود بود) و چون **کل فایل** حذف شده بود، ویرایش T10 روی `README.md:116` (بخش Feature toggles) هم از بین رفته بود — نسخهٔ `HEAD` فقط وضعیت پیش-T10 را داشت.
  2. **تصمیم (ب):** فایل بازگردانده و بازنویسی شد تا معماری **dual-plane** را توصیف کند. مرجع واقعیت فقط اسناد تأییدشده بودند: `docs/llm.md#current-architecture`، `docs/architecture-diagrams.md`، `docs/LOCAL_DEV.md`، `wrangler.v2.jsonc`، `package.json` و کد زندهٔ `worker/light/lightApp.ts` / `backend/pkg/`؛ هیچ ادعای معماری تازه‌ای خارج از آن‌ها اضافه نشد.
  3. بخش‌های بازنویسی‌شده: «What is VibeSDK?» (Go control plane + light Worker و صراحتِ `503 NOT_AVAILABLE` برای چت)، جدول + نمودار mermaid بخش `## Architecture`، «Capabilities»، «### Inside the control plane»، «### Previews and deploys»، «### Persistence»، «Agent workflow»، «Deploy your own VibeSDK» (هر دو پلن + Redis)، «Local development» (پیش‌نیازها، hybrid quick start، جدول دستورات)، «Feature toggles»، «Security and isolation» و «Troubleshooting».
  4. ادعاهایی که به‌عنوان «جاری» حذف/جایگزین شدند: ThinkAgent، SpaceDO، Cloudflare Artifacts، Worker Loader، Dynamic Worker previews، App Facet + SQLite؛ «Version history/restore points»؛ «Application data (facet inspect/reset)»؛ «Browser verification»؛ «signed, branch-scoped preview URLs»؛ توگل `ENABLE_ARTIFACTS`؛ و توضیح `bun run build` به‌عنوان «Build SpaceDO and the Vite application» (اکنون: `vite build` برای SPA + light Worker در `dist/client`).
  5. جایگزین‌های مبتنی بر کد: پیش‌نمایش درون‌مرورگری (`src/components/preview/PreviewPanel.tsx`، `requiresSandbox: false`)، deploy به Cloudflare Pages (`POST /api/projects/:id/deploy` → `deployment_started/deploy_progress/deployment_completed { previewURL }`)، VFS در Redis (`vfs:<chatId>`) + `idx:vfs` برای RAG، D1 برای auth + `workflow_dags`، تیم coordinator/coder/reviewer با ابزارهای least-privilege (`backend/pkg/engine/team.go`)، و مدل به‌ازای نقش از طریق AI Gateway / Workers AI.
  6. جملهٔ T10 دربارهٔ `ENABLE_USER_ACCOUNT_DEPLOY` (رزرو + `userAccountDeploy: false` در هر دو پلن + ارجاع به `docs/usage-limits-ui.md`) عیناً بازاعمال شد و پاراگراف توگل‌ها به جدول مرجع `docs/setup.md#dashboard-managed-feature-toggles` ارجاع می‌دهد.
  7. هیچ کد production تغییر نکرد (فقط سند + وضعیت پیگیری).
- **معیار پذیرش:** ✅
  - سرنوشت `README.md` صریح است: فایل موجود است (۲۲۴ خط) و از خروجی (ب) پیروی می‌کند.
  - هیچ جمله‌ای اجزای حذف‌شده را «جاری» معرفی نمی‌کند: `grep -niE 'thinkagent|spacedo|artifacts|space/|worker loader|app facet|dynamic worker' README.md` → خروجی خالی.
  - ارجاع T10 به فایل حذف‌شده باقی نمانده: سطرهای T5/T8/T10 با یادداشت پیگیری به `README.md:155-157` (نسخهٔ بازگردانده‌شده) به‌روز شدند.
- **تست:** بازبینی متنی + چک‌های تکرارپذیر (سبز در ۲۰۲۶-۰۹-۲۷):
  - `git status --short README.md` → ` M README.md` (نه `D`).
  - grep بالا → خروجی خالی؛ شمار بلوک‌های ` ``` ` → ۴ (دو جفت متوازن: mermaid + bash).
  - `bun run docs:check` → `validate-spec-refs: OK (46 refs in 15 files)` + `Postman contract validation PASSED`.
  - ولیدیتور README را اسکن نمی‌کند (اسکوپ پیش‌فرض `docs/**/*.md` است)، پس مرجع‌های متنی داخل README دستی بازبینی شدند: `docs/llm.md`، `docs/architecture-diagrams.md`، `docs/setup.md#dashboard-managed-feature-toggles`، `docs/LOCAL_DEV.md`، `docs/usage-limits-ui.md`، `docs/CF_LIMITS.md`، `docs/DEV_CHECKLIST.md`، `AGENTS.md` و `LICENSE` — همه موجودند.
- **آگاهانه انجام‌نشده:** برندینگ و URLهای upstream (نشان `build.cloudflare.dev`، دکمهٔ Deploy، لینک issue/discussion)، نام پکیج `vibesdk` و `git clone` دست‌نخورده ماندند — اسکوپ T11 فقط بازگردانی + هم‌خوان‌سازی معماری بود.
- **پیگیری بیرون از اسکوپ → به تسک **T12** ارتقا یافت و اجرا شد (۲۰۲۶-۰۹-۲۷؛ بخش T12 در پایین):** `docs/setup.md` هنوز در سطرهای ۵–۷ («Current generated-app previews use SpaceDO, a Worker Loader binding, and Dynamic Workers…»)، جدول توگل‌ها (`docs/setup.md:210` ردیف `ENABLE_ARTIFACTS`)، سطر ۳۱۱ و سطر ۴۵۰ همان معماری حذف‌شده را «جاری» توصیف می‌کند — در حالی که در درخت فعلی نه binding «ARTIFACTS» وجود دارد (نه در `wrangler.v2.jsonc` نه در `worker/`) و نه `ENABLE_ARTIFACTS` جایی خوانده می‌شود (`grep -rn ENABLE_ARTIFACTS` → فقط `docs/setup.md` + دو ورک‌فلو `\.github/workflows/deploy-*.yml` که آن را به‌صورت var قدیمی ست می‌کنند). این مورد **داخل T11 نبود** و دست‌نخورده ماند.

## T12 — انطباق `docs/setup.md` با معماری دو-پلنی (پیگیری T11) ✅

- **اولویت:** P2 — متوسط
- **وضعیت:** ✅ انجام شد (۲۰۲۶-۰۹-۲۷)
- **وابستگی:** T11 (این مورد در پیگیری بیرون از اسکوپ T11 ثبت شد) ← T10 (همان الگوی «رزرو/غیرفعال»)
- **فایل هدف:** `docs/setup.md` — سطرهای ۷، ۲۰۲، ۲۰۸–۲۲۲، ۳۱۲–۳۲۰، ۳۲۲–۳۲۳، ۴۲۸ و ۴۳۰–۴۴۵؛ به‌همراه به‌روزرسانی وضعیت در همین backlog، `docs/DEV_TASKS_SPEC.md` و `docs/DEV_CHECKLIST.md`
- **شرح:**
  1. **یافته (ثبت‌شده در T11):** چهار ناحیهٔ `docs/setup.md` معماری حذف‌شدهٔ پیش از dual-plane را «جاری» توصیف می‌کردند: سربرگ فایل (`docs/setup.md:7`: SpaceDO + Worker Loader + Dynamic Workers + `ENABLE_ARTIFACTS`)، «Generated-app preview requirements» (`docs/setup.md:202`)، ردیف `ENABLE_ARTIFACTS` در جدول توگل‌ها، و دو ناحیهٔ عیب‌یابی (سطر ۳۱۳ و سطر ۴۵۰).
  2. **سه یافتهٔ نو در حین اجرا:** (الف) `docs/setup.md:428` صریحاً می‌گفت «Current generated-app previews run as Dynamic Workers loaded by SpaceDO»؛ (ب) بلوک «Legacy Corporate Container Setup» به ویرایش `SandboxDockerfile` دستور می‌داد، در حالی که آن فایل در working tree حذف شده است (`git status` → `D SandboxDockerfile`)؛ (ج) دو بخش «Deploy to Cloudflare Button» (سطرهای ۳۱۷–۳۲۱ و ۴۳۰–۴۴۷) مسیر حذف‌شدهٔ dispatch namespace — به‌همراه «custom domain» و «dispatch worker» — را شرط لازم deploy معرفی می‌کردند.
  3. **راستی‌آزمایی (تکرارپذیر، ۲۰۲۶-۰۹-۲۷):** شمار ارجاعِ bindingهای حذف‌شده در کد زنده صفر است — `grep -rn -E 'SPACE_DO|\bLOADER\b|ARTIFACTS' worker/ backend/ wrangler.v2.jsonc src/` → `0`؛ و برای هر هشت توگل داشبورد، خوانندهٔ بیرون از `worker/types/env.d.ts` صفر است (`readers=0` برای `ENABLE_ARTIFACTS`، `ENABLE_READ_REPLICAS`، `ENABLE_EMAIL_AUTH`، `ENABLE_CLOUDFLARE_LIMITS`، `ALLOWED_EMAIL`، `ALLOCATION_STRATEGY`، `USE_CLOUDFLARE_IMAGES`، `USE_TUNNEL_FOR_PREVIEW`). پلن لبه فقط `DB`، `ASSETS`، `VibecoderStore`، `JWT_SECRET`، `GITHUB_EXPORTER_CLIENT_ID`/`_SECRET` و `CONTROL_PLANE_URL` را می‌خواند (`worker/light/lightApp.ts`).
  4. **اصلاح‌ها:**
     - سربرگ `docs/setup.md:7` → بلاک‌وضعیت dual-plane (پیش‌نمایش درون‌مرورگری + deploy به Pages از طریق کنترل‌پلن؛ بدون SpaceDO/Worker Loader/Artifacts/سندباکس).
     - `docs/setup.md:202` → «پیش‌نمایش به هیچ binding نیازی ندارد» + فهرست bindingهای واقعی لایت‌ورکر + نیازمندی `Pages:Edit` و نقش Docker (فقط Redis/کنترل‌پلن به‌صورت لوکال).
     - جدول توگل‌ها (`docs/setup.md:208-222`) → «Read-status note» تأییدشده بالای جدول، ردیف `ENABLE_ARTIFACTS` به «Legacy — not read by any plane»، بقیهٔ ردیف‌ها «Declared only»، و جملهٔ پایانی سطر ۲۲۲ اصلاح شد. برای `ENABLE_EMAIL_AUTH` صریح شد که `worker/light/lightApp.ts:368` مقدار `email: true` را بی‌قیدوبرشرط برمی‌گرداند.
     - دو ناحیهٔ عیب‌یابی (سطرهای ۳۱۲–۳۱۴ و ۴۵۰–۴۵۲) → بر پایهٔ مسیر واقعی: VFS اتاق + `GET /api/projects/:id/files` + سوکت اتاق برای پیش‌نمایش، و `CLOUDFLARE_ACCOUNT_ID` + `Pages:Edit` + لاگ کنترل‌پلن برای شکست deploy.
     - سطر ۴۲۸ و بلوک ۳۲۲–۳۲۳ → صریحاً «historical» علامت خوردند و ادعای «Dynamic Workers loaded by SpaceDO» حذف شد.
     - بخش‌های «Deploy to Cloudflare Button» (سطرهای ۳۱۷–۳۲۰ و ۴۳۰–۴۴۱) → نیازمندی‌های واقعی: کنترل‌پلنِ در حال اجرا + `VITE_CONTROL_PLANE_URL`، `CLOUDFLARE_ACCOUNT_ID` + `Pages:Edit`، و VFS دارای فایل؛ «dispatch namespace» / «dispatch worker» / «wrangler.jsonc» حذف شدند.
  5. هیچ کد production تغییر نکرد (فقط سند + وضعیت پیگیری).
- **معیار پذیرش:** ✅
  - `grep -niE 'spacedo|worker loader|dynamic worker' docs/setup.md` → خروجی خالی.
  - `grep -niE 'dispatch namespace not found|remote dispatch|Dispatch worker' docs/setup.md` → خروجی خالی.
  - `grep -rn -E 'SPACE_DO|\bLOADER\b|ARTIFACTS' worker/ backend/ wrangler.v2.jsonc src/` → `0` ارجاع (کد زنده دست‌نخورده).
  - هیچ ردیف توگلی ادعای «اثر دارد» نمی‌کند؛ همه با `readers=0` تأیید شده‌اند و ردیف `ENABLE_ARTIFACTS` اجزای حذف‌شده را «جاری» معرفی نمی‌کند.
- **تست:** بازبینی متنی + سه grep بالا + `bun run docs:check` → `validate-spec-refs: OK (47 refs in 15 files)` و `Postman contract validation PASSED` (ارجاع جدید `worker/light/lightApp.ts:368` پذیرفته شد؛ پیش از این تسک ۴۶ ارجاع بود).
- **پیگیری → به تسک **T13** ارتقا یافت و اجرا شد (۲۰۲۶-۰۹-۲۷؛ بخش T13 در پایین):** بازنویسی کامل `docs/setup.md` نسبت به دو-پلن انجام نشد. بخش‌های هنوز کهنه: «Prerequisites / For Production Features» با Workers for Platforms + ACM (سطرهای ۹–۲۵)؛ بخش «Login with Cloudflare / AI Gateway connect» که پلن زنده `CLOUDFLARE_OAUTH_CLIENT_ID`/`_SECRET`/`CF_OAUTH_ENCRYPTION_KEY` را نمی‌خواند و `/api/auth/providers` مقدار `cloudflare: false` برمی‌گرداند (`worker/light/lightApp.ts:368`)؛ «Manual Setup» با `wrangler.jsonc` و نام D1 قدیمی `vibesdk-db` (سطرهای ۲۲۲ به بعد)؛ ارجاع به `worker/agents/inferutils/config.ts` که وجود ندارد (سطر ۳۰۶)؛ و فهرست «What Gets Configured» که هنوز `wrangler.jsonc` و «dispatch namespaces» را جزو خروجی `bun run setup` می‌آورد (سطرهای ۴۱۲–۴۱۶). همچنین دو ورک‌فلو `.github/workflows/deploy-staging.yml:61` و `deploy-release-live.yml:58` هنوز `ENABLE_ARTIFACTS: "true"` را ست می‌کنند (خارج از اسکوپ سند).

## T13 — بازنویسی کامل `docs/setup.md` برای دو-پلنی (پیگیری T12) ✅

- **اولویت:** P2 — متوسط
- **وضعیت:** ✅ انجام شد (۲۰۲۶-۰۹-۲۷)
- **وابستگی:** T12 (یافته اینجا ثبت شد) ← T11 (ادعاهای README دربارهٔ `bun run setup` در همین تسک اصلاح شد)
- **فایل هدف:** `docs/setup.md` (بازنویسی)، `docs/archive/setup-legacy.md` (جدید)، `README.md`، `AGENTS.md`، `.github/workflows/deploy-staging.yml`، `.github/workflows/deploy-release-live.yml`؛ به‌همراه وضعیت در همین backlog، `docs/DEV_TASKS_SPEC.md` و `docs/DEV_CHECKLIST.md`
- **شرح:**
  1. **یافتهٔ کلیدی نو:** `bun run setup` در این درخت **قابل اجرا نیست**. `scripts/setup.ts` یک `wrangler.jsonc` در ریشه می‌خواهد (`scripts/setup.ts:1133`) و آن را **بیرون از `safeExecute`** از `validateAndSetupResources` صدا می‌زند (`scripts/setup.ts:532`)، پس اسکریپت با `wrangler.jsonc not found in project root` به `process.exit(1)` می‌رسد. `ls wrangler*` → فقط `wrangler.v2.jsonc` و `wrangler.test.jsonc`. یعنی «Quick Start» قدیمی setup.md و همچنین README (خوراک T11) و AGENTS.md به یک مسیر مرده ارجاع می‌دادند.
  2. **بازنویسی `docs/setup.md` (۳۸۳ → ۳۲۴ سطر):** Prerequisites واقعی (Bun، Go 1.25.5+، Redis با RediSearch) + جدول permissionها فقط برای جریان‌های زندهٔ v2؛ Quick start شش‌مرحله‌ای دستی + هشدار صریح دربارهٔ اسکریپت قدیمی؛ «Configuration values» برای هر سه محل واقعی (`‎.dev.vars` لایت‌ورکر، `.env` ریشه، `backend/`)؛ Domain/network (بدون wildcard DNS/ACM/dispatch)؛ AI provider (نقش→مدل در `backend/skills/`، `AI_GATEWAY_URL` یا Workers AI، به‌جای `worker/agents/inferutils/config.ts`)؛ OAuth (فقط email/password و GitHub — `google: false` و `cloudflare: false`)؛ Manual resource setup (D1 `v2-vibe`، KV `VibecoderStore`، بدون R2/dispatch)؛ Starting development (+ کنترل‌پلن و محدودیت workerd روی macOS < ۱۳.۵)؛ Troubleshooting بازنویسی‌شده؛ Production deployment دو-پلنی؛ Next steps؛ Files that matter.
  3. **`docs/archive/setup-legacy.md` (۲۴۱ سطر، جدید)** طبق الگوی T6/T7: پرامپت‌های اسکریپت V1 (domain/resources/providers)، لیست permission قدیمی (Containers/Cloudchamber/Browser Rendering/R2/Workers for Platforms)، «Login with Cloudflare» با scopes و redirect URLها، R2/templates/ARM64، عیب‌یابی V1 و بلوک CA شرکتی — همه با بنر «Historical / V1» (اسکوپ `docs/archive/**` از ولیدیتور بیرون است).
  4. **`README.md` (سه نقطه) و `AGENTS.md` (یک بولت):** دیگر `bun run setup` را به‌عنوان bootstrap کارآمد معرفی نمی‌کنند و مسیر واقعی (`.dev.vars` دستی) را نشان می‌دهند.
  5. **دو ورک‌فلو:** خط `ENABLE_ARTIFACTS: "true"` حذف شد (متغیر مرده؛ هیچ خواننده‌ای در کد ندارد).
  6. هیچ کد production تغییر نکرد (فقط سند + دو خط var مردهٔ CI).
- **معیار پذیرش:** ✅
  - `grep -ciE 'spacedo|worker loader|dynamic worker' docs/setup.md` → `0`
  - `grep -ciE 'node\.js|setup report' docs/setup.md` → `0`
  - `grep -niE 'vibesdk-db|vibesdk-templates|inferutils|cloudchamber|browser rendering|cloudflared' docs/setup.md` → `3` مورد، همه **نفی/تاریخی** (سطرهای ۳۵، ۲۴۷، ۳۰۷)
  - `grep -n 'wrangler\.jsonc' docs/setup.md` → فقط `4` ارجاع آگاهانه (سطرهای ۴۳، ۷۰، ۷۲، ۱۱۷) که همه «فایل حذف‌شده/قدیمی» را توضیح می‌دهند
  - `grep -rn ENABLE_ARTIFACTS .` (بدون `node_modules`/`.git`/`dist`) → صفر مورد اجرایی؛ فقط `docs/**` (بک‌لاگ و آرشیو)
  - هر دو ورک‌فلو YAML معتبر (پارس با `js-yaml`)، `ENABLE_ARTIFACTS` حذف شده و آخرین step هر دو `bun run build && bunx wrangler deploy --config wrangler.v2.jsonc` است
- **تست:** `bun run docs:check` → `validate-spec-refs: OK (50 refs in 15 files)` + `Postman contract validation PASSED`؛ تعادل fenceها: `docs/setup.md` = ۶ و `docs/archive/setup-legacy.md` = ۱۲؛ پارس YAML دو ورک‌فلو با `node --input-type=commonjs -e "require('js-yaml')"`؛ `bun run lint` → `0 errors, 3 warnings` (کد دست‌نخورده).
- **آگاهانه انجام‌نشده (کاندید تسک بعدی — T14، تغییر کد است نه سند):**
  1. ✅ **انجام شد در T14** — `scripts/setup.ts` به `wrangler.v2.jsonc` پورت شد (به‌همراه رفع باگ گارد اجرا، جلوگیری از ساخت KV ناخواسته و حالت read-only `--check`).
  2. ✅ **انجام شد در T15** — پاک‌سازی کامل بلوک env دو ورک‌فلو: `WRANGLER_CONFIG_PATH: wrangler.staging.jsonc` (که در این درخت وجود ندارد)، `SANDBOX_INSTANCE_TYPE`، `MAX_SANDBOX_INSTANCES`، `DISPATCH_NAMESPACE`، `ALLOCATION_STRATEGY`، `USE_CLOUDFLARE_IMAGES`، `TEMPLATES_REPOSITORY` — به‌همراه افزودن jobهای gating `typecheck`/`lint`/`go test ./...` (هم‌خانوادهٔ G8/G19 در `backend/docs/OUTPUT_QUALITY_BUGS.md`).
  3. تصمیم دربارهٔ خودِ اعلان‌های بی‌خوانندهٔ `worker/types/env.d.ts` (جدول توگل‌ها فعلاً با برچسب «Declared only» مستند شده است).

## T14 — پورت‌کردن `scripts/setup.ts` به `wrangler.v2.jsonc` ✅

- **اولویت:** P2 — متوسط (نوع تغییر: کد)
- **وضعیت:** ✅ انجام شد (۲۰۲۶-۰۹-۲۷)
- **وابستگی:** T13 (کاندید اینجا ثبت شد) ← T11 (ادعاهای README دربارهٔ اجرای اسکریپت)
- **فایل هدف:** `scripts/setup.ts` (کد) + `docs/setup.md`، `README.md`، `AGENTS.md` (هم‌خوان‌سازی متن)؛ به‌همراه وضعیت در همین backlog، `docs/DEV_TASKS_SPEC.md` و `docs/DEV_CHECKLIST.md`
- **شرح:**
  1. **یافتهٔ کلیدی (بدتر از آنچه در T13 ثبت شد):** گارد اجرای مستقیم اسکریپت `import.meta.url === \`file://${process.argv[1]}\`` بود. در مسیری که **فاصله** دارد (مثل این working tree) `import.meta.url` مسیر را با `%20` رمزگذاری می‌کند و مقایسه هرگز True نمی‌شد ⇒ **`bun run setup` بی‌صدا هیچ کاری نمی‌کرد و با exit 0 تمام می‌شد** (پیام خطای `wrangler.jsonc not found` فقط در مسیرهای بدون فاصله رخ می‌دهد). اصلاح: `fileURLToPath(import.meta.url) === resolve(process.argv[1])`.
  2. **مسیر کانفیگ:** تابع صادرشدهٔ `resolveWranglerConfigPath()` که `wrangler.v2.jsonc` را ترجیح می‌دهد و در نبودش به `wrangler.jsonc` (درخت‌های V1) برمی‌گردد و در نبود هر دو خطای صریح می‌دهد؛ `parseWranglerConfig()`، `updateWranglerConfig()` و پیام `deployTemplates` از آن استفاده می‌کنند و گتر `isV2Config` رفتار را شاخه‌بندی می‌کند.
  3. **جلوگیری از بازنویسی کانفیگ کامیت‌شده:** در حالت v2 فقط `id` منابعی نوشته می‌شود که واقعاً ساخته/تغییر کرده‌اند (بدون فلگ قدیمی `remote`) و بلوک routes/`vars.CUSTOM_DOMAIN`/`workers_dev`/`preview_urls` کلاً skip می‌شود. علاوه بر آن اگر id اعلام‌شدهٔ کانفیگ با حساب یکی باشد، هیچ نوشتنی انجام نمی‌شود تا فایل بایت‌به‌بایت دست‌نخورده بماند (جلوگیری از diff صرفاً فرمت‌بندی با تودرتو‌شدن تب/فاصله).
  4. **جلوگیری از ساخت منبع ناخواسته:** `ensureKVNamespace` قبلاً نام `vibesdk-<binding>-local` می‌ساخت که با کانفیگ v2 یعنی ایجاد یک KV جدید و بی‌استفاده و بازنویسی `id` در کانفیگ. اکنون: اول `id` اعلام‌شدهٔ کانفیگ بررسی می‌شود، در صورت نبود، عنوان خودِ binding (`VibecoderStore`) استفاده می‌شود، و لیست KV با `per_page=100` گرفته می‌شود (قبلاً فقط صفحهٔ اول، بدون pagination).
  5. **حالت read-only:** `bun run setup --check` (یا `VIBESDK_SETUP_CHECK=1`) کانفیگ حل‌شده و منابعی را که اجرای واقعی مدیریت می‌کند چاپ می‌کند و **هیچ prompt، تماس شبکه یا نوشتنی** ندارد (readline هم بسته می‌شود) ⇒ قابل اجرا در CI.
  6. **مراحل V1 خودکار skip می‌شوند** چون کانفیگ v2 هیچ‌کدام از `r2_buckets`/`dispatch_namespaces` را ندارد؛ `patchDockerfileForARM64` هم از قبل برای فایل ناموجود گارد دارد.
- **معیار پذیرش:** ✅
  - `bun run setup --check` → `config: <repo>/wrangler.v2.jsonc` · `mode: dual-plane (wrangler.v2.jsonc)` · `KV namespaces: VibecoderStore [56d249930a4447ee92493a7ff061a3ae]` · `D1 databases: DB -> v2-vibe` · `R2 buckets: none` · `dispatch namespaces: none` · `worker: vibesdk-v2 · entry: worker/light-index.ts` و خروج با کد `0` (بدون hang).
  - `bunx tsx scripts/setup.ts --check` (همان مسیری که `bun run setup` استفاده می‌کند) همان خروجی را می‌دهد.
  - `resolveWranglerConfigPath` در یک تست قابل اجرا: «فقط legacy» ✓، «هر دو موجود → v2 برنده» ✓، «ریشهٔ ریپو → v2» ✓، «هیچ‌کدام → خطا» ✓ (۴/۴ PASS).
  - `bunx tsc --noEmit … scripts/setup.ts` → `0` خطا (خط پایه پیش از تغییر هم `0` بود ⇒ بدون رگرسیون تایپ).
  - ایمپورت ماژول (برای تست) اجرای `main()` را trigger نمی‌کند ⇒ گارد درست کار می‌کند.
- **تست:** چهار چک بالا + `bun run docs:check` + `bun run typecheck` + `bun run lint`.
- **آگاهانه انجام‌نشده (شفاف):** مسیر **تعاملی کامل** (promptها + تماس واقعی با Cloudflare) اینجا اجرا **نشد**، چون نیازمند توکن واقعی و ساخت/تغییر منابع در حساب کاربر است؛ به‌جای آن `--check` (read-only)، تست مسیر کانفیگ، و بازرسی کد انجام شد. دو کاندید باقی‌مانده **هر دو در T15 انجام شدند**: ✅ پاک‌سازی بلوک env قدیمی دو ورک‌فلو (`WRANGLER_CONFIG_PATH: wrangler.staging.jsonc`، `SANDBOX_*`، `DISPATCH_NAMESPACE`، `ALLOCATION_STRATEGY`، `USE_CLOUDFLARE_IMAGES`، `TEMPLATES_REPOSITORY`) و ✅ افزودن jobهای gating `typecheck`/`lint`/`go test ./...` در CI.

---

## T15 — پاک‌سازی بلوک env دو ورک‌فلو + jobهای گیت CI (پیگیری T14؛ هم‌خانوادهٔ G8/G19) ✅

- **اولویت:** P2 — متوسط (نوع تغییر: کد/CI)
- **وضعیت:** ✅ انجام شد (۲۰۲۶-۰۹-۲۷)
- **وابستگی:** T14 (کاندید در «آگاهانه انجام‌نشده» همان تسک ثبت شد) ← T13؛ هم‌خانوادهٔ G8/G19 در `backend/docs/OUTPUT_QUALITY_BUGS.md`
- **فایل هدف:** `.github/workflows/deploy-staging.yml`, `.github/workflows/deploy-release-live.yml`, `.github/workflows/ci.yml` (+ وضعیت در همین backlog، `docs/DEV_CHECKLIST.md`, `AGENTS.md`, `README.md`, `backend/docs/OUTPUT_QUALITY_BUGS.md`)
- **شرح:**
  1. **پاک‌سازی env (دقیقاً فهرست T14):** از بلوک `env` هر دو ورک‌فلو حذف شد: `WRANGLER_CONFIG_PATH: wrangler.staging.jsonc` (فایلی که در این درخت وجود ندارد)، `SANDBOX_*` (`SANDBOX_INSTANCE_TYPE`)، `MAX_SANDBOX_INSTANCES`، `DISPATCH_NAMESPACE`، `ALLOCATION_STRATEGY`، `USE_CLOUDFLARE_IMAGES`، `TEMPLATES_REPOSITORY`؛ به‌همراه دو کامنت کهنهٔ «Cloudflare Artifacts access / ARTIFACTS binding» (توگل `ENABLE_ARTIFACTS` در T13 حذف شده بود). دلیل بی‌اثر بودن: step دیپلوی هر دو ورک‌فلو `bunx wrangler deploy --config wrangler.v2.jsonc` را پین می‌کند، پس vars ورکر از همان کانفیگ می‌آید و هیچ‌یک از این envها به ورکر نمی‌رسید.
  2. **یادداشت درون‌فایل:** یک کامنت در بلوک env هر دو ورک‌فلو نام‌های حذف‌شده و دلیل را ثبت می‌کند تا کسی آن‌ها را بی‌دلیل برنگرداند.
  3. **jobهای گیت:** هر دو ورک‌فلو سه job جدید گرفتند — `lint` (`bun run lint`)، `typecheck` (`bun run typecheck`) و `go-test` (`go vet ./...` + `go test ./...` با `actions/setup-go@v5`، `go-version-file: backend/go.mod`، `cache-dependency-path: backend/go.sum` و `defaults.run.working-directory: backend`) — و `deploy` از `needs: test-build` به `needs: [lint, typecheck, test-build, go-test]` تغییر کرد ⇒ تا هر چهار گیت سبز نشوند دیپلوی staging/release-live شروع نمی‌شود (پذیرش G19). job موجود `test-build` (ویتیست) دست‌نخورده ماند و فقط کامنت کهنه‌اش («CI already enforces those») به‌روز شد. هیچ‌یک از jobهای گیت سکرت مصرف نمی‌کند و `if: github.repository_owner == 'cloudflare'` فقط روی `deploy` است.
  4. **Go tests در `ci.yml`:** هیچ ورک‌فلویی تست Go را اجرا نمی‌کرد؛ یک job `go-test` با همان stepها به `ci.yml` اضافه شد تا روی PR/pushهای `main`/`nightly`/`staging` هم اجرا شود (قسمت (۱) باگ G8 با همین بسته شد). `backend/e2e` پشت build tag `e2e` است و در `go test ./...` اجرا نمی‌شود ⇒ بدون نیاز به ردیس، سکرت یا شبکه.
  5. **vars باقی‌مانده عمداً دست‌نخورده:** `CUSTOM_DOMAIN`، `CUSTOM_PREVIEW_DOMAIN`، `ENVIRONMENT`، `PLATFORM_MODEL_PROVIDERS`، `CLOUDFLARE_AI_GATEWAY`، `CLOUDFLARE_AI_GATEWAY_TOKEN`، `CLOUDFLARE_AI_GATEWAY_URL` در بلوک env ماندند چون در فهرست T14 نبودند؛ خوانندهٔ زندهٔ خارجی ندارند (فقط `scripts/setup.ts` مقادیر `CUSTOM_DOMAIN`/`ENVIRONMENT` را در `.dev.vars`/`.prod.vars` می‌نویسد) و با همان کامنت علامت خورده‌اند.
- **معیار پذیرش:** ✅
  - grep نام‌های حذف‌شده در `.github/workflows/*.yml` فقط خطوط کامنت همان دو فایل را برمی‌گرداند (۰ کلید env).
  - پارس YAML سه فایل با `yaml.parse`: `ci.yml` → jobهای `ci`, `go-test`؛ دو ورک‌فلوی دیپلوی → `lint`, `typecheck`, `go-test`, `test-build`, `deploy` با `deploy needs = [lint, typecheck, test-build, go-test]`؛ `deploy env` دیگر هیچ‌یک از هفت نام را ندارد.
  - خط پایهٔ محلی سبز: `go vet ./...` → کد ۰، `go test ./...` → همهٔ بسته‌ها `ok`، `bun run typecheck` → 0، `bun run lint` → `0 errors, 3 warnings`.
- **تست:** `go vet ./... && go test ./...`، `bun run typecheck`، `bun run lint`، `bun run docs:check` + پارس YAML + grepهای بالا.
- **آگاهانه انجام‌نشده (شفاف):** اجرای واقعی ورک‌فلوها روی رانر GitHub انجام نشد (نیازمند push و اعتبار حساب)؛ اعتبارسنجی محلی = پارس YAML، بررسی `jobs`/`needs`، و تطبیق دستور هر job با همان دستورهایی که در همین ماشین سبز شدند. `vars` باقی‌ماندهٔ بند ۵ کاندید پاک‌سازی بعدی است.

---

## وابستگی‌ها (نمودار)

```
T1 ──► T2 ──► T3 ──► T4
              │
              └──► T5 (مستقل)

T6 ──► T7 ✅ (هر دو انجام شد)

T8 ✅ (مستقل)
T9 ✅ (مستقل)
T10 ✅ ──► T11 ✅ (README.md بازگردانده و dual-plane شد — ۲۰۲۶-۰۹-۲۷)
T13 ✅ ──► T14 ✅ (پورت scripts/setup.ts به wrangler.v2.jsonc — ۲۰۲۶-۰۹-۲۷)
T14 ✅ ──► T15 ✅ (پاک‌سازی env دو ورک‌فلو + jobهای گیت CI — ۲۰۲۶-۰۹-۲۷)
```

## تعریف Done برای هر task

- [ ] کد/سند هدف ویرایش شده.
- [ ] معیار پذیرش بالا تیک خورده.
- [ ] تست مربوطه (در صورت وجود) سبز.
- [ ] `bun run typecheck && bun run lint && bun run build` سبز (برای taskهای کد).
- [ ] در `DEV_CHECKLIST.md` یا `docs/DEV_TASKS_SPEC.md` به‌عنوان انجام‌شده علامت خورده.
