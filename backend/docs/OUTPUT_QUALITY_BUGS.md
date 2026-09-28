# کل باگ‌های خروجی کد، ساختار و پیش‌نمایش (بررسی عمیق)

این سند فهرست کامل باگ‌هایی است که بررسی عمیق زنجیرهٔ
«پرامپت → پلن → تولید کد → VFS/ردیس → eventهای WS → انباشت فرانت → پیش‌نمایش»
پیدا کرد. هر باگ یه ساب‌تسک با معیار پذیرش (Acceptance) دارد؛ بعد از دیباگ،
چک‌مارک بزن.

مسیرهای اصلی:
- **dual-model** (`pkg/engine/dual_model.go` — پلنر JSON → coder‌های پله‌ای)
- **legacy fence-streaming** (`pkg/engine/room.go` — تک‌استریم با retry + gap-fill)
- **plan-execute** (`pkg/engine/plan_execute.go` — prebuilt eino)
- **پیش‌نمایش فرانت** (`src/components/preview/*`, `src/routes/chat/**`)

**فهرست بخش‌ها:**

| بخش | دامنه | وضعیت |
|---|---|---|
| A | باگ‌های قطعی خروجی/پیش‌نمایش | ✅ بسته |
| B | ضعف‌های متوسط | ✅ بسته |
| C | جزئی (کازمتیک) | ✅ بسته |
| D | باگ‌های دیباگ زندهٔ «coffee landing» | ✅ بسته |
| E | نقشهٔ فایل‌های تحت تغییر | مرجع |
| F | دیزاین‌سیستم + رجیستری بلاک | ✅ پیاده‌شده |
| **G** | **باگ‌های استک دو-پلنی (بررسی کامل استک)** | ⬜ G1–G20 باز |

وضعیت کلی: `go build ./...`, `go vet ./...`, `go test ./...` و
`bun run typecheck`/`bun run build` سبز؛ اما `bun run lint` (۱ ارور)،
`bun run test` (محیط workerd) و `bun run --cwd sdk typecheck` سبز نیستند —
این‌ها در بخش G ثبت شده‌اند. تمام باگ‌های بخش‌های A–F جز C3 عملکردی‌اند نه
بیلد‌شکن.

---

## بخش A — باگ‌های قطعی (ردیف اثر روی «خروجی/پیش‌نمایش افتضاح»)

- [x] **A1** — gap-fill و truncate-retry در مسیر dual-model (اصلی) وصل نیستند
  `fillMissingReferencedFiles` فقط در مسیر legacy صدا زده می‌شود
  (`room.go:988`)؛ `runDualModelPipeline` و `runPlanExecute` هر دو بدون
  gap-fill به `finalizeGeneration` می‌رسند. وقتی coder با
  `finish_reason=length` قطع می‌شود، محتوای ناتمام مستقیم در VFS/ردیس
  می‌شینه و `file_generated` برودکست می‌شود → فایل نهایی خراب → پیش‌نمایش
  خراب/سفید.
  - **رفع:** همان سازوکار مسیر legacy را بیار: خروجی هر `executeStepOnRoom`
    را با `StreamParser.TruncatedPaths()` ردگیری کن و فایل‌های ناتمام را با
    تکرار (بودجهٔ دوبل) بازتولید کن؛ قبل از `finalizeGeneration` در
    `runDualModelPipeline` و `runPlanExecute` «تکرارگر missing-referenced»
    را صدا بزن.
  - **پذیرش:** تستی که coder با `finish_reason=length` در وسط `index.html`
    قطع می‌شود → فایل در VFS ناتمام نیست و gap-fill مابقی را تولید می‌کند؛
    پیش‌نمایش سفید نمی‌شود.
  - **وضعیت:** انجام شد. `executeStepOnRoom` اکنون `StepFinish`
    (`finish_reason`/`Truncated`) برمی‌گرداند؛ `runDualModelPipeline`
    فایل‌های truncated را با `regenerateTruncatedSteps` (بودجهٔ دوبل، حداکثر
    `maxStepTruncateRetries`) بازتولید کرده و بعد `fillMissingReferencedFiles`
    را صدا می‌زند. `runPlanExecute` هم قبل از finalize gap-fill می‌راند.
    تست `TestRunDualModelPipelineTruncatedCoderRetries` اضافه شد.

- [x] **A2** — مسیر static پیش‌نمایش وصل نیست (کد مرده)
  `PreviewPanel` همیشه `template="node"` می‌گیرد (`main-content-panel.tsx:215`
  هاردکد) و `resolveTemplate` هیچ‌وقت `'static'` برنمی‌گرداند؛ درنتیجه شاخهٔ
  `resolvedTemplate === 'static'` و `buildStaticSrcDoc` + `sanitizeJs` کد
  مرده‌اند. هر SPA سادهٔ استاتیک ناچار از Nodebox (بوت WASM + تزریق
  static-server + باندلر) رد می‌شود — کند و شکننده، درحالی‌که iframe-srcdoc
  سبک و مطمئن از قبل نوشته شده و تستش هم هست.
  - **رفع:** در `resolveTemplate` پروژهٔ استاتیک (فقط `public/index.html` +
    js/css، بدون root package.json) را به `'static'` نگاشت کن؛
    `main-content-panel.tsx` را به‌جای هاردکد، خروجی `resolveTemplate`
    استفاده کن (template را از `templateDetails`/نوع پروژه بگیر).
  - **پذیرش:** پروژهٔ استاتیک (فقط public/) در iframe srcdoc رندر می‌شود،
    بدون بوت Nodebox؛ تست `preview-normalize.test.ts` پاس.
  - **وضعیت:** انجام شد. `isStaticProject` + `resolveTemplate` پیاده شد
    (index.html در root/public/src بدون root package.json → `'static'`)؛
    `main-content-panel.tsx` به‌جای `template="node"` خروجی `resolveTemplate`
    را استفاده می‌کند (با `templateDetails?.name`). تست‌ها اضافه شد.

- [x] **A3** — RAG/وکتور ردیس وصل نیست (کد مرده)
  `IndexFile`/`ReindexVFS`/`SearchVFS`/`BuildRAGContext` هیچ caller ندارند
  (`vector.go`). ایندکس `idx:vfs` در startup ساخته می‌شود ولی هیچ‌وقت پر یا
  خوانده نمی‌شود. پلنر فقط `vfsSnapshot()` (path + سایز بایت —
  `dual_model.go:220`) می‌بند → در iterateهای اصلاحی کد موجود را نمی‌بند →
  پلن‌های سطحی → خروجی بی‌کیفیت.
  - **رفع:** `ReindexVFS()` بعد از هر `finalizeGeneration`؛ خروجی
    `BuildRAGContext` را در `vfsSnapshot`/پرامپت پلنر برای iterateهای اصلی
    تزریق کن.
  - **پذیرش:** بعد از یه جنریشن، `idx:vfs` دارای هش‌های `vfs:vec:*` است و
    پرامپت پلنر شامل «Relevant existing code context» واقعی.
  - **وضعیت:** انجام شد. `finalizeGeneration` بعد از `persistWorkflowDag()`
    `ReindexVFS()` صدا می‌زند؛ `runDualModelPipeline` از
    `plannerVFSContext(ctx, prompt)` استفاده می‌کند (snapshot + خروجی
    `BuildRAGContext(prompt, 6)`). تست `TestPlannerVFSContextNoRedis` اضافه شد.

- [x] **A4** — stub فقط برای `.js` (CSS گم‌شده پیش‌نمایش را بی‌استایل می‌کند)
  `gapfill.go:359`: `if !strings.HasSuffix(p, ".js") { continue }`. اگر
  `styles.css` گم بشود (stream قبل از باز شدنش cut شده) هرگز stub نمی‌شود →
  preview بی‌استایل.
  - **رفع:** `.css` را هم به حلقهٔ stub اضافه کن (stub خالی/حداقلی معتبر).
  - **پذیرش:** تستی که `styles.css` گم‌شده → در VFS حضور دارد (فایل خالی
    مجاز) و پیش‌نمایش بدون 404 بی‌استایل رندر می‌شود.
  - **وضعیت:** انجام شد. `writeSafeStubForMissing` CSS را با
    `writeSafeStubForMissingCSS` (استایل‌شیت کمینهٔ معتبر) stub می‌کند.
    تست `TestWriteSafeStubForMissingCSS` اضافه شد.

- [x] **A5** — coder fence ناقص، محتوا را خراب می‌کند
  `coderFileContent`/`stripSingleFence` (`dual_model.go:199`) وقتی فنس باز
  هست ولی بسته نیست (truncate) `ok=false` برمی‌گرداند → محتوا با خود
  ` ```html ` توی فایل نهایی می‌ماد. (در مسیر dual-model salvage هم نیست —
  A1 را ببین.)
  - **رفع:** در `stripSingleFence` فنس بازِ بسته‌نشده را هم بشکن و
    body را Trim کن (وعلامت truncation برای A1 حفظ شود).
  - **پذیرش:** تستی که ` ```html\n<div>...` بدون بستن → فایل نهایی بدون
    ` ```html ` و سالم است.
  - **وضعیت:** انجام شد. `stripSingleFence` فنس بازِ بسته‌نشده را هم می‌شکند
    (opener حذف می‌شود، body تریم می‌شود). تست
    `TestStripSingleFenceUnclosed` اضافه شد.
---

## بخش B — ضعف‌های متوسط

- [x] **B1** — `planMaxTokens` ناسازگار با role config
  کامنت `room.go:651` می‌گوید «roadmap default 8192» ولی کد `4096`؛
  درحالی‌که نقش پلنر (`skills/registry.go`) 8192 است و `GeneratePlan` (مسیر
  dual-model) مستقیم 8192 استفاده می‌کند. دو مسیر پلن با بودجه متفاوت.
  - **رفع:** `planMaxTokens` را از role config پلنر بگیر، یا 8192 کن تا
    هم‌ساز بشود.
  - **پذیرش:** هر دو مسیر پلن با همان بودجه اجرا می‌شوند.
  - **وضعیت:** انجام شد. `const planMaxTokens` حذف شد؛ تابع
    `plannerMaxTokens()` بودجه را از `skills.NewRegistry().Config(RolePlanner)`
    می‌گیرد (پیش‌فرض 8192). تست `TestPlannerMaxTokensFromRoleConfig` اضافه شد.

- [x] **B2** — handshake WS state همگانی به کل room برودکست می‌شود
  `routes.go:518-528` — `cf_agent_state`/`agent_connected` به
  `room.BroadcastMessage` می‌رود (به همهٔ کلاینت‌ها) به‌جای ارسال فقط به
  کلاینت متصل. با دو تب، eventهای state تکراری/داخل‌همی می‌رسند.
  - **رفع:** متد ارسال «فقط به خود کلاینت» اضافه کن (مثلاً
    `room.SendClient(c, msg)`) و در handshake از آن استفاده کن.
  - **پذیرش:** با دو تب باز، هر تب state را دقیقاً یک بار دریافت می‌کند.
  - **وضعیت:** انجام شد. `ProjectRoom.SendClient(c, v)` + چنل `direct` در
    Run loop پیاده شد؛ handshake و `get_conversation_state` اکنون فقط به
    کلاینت صدازننده می‌روند.
---

## بخش C — جزئی (کازمتیک)

- [x] **C1** — شرط تکراری در `vector.go:75`
  `strings.Contains(s, "Index already exists") || strings.Contains(s, "Index already exists")` —
  دو بار همان رشته. (دومی احتمالاً ترجمه/حالت دیگه بوده.) باگ واقعی نیست
  ولی خوندنی را خراب می‌کند.
  - **رفع:** تکراری را بردار.
  - **وضعیت:** انجام شد.

- [x] **C2** — تابع بلااستفاده `createCmdArgs` در `vector.go:57`
  هیچ caller ندارد؛ همراه `float32ToBytes` ناتمام (در بند بالایی) می‌رود.
  - **رفع:** حذف کن یا وصل کن.
  - **وضعیت:** انجام شد. `createCmdArgs` حذف شد؛ مسیر باند FT.CREATE همان
    `buildCreateArgs` است.

- [x] **C3** — پیشوند `generated-output.txt` در `hub.go`/`room.go` دو بار
  چک می‌شود (`"generated-output.txt"` و `"/generated-output.txt"`).
  درست است ولی با `codeExts`/استاندارد فایل‌های پابلیک سازگاری ندارد؛
  برای پیش‌نمایش در آینده، `generated-output.txt` را در `IGNORED_FILES`
  فرانت هم نگه دار (قبلاً هست — `preview-normalize.ts:34`). فقط کانتکست.
  - **رفع:** چک تکراری در یک تابع کمکی مشترک متمرکز شد:
    `pkg/engine/legacyfiles.go` → `IsLegacyOutputFile(path)` (هردو املا:
    `generated-output.txt` و `/generated-output.txt`)؛
    `hub.GetVFSReadOnly` و `room.loadVFS` هر دو از همان صدا می‌زنند و
    رفتار (فیلتر/حذف از Redis) بدون تغییر ماند. یک قانون نوشته شد:
    «فقط pseudo-file بی‌پوشه، نه فایل واقعی `public/generated-output.txt»`
    — تست `TestIsLegacyOutputFile` (legacyfiles_test.go) هردو املا را قبول و
    `public/generated-output.txt`/فایل‌های عادی را رد می‌کند.
  - **فرانت:** `IGNORED_FILES` در `preview-normalize.ts:52` هردو املا را
    دارد (خط ۱۱۹ فیلتر می‌کند) — بدون تغییر؛ تست
    `preview-normalize.test.ts` این رفتار را پین می‌کند.
  - **وضعیت:** انجام شد. `go vet ./...` + `go test ./...` (۱۳ پکیج) سبز؛
    با این بستن، معیار پذیرش G15 («هیچ `- [ ]` باز در A–F») هم برقرار است.

---

## بخش D — باگ‌های کشف‌شده در دیباگ زندهٔ «coffee landing»

سه باگ ریشه‌ای در دیباگ زنده (prompt: «build simple coffe landing») پیدا شد:
خروجی «Welcome to Our Website» با رنگ آبی بود، هیچ coffee در آن نبود، CSS/JS
با HTML هم‌خوان نبود.

- [x] **D1** — coder هرگز prompt کاربر و بقیهٔ پلن را نمی‌بیند
  `coderTaskContent` فقط `{path, action, requirements}` می‌فرستد که
  `requirements` = `step.Description` پلنر است. پلنر برای index.html نوشت:
  «Basic HTML5 doctype, lang, charset, and responsive viewport meta tag.»
  هیچ کلمه‌ای از coffee نیست! coder دقیقاً همان را تولید کرد →
  `<title>Document</title>` + body خالی، و خروجی واقعی:
  «Welcome to Our Website / Your one-stop solution» با رنگ آبی.
  
  `plan_context` در Input contract ۰۲_coder.md ذکر شده بود ولی کد هرگز آن را
  نمی‌فرستاد. `StepContext` جدید اضافه شد (user_request + goal + subtasks +
  steps) و از `runDualModelPipeline` پاس داده می‌شود.
  
  - **رفع:** `agent/types.go`: `StepContext` + `StepContextFrom`;
    `agent/coder.go`: `ExecuteStepWithContext` + `coderTaskContent` با
    `plan_context`; `pkg/engine/dual_model.go`: `stepCtx :=
    StepContextFrom(prompt, plan)` و پاس به `executeStepOnRoom`;
    `skills/01_planner.md`: descriptionها باید domain را حمل کنند؛
    `skills/02_coder.md`: دستورالعمل استفاده از `plan_context`.
  - **پذیرش:** تست زنده با prompt «build simple coffe landing»:
    `<title>Fresh Roasted Coffee, Delivered</title>` + ۴ منوی قهوه
    (Espresso/Cappuccino/Latte/Cold Brew با قیمت) + hero با CTA +
    `<link rel="stylesheet" href="styles.css">` + `<script src="js/main.js">`.
  - **تست‌ها:** `TestRunDualModelPipelinePassesPlanContext`،
    `TestStepContextFrom`، `TestCoderTaskContentCarriesPlanContext`.

- [x] **D2** — RAG/وکتور ۱۰۰٪ مرده است (۴ خطا روی هم)
  لاگ: `[rag:…] search: Syntax error at offset 1 near >[`
  
  1. `*=>[KNN …]` فقط در **DIALECT ≥ 2** پارس می‌شود؛ کد `DIALECT` نمی‌فرستد
     (پیش‌فرض = 1).
  2. `SORTBY __embedding_score` — این alias هرگز تعریف نشده (باید
     `$BLOB AS score`).
  3. `RETURN 2 path text` — فیلدهای hash فقط `embedding` و `metadata` هستند؛
     `path`/`text` داخل JSONِ `metadata` اند (parser هم `metadata` را می‌خواند!).
  4. کلیدهای وکتور **بدون chatID** هستند (`vfs:vec:public/index.html`)، یعنی
     پروژه‌ها به هم نشت می‌کنند: room A و room B هر دو `public/index.html`
     می‌نویسند و یکی دیگری را بازنویسی می‌کند.
  
  - **رفع:** `pkg/engine/vector.go`: `buildSearchArgs` با `DIALECT 2` +
    `$BLOB AS score` + `RETURN metadata score`; `vectorKey(chatID, path)`
    با chatID؛ `FT.CREATE` با `chat TAG` برای scoping؛ `parseScore` برای
    نرمال‌سازی `-nan`; `escapeTag` برای UUIDها (خطای `Syntax error at offset
    25 near d9` بدون escape).
  - **پذیرش:** تست زنده روی ردیس واقعی: coffee room ۳ فایل خودش را می‌گیرد،
    carstore room ۲ فایل خودش را، بدون نشت cross-room، با scoreهای cosine
    درست. `[vector:room-id] reindexed N file(s)` در لاگ‌ها.
  - **تست‌ها:** `TestBuildSearchArgs`، `TestBuildSearchArgsEscapesChatID`،
    `TestBuildCreateArgsHasChatTag`، `TestParseSearchResponseMetadataAndScore`،
    `TestParseScoreNormalizesNaN`، `TestHasMagnitude`،
    `TestHashEmbeddingIsUnitLength`، `TestPreviewCutsAtRuneBoundary`،
    `TestVectorKeyScopesPerRoom`، `TestRoomSearchVFSWithoutRedis`.

- [x] **D3** — CSS/JS با markup هم‌خوان نیست (selectors نمی‌خورند)
  steps به‌ترتیب اجرا می‌شوند، پس وقتی `styles.css` نوشته می‌شود،
  `index.html` قبلاً در VFS است — ولی coder آن را نمی‌بیند!
  `currentFileContent` فقط محتوای **همان** path را پاس می‌دهد.
  
  نتیجه: markup می‌گوید `class="about-section"` ولی CSS تعریف می‌کند `.about`
  (section بدون استایل رندر می‌شود)، markup می‌گوید `id="order-now"` ولی JS
  می‌بندد `.order-now` (CTA هیچ کاری نمی‌کند).
  
  - **رفع:** `agent/types.go`: `StepContext.RelatedFiles` +
    `WithRelatedFiles(self, written)` که محتوای واقعی siblings را با cap
    (maxRelatedFiles=4، maxRelatedFileChars=6000) اضافه می‌کند؛
    `pkg/engine/dual_model.go`: `siblingFileContents` که محتوای VFS را
    جمع می‌کند و `stepCtx.WithRelatedFiles(step.FilePath, ...)` در
    `executeStepOnRoomBudget`؛ `skills/02_coder.md`: دستورالعمل دقیق برای
    استفاده از `related_files` (CSS باید class/idهای واقعی markup را
    استایل کند، JS باید همان‌ها را query کند).
  - **پذیرش:** تست زنده: HTML ۵ class دارد (`about`, `hero`, `menu-grid`,
    `menu-item`, `sticky-nav`)، CSS دقیقاً همان ۵ class را استایل می‌کند
    (۵/۵ match!)، JS از `.sticky-nav nav ul li a` و `#order-now` استفاده
    می‌کند که هر دو در markup وجود دارند (۰ dead selector).
  - **تست‌ها:** `TestWithRelatedFiles`، `TestWithRelatedFilesOrderAndCaps`،
    `TestWithRelatedFilesTruncatesLongSibling`، `TestWithRelatedFilesNoop`،
    `TestRunDualModelPipelinePassesRelatedFiles`.

---

## بخش E — نقشهٔ فایل‌های تحت تغییر

| فایل | باگ | تغییر |
|------|-----|-------|
| `pkg/engine/dual_model.go` | A1, A5 | بازتولید truncate + gap-fill + strip فنس ناقص |
| `pkg/engine/plan_execute.go` | A1 | gap-fill قبل از finalize |
| `pkg/engine/room.go` | A1, B1 | اتصال gap-fill به dual-model، هم‌سازی planMaxTokens |
| `pkg/engine/gapfill.go` | A4 | stub برای .css |
| `pkg/engine/vector.go` | A3, C1, C2 | اتصال Reindex/BuildRAG، تمیزسازی |
| `src/components/preview/preview-normalize.ts` | A2 | شناسایی 'static' |
| `src/routes/chat/components/main-content-panel.tsx` | A2 | template از resolveTemplate |
| `pkg/api/routes.go` | B2 | ارسال per-client در handshake |
| `agent/types.go` | D1, D3 | `StepContext` / `PlanStepRef` / `WithRelatedFiles` |
| `agent/coder.go` | D1 | `ExecuteStepWithContext` + `plan_context` در task JSON |
| `pkg/engine/dual_model.go` | D1, D3 | پاس `stepCtx` + `siblingFileContents` |
| `pkg/engine/vector.go` | D2 | DIALECT 2 / AS score / RETURN metadata / chat scoping |
| `skills/01_planner.md` | D1 | descriptionها domain را حمل کنند |
| `skills/02_coder.md` | D1, D3 | مصرف `plan_context` + `related_files` |
| `agent/stepcontext_test.go` | D1, D3 | تست‌های StepContext / related_files |
| `pkg/engine/vector_test.go` | D2 | تست‌های FT.SEARCH / FT.CREATE / parse |
---

# بخش F — لایهٔ دیزاین‌سیستم (Phase 1) + رجیستری بلاک استاتیک (Phase 2)

**هدف:** هر تولید کد یک دیزاین‌سیستم برند-محور مشترک بگیرد (پالت/پترن/تایپوگرافی/موشن/a11y) و ساختار صفحه از یک رجیستری بلاک قطعی بیاید — نه اختراع مدل. منبع داده: `ui-ux-pro-max` (MIT، vendored، آفلاین).

## Phase 1 — `pkg/design` (brief)

- `pkg/design/data/*.csv` — ۸ فایل CSV + LICENSE (۵۴۸KB، `go:embed`): styles/colors/typography/landing/ux-guidelines/motion/products/ui-reasoning.
- `catalog.go` (parse typed، `sync.Once`)، `search.go` (stopword + domain-synonym + BM25 سبک)، `brief.go` (`Brief(ctx, prompt)` → `DesignBrief`)، `tokens.go` (۱۶ توکن سمانتیک سازگار shadcn + `.dark` معکوس)، `render.go` (planner متن / coder فشرده با بودجه).
- تزریق: `StepContext.Design` (`*design.CoderBrief`) در `runDualModelPipeline`؛ به پلنر هم `RenderPlanner()` اضافه می‌شود.
- کدِ توکن: `cb.TokensCSS == b.TokensCSS()` (تست identity) — همهٔ فایل‌ها یک پالت مشترک.

## Phase 2 — رجیستری بلاک (`registry.go`، `blocks*.go`، `compose.go`)

- ۱۱ بلاک استاتیک (navbar/hero-split/hero-center/features/menu/about/testimonials/pricing/cta/contact/footer) — هر فرگمنت فقط توکن سمانتیک استفاده می‌کند.
- انتخاب: `SelectBlocks(pattern)` روی `SectionOrder` پترن + `domainSectionHints` (کافه→menu، SaaS→pricing، …) + الزام navbar/footer.
- تحویل per-file (`CoderBrief.FileFragments`): descriptor فشرده (name/section/slots) در همهٔ استپ‌ها؛ بدنهٔ فرگمنت فقط در استپ فایل خودش (html/css/js) — brief مشترک 3.3KB زیر سقف 3.5KB.
- `agent/coder.go`: کپی shallow از Design با فرگمنت‌های فایل + الحاق برنامه‌نویسی‌شدهٔ «MUST contain EVERY n section» برای استپ html (مدل وگرنه ۲ سکشن می‌نوشت و stop می‌کرد).
- skillها: `01_planner.md` (سکشن‌ها فقط از design direction + توکن‌ها در description)، `02_coder.md` (قرارداد design.blocks + markup-first + state-classها).

## نتیجهٔ E2E زنده (پرامپت coffee)

| متر | قبل از brief | بعد از Phase 1+2 |
|---|---|---|
| سکشن‌ها | اختراع مدل (value-prop بدون استایل) | ۹ سکشن رجیستری، ترتیب پترن، ۰ کلاس بی‌استایل |
| توکن CSS | ad-hoc (`--color-text`) | ۱۳+ توکن سمانتیک + `.dark`، بدون hex خارج توکن |
| سلکتور مرده JS | ۲/۲ | ۰ |
| title/محتوا | generic | "Coffee Haven"، ۱۹ اشارهٔ قهوه |
| brief coder | — | 3.3KB ≤ 3.5KB، یکسان بین planner/coder |

- تست‌ها: `pkg/design` (brief/search/tokens/render/registry/compose/FileFragments)، `TestCoderBriefCompactness`، `TestCoderTaskContentBlocksPerFile`، `TestRunDualModelPipelinePassesPlanContext`.
- فایل‌ها: `pkg/design/*`، `agent/types.go`، `agent/coder.go`، `pkg/engine/dual_model.go`، `skills/01_planner.md`، `skills/02_coder.md`.

---

# بخش G — باگ‌های استک دو-پلنی (بررسی کامل استک)

**دامنه:** کل استک فعلی — پلن کنترل Go (`backend/`)، Worker سبک ابری
(`worker/light-index.ts` + `worker/light/lightApp.ts` + `worker/workflow/VibeWorkflow.ts`)،
فرانت React (`src/`)، پیکربندی دیپلوی (`wrangler.v2.jsonc`، `docker-compose.yml`،
`.github/workflows/`)، و بقایای لگسی (`space/`، `sdk/`).

**بیس‌لاین تأییدشده (قبل از دیباگ):**

| چک | نتیجه |
|---|---|
| `go build ./...` / `go vet ./...` | ✅ سبز |
| `go test ./... -count=1` (۱۳ پکیج) | ✅ همه ok |
| `bun run typecheck` | ✅ سبز |
| `bun run build` | ✅ موفق (۷s) |
| `bun run lint` | ❌ ۱ ارور + ۱۴ warning |
| `bun run test` (vitest/workerd) | ❌ اجرا نمی‌شود (macOS 12.6 < 13.5) |
| `bun run --cwd sdk typecheck` | ❌ ۵+ ارور TS |

> توجه: باگ‌های بخش‌های A–F عمدتاً **کیفیت خروجی تولید کد** بودند و حل شده‌اند.
> بخش G باگ‌های **اتصال/پیکربندی/نگهداری خود استک** است — عملکردی و قابل دیباگ.

---

## G-A — شکستگی‌های قطعی (بلاک‌کننده)

- [ ] **G1** — SDK کاملاً شکسته است (import به ماژول‌های حذف‌شده)
  `bun run --cwd sdk typecheck` پنج ارور می‌دهد:
  `src/protocol.ts` از `../../worker/agents/core/state`،
  `../../worker/agents/core/types`، `../../worker/agents/schemas` و
  `../../worker/services/sandbox/sandboxTypes` ایمپورت می‌کند که **همه حذف
  شده‌اند** (`worker/services/` الان فقط `secrets/` دارد و `worker/agents/`
  وجود ندارد). علاوه بر این `src/state.ts:123/134` پارامترهای implicit-any و
  `src/ws.ts:129` عدم تطابق `Record<string, unknown>` با `AgentState` دارد.
  AGENTS.md هنوز می‌گوید «SDK باید سازگار بماند» ولی خود SDK بیلد نمی‌شود.
  - **رفع:** یا (الف) پروتکل SDK به مسیرهای جدید منتقل شود
    (`worker/api/websocketTypes.ts` + `worker/types/agent-state.ts` که موجودند)
    یا (ب) `sdk/` رسماً deprecate + حذف از ریشه/knip/مستندات. تصمیم یکسان
    در دو سمت: هیچ ارجاع زنده‌ای به SDK در `src/` نیست (grep خالی).
  - **پذیرش:** یا `bun run --cwd sdk typecheck && bun run --cwd sdk test` سبز،
    یا `sdk/` حذف شده و هیچ فایل/مستند/CI به آن ارجاع ندارد.

- [ ] **G2** — شکاف سطح API: ۱۸ endpoint فرانت در هر دو پلن بی‌پاسخ است
  `src/lib/api-client.ts` مسیرها را به دو پلن تقسیم می‌کند: `/api/auth/*` و
  `/api/github-app/*` → Worker سبک، بقیه → پلن کنترل Go
  (`api-client.ts:347-350`). اما Go فقط **۲۵ route** ثبت می‌کند
  (`backend/pkg/api/routes.go`) و Worker سبک برای همین مسیرها **۵۰۳** می‌دهد
  (`lightApp.ts:865-889`). نتیجه: ۱۸ فراخوانی بی‌پاسخ (۴۰۴ از Go، ۵۰۳ از
  Worker) — یعنی UI این قابلیت‌ها بی‌خطا نمی‌تواند کار کند:
  - `/api/cloudflare/connection|selection|ai-gateway-preference` (۳)
  - `/api/model-configs`، `/defaults`، `/byok-providers`، `/test`، `/reset-all`، `/:action` (۵–۶)
  - `/api/secrets/templates` (۲ مورد با کوئری)
  - `/api/stats`، `/api/stats/activity` (۲)
  - `/api/user/profile`، `/api/user/providers`، `/api/user/providers/test` (۳)
  - `/api/vault/config|setup|status|reset` (۴)
  بخشی از این‌ها با کامپوننت‌های فعال فرانت مصرف می‌شوند:
  `model-config-tabs.tsx`، `byok-api-keys-modal.tsx`، `usage-limits-badge.tsx`،
  `profile.tsx`، `connected-accounts.tsx`.
  - **رفع:** تصمیم صریح «مالک» هر گروه endpoint و پیاده‌سازی‌اش: یا در Go
    (روی D1/PG موجود) یا در Worker سبک (D1)، سپس حذف ۵۰۳ برای مسیرهای
    پشتیبانی‌شده و حذف ۴۰۴ با افزودن route در Go. یک تست قرارداد
    (`routes_test.go`) برای هر گروه اضافه شود.
  - **پذیرش:** برای هر ۱۸ مسیر یکی از دو پلن پاسخ `2xx`/`4xx` (نه 404/503)
    بدهد؛ تست: `grep -o "'/api/...'" src/lib/api-client.ts` ⊆ اجتماع routeهای
    دو پلن (اسکریپت چک اضافه شود).

- [x] **G3** — `bun run lint` در CI شکست می‌خورد (۱ ارور پارس)
  `scripts/check-workers-ai.cjs` با وجود پسوند `.cjs` از `import` استفاده
  می‌کند: «Parsing error: 'import' and 'export' may appear only with
  'sourceType: module'». این اسکریپت Diag Workers AI است، نه بخشی از بیلد.
  - **رفع:** به `scripts/check-workers-ai.mjs` تغییر نام شد (`git mv` —
    محتوای ESM با پسوند `.mjs` سازگار است). هیچ ارجاع دیگری به این اسکریپت
    در کد/CI نبود (فقط همین سند). ارجاع در جدول G-F هم به‌روز شد.
  - **پذیرش:** ✅ `bun run lint` با exit code 0 تمام می‌شود (۰ ارور) و هیچ
    ارور/هشداری از آن فایل نمی‌آید.

- [ ] **G4** — CORS با دامنهٔ دیپلوی هم‌خوان نیست
  `worker/light/lightApp.ts:22-26` فقط این دامنه‌ها را مجاز می‌داند:
  `https://vibeos-dda.pages.dev`، `https://production.vibeos-dda.pages.dev`،
  `https://vibesdk-v2.mehranjannati.workers.dev`
  ولی `wrangler.v2.jsonc:59` دامنهٔ جاری را
  `vibesdk-v2.apjkala25.workers.dev` می‌داند (`workers_dev: true` و
  `preview_urls: true`). چون `origin` ناشناخته → `''` برمی‌گردد
  (`lightApp.ts:126`)، همهٔ درخواست‌های cross-origin auth/GitHub export از
  دامنهٔ واقعی Worker رد می‌شوند.
  - **رفع:** یا `CUSTOM_DOMAIN` را از binding بخوان و همان را در لیست مجاز
    بگذار (`c.env.CUSTOM_DOMAIN`)، یا دامنهٔ واقعی را در
    `wrangler.v2.jsonc` `vars` تعریف کن و CORS را از یک منبع واحد تغذیه کن؛
    origins محلی فقط در dev.
  - **پذیرش:** preflight از `Origin: https://vibesdk-v2.apjkala25.workers.dev`
    پاسخ `Access-Control-Allow-Origin` هم‌نام می‌گیرد (تست سبک برای
    `buildLightApp()` با درخواست OPTIONS).

- [ ] **G5** — `CONTROL_PLANE_URL` در vars تولیدی روی `localhost` مانده
  `wrangler.v2.jsonc:60` → `"CONTROL_PLANE_URL": "http://localhost:8080"`.
  این binding برای fetch فایل‌های پروژه هنگام **export به GitHub** استفاده
  می‌شود؛ در دیپلوی واقعی به هیچ‌جا وصل نمی‌شود → دکمهٔ export شکست می‌خورد
  (دقیقاً همان قابلیتی که Worker سبک برایش باقی مانده).
  - **رفع:** مقدار تولیدی = URL عمومی پلن کنترل (Coolify/Hetzner)؛ برای dev
    از `.dev.vars`/`vars` توسعه‌ای جدا استفاده شود.
  - **پذیرش:** `POST /api/github-app/export` روی محیط staging با کنترل‌پلین
    واقعی فایل‌ها را می‌خواند و ریپو می‌سازد؛ هیچ ارجاع `localhost` در
    `wrangler.v2.jsonc` نماند.

---

## G-B — پیکربندی دیپلوی و محیط

- [ ] **G6** — D1 ID در compose و wrangler یکسان نیست
  `docker-compose.yml:52` پیش‌فرض `D1_DATABASE_ID` را
  `78a4275a-a78e-46e5-9adf-74566fbafd6b` می‌گذارد، ولی
  `wrangler.v2.jsonc:37` دیتابیس `v2-vibe` را با
  `374acf0b-5a29-4e1a-8b71-dc0e7178f4bd` بایند می‌کند. اگر Go روی D1 کار کند،
  compose پیش‌فرض به دیتابیس دیگری می‌نویسد (کاربر/limit جدا) → رفتار
  «کاربر ثبت‌نام می‌کند ولی پیدا نمی‌شود».
  - **رفع:** یک منبع حقیقت: ID را از `wrangler.v2.jsonc` بخوان یا در `.env`
    مشترک نگه دار و هر دو مصرف‌کننده از آن تغذیه شوند؛ پیش‌فرض کد را حذف کن
    تا مقدار اشتباه ساکت نماند (fail-fast اگر خالی بود).
  - **پذیرش:** با یک ID واحد، کاربر ساخته‌شده توسط Worker در پلن Go
    (`lookUpUserByEmail`) پیدا می‌شود — تست e2e روی staging.

- [ ] **G7** — مقادیر `PG_*` هاردکد مخصوص Docker Desktop مک
  `docker-compose.yml:44-50`: `PG_HOST=192.168.65.254` (آدرس ویژهٔ Docker
  Desktop روی macOS) و `PG_DATABASE=chatwoot_dev`. روی Coolify/لینوکس این
  مقادیر غلط‌اند و auth پستگرس پلن کنترل بالا نمی‌آید.
  - **رفع:** پیش‌فرض‌ها را بی‌طرف کن (`PG_HOST` بدون پیش‌فرض، `PG_DATABASE`
    نام پروژه) و اتصال صحیح را در `docker-compose.prod.yml`/متغیرهای Coolify
    بگذار؛ در `.env.example` توضیح بده.
  - **پذیرش:** بالا آمدن compose روی لینوکس بدون override دستی + یک health
    check سبک برای مسیر auth پستگرس.

- [x] **G8** — هیچ تست فرانت/Worker در این محیط اجرا نمی‌شود
  `bun run test` بلافاصله با «Unsupported macOS version … minimum requirement
  is macOS 13.5.0+» شکست می‌خورد (`@cloudflare/vitest-pool-workers`).
  یعنی `UserSecretsStore.test.ts`، `preview-normalize.test.ts`،
  `WorkflowVisualizer.test.tsx`، `src/lib/utils.test.ts` و
  `worker/workflow/VibeWorkflow.test.ts` **هیچ‌کدام** در dev محلی قابل
  اجرا نیستند و در CI هم (`.github/workflows`) هیچ مرحله‌ای برایشان نیست.
  - **رفع:** (۱) یک job CI روی `ubuntu-latest` برای `bun run test` اضافه شود؛
    (۲) در `AGENTS.md`/`README` صریح ذکر شود که vitest محلی نیاز به macOS 13.5+
    یا DevContainer لینوکسی دارد؛ (۳) `bun run test` وقتی محیط پشتیبانی
    نمی‌شود پیام راهنما بدهد، نه stack trace.
  - **وضعیت:** قسمت (۳) انجام شد — `bun run test` حالا از wrapper
    `scripts/run-tests.mjs` می‌رود که نسخهٔ macOS را پیش‌ارزیابی می‌کند و در
    این ماشین پیام راهنمای عملی (CI / DevContainer / SKIP_TESTS=1) می‌دهد و
    با کد ۱ خارج می‌شود؛ روی محیط‌های مجاز، argها به `vitest run` پاس داده
    می‌شوند (bunx با fallback npx). قسمت (۲) README انجام شد (یادداشت
    «Local test runtime» زیر جدول Development commands)؛ یادداشت AGENTS.md
    با G11 (بازنویسی AGENTS) بسته می‌شود. قسمت (۱) ✅ در T15 انجام شد: job
    `test-build` (`bun run test`) در هر دو ورک‌فلوی دیپلوی و job `ci` در
    `.github/workflows/ci.yml` روی `ubuntu-latest` اجرا می‌شوند و `deploy`
    به آن‌ها وابسته است (docs/DOCS_AUDIT_BACKLOG.md T15).
  - **پذیرش:** ✅ در CI سبز شدن `bun run test` قابل مشاهده است و مستندات
    پیشنیاز محیط را می‌گوید.

- [x] **G9** — ESLint فایل‌های بیلد `.wrangler` را هم lint می‌کند
  از ۱۵ مشکل lint، ۱۱ مورد از `.wrangler/tmp/deploy-*/light-index.js`
  (خروجی باندلشدهٔ Worker) می‌آید و ۳ مورد warning «unused eslint-disable».
  این نویز، خطاهای واقعی را می‌پوشاند.
  - **رفع:** `.wrangler/**` با توضیح «machine-generated» به `ignores` در
    `eslint.config.js` اضافه شد (الگوی `dist` از قبل بود).
  - **پذیرش:** ✅ `bun run lint` فقط فایل‌های منبع را گزارش می‌کند؛ شمارش
    مشکلات از ۱۵ (۱ ارور + ۱۴ warning) به **۳ warning** رسید — هر سه هشدار
    واقعی `react-refresh/only-export-components` در `src/` (خارج از دامنهٔ
    G3/G9).
---

## G-C — دود و نگهداری (کد/مسیرهای مرده)

- [ ] **G10** — `knip.json` به entryهای حذفشده اشاره می‌کند
  `knip.json` هنوز `worker/index.ts` (حذف‌شده) و `debug-tools/**` را entry
  می‌داند و هیچ‌جا `worker/light-index.ts` (entry واقعی) نیست → خروجی knip
  بی‌معنی و «unused files» انبوه می‌شود.
  - **رفع:** entryها را به `worker/light-index.ts`، `test/worker-entry.ts` و
    `scripts/**` به‌روزرسانی کن؛ فایل‌های بیمصرف واقعی را حذف کن.
  - **پذیرش:** `bun run knip` بدون خطای «file not found» و با لیست قابل اتکا.

- [ ] **G11** — `AGENTS.md` معماری حذفشده را «جاری» می‌داند
  AGENTS.md می‌گوید `space/` تنها workspace است و باید `bun run --cwd space
  typecheck` بزنی، `worker/index.ts` entrypoint است، `worker/app.ts` و
  `worker/api/routes/index.ts` مسیرهای تغییرند، و `worker/agents/tools/toolkit/`
  محل ابزارهای LLM است — **هیچکدام وجود ندارند**. در تضاد با `docs/llm.md`
  که درست است.
  - **رفع:** AGENTS.md را با معماری دو-پلنی بازنویسی کن: پلن کنترل Go
    (`backend/pkg/engine`، `backend/pkg/agent`، `backend/skills`)، Worker سبک
    (`worker/light-index.ts`)، بخش «Change Paths» برای مسیرهای واقعی
    (route Go / WS event / ابزار eino / DAG workflow) و دستورهای درست تست.
  - **پذیرش:** هر مسیر/دستور ذکرشده در AGENTS.md با `ls`/`grep` تأیید شود؛
    هیچ ارجاعی به SpaceDO/ThinkAgent/`space/`/`worker/app.ts` نماند.

- [ ] **G12** — `space/` منبعش حذف شده ولی بقایای آن مانده
  ۲۳ فایل `space/src/**` و `space/package.json` در git حذف شدهاند (D)، اما
  `space/dist/index.js` و `space/node_modules/` روی دیسک هستند و AGENTS.md/
  knip به این پکیج ارجاع می‌دهند.
  - **رفع:** بقایای دیسک را پاک کن (`space/dist`, `space/node_modules`) و هر
    ارجاع مستندات را بردار؛ یا اگر SpaceDO واقعاً لازم است، منبعش را برگردان
    (نه dist).
  - **پذیرش:** `ls space` خالی یا فقط یک README توضیحی؛ `grep -rn "space/"`
    روی مستندات بدون ارجاع جاری.

- [ ] **G13** — `test/worker-entry.ts` و آزمونهای FS به ساختار حذف‌شده وصل‌اند
  `test/worker-entry.ts` برای `SqlStorage` یک `FsHarnessDO` می‌سازد چون
  «FileSystem contract tests روی `Workspace`» وجود داشتند — اما `space/`
  حذف شده و `worker/services/sandbox/` هم نیست. `wrangler.test.jsonc` هنوز
  `FsHarnessDO` و `UserSecretsStore` را به‌عنوان DO migration نگه می‌دارد.
  - **رفع:** اگر فقط `UserSecretsStore` تست می‌شود، `FsHarnessDO` و migration
    v2 را از `wrangler.test.jsonc`/entry بردار؛ اگر نیاز است، تستهای FS را با
    محل جدید هم‌راستا کن.
  - **پذیرش:** `test/worker-entry.ts` فقط چیزی export می‌کند که تستی واقعاً
    مصرفش می‌کند و `wrangler.test.jsonc` بدون DO بی‌استفاده است.

- [ ] **G14** — `backend/Dockerfile` `testdata/` و `e2e/` را کپی نمیکند
  Dockerfile فقط `cmd/`, `pkg/`, `agent/`, `skills/` را COPY می‌کند. یعنی
  fixtureهای replay (`backend/testdata/llm_transcripts/*`) و تست‌های e2e داخل
  ایمیج نیستند → تست replay/e2e داخل کانتینر (جایی که Redis و شبکه واقعی
  هست) ممکن نیست و باید از هاست اجرا شود.
  - **رفع:** در stage build، `testdata/` را هم COPY کن (و به‌صورت اختیاری یک
    stage تست یا `-tags e2e` با `e2e/`).
  - **پذیرش:** `docker compose exec backend go test ./pkg/...` با تست‌های
    fixture سبز شود (یا صریحاً در مستندات بگو چرا لازم نیست).

- [x] **G15** — تنها چک‌مارک باز بخش‌های قبلی: **C3** (پیشوند تکراری
  `generated-output.txt` در `hub.go`/`room.go`)
  این مورد از قبل باز بود و به‌دشواری کازمتیک است (`IGNORED_FILES` فرانت از
  قبل آن را پوشش می‌دهد: `preview-normalize.ts:52`).
  - **رفع:** ✅ C3 بسته شد — چک تکراری در `pkg/engine/legacyfiles.go`
    (`IsLegacyOutputFile`) متمرکز و در `hub.go`/`room.go` جایگزین شد؛
    تست `TestIsLegacyOutputFile` اضافه شد؛ فرانت بدون تغییر ماند.
  - **پذیرش:** ✅ برقرار — هیچ `- [ ]` در بخش‌های A–F باقی نمانده
    (C3 اکنون `[x]` است).

---

## G-D — کیفیت و کارایی فرانت

- [ ] **G16** — chunkهای غول‌آسا و dynamic import بی‌اثر
  خروجی `bun run build`: `index-BG8Z8AaM.js` 2.67MB، `editor.api` 2.66MB
  (Monaco)، `toggleHighContrast` 1.17MB، `wasm` 0.62MB + هشدار
  `INEFFECTIVE_DYNAMIC_IMPORT`: `src/utils/sentry.ts` هم dynamic و هم static
  ایمپورت می‌شود (`useSentryUser.ts` + `main.tsx`) → chunk جدا نمی‌شود.
  - **رفع:** Monaco را lazy/worker-محور بارگذاری کن (`editor.api` فقط در
    مسیر ویرایشگر)، Sentry را فقط از یک نقطهٔ واحد lazy کن (حذف import
    استاتیک `main.tsx` یا انصراف از import داینامیک)، و high-contrast را از
    مسیر اصلی جدا کن.
  - **پذیرش:** هیچ chunkی >1.5MB نماند و هشدار INEFFECTIVE_DYNAMIC_IMPORT
    حذف شود؛ بار اول `/` نباید Monaco را دانلود کند (چک Network).

---

## G-E — روند و ریسک

- [ ] **G17** — ۵۳۳ فایل commit‌نشده (ریسک از دست رفتن کار)
  `git status`: ۳۸۲ حذف، ۹۳ افزوده (staged)، ۶۸ تغییر‌یافته و ۵ untracked
  شامل **کل `backend/pkg/design/`** (رجیستری بلاک + دیزاین‌سیستم)،
  `backend/agent/stepcontext_test.go`، `backend/pkg/engine/vector_test.go`،
  `backend/pkg/llm/live_account_test.go` و همین سند. یعنی همهٔ کار فاز F/D روی
  دیسک است و در هیچ کامیتی نیست.
  - **رفع:** کامیت‌های منطقی جدا و پیوسته: (۱) حذف لگسی، (۲) `backend/pkg/*`
    جدید + تست‌ها (شامل `pkg/design/`)، (۳) Worker سبک + workflow،
    (۴) فرانت، (۵) CI/پیکربندی/مستندات. `OUTPUT_QUALITY_BUGS.md` را همراه
    کامیت مربوطه نگه دار.
  - **پذیرش:** `git status --short` فقط موارد عمدی؛ `git log` هر فاز قابل
    ردیابی؛ `go test ./...` روی درخت کامیت‌شده سبز.

- [ ] **G18** — هویت کاربر بین پلن‌ها معتبر نیست (سه منبع auth)
  سه پیاده‌سازی موازی auth وجود دارد: (۱) Worker سبک با bcrypt + کوکی
  `access_token` و session در KV (`session:v2:{userId}`,
  `lightApp.ts:228-229`)، (۲) `backend/pkg/api/auth_d1.go` (D1)، (۳)
  `backend/pkg/api/auth_pg.go` (Postgres). جست‌وجو نشان می‌دهد **Go هیچ‌جا
  کوکی `access_token` یا کلید `session:*` KV را نمی‌خواند** (grep خالی در
  `backend/pkg/api` و `backend/pkg/handler`) → درخواست‌های کاربر احراز‌شده در
  Worker، در پلن کنترل ناشناس‌اند (و برعکس) و `user_id` بین apps/envs یکی
  نمی‌شود.
  - **رفع:** یک منبع حقیقت انتخاب کن (پیشنهاد: D1/KV لبه، چون Worker مالک
    auth است)، سپس در Go یک middleware اعتبارسنجی مشترک (JWT امضاشده با
    `JWT_SECRET` یا خواندن KV) اضافه کن و روی `/api/agent/session`،
    `/api/projects/*` و `/ws/:id` اعمال کن؛ در صورت نیاز یک سرویس مهاجرت
    کاربر بین PG و D1 بنویس.
  - **پذیرش:** توکن صادرشده در Worker روی Go پذیرفته شود و
    `/api/agent/session` بدون هویت ۴۰۱ بدهد؛ تست e2e: login در Worker سپس
    ساخت session در Go.

- [x] **G19** — CI/CD فقط دیپلوی می‌کند و هیچ گیتی ندارد
  `.github/workflows/deploy-*.yml` به `bun run build && bunx wrangler deploy`
  تغییر کرده‌اند، ولی نه `bun run typecheck`، نه `bun run lint`، نه `go test`
  و نه `bun run test` را اجرا نمی‌کنند — با وجود اینکه لینت الان واقعاً fail
  میشود (G3) و Go تست‌های گسترده دارد.
  - **رفع:** jobهای گیت: `go vet ./... && go test ./...` (با سرویس Redis در
    CI)، `bun run typecheck`، `bun run lint`، `bun run test` روی ubuntu؛
    دیپلوی به آن‌ها وابسته شود.
  - **وضعیت:** ✅ انجام شد (۲۰۲۶-۰۹-۲۷ — T15 در `docs/DOCS_AUDIT_BACKLOG.md`):
    هر دو ورک‌فلوی دیپلوی jobهای `lint` (`bun run lint`)، `typecheck`
    (`bun run typecheck`)، `test-build` (`bun run test`) و `go-test`
    (`go vet ./... && go test ./...`) را گرفتند و `deploy` با
    `needs: [lint, typecheck, test-build, go-test]` به هر چهار وابسته است؛
    `ci.yml` هم یک job `go-test` گرفت (پیش از این هیچ ورک‌فلویی تست Go را
    اجرا نمی‌کرد). **سرویس Redis لازم نشد:** `backend/e2e` پشت build tag
    `e2e` است و `go test ./...` هیچ اتصال Redis واقعی نمی‌زند (تست‌های
    وابسته با کلاینت nil کار می‌کنند) — در همین درخت `go vet ./...` با کد ۰
    و `go test ./...` با همهٔ بسته‌ها `ok` تمام شد.
  - **پذیرش:** ✅ هر چهار گیت قبل از `deploy` اجرا می‌شوند، پس یک کامیت
    شکسته (ارور lint یا تست) نمی‌تواند به staging/release-live دیپلوی شود.
    (اجرای واقعی روی رانر GitHub خارج از این محیط آزمایش نشده؛ اعتبارسنجی
    محلی = پارس YAML + سبز شدن هر چهار دستور + بازرسی `jobs`/`needs`.)

- [ ] **G20** — دو مستند متناقض معماری و نبود نقشهٔ واحد استک
  `README.md` کاملاً معماری ThinkAgent/SpaceDO/Artifacts را توصیف می‌کند،
  `docs/llm.md` می‌گوید همه حذف شده، و `AGENTS.md` (G11) سومی است. برای
  توسعه‌دهندهٔ جدید هیچ منبع قابل اتکایی از نقشهٔ استک نیست.
  - **رفع:** یک سند «As-built architecture» (در `docs/architecture.md` یا بخش
    بالای `docs/llm.md`) با دیاگرام سه‌پلنی، فهرست bindings، envهای هر پلن،
    مالکیت auth و سطح API؛ README/AGENTS را به همان ارجاع بده و بخش‌های کهنه
    را پاک کن.
  - **پذیرش:** README/AGENTS/llm هیچ توصیف متناقضی از اجزای حذف‌شده نداشته
    باشند؛ دیاگرام سه‌پلنی در یک جا.
---

## G-F — نقشهٔ فایل‌های تحت تغییر

| فایل | باگ | تغییر |
|------|-----|-------|
| `sdk/src/protocol.ts`, `sdk/src/state.ts`, `sdk/src/ws.ts` | G1 | انتقال به مسیرهای جدید یا deprecate |
| `src/lib/api-client.ts`, `backend/pkg/api/routes.go`, `worker/light/lightApp.ts` | G2 | بستن شکاف ۱۸ endpoint + تست قرارداد |
| `scripts/check-workers-ai.mjs`, `eslint.config.js`, `scripts/run-tests.mjs`, `package.json`, `README.md` | G3, G9, G8(۳،۲) | تغییر نام + ignore بیلدها + preflight تست |
| `worker/light/lightApp.ts` (CORS)، `wrangler.v2.jsonc` | G4, G5 | دامنه/کنترل‌پلین از یک منبع |
| `docker-compose.yml`, `.env.example` | G6, G7 | یکسان‌سازی D1 ID + PG بدون هاردکد |
| `.github/workflows/*` | G8, G19 | jobهای گیت (go/vitest/typecheck/lint) |
| `knip.json`, `AGENTS.md`, `README.md`, `docs/llm.md` | G10, G11, G20 | بازنویسی با معماری دو-پلنی |
| `space/**`, `test/worker-entry.ts`, `wrangler.test.jsonc` | G12, G13 | پاک‌سازی بقایا |
| `backend/Dockerfile` | G14 | COPY `testdata/` |
| `src/utils/sentry.ts`, `src/main.tsx`, مسیر Monaco | G16 | lazy/چانک‌بندی |
| `backend/pkg/api/*`, `backend/pkg/handler/*` | G18 | middleware هویت مشترک |

## G-G — ترتیب پیشنهادی دیباگ

1. **G3 + G9** ✅ (انجام شد — lint سبز: ۰ ارور، ۳ warning) + **G8** و
   **G19** ✅ (هر دو در T15 بسته شدند: jobهای گیت `lint`/`typecheck`/
   `test-build`/`go-test` و وابستگی `deploy` به آن‌ها): اول گیت را سبز کن تا
   بقیه قابل اطمینان باشد.
2. **G17** (نیم روز): کامیت‌های منطقی — پیش‌نیاز هر کار بعدی.
3. **G6 + G7 + G5 + G4** (نیم روز): پیکربندی دیپلوی، چون سریع و پراثر است.
4. **G2 + G18** (۱–۲ روز): سطح API و هویت — بزرگ‌ترین اثر کاربری.
5. **G1 + G12 + G13 + G10** (نیم روز): تصمیم SDK و پاک‌سازی بقایا.
6. **G11 + G20** (۱–۲ ساعت): مستندات هم‌راستا.
7. **G16 + G19 + G14** (۱ روز): کارایی، CI گیت، ایمیج. (G15/C3 ✅ بسته شد.)

**معیار پذیرش نهایی بخش G:**
`go build ./... && go vet ./... && go test ./...` سبز + `bun run typecheck &&
bun run lint && bun run build` سبز + `bun run test` سبز در CI + هیچ `- [ ]`
باز در G نماند.