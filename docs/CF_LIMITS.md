# CF LIMITS — سقف‌های واقعی Cloudflare که اسپک‌های ما به آن‌ها وابسته‌اند

> **تاریخ آخرین بازبینی این فایل: ۲۰۲۶-۰۹-۲۵** (بازبینی قبلی: ۲۰۲۶-۰۹-۲۳).
> **قرارداد منبع (الزامی — T9):** هر ادعای زمان‌محور یک شناسهٔ منبع دارد (`[S1]`…`[S6]`) و هر شناسه در §۷ به «لینک رسمی + `Last updated` خودِ سند + `Last verified` (تاریخ بازبینی ما)» نگاشته شده است؛ برای جدول‌های یک‌منبعی §۳/§۵/§۶، بلوک `Source`/`Last verified` سطح-بخش پوشش‌دهندهٔ همهٔ ردیف‌ها است. ادعاهای مشتق/اندازه‌گیری خودمان با `[derived]`، و مقدارهایی که در داک رسمی پیدا نشدند با `⚠️ unverified` علامت می‌خورند (نمونه در §۲). هیچ عدد تیک‌نخورده‌ای بدون منبع و تاریخ بازبینی در این فایل نیست.
> قاعده: هر عددی که در `docs/DEV_SPEC_*` نوشته می‌شود و منبعش اینجا نیست، قبل از تیک‌زدن باید از داک رسمی تأیید شود.
> چرا این فایل: راهنمای معماری (`docs/llm.md` + آرشیو تاریخی `docs/archive/llm-legacy.md`) جای مرجع عددی نیست — این فایل فقط «عدد + منبع + اثر روی تسک» است.

## ۱) Cron Triggers (مرجع P6.6.2)

**Source:** `[S1]` Cron Triggers (سینتکس و انتشار) · `[S2]` Scheduled Handler (رفتار runtime) · `[S3]` Workers Limits (سقف account plan و CPU/wall time)
**Last verified:** 2026-09-25 — همهٔ ردیف‌ها روی داک رسمی همین تاریخ دیده شدند.

| مورد | مقدار | منبع |
|---|---|---|
| تایم‌زون | **UTC** (قابل تغییر نیست) | `[S1]` |
| گرانولاریتی | ۵ فیلد؛ دقیقه `0-59` ⇒ **کوچک‌ترین بازه = ۱ دقیقه** (`* * * * *` = «At every minute») | `[S1]` |
| اکستنشن‌ها | Quartz-like: `* , - /` + در روز ماه `L W` و روز هفته `L #` | `[S1]` |
| نام‌ماه/روز هفته | ۳ حرفی و case-insensitive (`JAN`, `aug`, `fri`) | `[S1]` |
| روز هفته | **۱ = یکشنبه … ۷ = شنبه** (برخلاف cronهای رایج که ۰=Sunday) ⇒ در UI از `SUN`/`MON` استفاده کن | `[S1]` |
| تعداد Cron Trigger | **۵ per account (Free)** / **۲۵۰ per account (Paid)** — ⚠️ per **account** است، نه per Worker | `[S3]` |
| انتشار تغییرات | «may take several minutes (**up to 15 minutes**) to propagate to the Cloudflare global network» ⇒ در UI/تست نگو «فوری اعمال شد» | `[S1]` |
| Wall time هر invocation | **۱۵ دقیقه** (Free و Paid) | `[S3]` |
| CPU هر invocation | **۱۰ ms (Free)** / **۳۰s برای بازهٔ <۱ ساعت** و **۱۵ دقیقه برای بازهٔ ≥۱ ساعت (Paid)** | `[S3]` |
| `ctx.waitUntil` | تا ۳۰ ثانیه بعد از پایان پاسخ | `[S2]` `[S3]` |
| انتظار runtime | تا ۱۵ دقیقه منتظر resolve شدن promise هندلر `scheduled()` می‌ماند | `[S2]` |

**دقت اجرا (مهم برای P6.6.2) `[S1]`:** داک هیچ تضمین/SLA برای «دقیقاً همان دقیقه» نمی‌دهد؛ فقط می‌گوید scheduled Workers «on underutilized machines … to make the best use of Cloudflare's capacity» اجرا می‌شوند.
⇒ تسک P6.6.2 باید **idempotency + پنجرهٔ تحمل** را الزام کند (مثلاً «اگر امروز یک بار اجرا شده، دوباره اجرا نکن»)، نه فرض دقیق‌بودن زمان.

## ۲) Workflows — مسیر پیشنهادی برای cron (P6.6.1/P6.6.2)

**Source:** `[S5]` Trigger Workflows (`schedules` روی binding) · `[S4]` Workflows Limits (بودجهٔ concurrency و سقف‌های اجرا)
**Last verified:** 2026-09-25

به‌جای `scheduled()` دستی، خودِ Workflow از `schedules` روی binding پشتیبانی می‌کند:

| سقف | مقدار | منبع |
|---|---|---|
| حداکثر schedule (cron expression) per account | **۱۰۰** («add a `schedules` array (up to 100 cron expressions per account)») | `[S5]` |
| حداکثر طول cron expression | ⚠️ **unverified** — عدد ۲۵۶ کاراکتر در هیچ‌یک از S4/S5 پیدا نشد (توضیح زیر جدول) | — |
| در Workers Paid، instanceهای cron-triggered | تا **۱ ساعت به‌ازای هر firing** بدون مصرف اسلات concurrency اجرا می‌شوند؛ بعد از آن وارد صف عادی می‌شوند (fail/timeout نمی‌شوند) | `[S4]` |

> ⚠️ **unverified — ردیف «حداکثر طول cron expression» `[S4]` `[S5]`:** در بازبینی ۲۰۲۶-۰۹-۲۵ این عدد پیدا نشد؛ `[S5]` فقط «up to 100 cron expressions per account» را می‌گوید و `[S4]` هیچ ردیفی برای schedule یا طول cron ندارد (نمای `node_modules/wrangler/config-schema.json` هم فیلدی برای این سقف نشان نمی‌دهد). تا وقتی با تست واقعی (deploy یک عبارِ بلندتر از ۲۵۶ کاراکتر) یا پاسخ پشتیبانی Cloudflare تأیید نشود، **به این عدد در اسپک‌ها استناد نکن**.

## ۳) Workflows — جدول سقف‌ها (مرجع P6.5، P6.6، P2.3)

**Source:** `[S4]` Workflows Limits — هر ردیف جدول زیر ۱:۱ از جدول همان صفحه است (ردیف «حداکثر اجرا» به سقف روزانهٔ Workers گره خورده ⇒ `[S4]` + `[S3]`).
**Last verified:** 2026-09-25

| سقف | Workers Free | Workers Paid |
|---|---|---|
| Compute time per step | **۱۰ ms** | ۳۰s (پیش‌فرض) / قابل افزایش تا **۵ دقیقه** |
| Duration (wall) per step | Unlimited | Unlimited |
| حداکثر نتیجهٔ step غیر-stream | **۱ MiB** | **۱ MiB** |
| حداکثر حجم event payload | **۱ MiB** | **۱ MiB** |
| حداکثر state هر instance | **۱۰۰ MB** | **۱ GB** |
| حداکثر `step.sleep` | ۳۶۵ روز | ۳۶۵ روز |
| حداکثر steps هر Workflow | **۱٬۰۲۴** | **۱۰٬۰۰۰** (قابل افزایش تا ۲۵٬۰۰۰) |
| حداکثر اجرا | **۱۰۰٬۰۰۰ در روز** (مشترک با سقف روزانهٔ Workers) | Unlimited |
| instanceهای همزمان (running) per account | **۱۰۰** | **۵۰٬۰۰۰** |
| نرخ ساخت instance | **۱۰۰ در ثانیه** | ۳۰۰/s per account و ۱۰۰/s per workflow |
| instanceهای در صف | ۱۰۰٬۰۰۰ | ۲٬۰۰۰٬۰۰۰ |
| **Retention حالت instance تمام‌شده** | **۳ روز** | **۳۰ روز** |
| Subrequests | ۵۰/request | ۱۰٬۰۰۰/request (تا ۱۰ میلیون) |
| Retries هر step | ۱۰٬۰۰۰ | ۱۰٬۰۰۰ |
| طول نام Workflow / instance id | ۶۴ / ۱۰۰ کاراکتر | همان |

نکات:
- instanceهای در حالت **waiting** در سقف concurrency **شمرده نمی‌شوند** (فقط running).
- Workflows روی Workers for Platforms قابل deploy نیست.
- **Retention ۳ روزه (Free)** یعنی `workflow_instances/step_logs` خودت مرجع ماندگار observability (P6.8) است — نباید به state خودِ Workflow تکیه کنی.

## ۴) 🔴 سقف حجم Worker تغییر کرده (مرجع P2.8.2، P6.7، P4)

**Source:** `[S6]` Changelog 2026-09-04 · `[S3]` Workers Limits (ردیف `Worker size`) · `[S4]` Workflows Limits (شاهد متن قدیمی که هنوز به‌روز نشده)
**Last verified:** 2026-09-25

- ❌ **سقف قدیمی «۳MB فشرده (Free) / ۱۰MB فشرده (Paid)» در ۴ سپتامبر ۲۰۲۶ حذف شد.** `[S6]`
- ✅ سقف فعلی: **۶۴ MiB برای هر دو پلن — روی حجم فشرده‌نشدهٔ باندل**. Wrangler فقط عدد `Total Upload` را می‌شمارد؛ عدد `gzip` صرفاً اطلاعی است. `[S6]` `[S3]`
- چک کردن قبل از deploy: `wrangler deploy --outdir bundled/ --dry-run` → خط `Total Upload: ... KiB / gzip: ... KiB` `[S6]`.
- ⚠️ صفحهٔ Workflows هنوز عبارت قدیمی «3MB max script size (Free) / 10MB (Paid)» را نشان می‌دهد ⇒ متن داک عقب‌تر از changelog است؛ مرجع = changelog + جدول `Worker size`. `[S4]` `[S3]`

**اثر روی ما (این مهم‌ترین نتیجهٔ این فایل است):** استدلال «light worker باید سبک بماند چون سقف ۳MiB» **دیگر معتبر نیست**.
آنچه **هنوز واقعی است**: CPU ۱۰ms در پلن Free، wall time ۱۵ دقیقه برای cron/queue، نبود DO/sandbox/container در bindingهای فعلی، و مزیت زمان startup (سقف ۱ ثانیه). `[S3]`
⇒ در اسپک‌ها «۳MiB» را از «محدودیت پلتفرم» به **«بودجهٔ خودمان»** تبدیل کن (مثلاً باندل gzip زیر ۵MiB برای latency/preview)، وگرنه gate مرج روی عددی است که وجود ندارد.
**اثر روی ما `[derived]`:** این پاراگراف تحلیل داخلی است؛ اعداد پایه‌ای‌اش (۶۴ MiB، CPU ۱۰ms (Free)، wall ۱۵ دقیقه، startup ۱ ثانیه) منبع‌های ردیف‌های بالا را دارند.

## ۵) سایر عددهای مرتبط

**Source:** `[S3]` Workers Limits — هر ردیف جدول زیر از جدول‌های `Account plan limits` و `Request and response limits` همان صفحه است.
**Last verified:** 2026-09-25

| سقف | Workers Free | Workers Paid |
|---|---|---|
| Requests | ۱۰۰٬۰۰۰/روز | No limit |
| CPU per HTTP request | **۱۰ ms** | ۵ دقیقه (پیش‌فرض ۳۰ ثانیه) |
| Memory | ۱۲۸ MB | ۱۲۸ MB |
| Subrequests | ۵۰/request | ۱۰٬۰۰۰/request |
| Simultaneous outgoing connections/request | ۶ | ۶ |
| Environment variables | ۶۴/Worker | ۱۲۸/Worker (هر کدام ≤ ۵ KB) |
| Worker startup time | ۱ ثانیه | ۱ ثانیه |
| Number of Workers | ۱۰۰ | ۵۰۰ |
| Static assets | ۲۰٬۰۰۰ فایل/نسخه | ۱۰۰٬۰۰۰ فایل/نسخه (هر فایل ≤ **۲۵ MiB**) |
| URL / header | ۱۶ KB URL، ۱۲۸ KB request+response header | همان |
| Max request body (بسته به پلن zone) | ۱۰۰ MB (Free و Pro)، ۲۰۰ MB (Business)، تا ۵ GB (Enterprise) | — · `[S3]` |

نکته برای SPA ما `[derived]` — اندازه‌گیری روی بیلد `dist/client` (بیلد ۲۰۲۶-۰۹-۲۴، بازبینی ۲۰۲۶-۰۹-۲۵): **۱۴۹ فایل**؛ بزرگ‌ترین فایل `ts.worker-*.js` ≈ **۶٬۹۱۵٬۶۹۲ بایت (~۶.۶ MiB)** و بزرگ‌ترین چانک خودِ اپ `index-*.js` ≈ **۲٬۶۷۰٬۶۱۳ بایت (~۲.۶۷ MB)** ⇒ هر دو خیلی زیر سقف ۲۵ MiB هر فایل و ۲۰٬۰۰۰ فایل‌اند، ولی تعداد فایل/حجم را در بیلدهای بزرگ چک کن. دستور بازبینی در §۷.

## ۶) نقشهٔ اثر روی تسک‌ها

**Source:** `[derived]` — این جدول تحلیل داخلی است؛ هر عدد به منبع ردیف متناظرش در §۱–§۵ اشاره می‌کند (بدون سند پلتفرمی مستقل).
**Last verified:** 2026-09-25 (اعداد پایه دوباره تأیید شدند)

| تسک | عدد مرتبط | اقدام لازم |
|---|---|---|
| P6.6.2 | cron: UTC، دقیقه‌ای، ۵/۲۵۰ per account، wall ۱۵ دقیقه، CPU ۱۰ms(Free) | تسک را با این اعداد بنویس؛ برای cronهای پرتکرار مسیر **Workflow `schedules`** را ترجیح بده |
| P6.6.1 (webhook) | payload رویداد **≤ ۱ MiB** | در هندلر، بدنهٔ بزرگ‌تر → ۴۱۳ + لاگ؛ تست با بدنهٔ ۱MiB+ |
| P6.5.2 / P2.3 (step output) | نتیجهٔ step غیر-stream **≤ ۱ MiB** | خروجی بزرگ → `ReadableStream` یا ارجاع به R2/D1 (نه بازگشت مستقیم) |
| P2.8.2 / P6.7 | سقف واقعی **۶۴ MiB فشرده‌نشده** (۳MiB حذف شده) | آستانه را به بودجهٔ تیم تغییر بده + `Total Upload` را در CI لاگ کن |
| P2.2 / P2.6 (ولیدیتور DAG) | steps **۱۰۲۴ (Free) / ۱۰٬۰۰۰ (Paid)** | ولیدیتور سقف تعداد نود را هم چک کند |
| P5.1 (app runtime) | state هر instance ۱۰۰MB/۱GB؛ retention ۳/۳۰ روز | دادهٔ app در D1 بماند، نه در state خودِ Workflow |
| P6.8 (observability) | retention ۳ روز در Free | منبع حقیقت لاگ‌ها `workflow_step_logs` (D1) باشد، نه حالت Workflow |
| P4 (Sandbox) | سقف اسکریپت دیگر مانع نیست؛ CPU ۱۰ms(Free) هست | توجیه «کانتینر جدا» را به CPU/عدم binding پایه‌گذاری کن، نه به سقف ۳MiB |

## ۷) منابع، شناسه‌ها و تاریخ بازبینی (بازبینی ۲۰۲۶-۰۹-۲۵)

**Source:** خودِ این جدول — نگاشت `[S1]`…`[S6]` به لینک رسمی Cloudflare.
**Last verified:** 2026-09-25 (ستون `Last updated` = تاریخی که خودِ سند در همان لحظه اعلام می‌کرد).

| شناسه | منبع | لینک | Last updated (سند) | Last verified (ما) |
|---|---|---|---|---|
| `[S1]` | Cron Triggers (فیلدها، گرانولاریتی، UTC، `L W #`، ۱=یکشنبه، انتشار تا ۱۵ دقیقه) | https://developers.cloudflare.com/workers/configuration/cron-triggers/ | 2026-09-04 | 2026-09-25 |
| `[S2]` | Scheduled Handler (انتظار ۱۵ دقیقه، `controller.cron`، `ctx.waitUntil`) | https://developers.cloudflare.com/workers/runtime-apis/handlers/scheduled/ | 2026-09-04 | 2026-09-25 |
| `[S3]` | Workers Limits (account plan، CPU، Worker size، static assets، wall time، request/response) | https://developers.cloudflare.com/workers/platform/limits/ | 2026-09-05 | 2026-09-25 |
| `[S4]` | Workflows Limits (جدول Free/Paid، steps، payload، state، retention، concurrency، بودجهٔ cron) | https://developers.cloudflare.com/workflows/reference/limits/ | 2026-09-21 | 2026-09-25 |
| `[S5]` | Trigger Workflows (`schedules` روی binding؛ «up to 100 cron expressions per account») | https://developers.cloudflare.com/workflows/build/trigger-workflows/ | 2026-09-17 | 2026-09-25 |
| `[S6]` | Changelog: حذف سقف فشرده و ۶۴ MiB فشرده‌نشده | https://developers.cloudflare.com/changelog/post/2026-09-04-increased-worker-size-limit/ | 2026-09-04 | 2026-09-25 |

**روش بازبینی (تکرارپذیر، فقط خواندنی):**
1. نسخهٔ مارک‌داون هر صفحه را بگیر: `https://developers.cloudflare.com/<مسیر صفحه>/index.md` (مثلاً `.../workers/platform/limits/index.md`). اگر پاسخ ناقص برگشت، همان مارک‌داون را با پیشوند `https://r.jina.ai/` بخوان — بخش‌های وسط صفحه (مثل «Supported cron expressions») در fetch معمولی بریده می‌شوند.
2. ستون `Last updated` هر صفحه را از همان مارک‌داون بردار؛ اگر از `Last verified` این فایل جلو زد، ادعاهای آن منبع (`[S1]`…`[S6]`) باید دوباره چک شوند.
3. اندازه‌گیری بیلد SPA `[derived]`: `stat -f '%z %N' dist/client/assets/* | sort -rn | head` و `find dist/client -type f | wc -l`.

> **قاعدهٔ به‌روزرسانی:** هر بار یکی از صفحات بالا تغییر کرد، `Last verified` همان منبع و ادعاهای وابسته در §۱–§۵ را با تاریخ جدید به‌روز کن؛ اگر عددی در داک رسمی پیدا نشد، آن را `⚠️ unverified` علامت بزن (نمونه: §۲) و مبنای تیک‌زدن نکن.
>
> **قاعدهٔ ماشینی (T16):** بخشی از این قرارداد در `bun run docs:check` اجبار می‌شود — `scripts/validate-docs-invariants.mjs` بررسی می‌کند که (۱) هر `[Sn]` ارجاع‌داده‌شده در این فایل در جدول §۷ تعریف شده باشد، (۲) هر `## ` سکشن خط `**Last verified:**` خودش را داشته باشد، و (۳) هر ردیف §۷ هم لینک رسمی و هم دو تاریخ (تاریخ سند + تاریخ بازبینی ما) را نگه دارد. پس افزودن عدد/منبع جدید بدون به‌روزرسانی §۷ باعث fail شدن gate می‌شود. **بازبینی زندهٔ صفحات کلودفلر همچنان گام دستی و شبکه‌ای است** (روش مرحله‌به‌مرحله در بالای همین بخش) و ماشینی نمی‌شود.
