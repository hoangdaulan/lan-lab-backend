# API v1

Ngày chốt: **21/10/2026**. Quyết định nền: `BACKEND_CONTEXT.md` mục 17.

- Prefix: `/api/v1` (trừ `/healthz`).
- Vai trò: **T** = giáo viên, **S** = sinh viên, **—** = công khai.
- Xác thực: `Authorization: Bearer <access JWT>` cho mọi route trừ login/refresh/healthz.
- Trạng thái cột "Làm": ✅ đã triển khai, trống = chưa.

## 1. Auth

| Method | Path | Vai trò | Mục đích | Làm |
|---|---|---|---|---|
| POST | `/auth/login` | — | `{role, code, password}` → access + refresh token | ✅ |
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
| GET | `/exams` | T | Đề của giáo viên | |
| POST | `/exams` | T | Tạo đề | |
| GET | `/exams/{id}` | T | Chi tiết đề | |
| PUT | `/exams/{id}` | T | Sửa đề | |
| DELETE | `/exams/{id}` | T | Xóa đề (chặn nếu đã có bài nộp) | |
| GET | `/exams/{id}/sessions` | T | Các phiên của đề | |
| POST | `/exams/{id}/sessions` | T | Tạo phiên (`starts_at`, `ends_at` bắt buộc) | |
| GET | `/sessions/{id}` | T/S | Thông tin phiên, giờ server, trạng thái của mình (S), danh sách problem | |
| PUT | `/sessions/{id}` | T | Sửa phiên | |
| DELETE | `/sessions/{id}` | T | Xóa phiên (chặn nếu đã có bài nộp) | |
| GET | `/sessions/{id}/participants` | T | Người tham gia và trạng thái | |
| GET | `/me/sessions` | S | Mọi phiên + trạng thái tham gia của mình | |
| POST | `/sessions/{id}/join` | S | Tham gia (chỉ trong khung giờ; vào lại dùng bản ghi cũ) | |
| POST | `/sessions/{id}/finish` | S | Kết thúc làm bài (idempotent) | |

## 4. Problem và test

| Method | Path | Vai trò | Mục đích | Làm |
|---|---|---|---|---|
| GET | `/exams/{id}/problems` | T | Problem của đề | |
| POST | `/exams/{id}/problems` | T | Tạo problem; tạo thư mục test trên đĩa | |
| GET | `/problems/{id}` | T | Chi tiết problem | |
| PUT | `/problems/{id}` | T | Sửa; đổi time/memory limit → tăng version + chấm lại | |
| DELETE | `/problems/{id}` | T | Xóa (chặn nếu đã có bài nộp) | |
| GET | `/problems/{id}/tests` | T | Danh sách test hiện có trong DB (đường dẫn, kích thước) | |
| POST | `/problems/{id}/rejudge` | T | Quét thư mục test, đồng bộ `test_cases`, tăng version, chấm lại mọi bài nộp của problem ở mọi phiên | |
| GET | `/sessions/{sid}/problems/{pid}` | S | Đề bài; chỉ từ `starts_at` | |

## 5. Nộp bài

| Method | Path | Vai trò | Mục đích | Làm |
|---|---|---|---|---|
| POST | `/sessions/{sid}/problems/{pid}/submissions` | S | Upload 1 file `.cpp` ≤ 64 KB; trả 202 + id | |
| GET | `/sessions/{sid}/submissions` | T/S | S: bài của mình; T: mọi bài, lọc `student_id`, `problem_id` | |
| GET | `/submissions/{id}` | T/S | Trạng thái run hiện hành, verdict, passed/total, compile output đã cắt | |
| GET | `/submissions/{id}/source` | T/S | Tải mã nguồn (S chỉ bài của mình) | |
| GET | `/submissions/{id}/tests` | T | Kết quả từng test (phúc khảo) | |
| POST | `/submissions/{id}/rejudge` | T | Chấm lại thủ công 1 bài tại version hiện hành | |

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
| 400 | Body sai JSON, thiếu trường, `role` không hợp lệ, mật khẩu mới không hợp lệ |
| 401 | Sai `code`/mật khẩu/role, access token thiếu/hết hạn/sai, refresh token không tồn tại/hết hạn/đã dùng |
| 403 | Đúng token nhưng sai vai trò cho route |

### `POST /auth/login`

```json
// request
{"role": "student", "code": "B21DCCN001", "password": "B21DCCN001"}
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

Request `{"code": "b21dccn001", "fullname": "Nguyen Van A"}` → 201 + đối tượng sinh viên. 400 nếu không hợp lệ, 409 nếu `code` đã tồn tại.

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
{"created_count": 2, "skipped_count": 1,
 "created": [ ...đối tượng sinh viên... ],
 "skipped": [{"line": 3, "code": "SV02", "reason": "already exists"}]}
```

### `POST /students/{id}/reset-password`

204. Mật khẩu về lại `code`; xóa mọi refresh token của sinh viên. Access token đã cấp còn hiệu lực tới hết hạn (≤ 15 phút). 404 nếu không có sinh viên.
