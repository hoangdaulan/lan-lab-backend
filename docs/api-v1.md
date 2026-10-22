# API v1

## Test bằng Swagger UI

- Mở `/swagger/index.html` trên cùng host/port với API, ví dụ `http://localhost:8080/swagger/index.html`.
- UI, JavaScript và CSS được phục vụ từ ứng dụng; không cần CDN. Đặc tả: `/swagger/doc.json`.
- `POST /api/v1/auth/login` → copy `access_token` → **Authorize** → nhập `Bearer <access_token>`.
- Khi chuyển vai trò giáo viên/sinh viên, thay token trong Authorize. Refresh token nhập riêng trong body của refresh/logout.
- Import CSV: `POST /api/v1/students/import` → **Try it out** → chọn file ở trường `file`. Mẫu ở `examples/students-import.csv`.
- Chỉnh ngày giờ trong request tạo phiên cho phù hợp lúc test; ID lấy từ response thực tế.
- Swagger mô tả 38 thao tác hiện có. Worker chấm thật (g++ C++17 trong sandbox); chấm lại problem/bài nộp đã hoạt động; WebSocket chưa có.
- Đặc tả được duy trì trong `docs/swagger.json` và `docs/swagger.yaml`, nhúng vào binary bằng `docs/docs.go`. Không chạy `swag init` ghi đè khi handler chưa có annotation đầy đủ.
- Khi sửa code/spec trong môi trường Compose, build lại API: `docker compose up -d --build --no-deps api`.

Ngày chốt: **21/10/2026**. Quyết định nền: `BACKEND_CONTEXT.md` mục 17.

- Prefix: `/api/v1` (trừ `/healthz`).
- Vai trò: **T** = giáo viên, **S** = sinh viên, **—** = công khai.
- Xác thực: `Authorization: Bearer <access JWT>` cho mọi route trừ login/refresh/healthz.
- Trạng thái cột "Làm": ✅ đã triển khai, trống = chưa.

## 1. Auth

| Method | Path | Vai trò | Mục đích | Làm |
|---|---|---|---|---|
| POST | `/auth/login` | — | `{code, password}`; vai trò lấy từ DB → access + refresh token | ✅ |
| POST | `/auth/refresh` | — | `{refresh_token}` → cặp token mới; token cũ chỉ dùng được 1 lần | ✅ |
| POST | `/auth/logout` | — | `{refresh_token}` → xóa refresh token đó | ✅ |
| GET | `/me` | T/S | Thông tin tài khoản hiện tại | ✅ |
| PUT | `/me/password` | T/S | Đổi mật khẩu; xóa mọi refresh token của tài khoản | ✅ |

Giáo viên đầu tiên: `cmd/seed` (không qua API).

## 2. Sinh viên

| Method | Path | Vai trò | Mục đích | Làm |
|---|---|---|---|---|
| GET | `/students` | T | Danh sách sinh viên, tìm theo code/tên, phân trang | ✅ |
| POST | `/students` | T | Tạo 1 sinh viên, mật khẩu mặc định = MSV | ✅ |
| POST | `/students/import` | T | Import CSV (`code,full_name[,class]`) | ✅ |
| POST | `/students/{id}/reset-password` | T | Đặt lại mật khẩu về MSV, thu hồi refresh token | ✅ |

## 3. Đề và phiên

| Method | Path | Vai trò | Mục đích | Làm |
|---|---|---|---|---|
| GET | `/exams` | T | Đề của giáo viên | ✅ |
| POST | `/exams` | T | Tạo đề | ✅ |
| GET | `/exams/{id}` | T | Chi tiết đề | ✅ |
| PUT | `/exams/{id}` | T | Sửa đề | ✅ |
| DELETE | `/exams/{id}` | T | Xóa đề (chặn nếu còn problem hoặc phiên) | ✅ |
| GET | `/exams/{id}/sessions` | T | Các phiên của đề | ✅ |
| POST | `/exams/{id}/sessions` | T | Tạo phiên (`starts_at`, `ends_at` bắt buộc) | ✅ |
| GET | `/sessions/{id}` | T/S | Thông tin phiên, giờ server, trạng thái của mình (S), danh sách problem | ✅ |
| PUT | `/sessions/{id}` | T | Sửa phiên | ✅ |
| DELETE | `/sessions/{id}` | T | Xóa phiên (chặn nếu đã có bài nộp) | ✅ |
| GET | `/sessions/{id}/participants` | T | Người tham gia và trạng thái | ✅ |
| GET | `/me/sessions` | S | Mọi phiên + trạng thái tham gia của mình | ✅ |
| POST | `/sessions/{id}/join` | S | Tham gia (chỉ trong khung giờ; vào lại dùng bản ghi cũ) | ✅ |
| POST | `/sessions/{id}/finish` | S | Kết thúc làm bài (idempotent) | ✅ |

## 4. Problem và test

| Method | Path | Vai trò | Mục đích | Làm |
|---|---|---|---|---|
| GET | `/exams/{id}/problems` | T | Problem của đề | ✅ |
| POST | `/exams/{id}/problems` | T | Tạo problem; tạo thư mục test trên đĩa | ✅ |
| GET | `/problems/{id}` | T | Chi tiết problem | ✅ |
| PUT | `/problems/{id}` | T | Sửa; đổi time/memory limit → tăng version + chấm lại | ✅ |
| DELETE | `/problems/{id}` | T | Xóa (chặn nếu đã có bài nộp) | ✅ |
| GET | `/problems/{id}/tests` | T | Danh sách test hiện có trong DB (đường dẫn tương đối) | ✅ |
| POST | `/problems/{id}/rejudge` | T | Quét thư mục test, đồng bộ `test_cases`, tăng version, chấm lại mọi bài nộp của problem ở mọi phiên | ✅ |
| GET | `/sessions/{sid}/problems/{pid}` | S | Đề bài; chỉ từ `starts_at` và sau khi đã tham gia | ✅ |

## 5. Nộp bài

| Method | Path | Vai trò | Mục đích | Làm |
|---|---|---|---|---|
| POST | `/sessions/{sid}/problems/{pid}/submissions` | S | Upload 1 file `.cpp` ≤ 64 KB; trả 202 + id | ✅ |
| GET | `/sessions/{sid}/submissions` | T/S | S: bài của mình; T: mọi bài, lọc `student_id`, `problem_id` | ✅ |
| GET | `/submissions/{id}` | T/S | Trạng thái run hiện hành, verdict, passed/total, compile output đã cắt | ✅ |
| GET | `/submissions/{id}/source` | T/S | Tải mã nguồn (S chỉ bài của mình) | ✅ |
| GET | `/submissions/{id}/tests` | T | Kết quả từng test (phúc khảo) | ✅ |
| POST | `/submissions/{id}/rejudge` | T | Chấm lại thủ công 1 bài tại version hiện hành | ✅ |

## 6. Trạng thái chấm, WebSocket, hàng đợi

| Method | Path | Vai trò | Mục đích | Làm |
|---|---|---|---|---|
| GET | `/ws` | T/S | WebSocket; message đầu tiên `{"type":"auth","data":{"token":"..."}}`; server push `run.updated` | |
| GET | `/admin/queue` | T | Số job queued/running/failed, worker đang chạy | |

Message WebSocket: JSON `{"type": string, "data": object}`. Server kiểm tra Origin.

## 7. Tổng hợp AC

| Method | Path | Vai trò | Mục đích | Làm |
|---|---|---|---|---|
| GET | `/sessions/{sid}/summary` | T | Mỗi SV × problem: AC / chưa / đang chấm; tổng `x/N AC` | |
| GET | `/sessions/{sid}/summary/me` | S | Tổng hợp của chính mình | |

Chỉ tính run có `problem_version = problems.version`. Không có điểm số.

## 8. Export

| Method | Path | Vai trò | Mục đích | Làm |
|---|---|---|---|---|
| GET | `/sessions/{sid}/export.csv` | T | Xuất bảng tổng hợp AC | |

Export zip source: để sau v1.

## 9. Hệ thống

| Method | Path | Vai trò | Mục đích | Làm |
|---|---|---|---|---|
| GET | `/healthz` | — | Ping DB | ✅ |

## Chi tiết Auth

### Lỗi

Mọi lỗi trả `{"error": "<message>"}`.

| HTTP | Khi nào |
|---|---|
| 400 | Body sai JSON, thiếu trường, mật khẩu mới không hợp lệ |
| 401 | Sai `code`/mật khẩu, access token thiếu/hết hạn/sai, refresh token không tồn tại/hết hạn/đã dùng |
| 403 | Đúng token nhưng sai vai trò cho route |

### `POST /auth/login`

```json
// request
{"code": "B21DCCN001", "password": "B21DCCN001"}
// 200
{
  "access_token": "<jwt>",
  "access_expires_at": "2026-10-21T09:15:00Z",
  "refresh_token": "<random base64url>",
  "refresh_expires_at": "2026-10-28T09:00:00Z",
  "user": {"id": 1, "role": "student", "code": "B21DCCN001", "fullname": "..."}
}
```

`code` được trim và viết hoa trước khi tra cứu (`b21dccn001` = `B21DCCN001`).

### `POST /auth/refresh`

Request `{"refresh_token": "..."}`; response giống login. Token cũ bị xóa trong cùng transaction.

### `POST /auth/logout`

Request `{"refresh_token": "..."}`; 204. Token không tồn tại vẫn trả 204.

### `GET /me`

200 `{"id": 1, "role": "teacher", "code": "GV01", "fullname": "..."}`.

### `PUT /me/password`

Request `{"current_password": "...", "new_password": "..."}`; 204. Mật khẩu mới 1–72 byte. Xóa mọi refresh token của tài khoản; access JWT đã cấp còn hiệu lực đến hết hạn.

### JWT

HS256, ký bằng `JWT_SECRET`. Claims: `sub` (id dạng chuỗi), `role`, `iat`, `exp` (15 phút).

## Chi tiết Sinh viên

Mọi route yêu cầu access token của **giáo viên** (403 nếu là sinh viên). `code` được trim + viết hoa; `fullname` được gộp khoảng trắng. Giới hạn: `code` 1–50 ký tự, không chứa khoảng trắng; `fullname` 1–200 ký tự. Mật khẩu ban đầu = `code`.

Đối tượng sinh viên:

```json
{"id": 2, "code": "B21DCCN001", "fullname": "Nguyen Van A",
 "created_at": "2026-10-21T09:00:00Z", "updated_at": "2026-10-21T09:00:00Z"}
```

### `POST /students`

Request `{"code": "b21dccn001", "fullname": "Nguyen Van A"}` → 201 + đối tượng sinh viên. 400 nếu không hợp lệ, 409 nếu `code` đã thuộc một tài khoản khác (kể cả giáo viên).

### `GET /students?q=&limit=50&offset=0`

`q` tìm chuỗi con (không phân biệt hoa thường) trong `code` hoặc `fullname`. `limit` mặc định 50, tối đa 200. Sắp theo `code`.

```json
{"items": [ ... ], "total": 3, "limit": 50, "offset": 0}
```

### `POST /students/import`

`multipart/form-data`, trường `file` là CSV UTF-8 (cho phép BOM của Excel), tối đa 1 MiB / 1000 dòng.

- Dòng đầu là header, phải có cột `code` và `full_name` (hoặc `fullname`); thứ tự cột tùy ý. Cột khác (ví dụ `class`) bị bỏ qua vì bảng `students` không có cột lớp.
- **Dòng lỗi** (thiếu/quá dài, `code` lặp lại trong cùng file): từ chối cả file, **không lưu gì**, trả 400:

```json
{"error": "import rejected, nothing was saved: 2 invalid row(s)",
 "row_errors": [{"line": 3, "code": "SV02", "error": "fullname is required"},
                {"line": 4, "code": "SV01", "error": "duplicate of line 2 in this file"}]}
```

- **`code` đã có trong DB**: bỏ qua dòng đó và báo lại (import lại cùng danh sách là an toàn). Các dòng còn lại được thêm trong một transaction.

```json
{"created_count": 2, "skipped_count": 2,
 "created": [ ...đối tượng sinh viên... ],
 "skipped": [{"line": 3, "code": "SV02", "reason": "already exists"},
             {"line": 4, "code": "GV01", "reason": "code belongs to a teacher account"}]}
```

### `POST /students/{id}/reset-password`

204. Mật khẩu về lại `code`; xóa mọi refresh token của sinh viên. Access token đã cấp còn hiệu lực tới hết hạn (≤ 15 phút). 404 nếu không có sinh viên.

## Chi tiết Đề, phiên và problem

### Quy tắc chung

- Giáo viên chỉ thấy và sửa đề của chính mình; đề/phiên/problem của giáo viên khác trả 404.
- Thời gian dùng RFC 3339 (`2026-10-21T09:00:00Z`). Server là nguồn thời gian; `state` của phiên là `upcoming` (trước `starts_at`), `running` (`starts_at <= now < ends_at`) hoặc `ended`.
- Lỗi: 400 dữ liệu sai, 403 sai vai trò hoặc chưa được xem, 404 không có/không thuộc quyền, 409 trạng thái không cho phép. Body `{"error": "..."}`.

### Đề

- `POST /exams`, `PUT /exams/{id}`: `{"title": "...", "description": "..."}`; `title` 1–255 ký tự.
- `GET /exams` → `{"items": [exam...]}`, mới nhất trước. Exam: `{id, title, description, created_at, updated_at}`.
- `DELETE /exams/{id}` → 204; 409 nếu đề còn problem hoặc phiên (xóa chúng trước).

### Phiên

- `POST /exams/{id}/sessions`, `PUT /sessions/{id}`: `{"name": "...", "starts_at": "...", "ends_at": "..."}`; bắt buộc cả hai mốc, `ends_at > starts_at`. Backend không tự đặt thời lượng.
- Session: `{id, exam_id, exam_title, name, starts_at, ends_at, state}`.
- `DELETE /sessions/{id}` → 204 (xóa luôn danh sách tham gia); 409 nếu đã có bài nộp.
- `GET /sessions/{id}/participants` (T) → `{"items": [participant...]}`. Participant: `{id, session_id, student_id, student_code, student_fullname, status, joined_at, finished_at}`; `status` là `in_progress` hoặc `finished`.
- `GET /sessions/{id}` (T/S) → `{"session": ..., "server_time": ..., "problems": [{id, code, title}], "problems_hidden": bool}`; sinh viên có thêm `participation` (participant hoặc `null`). Với sinh viên, trước `starts_at` thì `problems` rỗng và `problems_hidden = true`.
- `GET /me/sessions` (S) → `{"server_time": ..., "items": [session + "participation"]}`: mọi phiên của mọi đề.
- `POST /sessions/{id}/join` (S) → 200 participant. Chưa tham gia: chỉ được khi `running`, ngoài ra 409 (`session has not started` / `session has ended`). Đã tham gia: trả bản ghi cũ ở mọi trạng thái (vào lại sau mất mạng).
- `POST /sessions/{id}/finish` (S) → 200 participant `finished`. Gọi lại trả kết quả cũ, không đổi `finished_at`. 409 nếu chưa tham gia. Không có API mở lại.

### Problem

- `POST /exams/{id}/problems`, `PUT /problems/{id}` (cập nhật toàn bộ):

```json
{"code": "A", "title": "Tong hai so", "statement": "...", "time_limit_ms": 1000, "memory_limit_mb": 256}
```

  `code` trim + viết hoa, 1–20 ký tự, không khoảng trắng, duy nhất trong đề (409 nếu trùng). `time_limit_ms` 1–10000, `memory_limit_mb` 1–1024 (chặn nhập nhầm, chưa phải giới hạn chính thức).
- Problem: `{id, exam_id, code, title, statement, time_limit_ms, memory_limit_mb, version, test_dir, created_at, updated_at}`. `test_dir` là thư mục test tương đối với `DATA_DIR` (trên máy server: `./data/problems/<id>/tests`), được tạo khi tạo problem.
- `PUT /problems/{id}` → `{"problem": ..., "rejudge_queued": bool, "requeued_submissions": n}`. Đổi `time_limit_ms` hoặc `memory_limit_mb` → `version + 1` và chấm lại mọi bài nộp (`rejudge_queued: true`); chỉ sửa chữ thì không.
- `DELETE /problems/{id}` → 204 (xóa luôn các dòng `test_cases`; file test trên đĩa giữ nguyên); 409 nếu đã có bài nộp.
- `GET /problems/{id}/tests` → `{"test_dir": ..., "items": [{id, input, output}]}` theo thứ tự chấm.
- `POST /problems/{id}/rejudge` → quét `test_dir`:
  - mỗi `N.in` phải có `N.out` và ngược lại; file rỗng hợp lệ; file khác đuôi và file ẩn bị bỏ qua; symlink/thư mục con bị từ chối; tối đa 500 test;
  - thứ tự: tên là số thì theo số (`2` trước `10`), còn lại theo tên;
  - lỗi file → 409, không đổi gì;
  - thành công → thay `test_cases`, `version + 1`, chấm lại mọi bài nộp của problem ở mọi phiên → `{"problem": ..., "tests": [{name, input, output, input_size, output_size}], "rejudge_queued": true, "requeued_submissions": n}`.
  - Chấm lại (cả khi đổi limit): trong cùng transaction, mọi run/job hiện hành của problem chuyển `obsolete` (worker đang chấm version cũ bị loại kết quả), tạo run + job `queued` ở version mới cho mọi bài nộp, mọi phiên. Trong lúc chờ, `result` của bài nộp là run mới `queued`; kết quả cũ không còn là hiện hành (AC cũ có thể mất).
- `GET /sessions/{id}/problems/{pid}` (S) → `{id, code, title, statement, time_limit_ms, memory_limit_mb}`. 403 trước `starts_at` hoặc khi chưa tham gia phiên; 404 nếu problem không thuộc đề của phiên. Sau khi phiên kết thúc vẫn xem được.

## Chi tiết Nộp bài

### `POST /sessions/{id}/problems/{pid}/submissions` (S)

`multipart/form-data`, trường `file`: tên đuôi `.cpp` (không phân biệt hoa thường), 1 byte – 64 KB, không bắt buộc UTF-8. Tên gốc không được lưu; server lưu `DATA_DIR/submissions/<năm>/<tháng>/<32 hex>.cpp` và `fsync` trước khi ghi DB.

- `received_at` = giờ server khi đã nhận đủ file.
- Trong một transaction (khóa problem → phiên → participant): problem thuộc đề của phiên (404), đã join (409), chưa `finished` (409), `starts_at <= received_at < ends_at` (409), problem có test (409). Tạo submission + run `queued` (version hiện tại) + job `queued`, commit rồi trả **202**.
- Nộp và finish cùng lúc: thao tác nào giữ khóa participant và commit trước thì thắng.
- Lỗi: 400 (thiếu `file`, sai đuôi, rỗng), 413 (> 64 KB), 404, 409. Bị từ chối theo quy tắc → file vừa lưu bị xóa; lỗi DB khác → file giữ lại chờ dọn.

Đối tượng bài nộp:

```json
{"id": 12, "session_id": 3, "problem_id": 5, "problem_code": "A",
 "student_id": 7, "student_code": "B21DCCN001", "student_fullname": "Nguyen Van A",
 "received_at": "2026-10-22T04:10:00Z", "problem_version": 2,
 "result": {"run_id": 30, "problem_version": 2, "status": "queued", "verdict": null,
            "passed_tests": 0, "total_tests": 0, "created_at": "...", "started_at": null, "finished_at": null}}
```

`result` là run hiện hành (cùng version với problem, không `obsolete`); `null` khi problem vừa đổi version và chưa có lượt chấm mới. `status`: `queued`, `judging`, `completed`, `system_error`. FE hiển thị `passed_tests/total_tests verdict`, ví dụ `10/15 TLE`.

### `GET /sessions/{id}/submissions?student_id=&problem_id=` (T/S)

`{"items": [bài nộp...]}`, mới nhất trước. SV chỉ thấy bài của mình (`student_id` bị bỏ qua); GV phải sở hữu đề (404 nếu không).

### `GET /submissions/{id}` (T/S)

Bài nộp + `compiler_info`, `compile_output` trong `result`; `system_error` chỉ trả cho GV. SV khác / GV khác → 404.

### `GET /submissions/{id}/source` (T/S)

`text/plain`, tải về dạng `submission-<id>.cpp`. Quyền như trên.

### `GET /submissions/{id}/tests` (T)

`{"submission": ..., "items": [{test_case_id, verdict, execution_time_ms, memory_used_kb}]}` của run hiện hành. Khi dừng ở test sai đầu tiên, chỉ các test đã chạy có dòng.

### `POST /submissions/{id}/rejudge` (T)

Chấm lại một bài: run hiện hành và job của nó chuyển `obsolete`, tạo run + job mới ở version hiện tại; bài khác không đổi. Trả **202** + đối tượng bài nộp (`result.status = "queued"`). GV không sở hữu đề → 404. Mỗi (bài nộp, version) có tối đa một run không `obsolete` (unique index một phần `judge_runs_current_idx`).
