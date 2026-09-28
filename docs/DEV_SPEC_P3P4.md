# SPEC P3+P4 — Suspense/AutoFix/Circuit + Sandbox

## P3.1 — Suspense صفر-توکن (با P1.10.4 یکی شود)

- محل: `notifyWriteTool` در `backend/pkg/engine/plan_execute.go` + pass جدید `src/components/preview/preview-normalize.ts`.
- ۴ ترمیم قطعی، هرکدام rule جدا با unit-test: (۱) `id` تکراری/گمشده → id یکتا؛ (۲) `src` ناموجود در VFS → nearest-path یا بنر؛ (۳) `var(--*)` تعریف‌نشده → از brief یا بنر؛ (۴) تگ باز → بستن خودکار.
- تست: هر rule یک کیس مثبت + یک منفی؛ متریک: روی ۲۰ generation نمونه، چند خطا بدون کال مدل گرفته شد (لاگ شمارنده).

## P3.2 — prompt-cache-aware + RAG کاتالوگ

- قانون: سکشن‌های static (`00/01/02/03` هسته + قرارداد خروجی) همیشه اول با wording فریز؛ داینامیک (VFS snapshot + brief مرتبط) همیشه آخر؛ tool-result داخل ناحیه cacheable نماند.
- RAG: به‌جای paste کامل `design.Brief()` فقط سکشن مرتبط (embed query=goal، top-2)؛ برای نودها فقط manifest نودهای استفاده‌شده در plan.
- تست: (۱) snapshot تست: ترتیب سکشن‌های پرامپت نهایی ثابت؛ (۲) E2E: رشد کاتالوگ به ۲۰ نود → context coder بیش از X٪ رشد نکند (عدد در تست).

## P3.3 — لاگ RFT (فقط لاگ؛ مثال‌ها با P2.6.6 یکی است)

- هر `REQUEST_CHANGES` reviewer + هر خطای runtime نود با الگو (`{category, file/kind, before, verdict}`) در `workflow_step_logs`/جدول جدا لاگ شود.
- تست: سناریو خطا → ردیف لاگ با همه فیلدها؛ آستانه ۵۰۰ نمونه برای LoRA آینده (فعلاً فقط شمارنده).

## P3.4 — Circuit Breaker

- P3.4.1: هوک pre-persist در Go: گارد «حذف فاجعه‌بار» (حذف >۵۰٪ فایل‌ها در یک generation بدون تأیید → بلاک) + سقف حجم + bailout: بعد از ۳ تلاش ناموفق همان فایل → توقف + بنر «مداخله انسانی» (مبنای ZeroLabs: شانس موفقیت بعدش <۱۲٪).
- P3.4.2: fix موضعی با temperature 0 روی همان بلوک (نه rewrite فایل، نه patch یکپارچه off-by-one‌ساز).
- P3.4.3: هر دو نگه داشته شود: reflect درونی (پرامپت coder: «قبل از write، یک بازبینی ۳خطی») برای خطای سطحی + reviewer بیرونی برای verdict (تفکیک قوا برای governance).
- تست: (۱) حذف انبوه → بلاک؛ (۲) ۳ fail متوالی → bailout + بنر؛ (۳) micro-pass → diff فقط همان بلوک.

## P3.5 — کالبدشکافی‌های باز (اختیاری، بعد از P2)

- Temporal (مقایسه step.do با activity برای فلوهای ساعتی/روزانه)؛ Dify Human-Input؛ Mastra runners. خروجی هرکدام: یادداشت تصمیم یک‌صفحه‌ای، نه کد.

## P4 — Sandbox (خارج از light worker؛ دلیل: CPU ۱۰ms در پلن Free و نبود sandbox/container binding — **نه** سقف ۳MiB که در ۲۰۲۶-۰۹-۰۴ حذف شد، `docs/CF_LIMITS.md` §۴)

- P4.1: Sandbox per-chat در کنترل‌پلین/کانتینر جدا: lazy-create در اولین نیاز → FS پایدار بین sessionها → TTL ۳۰min + heartbeat تمدید + سقف ۲۴h. داخلش: Node+runner، Bash tool و ایجنت در همان FS.
- P4.2: Verifier واقعی (آستانه P5.6/P6.7 را خودش چک کند) + Dual-key (کلید app جدا از کلید executor محدودشده).
- تست: (۱) create→write→reconnect→read پایدار؛ (۲) انقضای TTL → recreate تمیز؛ (۳) executor-key نتواند بیرون FS بخواند.
