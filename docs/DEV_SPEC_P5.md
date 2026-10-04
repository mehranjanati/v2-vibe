# SPEC P5 — App Runtime as First Reference System (per-app backend)

> اصل: جدول‌های ثابت + `data_json` (نه DDL پویا — محدودیت D1). فقط `stock` جدول واقعی جدا دارد (برای decrement اتمی).

## P5.1 — Data model

- P5.1.1: `app_records(app_id, table_name, row_id, data_json, created_at, updated_at)` + ایندکس `(app_id, table_name)` و `(app_id, table_name, row_id)`.
- P5.1.2: `stock(app_id, sku, qty, updated_at)` با یکتایی `(app_id, sku)` + template مرجع `products/movements/invoices` (روی `app_records`، نه جدول جدا).
- P5.1.3: مایگریشن `0013_app_runtime.sql` via `db:generate` + migrate لوکال + idempotency.
- تست: migrate سبز؛ insert تکراری `(app_id, sku)` → خطای یکتایی.

## P5.2 — Transactional API (standard Change Path)

- لایهٔ اجرا: روی **Go control plane** (تراکنش + اسکوپ `user_id` کنار room/VFS است) — هندلر جدید مثل `backend/pkg/api/appdata.go` + ثبت در `backend/pkg/api/routes.go` + D1 از `backend/pkg/cloudflare/d1.go`. مسیرهای `worker/database/services/`/`worker/api/routes/` وجود ندارند (جزئیات Change Path در `docs/DEV_SPEC_P1A.md` → P1.4).
- P5.2.1: CRUD روی `app_records` با اسکوپ اجباری `app_id + user_id` در هر کوئری (فراموش شدن اسکوپ = ۵۰۰ نه، تست دارد).
- P5.2.2: decrement اتمی: `UPDATE stock SET qty = qty - ? WHERE app_id=? AND sku=? AND qty >= ?` → `rowsAffected==0` یعنی «موجودی ناکافی» (هرگز read-then-write).
- P5.2.3: audit هر write: `{who, app_id, table, row_id, before, after, at}` در `generation_audit` یا `app_audit` جدا.
- تست (حیاتی): **فروش همزمان**: دو ریکوئست موازی روی آخرین قلم → دقیقاً یکی موفق + qty هرگز منفی نشود (تست race با N=۲۰). + unit اسکوپ: کاربر B → ردیف اپ A → خالی/403.

## P5.3 — Roles

- P5.3.1: `app_members(app_id, user_id, role)` با `role ∈ admin/storekeeper/accountant`.
- P5.3.2: ماتریس policy در کد (نه DB): `storekeeper: stock RW, invoices RO` / `accountant: invoices RW, stock RO` / `admin: all`. هر endpoint اول `RequireRole`.
- P5.3.3: اتصال به auth فعلی (همان JWT/session) + تست نفوذ پایه: بدون توکن → 401؛ نقش اشتباه → 403 (روی هر endpoint، پارامتری).
- تست: ماتریس کامل ۳ نقش × ۴ عملیات → ۱۲ assertion.

## P5.4 — Frontend + agent wiring

- P5.4.1: داشبورد از `api-client` واقعی؛ localStorage فقط کش خوانا + بنر «آفلاین — فقط خواندن».
- P5.4.2: در `02_coder.md`: «فرم/گزارش فقط روی API واقعی؛ mock-data ممنوع».
- P5.4.3: در `03_reviewer.md`: چک «دیتا از API می‌آید؟ mock هست؟ policy نقش رعایت شده؟».
- تست: typecheck/lint + سناریو دستی: قطع API → حالت آفلاین-خوانا.

## P5.5 — Excel/CSV migration

- آپلود → پارس سمت سرور → اعتبارسنجی سطری → commit batch؛ سطر خراب → گزارش `{row, reason}` و ادامه بقیه (هرگز fail کل فایل). سقف ۱۰k سطر.
- تست: فایل ۱۰۰ سطری با ۵ سطر خراب → ۹۵ insert + گزارش دقیق ۵ سطر.

## P5.6 — Wave-1 validation

- سناریو E2E: ساخت اپ → import نمونه → دو فروش همزمان → موجودی درست → گزارش فروش امروز → نقش انباردار نتواند فاکتور حذف کند. همه سبز = P5 done.
