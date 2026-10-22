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
| 0 | Repo, công cụ, quy ước | ✅ xong (trừ sqlc) |
| 1 | Docker Compose, cấu hình, biến môi trường | ✅ xong |
| 2 | Schema DB, migration, gộp bảng `users` | ✅ xong |
| 3 | Auth (JWT, refresh token) | ✅ xong |
| 4 | Quản lý sinh viên | kiểm chứng |
| 5 | Đề, phiên, tham gia | kiểm chứng |
| 6 | Problem, test trên đĩa, chấm lại (quét + tăng version) | kiểm chứng |
| 7 | Nộp bài | chưa làm |
| 8 | Hàng đợi chấm bền vững | chưa làm |
| 9 | Sandbox chấm C++17 | chưa làm |
| 10 | Chấm lại khi đổi version + chấm lại thủ công | chưa làm |
| 11 | WebSocket trạng thái chấm | chưa làm |
| 12 | Tổng hợp AC, export CSV, thống kê hàng đợi | chưa làm |
| 13 | Chống gian lận trong phạm vi cho phép | chờ quyết định |
| 14 | Gia cố, kiểm thử tải ~50 SV, quan sát | chưa làm |
| 15 | Tài liệu, báo cáo, kịch bản demo | chưa làm |

---

## Phase 0 — Repo, công cụ, quy ước

**Mục tiêu:** repo build/test được bằng một lệnh; bố cục và quy ước code rõ ràng để các phase sau làm theo.

**Công việc**
- Module Go `github.com/hoangdaulan/lan-lab-backend`, Go 1.26; thư viện đã chốt (mục 16): chi v5, pgx v5, goose v3, coder/websocket, golang-jwt v5, x/crypto (bcrypt).
- Bố cục package-by-feature: `cmd/{api,worker,migrate,seed}`, `internal/{config,db,httpx,apperr,auth,student,exam,problem,submission,judge}`; mỗi feature `handler.go → service.go → repository.go`, interface `Repository` khai báo trong `service.go`.
- Makefile: `up`, `down`, `migrate`, `seed`, `sqlc`, `build`, `test`.
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
- [ ] Truy vấn DB qua sqlc (`make sqlc`) — **chưa**: không tải được sqlc trong môi trường agent; repository viết tay bằng pgx. Cần người dùng `go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest` nếu muốn chuyển
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
- [ ] Unit test pass
- [ ] Tạo SV: mã trim + viết hoa, tên gộp khoảng trắng; 201
- [ ] Trùng mã SV hoặc mã GV → 409
- [ ] Import CSV lỗi → 400, `row_errors` đúng dòng, không lưu gì
- [ ] Import có mã trùng → bỏ qua đúng dòng + lý do; phần còn lại lưu
- [ ] List chỉ có SV (không lẫn GV), tìm theo mã/tên, phân trang
- [ ] SV đăng nhập bằng MSV; SV gọi `/students` → 403
- [ ] Reset → mật khẩu về MSV, refresh token cũ 401; id của GV → 404

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
- [ ] Unit test pass
- [ ] Tạo phiên `ends_at <= starts_at` → 400
- [ ] GV khác không thấy/sửa được đề → 404
- [ ] SV thấy mọi phiên; trước giờ bắt đầu `problems_hidden = true`
- [ ] Join trước giờ / sau giờ → 409; trong giờ → 200; join lại trả bản ghi cũ
- [ ] Finish idempotent, `finished_at` không đổi; chưa join → 409
- [ ] Xóa đề còn con → 409; xóa phiên có bài nộp → 409
- [ ] `participants` hiện mã, tên, trạng thái

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
- [ ] Unit test pass (gồm quét thư mục, symlink)
- [ ] Tạo problem tạo thư mục test trên host, đúng chủ sở hữu
- [ ] Trùng mã trong đề → 409; giới hạn ngoài 1–10000 ms / 1–1024 MB → 400
- [ ] Chấm lại khi chưa có test / thiếu cặp → 409, version không đổi
- [ ] Chấm lại hợp lệ → version + 1, test theo thứ tự số
- [ ] Sửa chữ không tăng version; đổi limit tăng version
- [ ] SV xem đề: chưa join 403, trước giờ 403, problem khác đề 404, sau khi join 200
- [ ] Xóa problem chưa có bài nộp → 204, file test giữ nguyên
- [ ] Phần tạo lại run/job khi tăng version: chuyển sang phase 10 (hiện là stub)

**Kiểm tra cuối phase**
```bash
go test -race -count=1 ./internal/problem/
scripts/smoke.sh problems
```

---

## Phase 7 — Nộp bài

**Mục tiêu:** SV nộp một file `.cpp` ≤ 64 KB trong phiên; server ghi nhận bền vững rồi mới báo thành công.

**Công việc**
- `POST /sessions/{id}/problems/{pid}/submissions` (multipart `file`): ghi `received_at` ngay khi nhận đủ file; lưu file vào `DATA_DIR/submissions/<yyyy>/<mm>/<uuid>.cpp` (tên server tạo), `fsync`.
- Transaction: khóa problem (SHARE) → session → participant `FOR UPDATE`; kiểm tra SV từ JWT là chủ participant, `status = in_progress`, problem thuộc đề của phiên, `starts_at <= received_at < ends_at`, bộ test không rỗng; tạo submission + run (version hiện hành) + job; COMMIT rồi mới trả 202.
- DB lỗi: không báo nhận; file mồ côi dọn sau (job dọn định kỳ, có khoảng chờ).
- `GET /sessions/{id}/submissions` (S: của mình; T: lọc), `GET /submissions/{id}`, `GET /submissions/{id}/source`, `GET /submissions/{id}/tests` (T).
- Chặn: file > 64 KB, rỗng, không UTF-8 hợp lệ (cần chốt), sai đuôi.

**Checklist**
- [ ] Nộp hợp lệ → 202 + id; có đủ submission + run `queued` + job `queued` trong DB
- [ ] Nộp sau `ends_at` (theo `received_at`) → 409; trước `starts_at` → 409
- [ ] Đã finish → 409; chưa join → 409/403
- [ ] File 64 KB + 1 byte → 413/400; file rỗng → 400
- [ ] Nộp problem của đề khác → 404
- [ ] Problem chưa có test → 409
- [ ] Nộp và finish đồng thời (2 request song song): kết quả nhất quán với thứ tự khóa (test tích hợp)
- [ ] DB lỗi giả lập → không trả 202, file được đánh dấu mồ côi
- [ ] SV không xem được bài/mã nguồn của SV khác (404)
- [ ] Unit test service với fake repo + fake storage

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
- Retry lỗi hạ tầng với backoff + số lần tối đa (cần chốt); hết lần → run `system_error`, job `failed`.
- Không giữ transaction trong lúc biên dịch/chạy.
- `WORKER_CONCURRENCY` giới hạn số bài chấm đồng thời mỗi worker.

**Checklist**
- [ ] Test tích hợp với PostgreSQL thật: 2 worker × 20 job → mỗi job chấm đúng một lần
- [ ] Giết worker giữa chừng → sau lease hết hạn job được chấm lại, không trùng kết quả
- [ ] Job của version cũ hoàn tất muộn → bị loại, không ghi đè
- [ ] Lỗi hạ tầng lặp lại → `system_error` sau N lần, không thành WA/RE
- [ ] Thứ tự khóa đúng mục 10 (rà soát code + test deadlock song song với chấm lại)
- [ ] Log có `worker_id`, `job_id`, thời gian chờ hàng đợi

**Kiểm tra cuối phase**
```bash
go test -race -count=1 -tags=integration ./internal/judge/
```

---

## Phase 9 — Sandbox chấm C++17

**Mục tiêu:** biên dịch và chạy bài SV an toàn; verdict đúng.

**Công việc**
- Snapshot: chép source + test + giới hạn + version vào thư mục tạm riêng của job (giữ khóa SHARE problem khi chép).
- Biên dịch: `g++ -std=c++17 -O2 -pipe` (cờ cần chốt), giới hạn thời gian/bộ nhớ/output biên dịch; ghi `compiler_info` thực tế; compile output cắt gọn.
- Chạy từng test: user không đặc quyền, không mạng (network namespace riêng hoặc tương đương), `setrlimit` CPU/AS/NPROC/FSIZE/NOFILE, wall-clock timeout, giới hạn stdout; đo thời gian + bộ nhớ.
- So sánh theo token (bỏ qua khoảng trắng/xuống dòng).
- Verdict test: AC/WA/TLE/MLE/RE; verdict run: CE ở cấp run; AC khi tất cả test AC; quy tắc chọn verdict tổng khi nhiều loại lỗi **cần chốt**.
- Ghi kết quả từng test + tổng hợp run + job done trong một transaction (kiểm tra lại version/lease dưới khóa).

**Checklist**
- [ ] Bộ bài mẫu: AC, WA, TLE (vòng lặp vô hạn), MLE (cấp phát lớn), RE (chia 0 / segfault), CE (lỗi cú pháp) → đúng verdict
- [ ] Chương trình mở socket → thất bại (không mạng)
- [ ] Fork bomb bị chặn (NPROC), output 1 GB bị cắt (FSIZE/giới hạn đọc)
- [ ] Đọc file ngoài thư mục chấm (vd. `/data/problems/...`) → không được
- [ ] `compiler_info` ghi `g++ (Debian 12.2.0-…) 12.2.0`
- [ ] SV không thấy input/output test ẩn, không thấy `system_error`
- [ ] Bài không có test không được AC

**Kiểm tra cuối phase**
```bash
go test -race -count=1 ./internal/judge/...
scripts/smoke.sh judge   # nộp bộ bài mẫu, chờ verdict
```

---

## Phase 10 — Chấm lại khi đổi version + chấm lại thủ công

**Mục tiêu:** thay test hoặc đổi limit thì kết quả hiện hành luôn theo version mới.

**Công việc**
- Thay stub `requeueProblem`: trong transaction tăng version, run/job cũ → `obsolete` (xóa verdict, xóa lease), tạo run + job mới tại version mới cho **mọi** bài nộp của problem (mọi phiên).
- `POST /submissions/{id}/rejudge` (T): tạo lại lượt chấm cho một bài tại version hiện hành (cần chốt: dùng lại run hiện hành hay obsolete + tạo mới, vì `UNIQUE(submission_id, problem_version)`).
- FE/REST hiển thị "đang chấm lại", không coi kết quả cũ là hiện hành.

**Checklist**
- [ ] Chấm lại problem có N bài → N run mới version mới, run cũ `obsolete`
- [ ] Worker đang chấm version cũ → kết quả bị loại
- [ ] AC tính lại theo version mới; AC cũ mất nếu bài không còn AC
- [ ] Chấm lại thủ công 1 bài → chỉ bài đó được chấm lại
- [ ] Test đồng thời: chấm lại problem trong lúc worker claim job (không deadlock)

**Kiểm tra cuối phase**
```bash
go test -race -count=1 -tags=integration ./internal/problem/ ./internal/judge/
scripts/smoke.sh rejudge
```

---

## Phase 11 — WebSocket trạng thái chấm

**Mục tiêu:** client nhận cập nhật trạng thái chấm theo thời gian thực; REST vẫn là nguồn chính.

**Công việc**
- `GET /api/v1/ws`: kiểm tra `Origin`; message đầu tiên `{"type":"auth","data":{"token":"<access JWT>"}}` trong N giây, sai thì đóng.
- Server push `{"type":"run.updated","data":{submission_id, run_id, status, verdict, passed_tests, total_tests}}`: SV chỉ nhận bài của mình, GV nhận theo phiên đang xem (`{"type":"subscribe","data":{"session_id":…}}`).
- Nguồn sự kiện: worker ghi DB rồi `NOTIFY`; API `LISTEN` và phát cho client (cần chốt so với polling).
- Ping/pong, giới hạn kích thước message, đóng khi token hết hạn.

**Checklist**
- [ ] Không gửi auth / token sai → đóng kết nối với mã lỗi
- [ ] Origin lạ → từ chối
- [ ] SV nhận `run.updated` của bài mình, không nhận bài SV khác
- [ ] Mất kết nối rồi nối lại → REST trả trạng thái đúng
- [ ] 50 kết nối đồng thời ổn định (đo bộ nhớ/CPU)

**Kiểm tra cuối phase**
```bash
go test -race -count=1 ./internal/realtime/
scripts/smoke.sh ws
```

---

## Phase 12 — Tổng hợp AC, export CSV, thống kê hàng đợi

**Mục tiêu:** GV xem bảng "x/N AC"; SV xem của mình; xuất CSV; theo dõi hàng đợi.

**Công việc**
- `GET /sessions/{id}/summary` (T), `/summary/me` (S): mỗi SV × problem: `AC` / `chưa AC` / `đang chấm`; chỉ dùng run `problem_version = problems.version`; mẫu số = số problem của đề.
- `GET /sessions/{id}/export.csv` (T).
- `GET /admin/queue` (T): số job theo trạng thái, worker đang chạy, thời gian chờ trung bình.

**Checklist**
- [ ] Bảng tổng hợp khớp truy vấn SQL độc lập trên dữ liệu mẫu
- [ ] Lần nộp sai/lỗi hạ tầng sau không làm mất AC cùng version
- [ ] SV không xem được tổng hợp của SV khác
- [ ] CSV mở được bằng Excel (UTF-8 BOM, dấu phẩy, tiếng Việt)
- [ ] `/admin/queue` số liệu khớp `SELECT status, count(*) FROM judge_jobs`

**Kiểm tra cuối phase**
```bash
scripts/smoke.sh summary
```

---

## Phase 13 — Chống gian lận trong phạm vi cho phép

**Mục tiêu:** chỉ làm những gì `BACKEND_CONTEXT.md` cho phép. Mục 13 ghi **ngoài phạm vi bản đầu**: phát hiện giống code, theo dõi rời tab; mục 3: "LAN không phải cơ chế chống gian lận".

**Đã có sẵn nhờ thiết kế**
- Danh tính lấy từ JWT, không tin ID client; thời gian theo server; không lộ test ẩn; SV không xem bài người khác.

**Đề xuất (chờ quyết định, không tự làm)**
- [ ] Ghi log IP/User-Agent theo lần nộp (cần cột mới hoặc log file)
- [ ] Giới hạn một phiên đăng nhập SV tại một thời điểm
- [ ] Danh sách IP/subnet được phép nộp bài
- [ ] Xuất toàn bộ source để GV tự so sánh bằng công cụ ngoài (zip export — đã để sau v1)

---

## Phase 14 — Gia cố, kiểm thử tải, quan sát

**Mục tiêu:** chạy ổn với ~50 SV trên laptop i3/12 GB; có số liệu thật cho báo cáo.

**Công việc**
- Rate limit login (chống dò mật khẩu) và nộp bài (cần chốt ngưỡng).
- Log có cấu trúc (request id, user id, job id); thống kê thời gian chờ/chấm.
- Kịch bản tải: 50 SV đăng nhập, 30 bài nộp gần như đồng thời, 15 test/bài; so sánh 1 worker vs nhiều worker (đề xuất thực nghiệm mục 14).
- Sao lưu/khôi phục: `pg_dump` + `./data` cùng lúc.
- Dọn file mồ côi, thư mục chấm tạm.

**Checklist**
- [ ] Đo: thời gian chờ hàng đợi, thời gian chấm, thông lượng, CPU/RAM với 1/2/4/8 worker — số liệu thật, ghi vào báo cáo
- [ ] Không lỗi 5xx khi 50 client đồng thời
- [ ] Khởi động lại toàn stack giữa chừng → không mất bài đã nhận, job được chấm tiếp
- [ ] Sao lưu → xóa → khôi phục → dữ liệu và file khớp
- [ ] Rate limit login hoạt động

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

Từ `BACKEND_CONTEXT.md` mục 13 và các điểm phát sinh khi triển khai:

1. **Chọn verdict tổng** khi một bài có nhiều loại lỗi (ví dụ test 2 WA, test 5 TLE): lấy lỗi đầu tiên theo thứ tự test, hay theo độ ưu tiên (CE > RE > TLE > MLE > WA)? Có dừng sau test sai đầu tiên không (ảnh hưởng `passed_tests`)?
2. **Cờ biên dịch** chính thức (`-O2`? `-static`? `-DONLINE_JUDGE`?) và giới hạn thời gian/bộ nhớ khi biên dịch.
3. **Sandbox**: dùng `setrlimit` + network namespace (`unshare`) trong container worker (cần capability), hay công cụ có sẵn (nsjail/isolate — cần tải về, có thể bị chặn)?
4. **Số worker, `WORKER_CONCURRENCY`, thời hạn lease, số lần retry, backoff** — chưa có số đo.
5. **Chấm lại thủ công một bài**: dùng lại run hiện hành (reset) hay obsolete + run mới (ràng buộc `UNIQUE(submission_id, problem_version)` chặn run mới cùng version)?
6. **Nguồn sự kiện WebSocket**: PostgreSQL `LISTEN/NOTIFY` hay API polling DB định kỳ?
7. **Kiểm tra mã nguồn khi nộp**: bắt buộc UTF-8? Chặn ký tự NUL? Chấp nhận đuôi khác `.cpp`?
8. **Rate limit**: ngưỡng login sai và số lần nộp mỗi phút (doc: không giới hạn tổng số lần nộp).
9. ~~Swagger UI + cổng Postgres `127.0.0.1:5433`~~ — đã chốt 22/10/2026: commit riêng (`88b294e`), login trong spec đã bỏ `role`; giữ đồng bộ theo quy tắc phase 0.
10. **Chống gian lận** (phase 13): làm mục nào trong danh sách đề xuất, hay giữ nguyên phạm vi "ngoài bản đầu"?
11. **Giữ hay bỏ lớp học**: CSV có cột `class` nhưng bảng `users` không có — có cần thêm cột/bảng lớp?
12. **Xác nhận các lựa chọn của agent** ở `BACKEND_CONTEXT.md` mục 17 (ghi chú triển khai) và 18: SV phải join mới xem đề; GV chỉ thấy đề của mình; xóa đề khi còn con bị chặn; giới hạn nhập time/memory.
13. **Bàn giao**: Google Drive hay USB (doc ghi cả hai, cần hỏi giảng viên).
