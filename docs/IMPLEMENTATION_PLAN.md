# Kế hoạch triển khai backend chấm bài C++

Ngày lập: **22/10/2026**. Hạn nộp môn học: **23:59 31/10/2026** (`Submission.md`).

Nguồn quyết định: `BACKEND_CONTEXT.md` (mục 1–18; ưu tiên mục mới hơn khi đã ghi "đã thay thế"), `docs/api-v1.md`, `database/schema-design.sql`. Kế hoạch coi backend như xây từ đầu; các phase 0–6 đã có code nên được **kiểm chứng lại** theo checklist, sửa ngay chỗ sai.

## Quy ước

- Mỗi phase: **Mục tiêu**, **Công việc**, **Checklist** (`- [ ]`), **Kiểm tra cuối phase**.
- Chỉ đánh `[x]` khi đã chạy kiểm tra thật; ghi ngắn lệnh/kết quả sau dấu `—`. Mục chưa làm được ghi lý do.
- Một commit local cho mỗi phase; không push khi chưa được duyệt.
- Lệnh dưới đây chạy trong thư mục `backend/`. Stack dev chạy bằng Docker Compose; `.env` local (git-ignore) có secret thật.
- Smoke test HTTP: `scripts/smoke.sh <phần>` (cần `SMOKE_TEACHER_CODE`, `SMOKE_TEACHER_PASSWORD`; xem đầu file script).
- Thuật ngữ: **GV** = giáo viên, **SV** = sinh viên, **run** = `judge_runs`, **job** = `judge_jobs`.

## Tổng quan phase

| Phase | Nội dung | Trạng thái |
|---|---|---|
| 0 | Repo, công cụ, quy ước | ✅ xong |
| 1 | Docker Compose, cấu hình, biến môi trường | ✅ xong |
| 2 | Schema DB, migration, gộp bảng `users` | ✅ xong |
| 3 | Auth (JWT, refresh token) | ✅ xong |
| 4 | Quản lý sinh viên | ✅ xong |
| 5 | Đề, phiên, tham gia | ✅ xong |
| 6 | Problem, test trên đĩa, chấm lại (quét + tăng version) | ✅ xong |
| 7 | Nộp bài | ✅ xong |
| 8 | Hàng đợi chấm bền vững | ✅ xong (chờ sandbox để chấm thật) |
| 9 | Sandbox chấm C++17 | ✅ xong |
| 10 | Chấm lại khi đổi version + chấm lại thủ công | ✅ xong |
| 11 | WebSocket: kết quả chấm, bảng GV, thông báo | ✅ xong |
| 12 | Tổng hợp AC, export CSV, thống kê hàng đợi | một phần (tổng hợp xong; còn CSV, `/admin/queue`) |
| 13 | Chống gian lận | ❌ không làm (Đã chốt 22/10/2026) |
| 14 | Gia cố, kiểm thử tải ~50 SV, quan sát | chưa làm |
| 15 | Tài liệu, báo cáo, kịch bản demo | chưa làm |

---

## Phase 0 — Repo, công cụ, quy ước

**Mục tiêu:** repo build/test được bằng một lệnh; bố cục và quy ước code rõ ràng để các phase sau làm theo.

**Công việc**
- Module Go `github.com/hoangdaulan/lan-lab-backend`, Go 1.26; thư viện đã chốt (mục 16): chi v5, pgx v5, goose v3, coder/websocket, golang-jwt v5, x/crypto (bcrypt).
- Bố cục package-by-feature: `cmd/{api,worker,migrate,seed}`, `internal/{config,db,httpx,apperr,auth,student,exam,problem,submission,judge}`; mỗi feature `handler.go → service.go → repository.go`, interface `Repository` khai báo trong `service.go`.
- Makefile: `up`, `down`, `migrate`, `seed`, `build`, `test`. Truy vấn DB viết tay bằng pgx (bỏ sqlc — Đã chốt 22/10/2026).
- `.gitignore`: `.env`, `bin/`, `data/`. `.dockerignore`.
- Script smoke test HTTP `scripts/smoke.sh`.
- Swagger UI tại `/swagger/` (spec duy trì tay trong `docs/swagger.json` + `docs/swagger.yaml`, nhúng qua `docs/docs.go`), ví dụ tay `examples/api-manual.http`.
- **Quy tắc đồng bộ:** mỗi phase thêm/sửa endpoint phải cập nhật cùng lúc `docs/api-v1.md`, `docs/swagger.json`, `docs/swagger.yaml` (và `examples/` nếu ảnh hưởng); số thao tác trong `internal/httpx/swagger_test.go` phải khớp.

**Checklist**
- [x] `go build ./...` không lỗi — 22/10: OK
- [x] `go vet ./...` không cảnh báo — 22/10: OK
- [x] `gofmt -l .` không in file nào — 22/10: rỗng
- [x] `go test -race -count=1 ./...` pass — 22/10: auth, exam, httpx, problem, student ok
- [x] `make build` tạo `bin/api`, `bin/worker`, `bin/migrate`, `bin/seed` — 22/10: đủ 4 file
- [x] `.gitignore` chứa `.env`, `bin/`, `data/`; `git check-ignore .env data` khớp — 22/10: `git check-ignore -v .env data bin` khớp 3 dòng
- [x] Mỗi feature có `handler.go`, `service.go`, `repository.go`; `Repository` là interface trong `service.go` — 22/10: auth, student, exam, problem; `submission`, `judge` mới là stub
- [x] Lỗi nghiệp vụ map HTTP tại một chỗ (`httpx.WriteError` + `apperr`) cho feature mới — 22/10: exam, problem dùng `apperr`; auth, student có `writeError` riêng (viết trước khi có `apperr`, giữ nguyên, hành vi đã test)
- [x] `scripts/smoke.sh` tồn tại, chạy được với `bash -n` — 22/10: thêm mới ở phase này, `bash -n` OK
- [x] ~~Truy vấn DB qua sqlc (`make sqlc`)~~ — bỏ (Đã chốt 22/10/2026, câu 14): xóa `sqlc.yaml`, `queries/`, target `make sqlc`; dùng pgx viết tay
- [x] Swagger UI `/swagger/index.html` phục vụ được, spec hợp lệ, `swagger.json` ≡ `swagger.yaml` — 22/10: `go test ./internal/httpx/` ok (UI + doc.json, 32 thao tác); so sánh JSON/YAML bằng Python: bằng nhau. Kiểm tra qua HTTP thật ở phase 1 sau khi build lại image
- [x] Swagger khớp `docs/api-v1.md` (đường dẫn + body login) — 22/10: 32 route ✅ trong doc = 32 trong spec, không lệch; login spec `{code,password}` (áp dụng sau phase 2)

**Kiểm tra cuối phase**
```bash
go build ./... && go vet ./... && test -z "$(gofmt -l .)" && go test -race -count=1 ./...
make build && ls bin/
bash -n scripts/smoke.sh
go test ./internal/httpx/ && curl -s -o /dev/null -w '%{http_code}' localhost:8080/swagger/index.html
```

---

## Phase 1 — Docker Compose, cấu hình, biến môi trường

**Mục tiêu:** `make up` dựng PostgreSQL + API + worker trên một máy Linux trong LAN; cấu hình qua `.env`.

**Công việc**
- Dockerfile multi-stage: một stage build Go; stage `api` (kèm `migrate`, `seed`), stage `worker` (+ `build-essential`, g++ 12.2 Debian bookworm); chạy user không root.
- Compose: `postgres:16` (healthcheck, volume `pgdata`, chỉ publish `127.0.0.1:5433` cho công cụ DB trên host), `api` (publish `0.0.0.0:${API_PORT:-8080}`), `worker` (chỉ mạng `db` internal); bind mount `./data:/data`; `user: ${APP_UID}:${APP_GID}`.
- `internal/config`: đọc `DATABASE_URL`, `JWT_SECRET` (≥ 32 ký tự), `DATA_DIR`, `HTTP_ADDR`, `ACCESS_TOKEN_TTL`, `REFRESH_TOKEN_TTL`, `WORKER_*`; báo lỗi rõ khi thiếu.
- `/healthz` ping DB; tắt êm khi SIGTERM.

**Checklist**
- [x] `docker compose --env-file .env.example config --quiet` hợp lệ; thiếu `.env` thì báo biến bắt buộc — 22/10: OK; `--env-file /dev/null` báo thiếu `JWT_SECRET`, `POSTGRES_DB`, `POSTGRES_PASSWORD`, `POSTGRES_USER`
- [x] `docker compose build` thành công — 22/10: `backend-api`, `backend-worker` built
- [x] Postgres healthy; chỉ lắng nghe `127.0.0.1:5433` trên host, không mở ra LAN — 22/10: `ss -ltn` chỉ có `127.0.0.1:5433`; nối `192.168.1.9:5433` bị từ chối
- [x] `curl http://localhost:8080/healthz` → 200 `{"db":"up"}`; qua IP LAN cũng 200 — 22/10: localhost 200, `192.168.1.9:8080` 200
- [x] Dừng postgres → `/healthz` 503; bật lại → 200 — 22/10: 503 `{"db":"down"}` rồi 200
- [x] Worker không ra được Internet, nối được `postgres:5432` — 22/10: `1.1.1.1:80` unreachable, `postgres:5432` reachable
- [x] Container chạy user không root (`id`) — 22/10: api, worker `uid=1000` (theo `APP_UID` trong `.env`; mặc định image 10001)
- [x] Image worker có `g++ 12.2.0` — 22/10: `g++ (Debian 12.2.0-14+deb12u1) 12.2.0`
- [x] `./data` là bind mount, file tạo trong container thuộc `APP_UID` — 22/10: file tạo từ api thuộc 1000:1000 trên host (đã xóa file thử)
- [x] Config: thiếu/ngắn `JWT_SECRET` → API thoát với thông báo rõ (unit test) — 22/10: **bổ sung** `internal/config/config_test.go`; `TestRequireAPIRejectsMissingOrShortSecret`, `TestRequireAPIReportsAllMissing` pass
- [x] Config: TTL/concurrency sai định dạng → lỗi rõ (unit test) — 22/10: `TestLoadRejectsMalformedValues` pass (lỗi nêu tên biến)
- [x] SIGTERM: API log `api shutting down`, worker log `worker stopping`, thoát mã 0 — 22/10: `docker compose restart api worker` → đủ 2 log, exit code 0

**Kiểm tra cuối phase**
```bash
docker compose --env-file .env.example config --quiet
make up && curl -s localhost:8080/healthz
docker compose exec -T worker g++ --version
go test ./internal/config/
docker compose restart api worker && docker compose logs --tail 20 api worker
```

---

## Phase 2 — Schema DB, migration, bảng `users`

**Mục tiêu:** schema theo `BACKEND_CONTEXT.md` mục 9 + 18, migration goose chạy được lên/xuống; một bảng `users` có `role`.

**Công việc**
- `migrations/00001_init.sql` = DDL của `database/schema-design.sql` (bỏ BEGIN/COMMIT), có `-- +goose Down`.
- Gộp `teachers` + `students` → `users(id, code UNIQUE, fullname, role CHECK, password_hash, created_at, updated_at)`; `exams.teacher_id`, `exam_participants.student_id`, `refresh_tokens.user_id` → `users`. Ràng buộc vai trò ở tầng service.
- Cập nhật code auth/student/exam, test, `docs/api-v1.md`, `BACKEND_CONTEXT.md` mục 18.
- Tạo lại DB dev: chỉ xóa volume `pgdata`, giữ `./data`.

**Checklist**
- [x] DDL migration khớp `schema-design.sql` (so sánh tự động) — 22/10: so dòng DDL bằng Python (`difflib`), giống hệt
- [x] `make migrate` trên DB trống → version 1; 11 bảng; 13 index `*_idx` — 22/10: DB nháp `judge_scratch`: `OK 00001_init.sql`, version 1, 11 bảng, 13 index
- [x] Down rồi up lại trên DB nháp chạy sạch — 22/10: down → 0 bảng, up lại → version 1; DB nháp đã xóa
- [x] `users.code` UNIQUE toàn bảng; `role` chỉ nhận `teacher`/`student` — 22/10: SV trùng mã `GV01` → `users_code_key`; `role='admin'` → `users_role_check`; mã toàn khoảng trắng → `users_code_check`
- [x] FK tới `users`: `exams.teacher_id`, `exam_participants.student_id`, `refresh_tokens.user_id` (CASCADE) — 22/10: `pg_constraint`: exams/participants RESTRICT, refresh_tokens CASCADE (xóa user → 0 token còn lại). DB **không** chặn GV làm participant — đúng thiết kế, kiểm tra ở service (mục 18)
- [x] CHECK của `judge_runs`/`judge_jobs` chặn trạng thái sai (thử insert sai) — 22/10: completed thiếu verdict, verdict khi chưa completed, AC thiếu test đều bị chặn; WA 2/3 hợp lệ; trùng (submission, version) và job running thiếu lease bị chặn
- [x] `docs/api-v1.md` và `BACKEND_CONTEXT.md` ghi quyết định gộp bảng, đánh dấu quyết định cũ đã thay thế — 22/10: mục 18 mới; mục 4, 9, 17 gắn "[Đã thay thế 22/10/2026]"; Swagger login `{code,password}` (`/swagger/doc.json` thật: required `[code, password]`)
- [x] DB dev tạo lại: chỉ volume `pgdata` bị xóa; `./data` giữ nguyên — 22/10 10:18: `docker volume rm backend_pgdata`, migrate + seed GV01; `./data/problems/*` còn nguyên

**Kiểm tra cuối phase**
```bash
make migrate
docker compose exec -T postgres psql -U $POSTGRES_USER -d $POSTGRES_DB -c '\dt'
go test -race -count=1 ./...
```

---

## Phase 3 — Auth

**Mục tiêu:** đăng nhập bằng `code` + mật khẩu; JWT HS256 15 phút; refresh token 7 ngày, xoay vòng một lần dùng.

**Công việc**
- `POST /auth/login {code,password}`, `/auth/refresh`, `/auth/logout`, `GET /me`, `PUT /me/password`.
- bcrypt cost 10, tối đa 72 byte; `code` trim + viết hoa; so mật khẩu giả khi `code` không tồn tại.
- Refresh token 32 byte ngẫu nhiên, DB giữ SHA-256; khóa account → token; login/đổi mật khẩu kiểm tra lại hash dưới khóa.
- Middleware `Authenticate`, `RequireRole`; `cmd/seed` tạo GV đầu tiên (idempotent).

**Checklist**
- [x] Unit test service + middleware pass — 22/10: `go test -race ./internal/auth/` ok (16 test)
- [x] Login đúng → 200, `user.role` lấy từ DB, JWT `alg=HS256`, `role` đúng, `exp - iat = 900` — 22/10: `scripts/smoke.sh auth` 20/20
- [x] Gửi `role` trong body login → 400 (trường lạ) — 22/10: 400 `unknown field "role"`
- [x] Sai mật khẩu / mã không tồn tại → 401 cùng thông báo — 22/10: smoke so sánh body giống hệt
- [x] Refresh → token mới khác cũ; dùng lại token cũ → 401 — 22/10: `scripts/smoke.sh auth` 20/20
- [x] Logout → refresh token đó 401 — 22/10: 204 rồi 401
- [x] Đổi mật khẩu: sai mật khẩu hiện tại 401; đúng → 204, mọi refresh token của tài khoản bị xóa — 22/10: `scripts/smoke.sh auth` 20/20
- [x] `/me` không token 401; token sai chữ ký 401 — 22/10: **lỗi phát hiện và sửa**: đổi ký tự cuối của chữ ký (chỉ khác bit đệm base64) vẫn được chấp nhận (200). Thêm `jwt.WithStrictDecoding()`; unit test thử mọi ký tự ở mọi vị trí chữ ký (fail khi bỏ fix, pass khi có fix); smoke giờ 401
- [x] Route GV gọi bằng token SV → 403 và ngược lại — 22/10: `/students` bằng SV 403, `/me/sessions` bằng GV 403
- [x] DB chỉ lưu hash (`$2a$10$…`, token_hash 32 byte) — 22/10: mọi `password_hash` bắt đầu `$2a$10$`; mọi `token_hash` dài 32 byte
- [x] `make seed` chạy 2 lần: lần 2 không đổi gì — 22/10: `teacher already exists, nothing changed`

**Kiểm tra cuối phase**
```bash
go test -race -count=1 ./internal/auth/
scripts/smoke.sh auth
```

---

## Phase 4 — Quản lý sinh viên

**Mục tiêu:** GV tạo/import/tìm/reset mật khẩu sinh viên; mật khẩu mặc định = MSV.

**Công việc**
- `POST /students`, `GET /students?q&limit&offset`, `POST /students/import` (CSV), `POST /students/{id}/reset-password`.
- Import: dòng lỗi → từ chối cả file; mã đã có → bỏ qua và báo (`already exists` / `code belongs to a teacher account`).

**Checklist**
- [x] Unit test pass — 22/10: `go test -race ./internal/student/` ok (11 test, gồm mã của GV bị 409 và báo lý do khi import)
- [x] Tạo SV: mã trim + viết hoa, tên gộp khoảng trắng; 201 — 22/10: `scripts/smoke.sh students` 15/15
- [x] Trùng mã SV hoặc mã GV → 409 — 22/10: cả hai 409 `code already used by another account`; mã có khoảng trắng 400
- [x] Import CSV lỗi → 400, `row_errors` đúng dòng, không lưu gì — 22/10: dòng 3 (thiếu tên), 4 (trùng trong file); tìm lại mã → total 0
- [x] Import có mã trùng → bỏ qua đúng dòng + lý do; phần còn lại lưu — 22/10: CSV có BOM + cột `class`: tạo 2; bỏ qua dòng 3 `already exists`, dòng 5 `code belongs to a teacher account`
- [x] List chỉ có SV (không lẫn GV), tìm theo mã/tên, phân trang — 22/10: `limit=2` trả 2 bản ghi, `total` đúng; tìm `GV01` không ra giáo viên
- [x] SV đăng nhập bằng MSV; SV gọi `/students` → 403 — 22/10: `scripts/smoke.sh students` 15/15
- [x] Reset → mật khẩu về MSV, refresh token cũ 401; id của GV → 404 — 22/10: `scripts/smoke.sh students` 15/15

**Kiểm tra cuối phase**
```bash
go test -race -count=1 ./internal/student/
scripts/smoke.sh students
```

---

## Phase 5 — Đề, phiên, tham gia

**Mục tiêu:** GV quản lý đề/phiên của mình; SV tham gia trong khung giờ, kết thúc làm bài.

**Công việc**
- CRUD đề, phiên; `GET /sessions/{id}` cho cả hai vai trò; `/me/sessions`, `join`, `finish`, `participants`.
- Giờ server quyết định: `starts_at <= now < ends_at`; `ends_at > starts_at` bắt buộc, backend không tự đặt thời lượng.

**Checklist**
- [x] Unit test pass — 22/10: `go test -race ./internal/exam/` ok (7 test)
- [x] Tạo phiên `ends_at <= starts_at` → 400 — 22/10: `scripts/smoke.sh exams` 20/20
- [x] GV khác không thấy/sửa được đề → 404 — 22/10: seed thêm `GV02`; đọc/sửa đề, xem participants → 404, danh sách đề của GV02 không có đề của GV01 (smoke thêm biến `SMOKE_TEACHER2_*`)
- [x] SV thấy mọi phiên; trước giờ bắt đầu `problems_hidden = true` — 22/10: `scripts/smoke.sh exams` 20/20
- [x] Join trước giờ / sau giờ → 409; trong giờ → 200; join lại trả bản ghi cũ — 22/10: trước giờ 409 (smoke); sau giờ 409 và mốc `ends_at` loại trừ: unit test `TestJoinOnlyWhileRunning`
- [x] Finish idempotent, `finished_at` không đổi; chưa join → 409 — 22/10: `scripts/smoke.sh exams` 20/20
- [x] Xóa đề còn con → 409; xóa phiên có bài nộp → 409 — 22/10: đề còn con 409 (smoke); chèn tạm 1 dòng `submissions` bằng psql → xóa phiên 409, xóa problem 409, rồi xóa dòng tạm
- [x] `participants` hiện mã, tên, trạng thái — 22/10: `scripts/smoke.sh exams` 20/20

**Kiểm tra cuối phase**
```bash
go test -race -count=1 ./internal/exam/
scripts/smoke.sh exams
```

---

## Phase 6 — Problem, test trên đĩa, chấm lại (quét)

**Mục tiêu:** problem có thư mục test `DATA_DIR/problems/<id>/tests`; nút "Chấm lại" đồng bộ test và tăng version.

**Công việc**
- CRUD problem; đổi `time_limit_ms`/`memory_limit_mb` → `version + 1`.
- `POST /problems/{id}/rejudge`: quét cặp `N.in/N.out`, từ chối symlink/thiếu cặp/rỗng, thay `test_cases`, tăng version một lần trong cùng transaction.
- `GET /sessions/{id}/problems/{pid}` cho SV: chỉ từ `starts_at` và đã join.

**Checklist**
- [x] Unit test pass (gồm quét thư mục, symlink) — 22/10: `go test -race ./internal/problem/` ok (7 test, `TestScanTestsErrors` chặn symlink)
- [x] Tạo problem tạo thư mục test trên host, đúng chủ sở hữu — 22/10: `./data/problems/<id>/tests` thuộc `toan` (UID 1000 = `APP_UID`), ghi được
- [x] Trùng mã trong đề → 409; giới hạn ngoài 1–10000 ms / 1–1024 MB → 400 — 22/10: `scripts/smoke.sh problems` 17/17
- [x] Chấm lại khi chưa có test / thiếu cặp → 409, version không đổi — 22/10: smoke 409 hai lần rồi lần hợp lệ ra version 2 (không tăng khi lỗi); unit test kiểm version = 1 sau lỗi
- [x] Chấm lại hợp lệ → version + 1, test theo thứ tự số — 22/10: `1, 2, 10` (file `2.in` rỗng hợp lệ)
- [x] Sửa chữ không tăng version; đổi limit tăng version — 22/10: `scripts/smoke.sh problems` 17/17
- [x] SV xem đề: chưa join 403, trước giờ 403, problem khác đề 404, sau khi join 200 — 22/10: `scripts/smoke.sh problems` 17/17
- [x] Xóa problem chưa có bài nộp → 204, file test giữ nguyên — 22/10: 204, GET sau đó 404, thư mục `./data/problems/<id>` vẫn còn
- [x] Phần tạo lại run/job khi tăng version — làm ở phase 10 (`requeueProblem`)

**Kiểm tra cuối phase**
```bash
go test -race -count=1 ./internal/problem/
scripts/smoke.sh problems
```

---

## Phase 7 — Nộp bài

**Mục tiêu:** SV nộp một file `.cpp` ≤ 64 KB trong phiên; server ghi nhận bền vững rồi mới báo thành công.

**Công việc**
- `POST /sessions/{id}/problems/{pid}/submissions` (multipart `file`): ghi `received_at` ngay khi nhận đủ file; lưu file vào `DATA_DIR/submissions/<yyyy>/<mm>/<32 hex>.cpp` (tên server tạo), `fsync`.
- Transaction: khóa problem (SHARE) → session → participant `FOR UPDATE`; kiểm tra SV từ JWT là chủ participant, `status = in_progress`, problem thuộc đề của phiên, `starts_at <= received_at < ends_at`, bộ test không rỗng; tạo submission + run (version hiện hành) + job; COMMIT rồi mới trả 202.
- DB lỗi: không báo nhận; file mồ côi dọn sau (job dọn định kỳ, có khoảng chờ).
- `GET /sessions/{id}/submissions` (S: của mình; T: lọc), `GET /submissions/{id}`, `GET /submissions/{id}/source`, `GET /submissions/{id}/tests` (T).
- Chặn (Đã chốt 22/10/2026, câu 4): tên file không đuôi `.cpp`, file > 64 KB, file rỗng. Không bắt buộc UTF-8.

**Checklist**
- [x] Swagger + `docs/api-v1.md` đồng bộ — 22/10: thêm 5 thao tác (tổng 37), `swagger.json` ≡ `swagger.yaml`, `go test ./internal/httpx/` ok
- [x] Nộp hợp lệ → 202 + id; có đủ submission + run `queued` + job `queued` trong DB — 22/10: `scripts/smoke.sh submissions` 23/23; psql: 18 submission = 18 run queued = 18 job queued, mỗi bài đúng 1 run + 1 job, version run = version problem, 18 file trên đĩa
- [x] Nộp sau `ends_at` (theo `received_at`) → 409; trước `starts_at` → 409 — 22/10: GV dời giờ phiên sau khi SV join → `not started`; phiên hết hạn 2 s → `ended`
- [x] Đã finish → 409; chưa join → 409/403 — 22/10: cả hai 409
- [x] File 64 KB + 1 byte → 413/400; file rỗng → 400 — 22/10: đúng 64 KB → 202; 64 KB + 1 → 413; body 300 KB → 413; rỗng 400; đuôi `.c` 400
- [x] Nộp problem của đề khác → 404 — 22/10: `scripts/smoke.sh submissions` 23/23
- [x] Problem chưa có test → 409 — 22/10: `the problem has no tests yet`
- [x] Nộp và finish đồng thời (2 request song song): kết quả nhất quán với thứ tự khóa (test tích hợp) — 22/10: 16 lần thử song song (finish lệch 0–70 ms): 14 nộp thắng, 2 finish thắng; 202 ⇔ có bản ghi, 409 `finished` ⇔ không có bản ghi; psql: 0 bài có `received_at >= finished_at`
- [x] DB lỗi giả lập → không trả 202, file được đánh dấu mồ côi — 22/10: unit test `TestSubmitDBErrorLeavesOrphanAndFails` (lỗi DB → lỗi, file giữ lại); bị từ chối theo quy tắc → file xóa ngay (`TestSubmitRejectedByRuleRemovesFile`). Dọn file mồ côi định kỳ: phase 14
- [x] SV không xem được bài/mã nguồn của SV khác (404) — 22/10: chi tiết + source 404; danh sách SV khác rỗng; SV đọc `/tests` → 403
- [x] Unit test service với fake repo + fake storage — 22/10: `go test -race ./internal/submission/` ok (8 test, gồm storage thật trên thư mục tạm, chặn path traversal)

**Kiểm tra cuối phase**
```bash
go test -race -count=1 ./internal/submission/
scripts/smoke.sh submissions
```

---

## Phase 8 — Hàng đợi chấm bền vững

**Mục tiêu:** worker lấy job an toàn khi nhiều worker chạy song song; phục hồi khi worker chết.

**Công việc**
- Claim: đọc ứng viên `queued` không khóa; transaction ngắn khóa problem SHARE → run FOR UPDATE → job `FOR UPDATE SKIP LOCKED`; kiểm tra lại status/`available_at`/version; gán `running`, `worker_id`, `lease_token`, `lease_expires_at`, `attempt_count + 1`; run → `judging`, `started_at`.
- Gia hạn lease định kỳ khi đang chấm; chỉ ghi kết quả khi token khớp, lease còn hạn, version còn hiện hành.
- Phục hồi lease hết hạn (định kỳ + khi khởi động): job về `queued` hoặc `obsolete` nếu version cũ; reset kết quả lần trước.
- Retry lỗi hạ tầng tối đa 3 lần, backoff 5 s / 30 s / 2 phút; lease 60 s + heartbeat (Đã chốt 22/10/2026, câu 5); hết lần → run `system_error`, job `failed`.
- Không giữ transaction trong lúc biên dịch/chạy.
- `WORKER_CONCURRENCY` giới hạn số bài chấm đồng thời mỗi worker.

**Checklist**
- [x] Test tích hợp với PostgreSQL thật: 2 worker × 20 job → mỗi job chấm đúng một lần — 22/10: `make test-integration` (PostgreSQL thật, mỗi test tạo/xóa DB riêng): `TestTwoWorkersJudgeEachJobExactlyOnce` — 20 run chấm đúng 1 lần, 20 job `done` attempt 1, lease đã xóa, 40 dòng kết quả
- [x] Giết worker giữa chừng → sau lease hết hạn job được chấm lại, không trùng kết quả — 22/10: `TestExpiredLeaseRecoveredAndOldWorkerDropped`: heartbeat sau hạn → `ErrLeaseLost`; recovery đưa job về `queued` (`last_error` = lease expired); worker mới attempt 2 ghi 2 dòng; worker cũ `Complete` → `ErrLeaseLost`, không ghi gì. Kiểm tra đột biến: bỏ so khớp `lease_token` → test fail
- [x] Job của version cũ hoàn tất muộn → bị loại, không ghi đè — 22/10: `TestStaleVersionResultDropped`: tăng version giữa chừng → `ErrStale`, run/job `obsolete`, 0 dòng kết quả; job version cũ còn trong hàng đợi bị obsolete khi claim, không chấm
- [x] Lỗi hạ tầng lặp lại → `system_error` sau N lần, không thành WA/RE — 22/10: `TestInfrastructureFailuresBecomeSystemError`: 3 lần gọi judge, run `system_error` verdict NULL, job `failed` attempt 3; `TestBackoffDelaysRetry`: job không được claim lại trước `available_at`
- [x] Thứ tự khóa đúng mục 10 (rà soát code + test deadlock song song với chấm lại) — 22/10: mọi transaction trong `internal/judge/repository.go` khóa problem (SHARE) → run → job; `TestNoDeadlockWithConcurrentRejudge`: 3 worker + 115 lần chấm lại giả lập trong 4 s, 0 deadlock (40P01), không có run `completed` ở version cũ
- [x] Log có `worker_id`, `job_id`, thời gian chờ hàng đợi — 22/10: rà soát code `Worker.ProcessOne` (`worker_id`, `job_id`, `run_id`, `submission_id`, `attempt`, `queue_wait_ms`, `judge_ms`); log runtime thật sẽ thấy khi có sandbox (phase 9). Worker hiện log `judging=false` và để job ở `queued`

**Ghi chú triển khai**
- Snapshot (source, test, giới hạn, version) đọc trong transaction claim. Vì test được sửa trực tiếp trên đĩa rồi bấm "Chấm lại" (mục 17), việc giữ khóa SHARE khi chép file không còn bảo vệ được gì; tính đúng dựa vào kiểm tra version lúc `Complete`.
- Mọi mốc thời gian của hàng đợi lấy từ `clock_timestamp()` của DB (một đồng hồ chung cho mọi worker).
- Chưa có sandbox: `cmd/worker` dùng `DisabledJudger` → không claim job, chỉ chạy phục hồi lease.

**Kiểm tra cuối phase**
```bash
go test -race -count=1 ./internal/judge/
make test-integration
```

---

## Phase 9 — Sandbox chấm C++17

**Mục tiêu:** biên dịch và chạy bài SV an toàn; verdict đúng.

**Công việc**
- Snapshot đọc trong transaction claim (phase 8); source chép vào thư mục tạm riêng của job (`/var/judge/job<id>-a<attempt>`, 0700, chủ là runner uid).
- Biên dịch: `g++ -std=c++17 -O2 -pipe -static -DONLINE_JUDGE` (Đã chốt 22/10/2026, câu 2), timeout 10 s, giới hạn bộ nhớ/output biên dịch; ghi `compiler_info` thực tế; compile output cắt gọn.
- Chạy từng test (Đã chốt 22/10/2026, câu 3 — `setrlimit`): uid riêng mỗi slot (20000+slot), network namespace rỗng (`CLONE_NEWNET`), không capability, `no_new_privs`; `setrlimit` CPU (= time limit), AS (= memory + 64 MB), STACK (= memory), FSIZE (stdout 64 MB), NPROC 1, NOFILE 16, CORE 0; wall-clock = 2 × time limit + 1 s; dọn tiến trình còn sót bằng `kill(-1)` dưới uid đó. Biên dịch cũng chạy trong sandbox (uid riêng, 10 s, 1.5 GB, NPROC 64).
- Phát hiện: TLE = hết wall-clock / CPU > time limit / SIGXCPU; MLE = RSS đỉnh > giới hạn hoặc chương trình chết với `std::bad_alloc`; RE = tín hiệu khác / mã thoát ≠ 0 (kể cả SIGXFSZ khi stdout > 64 MB). Giới hạn đã biết: chương trình C bỏ qua `malloc` thất bại rồi crash sẽ là RE thay vì MLE (không có cgroup).
- `DATA_DIR` được worker `chmod 0750` khi khởi động (chủ là `APP_UID`) để runner uid không đọc được test/bài nộp khác; worker là root trong container với `cap_drop: ALL` + `cap_add: SYS_ADMIN, SETUID, SETGID, CHOWN, FOWNER, DAC_OVERRIDE, KILL`, `no-new-privileges`.
- So sánh theo token (bỏ qua khoảng trắng/xuống dòng).
- Verdict test: AC/WA/TLE/MLE/RE; verdict run: CE ở cấp run; AC khi tất cả test AC. Chạy test theo thứ tự, **dừng ở test không AC đầu tiên**; `passed_tests` = số test đúng trước đó, verdict tổng = verdict của test đó (Đã chốt 22/10/2026, câu 1); API trả `passed_tests`, `total_tests`, `verdict` để FE hiện `10/15 TLE`.
- Ghi kết quả từng test + tổng hợp run + job done trong một transaction (kiểm tra lại version/lease dưới khóa).

**Checklist**
- [x] Bộ bài mẫu: AC, WA, TLE (vòng lặp vô hạn), MLE (cấp phát lớn), RE (chia 0 / segfault), CE (lỗi cú pháp) → đúng verdict — 22/10: `make test-sandbox` (binary test chạy trong container worker, g++ thật): AC, WA dừng ở test 2, TLE (vòng lặp, `sleep`), MLE (`new` 1 GB → bad_alloc; malloc 150 MB với giới hạn 128 MB → RSS), RE (segfault, chia 0, `return 3` sau 1 test đúng), CE, đệ quy sâu trong giới hạn vẫn AC. Qua API (`scripts/smoke.sh judge`): `3/3 AC`, `1/3 WA`, `2/3 TLE`, `0/3 RE`, `0/3 CE`
- [x] Chương trình mở socket → thất bại (không mạng) — 22/10: `TestNoNetwork`: worker tự nối được `postgres:5432`, chương trình SV nối `postgres:5432`, `1.1.1.1:80`, `127.0.0.1:5432` đều thất bại. Đột biến: bỏ `CLONE_NEWNET` → test fail
- [x] Fork bomb bị chặn (NPROC), output 1 GB bị cắt (FSIZE/giới hạn đọc) — 22/10: 1000 lần `fork` đều thất bại (in `0`); ghi 1 GB → SIGXFSZ ở 64 MB → RE; sau mỗi bài không còn tiến trình nào của runner uid
- [x] Đọc file ngoài thư mục chấm (vd. `/data/problems/...`) → không được — 22/10: đáp án của chính test, source SV khác, `/proc/1/environ` (chứa `DATABASE_URL`) đều không mở được; `#include` file source SV khác lúc biên dịch → CE `Permission denied`. Chương trình chạy với uid 20000+, `CapEff` = 0, `NoNewPrivs` = 1 (`TestIdentityAndPrivileges`). `./data` trên host thành `drwxr-x---`
- [x] `compiler_info` ghi `g++ (Debian 12.2.0-…) 12.2.0` — 22/10: kiểm trong mọi case của `TestVerdicts`; log worker `judger ready compiler="g++ (Debian 12.2.0-14+deb12u1) 12.2.0"`
- [x] SV không thấy input/output test ẩn, không thấy `system_error` — 22/10: API SV không có `system_error`, `/submissions/{id}/tests` → 403 (phase 7); chương trình không đọc được file test (ở trên)
- [x] Bài không có test không được AC — 22/10: nộp vào problem 0 test bị chặn 409 (phase 7); judger trả lỗi hạ tầng cho job không có test (`TestJudgeInfrastructureErrors`); `Outcome.Validate` từ chối `total_tests = 0`

- [x] 18 bài nộp `queued` từ các phase trước được chấm qua stack thật — 22/10: 16 × `1/1 AC`, 2 × `0/1 CE` (file 64 KB toàn khoảng trắng, không có `main`)

**Kiểm tra cuối phase**
```bash
go test -race -count=1 ./internal/cppjudge/ ./internal/judge/
make test-sandbox
scripts/smoke.sh judge
```

---

## Phase 10 — Chấm lại khi đổi version + chấm lại thủ công

**Mục tiêu:** thay test hoặc đổi limit thì kết quả hiện hành luôn theo version mới.

**Công việc**
- Thay stub `requeueProblem`: trong transaction tăng version, run/job cũ → `obsolete` (xóa verdict, xóa lease), tạo run + job mới tại version mới cho **mọi** bài nộp của problem (mọi phiên).
- `POST /submissions/{id}/rejudge` (T): run cũ → `obsolete`, tạo run + job mới tại version hiện hành; đổi `UNIQUE(submission_id, problem_version)` thành unique index một phần `WHERE status <> 'obsolete'` trong migration `00001` (Đã chốt 22/10/2026, câu 6).
- FE/REST hiển thị "đang chấm lại", không coi kết quả cũ là hiện hành.

**Checklist**
- [x] Unique index một phần thay `UNIQUE (submission_id, problem_version)` — 22/10: migration `00001` + `schema-design.sql` (DDL vẫn khớp), DB dev `ALTER` tại chỗ; 14 index bổ sung
- [x] Swagger + `docs/api-v1.md` đồng bộ — 22/10: thêm `POST /submissions/{id}/rejudge` (tổng 38), mô tả `requeued_submissions`
- [x] Chấm lại problem có N bài → N run mới version mới, run cũ `obsolete` — 22/10: `make test-integration` `TestLimitChangeRequeuesEverySubmission` (5 bài ở 2 phiên → 5 run+job `queued` v2, 5 run v1 `obsolete`), `TestReplaceTestsRequeues`; API trả `requeued_submissions`
- [x] Worker đang chấm version cũ → kết quả bị loại — 22/10: job đang chạy bị đánh `obsolete` khi requeue → `Complete` trả `ErrLeaseLost`, không ghi gì
- [x] AC tính lại theo version mới; AC cũ mất nếu bài không còn AC — 22/10: `scripts/smoke.sh rejudge` 10/10 (worker chấm thật): sửa `2.out` trên server + bấm chấm lại → `2/2 AC@v2` thành `1/2 WA@v3`; sửa lại → `2/2 AC@v4`; hạ time limit 100 ms → bài chậm `0/2 TLE@v5`, bài nhanh vẫn AC. Trong lúc chờ, kết quả hiện hành là run version mới (không hiện AC cũ)
- [x] Chấm lại thủ công 1 bài → chỉ bài đó được chấm lại — 22/10: `TestManualRejudgeOnlyThatSubmission` + smoke: run mới cùng version, run cũ `obsolete`, bài khác giữ nguyên `run_id`; chấm lại liên tiếp vẫn chỉ 1 run hiện hành; SV gọi → 403
- [x] Test đồng thời: chấm lại problem trong lúc worker claim job (không deadlock) — 22/10: `TestNoDeadlockRequeueVsWorkers`: 4 worker + 186 lần đổi limit/chấm lại thủ công trong 4 s → 0 deadlock, mỗi bài đúng 1 run hiện hành ở version hiện tại, không run không-obsolete ở version cũ

**Kiểm tra cuối phase**
```bash
make test-integration
scripts/smoke.sh rejudge
scripts/smoke.sh all   # 22/10: 111/111
```

---

## Phase 11 — WebSocket: kết quả chấm, bảng GV trực tiếp, thông báo

**Mục tiêu:** client nhận cập nhật theo thời gian thực; REST vẫn là nguồn chính.

**Phạm vi (Đã chốt 22/10/2026)** — tối thiểu + 3 tính năng:
- **Tối thiểu:** `GET /api/v1/ws`; kiểm tra `Origin`; message đầu `{"type":"auth","data":{"token":…}}` trong vài giây, sai thì đóng; ping/pong; `subscribe` theo phiên; `run.updated` qua PostgreSQL `NOTIFY/LISTEN`; nối lại + REST bù trạng thái.
- **(3) Bảng GV trực tiếp:** `summary.updated` đẩy `x/N AC` theo SV × problem, SV đang online (từ kết nối WS), độ dài hàng đợi; làm luôn REST tổng hợp của phase 12 (`/sessions/{id}/summary`, `/summary/me`) để có dữ liệu ban đầu.
- **(4) Thông báo của GV** tới cả phiên → SV nhận `announcement`.
- **(5) Chấm lại hiện ngay:** GV chấm lại (problem hoặc 1 bài) → mọi SV bị ảnh hưởng nhận `run.updated` "đang chờ" rồi kết quả mới (fan-out).
- **Không làm:** tiến độ từng test (`run.progress`), hẹn giờ phiên (`session.started/ended`).

**Công việc**
- `NOTIFY` trong cùng transaction ở mọi chỗ đổi trạng thái run (nhận bài, worker nhận, có kết quả, retry/lỗi hạ tầng, chấm lại); API giữ một kết nối `LISTEN`, nạp lại bài nộp rồi phát cho đúng người; mất kết nối `LISTEN` → nối lại và gửi `resync` cho client.
- Hub kết nối: SV nhận bài của mình; GV nhận bài + bảng của phiên mình sở hữu đang subscribe; client chậm bị ngắt thay vì làm nghẽn hub.
- Thông báo: `POST /sessions/{id}/announcements` (REST, GV) rồi phát qua WS; `GET` để client mới vào / nối lại xem lại.
- Giới hạn kích thước message, đóng khi token hết hạn (client gửi `auth` mới để gia hạn).

**Checklist**
- [x] Không gửi auth / token sai → đóng kết nối với mã lỗi — 22/10: `make test-integration` (`internal/realtime`, client WebSocket thật + PostgreSQL thật) `TestAuthenticationAndOrigin`: im lặng quá hạn / token sai / `subscribe` trước `auth` → đóng `4001`; token hết hạn → `4001`, gửi `auth` mới trước hạn thì giữ kết nối, token của người khác bị từ chối (`TestTokenExpiryAndRefresh`). **Sửa trong lúc test:** hết hạn auth trước đây đóng kết nối không có mã (do huỷ context đọc) → đổi sang hẹn giờ đóng `4001`
- [x] Origin lạ → từ chối — 22/10: test + smoke: `Origin: http://evil.example` → 403; cùng origin → 101
- [x] SV nhận `run.updated` của bài mình, không nhận bài SV khác — 22/10: `TestResultsBoardAndPrivacy` + smoke: SV1 nhận `queued@v2 → judging@v2 → completed@v2 (AC)`; SV2 nhận 0 `run.updated`; unit test định tuyến (`TestRunUpdatedRouting`)
- [x] GV subscribe phiên của mình nhận `run.updated` + `summary.updated`; phiên của GV khác → từ chối — 22/10: smoke: GV nhận 6 `run.updated` + bảng (1 AC, 2 online); GV khác subscribe → `error`
- [x] Bảng tổng hợp (REST + `summary.updated`) đúng `x/N AC`, SV online, độ dài hàng đợi — 22/10: `internal/board` `TestSummaryStates` (AC giữ nguyên khi bài sau WA, `system_error` = attempted, pending, none, hàng đợi, khớp SQL đếm độc lập); smoke REST: `online_count` 2 khi 2 SV mở WS
- [x] Thông báo GV tới đúng các client của phiên — 22/10: unit (SV phiên khác không nhận) + integration + smoke (2 SV nhận, `GET /announcements` trả lại khi nối lại)
- [x] Chấm lại → SV bị ảnh hưởng nhận "đang chờ" rồi kết quả mới — 22/10: `TestAnnouncementAndRejudgeFanOut` (3 SV nhận `queued@v2`, rồi WA@v2); smoke: đổi limit → `queued@v3 → judging@v3 → completed@v3`
- [x] Mất kết nối rồi nối lại → REST trả trạng thái đúng; mất `LISTEN` → `resync` — 22/10: `TestListenerReconnectSendsResync`: `pg_terminate_backend` kết nối LISTEN → client nhận `resync`, sự kiện chạy lại sau khi listener tự nối lại; REST (`/submissions/{id}`, `/summary`, `/announcements`) trả trạng thái hiện tại (smoke)
- [x] 50 kết nối đồng thời: mọi client nhận sự kiện, đo độ trễ phát tán — 22/10: `TestFiftyClientsFanOut` (máy dev, 1 API): thông báo tới 50 client p50 0,68 ms / p95 0,73 ms / max 0,75 ms; kết quả chấm commit → client (NOTIFY → LISTEN → nạp DB → đẩy) p50 2,6 ms / p95 3,4 ms / max 10,7 ms. Đo CPU/RAM với tải thật: phase 14

- [x] Swagger + `docs/api-v1.md` — 22/10: Swagger 42 thao tác (thêm summary, summary/me, announcements GET/POST); `/ws` mô tả trong `docs/api-v1.md` ("Chi tiết WebSocket")

**Kiểm tra cuối phase**
```bash
go test -race -count=1 ./internal/realtime/ ./internal/board/
make test-integration
scripts/smoke.sh ws     # 22/10: 11/11
scripts/smoke.sh all    # 22/10: 122/122
```

---

## Phase 12 — Tổng hợp AC, export CSV, thống kê hàng đợi

**Mục tiêu:** GV xem bảng "x/N AC"; SV xem của mình; xuất CSV; theo dõi hàng đợi.

**Công việc**
- `GET /sessions/{id}/summary` (T), `/summary/me` (S): mỗi SV × problem: `AC` / `chưa AC` / `đang chấm`; chỉ dùng run `problem_version = problems.version`; mẫu số = số problem của đề.
- `GET /sessions/{id}/export.csv` (T).
- `GET /admin/queue` (T): số job theo trạng thái, worker đang chạy, thời gian chờ trung bình.

**Checklist**
- [x] Bảng tổng hợp khớp truy vấn SQL độc lập trên dữ liệu mẫu — 22/10: làm cùng phase 11 (`TestSummaryStates`)
- [x] Lần nộp sai/lỗi hạ tầng sau không làm mất AC cùng version — 22/10: `TestSummaryStates` (AC rồi WA mới hơn → ô vẫn `ac`, `latest` là WA)
- [x] SV không xem được tổng hợp của SV khác — 22/10: `/summary/me` chỉ một dòng; chưa tham gia → 403; route GV → 403 (role)
- [ ] CSV mở được bằng Excel (UTF-8 BOM, dấu phẩy, tiếng Việt)
- [ ] `/admin/queue` số liệu khớp `SELECT status, count(*) FROM judge_jobs`

**Kiểm tra cuối phase**
```bash
scripts/smoke.sh summary
```

---

## Phase 13 — Chống gian lận

**Đã chốt 22/10/2026: không làm** thêm tính năng nào (câu 10). Những gì đã có nhờ thiết kế: danh tính lấy từ JWT, không tin ID client; thời gian theo server; không lộ test ẩn; SV không xem bài người khác. `BACKEND_CONTEXT.md` mục 13: theo dõi rời tab, phát hiện giống code vẫn ngoài phạm vi.

---

## Phase 14 — Gia cố, kiểm thử tải, quan sát

**Mục tiêu:** chạy ổn với ~50 SV trên laptop i3/12 GB; có số liệu thật cho báo cáo.

**Công việc**
- Log có cấu trúc (request id, user id, job id); thống kê thời gian chờ/chấm.
- Kịch bản tải: 50 SV đăng nhập, 30 bài nộp gần như đồng thời, 15 test/bài; so sánh 1 worker vs nhiều worker (đề xuất thực nghiệm mục 14).
- Sao lưu/khôi phục: `pg_dump` + `./data` cùng lúc.
- Dọn file mồ côi, thư mục chấm tạm.

**Checklist**
- [ ] Đo: thời gian chờ hàng đợi, thời gian chấm, thông lượng, CPU/RAM với 1/2/4/8 worker — số liệu thật, ghi vào báo cáo
- [ ] Không lỗi 5xx khi 50 client đồng thời
- [ ] Khởi động lại toàn stack giữa chừng → không mất bài đã nhận, job được chấm tiếp
- [ ] Sao lưu → xóa → khôi phục → dữ liệu và file khớp

---

## Phase 15 — Tài liệu, báo cáo, demo

**Mục tiêu:** đáp ứng `Instruction.md` (README, báo cáo dạng paper, Novelty, số liệu thật) và `Submission.md` (thư mục `NhomXX_TenProject_Source`, hạn 31/10/2026).

**Công việc**
- README dự án đủ mục 13 của `Instruction.md`: mô tả, kiến trúc, công nghệ, yêu cầu, cài đặt, cấu hình, DB, chạy server/client, test, kết quả, novelty, hạn chế, tham khảo.
- Sơ đồ kiến trúc, sơ đồ message flow (REST + WebSocket + queue), mô tả protocol/message format.
- Novelty: hàng đợi bền vững + lease + chấm lại theo version (so với chấm tuần tự) — kèm bằng chứng thực nghiệm phase 14.
- Kịch bản demo 10 phút: GV tạo đề/phiên, SV nộp AC/WA/TLE, GV sửa test → chấm lại, WebSocket cập nhật, worker chết → phục hồi.

**Checklist**
- [ ] Người ngoài làm theo README dựng được stack trên máy sạch
- [ ] Báo cáo có số liệu thật từ phase 14
- [ ] Demo chạy trọn kịch bản 2 lần liên tiếp không lỗi
- [ ] Thư mục nộp đúng tên, đủ source/config/schema/README

---

## Câu hỏi cần chốt

Từ `BACKEND_CONTEXT.md` mục 13 và các điểm phát sinh khi triển khai. Mỗi câu có **Đề xuất** của agent (22/10/2026). Người dùng trả lời cùng ngày; kết quả ghi ngay dưới từng câu và trong `BACKEND_CONTEXT.md` mục 19.

### Chặn phase 7–9

1. **Chọn verdict tổng** khi một bài có nhiều loại lỗi; có dừng sau test sai đầu tiên không?
   - **Đã chốt 22/10/2026 (người dùng sửa đề xuất): dừng ở test không AC đầu tiên.** `passed_tests` = số test đúng trước test đó, `total_tests` = tổng số test, verdict tổng = verdict của test đó → FE hiện `10/15 TLE` khi test 11 TLE. Test còn lại không chạy, không ghi dòng kết quả.
   - ~~Đề xuất ban đầu: chạy **hết** mọi test (để có `passed_tests/total_tests` cho phúc khảo); verdict tổng = verdict của **test sai đầu tiên theo thứ tự test** (thứ tự `test_cases.id`). CE xét ở cấp run trước khi chạy test. Lý do: dễ giải thích cho SV, đúng với "đối chiếu số test đúng/tổng test" ở mục 6.~~
2. **Cờ biên dịch** và giới hạn khi biên dịch.
   - **Đã chốt 22/10/2026: duyệt.**
   - Đề xuất: `g++ -std=c++17 -O2 -pipe -static -DONLINE_JUDGE`, timeout biên dịch **10 giây**. `-static` để chương trình không phụ thuộc thư viện động khi chạy trong môi trường hạn chế; `ONLINE_JUDGE` theo thói quen của các hệ chấm. Compile output trả SV được cắt gọn (vd. 8 KB).
3. **Sandbox**.
   - **Đã chốt 22/10/2026: dùng `setrlimit`** (không dùng isolate). Đã làm ở phase 9.
   - Đề xuất: không dùng nsjail/isolate (phải tải về). Chạy bài SV bằng `setrlimit` (CPU, AS, FSIZE, NPROC) + **uid riêng không đặc quyền** + `unshare -n` (namespace mạng rỗng, kể cả không tới được `postgres`); container worker cần `cap_add: [SYS_ADMIN]`. Tiến trình con bỏ mọi capability, bật `no_new_privs`, chạy trong thư mục tạm riêng của job. Rủi ro: `SYS_ADMIN` là quyền rộng cho tiến trình worker — chấp nhận vì chỉ chạy trong LAN, worker không ra Internet; kiểm chứng bằng bộ bài tấn công ở phase 9.
4. **Kiểm tra file nộp**.
   - **Đã chốt 22/10/2026: duyệt.**
   - Đề xuất: chỉ nhận tên file đuôi **`.cpp`**, **≤ 64 KB**, không rỗng; **không** bắt buộc UTF-8 (tránh từ chối bài có chú thích tiếng Việt mã hóa khác). Server tự đặt tên khi lưu, không giữ tên gốc (mục 7).
5. **Retry, backoff, lease, số worker**.
   - **Đã chốt 22/10/2026: duyệt.**
   - Đề xuất: tối đa **3** lần thử cho lỗi hạ tầng, backoff **5 s / 30 s / 2 phút**; lease **60 s**, worker gia hạn (heartbeat) định kỳ khi đang chấm (vd. mỗi 20 s). `WORKER_CONCURRENCY` mặc định 2, chỉnh theo số đo phase 14.

### Các phase sau

6. **Chấm lại thủ công một bài** (`UNIQUE(submission_id, problem_version)` chặn tạo run mới cùng version).
   - **Đã chốt 22/10/2026: duyệt** (gồm đổi UNIQUE thành unique index một phần — làm ở phase 10).
   - Đề xuất: tạo **run mới**, run cũ chuyển `obsolete` (giữ lịch sử). Cách làm: thay ràng buộc `UNIQUE (submission_id, problem_version)` bằng **unique index một phần** `ON judge_runs (submission_id, problem_version) WHERE status <> 'obsolete'` — vẫn bảo đảm tối đa một run hiện hành mỗi (bài, version). Sửa thẳng migration `00001` (chưa có dữ liệu thật) và mục 9 `BACKEND_CONTEXT.md` ("một run cho mỗi (submission, version)" → "một run **không obsolete**"). Run đang `judging` bị thay thì worker cũ mất quyền ghi theo kiểm tra lease/run hiện hành.
7. **Nguồn sự kiện WebSocket**.
   - **Đã chốt 22/10/2026: duyệt.**
   - Đề xuất: PostgreSQL **`LISTEN/NOTIFY`** (worker `NOTIFY run_updated, '<run_id>'` trong transaction ghi kết quả; API giữ một kết nối `LISTEN`, đọc lại run rồi phát), kèm **polling dự phòng** (vd. 5 s) khi mất kết nối LISTEN. Không cần thêm dịch vụ (Redis…).
8. **Rate limit**.
   - **Đã chốt 22/10/2026: không làm rate limit**, giữ đơn giản, tập trung vào nộp và chấm bài.
   - ~~Đề xuất: nộp bài: **cooldown 10 s** mỗi SV (không giới hạn tổng số lần nộp — mục 6); đăng nhập: tối đa **10 lần sai / 5 phút** theo `code` + IP, quá thì 429. Lưu bộ đếm trong bộ nhớ API (một instance).~~
9. ~~Swagger UI + cổng Postgres `127.0.0.1:5433`~~ — **Đã chốt 22/10/2026**: commit riêng (`88b294e`), login trong spec đã bỏ `role`; giữ đồng bộ theo quy tắc phase 0.
10. **Chống gian lận** (phase 13).
    - **Đã chốt 22/10/2026: không làm thêm** tính năng chống gian lận nào ngoài những gì đã có.
    - ~~Đề xuất: chỉ làm phần `BACKEND_CONTEXT.md` cho phép mà không đổi phạm vi: (a) **một phiên đăng nhập SV hoạt động** — login mới xóa refresh token cũ của SV; (b) sau kỳ thi xuất source để GV tự so sánh bằng công cụ ngoài. Hai mục **theo dõi rời tab** và **báo cáo giống code** đang ghi "ngoài phạm vi bản đầu" ở mục 13 — chỉ làm nếu người dùng đổi quyết định đó (cần thêm bảng sự kiện + API FE gửi sự kiện, và một bước so sánh source).~~
11. **Lớp học** (CSV có cột `class`, bảng `users` không có).
    - **Đã chốt 22/10/2026: bỏ qua** — làm trên LAN, chỉ một lớp ngồi trong phòng làm bài cùng lúc; cột `class` trong CSV tiếp tục bị bỏ qua.
    - ~~Đề xuất: thêm cột **`class_name varchar(50) NULL`** vào `users` (sửa migration `00001`), import CSV lưu cột `class`, `GET /students` cho lọc theo lớp. Không tạo bảng lớp riêng (không có nghiệp vụ gắn lớp với đề/phiên).~~
12. **Các lựa chọn agent tự đặt** ở `BACKEND_CONTEXT.md` mục 17 (ghi chú triển khai) và 18: SV phải join mới xem đề; GV chỉ thấy đề của mình; xóa đề khi còn con bị chặn; giới hạn nhập time/memory; mã trim + viết hoa.
    - **Đã chốt 22/10/2026: giữ nguyên.**
    - Đề xuất: **giữ nguyên** — đã có test và smoke test bao phủ.
13. **Bàn giao** (`Instruction.md` ghi Google Drive, `Submission.md` ghi USB qua lớp trưởng).
    - **Đã chốt 22/10/2026: duyệt.**
    - Đề xuất: làm theo **`Submission.md`** (văn bản cụ thể hơn, có hạn 31/10/2026): thư mục `NhomXX_TenProject/{Source,Report}`, báo cáo `.docx` qua Compilatio, source cho lớp trưởng chép USB; đồng thời giữ bản trên Google Drive phòng khi giảng viên yêu cầu theo `Instruction.md`.
14. **sqlc** (Makefile + `sqlc.yaml` có, nhưng chưa dùng vì không tải được).
    - **Đã chốt 22/10/2026: duyệt, đã làm** — xóa `sqlc.yaml`, `queries/`, target `make sqlc`.
    - Đề xuất: giữ repository pgx viết tay (đã có test + smoke), **bỏ** `sqlc.yaml`, `queries/` và target `make sqlc` để tài liệu khớp thực tế; nếu sau này cài được sqlc thì chuyển dần từng feature.
