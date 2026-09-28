# DEV TASKS SPEC — مشخصات اجرایی تسک‌ها + تست‌ها

> مکمل `DEV_CHECKLIST.md`. چک‌لیست می‌گوید «چی»؛ این فایل‌ها می‌گویند «چطور + با چه تستی».
> قرارداد تست عمومی همه Pها:
> - Go: `cd backend && go vet ./... && go test ./...` سبز؛ تست جدید در فایل `*_test.go` کنار کد. (`./...` نه `./pkg/...` — پکیج `backend/agent` هم تست دارد و planner/gate همان‌جاست؛ `backend/e2e` خودش `t.Skip` می‌زند مگر `VIBE_E2E=1`.)
> - TS: از روت `bun run typecheck && bun run lint && bun run build` سبز.
> - DB: مایگریشن‌های لوکال فقط بعد از **P1.0.0** (رفع Node ≥۲۲ + `--config wrangler.v2.jsonc` + نام `v2-vibe`) اجراپذیرند.
> - خط پایهٔ تأییدشده (اندازه‌گیری ۲۰۲۶-۰۹-۲۸): `go vet ./...` تمیز، `go test ./...` سبز (شامل `backend/agent`؛ `backend/e2e` بدون `VIBE_E2E=1` خودش skip می‌شود)، `typecheck` سبز، `lint` ۰ error/۳ warning، `build` سبز (~۷s)، `docs:check` سبز.
> - قانون تیک: بدون تست سبز، تیک نزن (قانون ۱ چک‌لیست).

| فاز | فایل اسپک |
|---|---|
| P1 (۰ تا ۵: lineage + Git داخلی + mirror + History) | `DEV_SPEC_P1A.md` |
| P1 (۶ تا ۱۰: reviewer + gate + claim + شناسنامه‌دار) | `DEV_SPEC_P1B.md` |
| P2 (کاتالوگ نود) | `DEV_SPEC_P2.md` |
| P3 + P4 (Suspense/Circuit + Sandbox) | `DEV_SPEC_P3P4.md` |
| P5 (App Runtime) | `DEV_SPEC_P5.md` |
| P6 (ادیتور ورکفلو) | `DEV_SPEC_P6.md` |

## Backlog اصلاح مستندات

> خروجی بررسی `docs/` (۲۰۲۶-۰۹-۲۴). ۱۱ تسک با اولویت، وابستگی، فایل هدف، معیار پذیرش و تست.
>
> **مرجع:** `docs/DOCS_AUDIT_BACKLOG.md`
> **وضعیت (۲۰۲۶-۰۹-۲۸):** ✅ هر ۱۶ تسک انجام شد (T1 … T16) — backlog بسته است. (T16: سه روتین نگهداشت دوره‌ای — route / عدد پلتفرم / binding — به gate ماشینی در `bun run docs:check` تبدیل شدند.)

