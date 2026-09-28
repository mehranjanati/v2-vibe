# پلن هم‌راستاسازی بک‌اند Go با eino / eino-examples

مرجع نمونه‌ها: https://github.com/cloudwego/eino-examples
این سند فهرست کامل باگ‌ها، برنامه دیباگ و فازهای بازسازی را بر اساس الگوهای
رسمی eino-examples شرح می‌دهد. هر فاز معیار پذیرش (Acceptance) دارد.

---

## بخش A — باگ‌ها به‌صورت ساب‌تسک

- [x] **B1** — خروجی خام LLM به‌عنوان فایل `generated-output.txt` در VFS/پیش‌نمایش (`pkg/engine/room.go`) — ✅ رفع: StreamParser + `applyEvents` دیگر هرگز transcript را فایل نمی‌کند (P0)
- [x] **B2** — Parse فقط بعد از پایان استریم → پیش‌نمایش وسط کار خراب — ✅ رفع: eventهای per-file همزمان با استریم (P0)
- [x] **B3** — Truncate با `MaxTokens: 12288` → drop فایل آخر (styles.css / js) — ✅ رفع (P2): حلقه retry پله‌ای در `runGeneration` (الگوی `adk/agentic/retry_max_output_tokens`) + salvage پارسر + تست `TestReplayTruncatedFixtureSalvagesCSS`
- [x] **B4** — دو استک LLM موازی (`pkg/llm/client.go` خام و `pkg/agent/eino_engine.go`) — ✅ رفع (P3): `Room.streamLLM` همهٔ جریان‌ها را از `Engine.RunStepWith` (adk.Runner) عبور می‌دهد؛ `StreamChat` فقط fallback وقتی engine nil است. `hub.SetEngine` در `cmd/main.go`. معیار پذیرش: `grep StreamChat pkg/engine` فقط داخل fallback ✅
- [x] **B5** — پایپلاین دستی plan→generate→parse خارج از ADK؛ بدون Replanner — ✅ رفع (P5): `pkg/engine/plan_execute.go` — prebuilt `adk/prebuilt/planexecute` (Planner/Executor/Replanner روی مدل tool-calling موتور): Planner plan تأییدشده را به stepهای ساختاریافته تبدیل می‌کند، Executor هر step را با `vfs_write/read/list` (روی adapter `roomVFSStore`) اجرا می‌کند و Replanner stepهای باقی‌مانده را بازبینی می‌کند (MaxIterations 4، executor 8). نوشته‌های ابزار از طریق decorator `notifyWriteTool` به eventهای استاندارد `file_generating`/`file_generated` تبدیل می‌شوند — فرانت بدون تغییر. مسیر مسیر fence-streaming به‌عنوان fallback باقی است. `canPlanExecute` gate. تست‌های `TestPlanStepsSummary`/`TestRoomVFSStore`
- [x] **B6** — کوت کورکورانه تاریخچه به ۱۶ پیام (`recentChatHistory`) — ✅ رفع (P4): summarization تاریخچه به الگوی `adk/intro/agent_with_summarization` — بالای ۲۴ turn، turnهای قدیمی به یک خلاصه (با `streamLLM`، best-effort) فشرده می‌شوند؛ ۸ turn آخر دست‌نخورده
- [x] **B7** — Plan بدون تأیید کاربر (بدون interrupt) — ✅ رفع (P5): gate تأیید در `runGeneration` — بعد از plan، event `plan_proposed {conversationId, plan}` بروکست و room روی `waitForPlanApproval` مکث می‌کند (auto-approve بعد از ۱۰ دقیقه، abort روی reject/cancel). مسیر کلاینت: `plan_approved`/`plan_rejected` → `ApprovePlan`/`RejectPlan`. فرانت: type `PlanProposedMessage` + هندلر + state `pendingPlan` + دکمه‌های Approve/Reject در `aboveContent` چت‌اینپوت (الگوی `human-in-the-loop/1_approval`). تست‌های `TestPlanApprovalGate`/`TestPlanVerdictWithoutPending`
- [x] **B8** — args خراب JSON در tool-call بدون ترمیم — ✅ رفع (P4): `pkg/agent/tools/repair.go` — decorator `NewJSONRepairTool` (الگوی middleware های `components/tool` eino-examples): یک تلاش ترمیم (comma انتهایی، brace باز، string بسته‌نشده، newline خام) + retry؛ همه ابزارها در `cmd/main.go` wrap شده‌اند + ۵ تست
- [x] **B9** — `ProjectRoom.stop` هنگام تولید گوش نمی‌دهد — ✅ رفع (P5): `generationContext` (ctx تولید با cancel روی Stop) + `BroadcastMessage` بدون block بعد از Stop (رفع goroutine-leak) + event `generation_cancelled` + گاردهای nil-Redis در `loadVFS`/`UpsertFile`/`DeleteFile` (panic واقعی که تست cancel کشف کرد) + ۲ تست `room_cancel_test.go` — ✅ end-to-end: `stop_generation` کلاینت → `CancelGeneration` (channel per-run با genMu، ایمن روی double-close) → route در `handleClientMessage`؛ دکمه Stop فرانت از قبل `stop_generation` می‌فرستاد + تست `TestCancelGenerationPerRun` (fresh channel per run، بدون panic روی cancel دوباره)
- [x] **B10** — بقایای `generated-output.txt` در Redis تمیز نمی‌شود — ✅ رفع (P2): `loadVFS` کلیدهای legacy را HDel می‌کند
- [x] **B11** — `finish_reason` استریم خوانده نمی‌شود — ✅ رفع (P2): `StreamChunk.FinishReason` روی chunk پایانی + تست `client_finish_test.go`
- [x] **B12** — state نیمه‌کاره بدون event علامت‌گذاری نمی‌شود — ✅ رفع (P4): event `generation_interrupted {reason}` در `models/websocket.go`؛ بروکست وقتی استریم وسط کار error می‌دهد (با شمار فایل‌ها) یا وقتی retryها تمام شد و فایل salvage-شده باقی ماند

---

## بخش B — ساب‌تسک‌های دیباگ

- [x] **D1 — تست‌های واحد پارسر** ✅ انجام شد
  - `pkg/llm/stream_parser_test.go` (۶ تست: feed تک‌کاراکتری، heading، skip بلاک زبانی، salvage، عدم نشت transcript، مطابقت با پارسر batch) + `parser_test.go`
  - اجرا: `cd backend && go test ./pkg/llm/ -v`
- [x] **D2 — Fixture replay** ✅ انجام شد
  - `backend/testdata/llm_transcripts/`: `heading_and_info.txt`، `truncated.txt`، `prose_blocks.txt`
  - `pkg/llm/replay_test.go`: `TestReplayTranscripts` (invariance بین chunk-size های ۱/۳/۱۳/۴۰۹۶ + عدم نشت transcript + غیرخالی بودن فایل‌ها) و `TestReplayTruncatedFixtureSalvagesCSS`
  - قانون: هر باگ جدید = یک fixture جدید در همین دایرکتوری
- [x] **D3 — لاگ ساخت‌یافته رویدادها** ✅ انجام شد
  - `pkg/engine/debuglog.go`: `DEBUG_EVENTS=1` → خطوط `ev=file_chunk_generated path="public/styles.css" bytes=1024`
  - وصل‌شده به `applyEvents` در `room.go` (file_generating / file_chunk / file_generated با seq و bytes)
- [x] **D4 — Endpoint دیباگ replay** ✅ انجام شد
  - `POST /api/debug/replay` body `{"transcript": "..."}` → پاسخ `{files:[{path,content,bytes}], events:[...]}`
  - پیاده‌سازی: `pkg/api/debug_routes.go`، ثبت در `RegisterRoutes` (`routes.go`) — بدون touch به VFS/Redis
  - استفاده: `curl -s localhost:8080/api/debug/replay -d '{"transcript":"```public/index.html\\n<p>x</p>\\n```"}' | jq`
- [ ] **D5 — دیباگ گراف eino** — فاز P3 (بعد از مهاجرت به ADK Runner؛ callback های eino مثل `devops/debug` نمونه)


---

(فازها در بخش بعدی همین فایل)


---

## بخش C — فازهای بازسازی (مطابق eino-examples)

### P0 — استریم‌پارسر per-file (✅ انجام شد)
الگو: رفتار استریم فایل‌محور vibesdk. فایل‌ها:
- `pkg/llm/stream_parser.go` + تست‌ها
- `room.go`: `applyEvents` → `file_generating` / `file_chunk_generated(path واقعی)` / `file_generated`

### P2 — رفع truncate و بقایای transcript (الزامی)
**الگوی اجباری:** `adk/agentic/retry_max_output_tokens/main.go`
- retry پله‌ای: `retryMaxTokens = []int{8192, 16384, 32768}`
- تشخیص cut: `finish_reason == "length"` (نیازمند B11)

کارها:
1. `pkg/llm/client.go`: `StreamChunk` دو فیلد جدید بگیرد:
   `FinishReason string` و `Usage`. در حلقه SSE، `choices[0].finish_reason`
   chunk آخر را propagate کن (B11).
2. `room.go → runGeneration`: پس از پایان استریم، اگر هر `finish_reason`
   برابر `length` بود → **ادامهٔ نسل** با درخواستِ «فقط فایل‌های ناقص را
   کامل کن» با MaxTokens پله بعدی (حداکثر ۲ retry). فایل‌های ناقص از
   StreamParser مشخص‌اند (event End بدون fence بسته = salvage flag).
3. پاکسازی B10: در `loadVFS` هر کلید `generated-output.txt` از hash حذف شود
   (HDel) + یک migration یک‌باره در start.
4. Fixture های D2 + تست replay.

**پذیرش:** با fixture `truncated.txt` هر ۴ فایل (index/styles/js/workflow)
در VFS حضور دارند؛ هیچ مسیری به `generated-output.txt` ختم نمی‌شود.

### P3 — یک‌دست‌سازی روی ADK Runner (الزامی)
**الگو:** `adk/helloworld` + `adk/intro/workflow` + `http-sse-service`
- همهٔ جریان‌ها (generation، chat، plan) از `adk.NewRunner` + event iterator
  بروند؛ `pkg/llm/client.go` فقط برای fallback/تست بماند (B4).
- `ProjectRoom` به‌جای LLM خام، `*agent.Engine` را بگیرد (تزریق از hub).
- استریم توکن/ابزار از یک مسیر: `StreamEvent` موجود در eino_engine گسترش
  یابد (فیلد `FilePath` برای eventهای ابزار vfs_write).
- Endpoint دیباگ D4 + D5 در همین فاز.

**پذیرش:** `grep -n "StreamChat" backend/pkg/engine` فقط در fallback؛
رویدادهای WS با هر دو مسیر یکسان.

### P4 — کیفیت agent (توصیه‌شده)
**الگوها:** `adk/intro/agent_with_summarization`، `components/tool` middlewares
1. Summarization تاریخچه (B6): وقتی `len(history) > N` یک call ارزان با
   prompt «خلاصهٔ تصمیم‌ها و فایل‌ها» بزن و جایگزین کن؛ در
   `store/redis_checkpoint.go` متد `Condense` اضافه شود.
2. Middleware ترمیم JSON دور ابزارها (B8): wrapper روی `tool.BaseTool` که
   args خراب را با تلاش repair (حذف trailing comma، بستن brace) اجرا کند؛
   در `ToolsNodeConfig.Tools` در `eino_engine.go`.
3. علامت‌گذاری state نیمه‌کاره (B12): event جدید
   `generation_interrupted {reason}` تا فرانت بداند فایل‌ها salvage شده‌اند.

**پذیرش:** fixture `prose_blocks.txt` و args خراب مصنوعی → tool اجرا و
فایل سالم نوشته می‌شود؛ تاریخچهٔ ۵۰ پیامی به ≤ N پیام خلاصه می‌رسد.

### P5 — تعامل و چرخهٔ عمر (توصیه‌شده)
**الگوها:** `adk/intro/chatmodel` (interrupt)، `human-in-the-loop/1_approval`،
`adk/cancel/graceful-exit`
1. تأیید Plan (B7): بعد از plan، agent با interrupt متوقف شود؛ فرانت دکمه
   Approve/Edit → resume از checkpoint (`store` از قبل `adk.CheckPointStore`
   را پیاده کرده). eventهای `plan_proposed` / `plan_approved`.
2. Cancel تمیز (B9): ctx تولید از lifecycle room مشتق شود؛ `Stop()` →
   ctx.cancel → استریم بسته → event `generation_cancelled` → checkpoint سالم.
3. پس از این فاز، مهاجرت به `adk/prebuilt/planexecute` (الگوی
   `multiagent/plan-execute-replan`) به‌عنوان Replanner فاز اصلاح —
   UI «phases» واقعی می‌شود.

**پذیرش:** رفرش صفحه وسط تولید → ادامهٔ صحیح؛ دکمهٔ stop وسط استریم →
بدون زامبی goroutine (تست با `-race`).

---

## بخش D — نقشهٔ فایل‌های تحت تغییر

| فایل | فاز | تغییر |
|------|-----|-------|
| `pkg/llm/client.go` | P2 | finish_reason + usage در StreamChunk |
| `pkg/engine/room.go` | P2..P5 | retry، Engine تزریقی، ctx lifecycle، summarization |
| `pkg/engine/debuglog.go` | ✅ P1 (انجام) | لاگ ساخت‌یافته رویدادها |
| `backend/testdata/llm_transcripts/*` | P1 | fixture های replay |
| `pkg/agent/eino_engine.go` | P3,P4 | گسترش StreamEvent، middleware ابزارها |
| `pkg/agent/tools/` | P4 | JSON-repair wrapper |
| `pkg/store/redis_checkpoint.go` | P4,P5 | Condense، resume |
| `pkg/models/websocket.go` | P4,P5 | eventهای جدید (interrupted/cancelled/plan) |
| `cmd/main.go` | P3 | سیم‌کشی Engine داخل hub |

## بخش E — ترتیب اجرا و کمی‌سازی

1. P2 (نیم روز): B3+B10+B11 — بزرگ‌ترین ریسک خروجی بی‌کیفیت را می‌بندد.
2. P3 (۱-۲ روز): حذف استک دوقلو — ✅ انجام شد: `RunStepWith` + `Room.streamLLM` + `hub.SetEngine`. باقی: D5 (دیباگ گراف eino).
3. P4 (۱ روز): کیفیت کانتکست و مقاومتی — ✅ انجام شد: B6 (summarization) + B8 (JSON-repair middleware) + B12 (`generation_interrupted`).
4. P5 (۱-۲ روز): UX و planexecute — B9 ✅ (cancel تمیز، end-to-end با دکمه Stop فرانت + route `stop_generation`)، B7 ✅ (gate تأیید plan با UI) و B5 ✅ (planexecute prebuilt + Executor ابزارمحور). باقی: D5 (دیباگ گراف eino — اختیاری).
هر فاز: `go vet ./... && go test ./... && build` سبز + تست replay fixture.
