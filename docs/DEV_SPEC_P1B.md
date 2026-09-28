# SPEC P1B — P1.6 تا P1.10 (reviewer + gate + claim + شناسنامه‌دار)

## P1.6 — reviewer + hub-awareness

- P1.6.1: به `backend/skills/03_reviewer.md` یک بخش: «diff نسل قبلی را هم ببین؛ نظرت باید نسبی باشد (این تغییر درست است؟) نه مطلق (این فایل تمیز است؟)».
- P1.6.2: در `backend/pkg/engine/team.go` موقع ساخت task reviewer، لیست `changed_files` (path+op از `generation_files` جاری) ضمیمه شود — فقط path+op، نه محتوا (context باد نکند).
- P1.6.3: hub-awareness: تابع `HubFiles(plan, vfs) []string` — فایل‌های با بیشترین inbound-ref (مثل `store.js`)؛ در task reviewer خط `HUB: {files} — تغییر این‌ها blast-radius بالا دارد`. ⚠️ نام `backend/pkg/engine/hub.go` از قبل برای `EngineHub` (رجیستری roomها) اشغال است — این تابع را در فایل جدا بگذار (`backend/pkg/engine/team_hub.go`) و با `EngineHub` قاطی نکن.
- تست: unit `HubFiles` (plan سه‌فایله → hub درست)؛ E2E: تغییر hub → verdict reviewer به آن اشاره کند (دستی).

## P1.7 — اعتبارسنجی P1 (در DEV_CHECKLIST.md)

> **توجه:** مشخصات تفصیلی و چک‌لیست اعتبارسنجی P1 (تست‌های `go vet/test`، بیلد فرانت و سناریوهای E2E rollback/push) مستقیماً در `docs/DEV_CHECKLIST.md` (بخش `P1.7 — اعتبارسنجی P1`) مستند شده‌اند و اسپک مجزای پیاده‌سازی کد ندارند.


## P1.8 — difficulty gate (`canRunTeam`)

- هدف: تسک آسان (≤۲ فایل، بدون وابستگی بین steps) → تک-coder؛ وگرنه تیم کامل.
- پیاده‌سازی در `backend/pkg/engine/team.go` (یا فایل جدا `backend/pkg/engine/gate.go`): `ShouldUseTeam(plan) bool` = `len(steps) > 2 || dependencyDensity > 0`. تصمیم در `generation_audit` لاگ شود (`actor=system, action=gate_decision`).
- تست: (۱) plan تک‌فایل → false؛ (۲) plan چهارفایل با وابستگی → true؛ (۳) مرزی (۲ فایل با وابستگی) → true؛ (۴) لاگ audit ثبت شده.

## P1.9 — ابزار `vfs_claim`

- هدف: سیگنال مالکیت path (نه اشتراک history). API در `teamTools`: `vfs_claim{path, holder}` → ok/denied؛ `vfs_release{path}`؛ `vfs_status{}` → لیست claimها.
- P1.9.2: coordinator قبل از delegate به coder روی pathهای plan claim بگیرد؛ بعد از اتمام release؛ timeout خودکار ۱۰ دقیقه (claim یتیم نماند).
- تست: (۱) دو claim همزمان یک path → دومی denied؛ (۲) release → claim بعدی ok؛ (۳) timeout → آزاد شدن خودکار. برای تست‌پذیری clock را تزریق‌پذیر کن (`now func() time.Time` در ساختار claim store) — در `pkg/engine` هیچ انتزاع زمانی تزریق‌پذیری وجود ندارد و همه‌جا `time.Now` مستقیم استفاده می‌شود، پس بدون این کار تست timeout فلیکی می‌شود.

## P1.10 — تولید شناسنامه‌دار + خط لوله دستی

- P1.10.1: به `backend/skills/02_coder.md`: «هر section ریشه دقیقاً این ۴ attribute: `data-vibe-block` (از رجیستری `backend/pkg/design/blocks.go`)، `data-vibe-section`، `data-vibe-id` (یکتا، پیشوند `b-`)، `data-vibe-slots` (JSON معتبر)». ولیدیتور Go در finalize: id تکراری/گمشده → Suspense ترمیم (id بساز؛ block را از ترتیب plan حدس بزن)؛ block ناموجود → `unmanaged`.
- P1.10.2: موقع finalize، پارس `index.html` → `vibe.meta.json` در VFS: `[{id, block, section, slots}]`. فرانت فقط این را می‌خواند.
- P1.10.3: طبق P1.3.5 (جدا نزن) — فقط مطمئن شو `author` از هر ۴ کانال (Monaco/Content/GrapesJS/import) می‌آید.
- P1.10.4: دو پاس جدا (نام‌ها قاطی نشوند): (الف) **فرانت** — `src/components/preview/preview-normalize.ts`، تابع فعلی `sanitizeJs` که امروز فقط regex است (`src/components/preview/preview-normalize.ts:274-288`) → افزودن `syntaxCheck` با `acorn` (JS) + `htmlparser2` (HTML) + `jsonc-parser` (jsonc)؛ خروجی `{status: ok|broken, errors: [{file, line, msg}]}`. (ب) **Go** — `llm.SanitizeJS` (`backend/pkg/llm/sanitize.go:33`؛ مصرف در `backend/pkg/engine/plan_execute.go:91`). در هر دو حالت **ذخیره همیشه انجام شود** (broken = بنر، نه reject). با P3.1 یکی شود.
  - توجه: هر سه پکیج در `package.json` هستند (acorn ^8.14، htmlparser2 ^10، jsonc-parser ^3.3.1) ولی امروز در `src`/`worker` هیچ‌جا import نمی‌شوند. چانک فعلی `index` ۲.۶۷MB و `editor.api` ۲.۶۶MB است؛ بعد از افزودن acorn سایز را چک کن.
- P1.10.5: `backend/pkg/design/identity.go`: `ExtractIdentity(html) []BlockRef` + چک reviewer «قابل‌شناسایی است؟»؛ نباشد → `unmanaged` (degrade شفاف، نه fail).
- P1.10.6: G3: پایه را روی تشخیص موجود بگذار، نه `design.resolve()` (چنین تابعی در `backend/pkg/design` وجود ندارد): `missingReferencedAssets(files)` در `backend/pkg/engine/gapfill.go:97` (سطح path، با تست‌های موجود `TestMissingReferencedAssets*`). آن را به سطح DOM-id گسترش بده → لیست `{file, ref, nearest}` (nearest با edit-distance) → بنر فرانت + دکمه micro-fix صفر-توکن («JS را به #menue وصل کن»).
- P1.10.7: طبق P1.1.1b (جدا نزن) — فقط بنر «مال من / مال ایجنت / هر دو را ببین» در فرانت + تست fork.
- P1.10.8: بنر preview سه‌رنگ: قرمز (broken) + «برگرد به آخرین سالم»؛ زرد (warning wiring)؛ خاکستری (unmanaged) + «با AI درست کن» (task با کانتکست block).
- P1.10.9: `Monaco` از `readOnly` به editable + دکمه Save صریح (نه autosave)؛ lazy-load حفظ شود (هشدار G16: چانک ۲.۶MB).
- تست‌ها: (۱) `identity_test.go`: HTML شناسنامه‌دار → ۳ BlockRef درست؛ بدون attribute → empty + unmanaged؛ (۲) syntax: JS خراب → broken + خطِ line درست + فایل ذخیره شده؛ (۳) wiring: id عوض‌شده → missing-ref + nearest درست؛ (۴) fork: write دستی حین generating → دو شاخه + بنر؛ (۵) E2E سناریوهای ۱-۳ پیام ورکفلو (دستی).
