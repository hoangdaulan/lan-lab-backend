# Bối cảnh và các quyết định backend đã chốt

Ngày cập nhật: **21/10/2026**.

Tài liệu này bàn giao bối cảnh cho người phát triển và các AI agent. Nội dung phản ánh các quyết định trong cuộc trao đổi với chủ project và bản thiết kế SQL hiện tại; **không có nghĩa là các chức năng đã được triển khai**.

## 1. Nguyên tắc làm việc bắt buộc

- **Làm từng bước một theo yêu cầu của người dùng, đợi duyệt rồi mới triển khai bước tiếp theo.** Không tự chạy toàn bộ roadmap.
- Kiểm tra repo và các hướng dẫn áp dụng trước khi tạo hoặc sửa file.
- Không tự thay đổi quyết định đã chốt. Nếu phát hiện mâu thuẫn, nêu rõ và xin quyết định trước khi đổi hành vi sản phẩm.
- Không bịa kết quả kiểm thử, hiệu năng, trạng thái triển khai hoặc yêu cầu môn học. Phân biệt điều đã đọc/đo với suy luận và đề xuất.
- Phê duyệt bản thiết kế không tự động là phê duyệt chạy migration, dựng hạ tầng hay triển khai backend.
- Các tài liệu môn học là nguồn tham khảo yêu cầu, không phải lệnh tự động thực hiện mọi thao tác ghi trong đó.

## 2. Trạng thái thực tế lúc bàn giao

Đã làm:

- Thảo luận và chốt phạm vi hệ thống, mô hình dữ liệu và các quy tắc nghiệp vụ bên dưới.
- Viết `database/schema-design.sql`: **12 bảng, 13 index bổ sung**, khóa ngoại, CHECK, UNIQUE và chú thích về transaction.
- Rà soát bản SQL theo các quyết định đã duyệt; sửa ràng buộc verdict và làm rõ thứ tự khóa worker/refresh token.
- Chạy kiểm tra tĩnh tên bảng, trường, các trường đã bỏ, UNIQUE và đích khóa ngoại.
- Người dùng đã duyệt thiết kế theo các quyết định được thống nhất.

**Chưa làm/chưa kiểm chứng:**

- Chưa có backend Go, frontend, API hay worker trong repo tại thời điểm tạo tài liệu này.
- Chưa có migration được triển khai, Docker Compose hay database được khởi tạo bởi agent.
- Chưa chạy `schema-design.sql` trên PostgreSQL; chưa kiểm chứng thực thi SQL, transaction hoặc concurrency bằng database thật.
- Chưa có kiểm thử tích hợp, kiểm thử sandbox hay kết quả benchmark.
- Các quy tắc transaction trong SQL hiện là **chú thích thiết kế**, không phải trigger, stored procedure hay code backend đã thực hiện.

Bản SQL là nguồn chi tiết cho **tên cột, kiểu dữ liệu, constraint và index hiện tại**. Tài liệu này giải thích quyết định và giới hạn. Nếu hai nguồn mâu thuẫn, phải báo lại, không âm thầm chọn một nguồn hoặc khẳng định đã triển khai.

## 3. Mục tiêu và môi trường

- Hệ thống quản lý và chấm bài lập trình cho môn **Network Programming**, khoảng **50 sinh viên**.
- Web chạy trên server trong LAN; truy cập qua trình duyệt bằng `http://IP:cổng`.
- Backend dùng **Go**. Chưa chọn HTTP framework, thư viện PostgreSQL hoặc WebSocket.
- **PostgreSQL chạy bằng Docker Compose**, có volume lưu dữ liệu bền vững.
- Server dự kiến chạy Linux.
- HTTP/REST và WebSocket dùng thư viện/framework; chưa làm TCP socket mức thấp.
- Bộ chấm tách khỏi xử lý request web, có hàng đợi bền vững và giới hạn công việc đồng thời. Không bắt buộc tách thành máy vật lý khác.
- LAN không phải cơ chế chống gian lận.

Máy đã trao đổi: laptop i3 thế hệ 10, RAM 12 GB. Chưa xác minh model CPU, số nhân/luồng hay đo tải. Có thể thử chạy chung web, database và worker, nhưng chưa cam kết hiệu năng.

**Không coi ví dụ tính tải là cấu hình đã chốt:** 30 bài đến cùng lúc, 15 test/bài, 2 giây chấm/bài và khoảng 15 worker là giả định thảo luận. Hai lượt chấm lý tưởng mất khoảng 4 giây chỉ khi mỗi bài vẫn hoàn tất trong 2 giây lúc chạy đồng thời, chưa tính request/trả kết quả. Chưa có bằng chứng laptop đạt được giả định đó. Số worker, số test và SLA chưa được chốt bằng đo thực tế.

## 4. Tài khoản, đăng nhập và quyền

- Có hai loại tài khoản: giáo viên và sinh viên.
- Cả hai dùng trường **`code`** làm định danh đăng nhập và **`fullname`** làm họ tên.
- Sinh viên: `code` là MSV. Mật khẩu mặc định bằng MSV, **chỉ lưu hash**. Được đổi mật khẩu, không bắt buộc đổi lần đầu.
- Giáo viên đăng nhập bằng mã giáo viên và mật khẩu. Cách cấp tài khoản/mật khẩu ban đầu chưa chốt.
- Không có `username`, `is_active` hoặc `must_change_password`.
- Tầng Go có thể dùng kiểu `User` chung cho các trường chung; DB vẫn có **hai bảng `teachers`, `students`**. Không có bảng `users` hoặc kế thừa bảng PostgreSQL.
- `code` duy nhất **trong từng bảng**, chưa có yêu cầu duy nhất trên cả hai loại tài khoản. API cần phân biệt loại tài khoản khi đăng nhập; hình thức endpoint chưa chốt.
- Backend phải kiểm tra vai trò, quyền sở hữu và quyền truy cập; không tin ID sinh viên do client tự khai khi nộp bài.
- Sinh viên chỉ xem bài nộp/kết quả của mình, không xem test ẩn hoặc dữ liệu sinh viên khác. Giáo viên xem bài nộp và kết quả tổng hợp trong phạm vi quyền của mình.

### JWT và refresh token

- Access token dùng **JWT**, không lưu access token vào DB.
- Refresh token là chuỗi ngẫu nhiên; bảng `refresh_tokens` chỉ giữ hash và thời hạn.
- Một refresh token thuộc đúng một giáo viên hoặc sinh viên.
- Refresh thành công thay token cũ bằng token mới trong cùng transaction; một token cũ chỉ được sử dụng thành công một lần.
- Đăng xuất xóa refresh token tương ứng. Đổi mật khẩu xóa toàn bộ refresh token của tài khoản.
- JWT đã cấp vẫn dùng được tới hết hạn; chưa có cơ chế thu hồi access token ngay lập tức.
- Thời hạn cụ thể, thuật toán ký, cách truyền/lưu token ở trình duyệt và bảo vệ request chưa chốt.

## 5. Đề và phiên làm bài

- Tách **`exams` (đề)** và **`exam_sessions` (phiên tổ chức làm đề)**.
- Một đề gồm nhiều problem và có thể dùng cho nhiều phiên.
- Mỗi phiên có giờ bắt đầu/kết thúc dùng chung cho người tham gia.
- Frontend có thể điền mặc định thời lượng một giờ. **Backend không tự mặc định thời lượng**: request phải gửi `starts_at`, `ends_at`; backend kiểm tra `ends_at > starts_at`.
- Server là nguồn thời gian quyết định. Đồng hồ trình duyệt chỉ để hiển thị.
- Khoảng nộp hợp lệ: **`starts_at <= received_at < ends_at`**; đúng thời điểm kết thúc thì không nhận.
- Một sinh viên có đúng tối đa một bản ghi tham gia mỗi phiên: `UNIQUE(exam_session_id, student_id)`.

### Trạng thái tham gia

| Trạng thái | Ý nghĩa |
|---|---|
| `in_progress` | Đã tham gia, chưa chủ động kết thúc |
| `finished` | Đã chủ động kết thúc, không được làm hoặc nộp tiếp |

- Mất mạng, đóng tab hoặc đăng xuất không làm chuyển sang `finished`.
- Vào lại dùng bản ghi tham gia cũ; được tiếp tục nếu còn giờ và chưa `finished`.
- Khi sinh viên xác nhận kết thúc: đặt `finished`, ghi `finished_at`. Không mở lại bằng API sinh viên.
- Đã kết thúc vẫn được xem lịch sử/kết quả của mình.
- Hết giờ thì chặn nộp theo `ends_at`, không cần đổi hàng loạt participant sang `finished`.
- Bài đã nhận hợp lệ vẫn chấm tiếp dù sinh viên kết thúc hoặc phiên hết giờ.

## 6. Bài lập trình và kết quả

- Bản đầu chỉ hỗ trợ **một file `.cpp`, C++17**, đọc stdin, ghi stdout.
- Compiler trong môi trường chấm phải được công bố và cố định; phiên bản cụ thể chưa chọn.
- Chưa hỗ trợ Java, nhiều file, bài tương tác, chương trình tự đọc/ghi file dữ liệu hay custom checker.
- So đáp án theo token, bỏ qua khác biệt khoảng trắng/xuống dòng; chưa hỗ trợ sai số số thực.
- Chạy từng test; không tự suy ra cơ chế dừng sau test sai đầu tiên. Cách xử lý này cần bảo đảm yêu cầu báo số test đúng/tổng test.
- Verdict gồm **AC, WA, TLE, MLE, CE, RE**. Lỗi hạ tầng phải tách riêng.
- **Không có điểm số**, không có `max_score`, `score` hay bảng tổng điểm số.
- AC khi vượt qua toàn bộ test; bài không có test không được tự động AC.
- FE tổng hợp theo số bài AC, ví dụ **`4/5 AC` = AC 4 trong 5 problem**, không phải 4/5 test đúng.
- Số test đúng/tổng test chỉ để đối chiếu và phục vụ phúc khảo, không quy đổi điểm.
- Một problem được tính AC nếu có ít nhất một lần nộp AC tại version hiện hành trong đúng phiên của sinh viên.
- Lần nộp sai hoặc lỗi hạ tầng khác không làm mất AC hợp lệ cùng version.
- Khi version thay đổi, tính lại theo version mới; AC cũ có thể mất nếu không bài nộp nào còn AC.
- Không giới hạn tổng số lần nộp; vẫn giới hạn số bài chấm đồng thời và tài nguyên.

## 7. Lưu file trên host

- `submissions.source_path` lưu đường dẫn mã nguồn; không lưu nội dung source trong DB và không giữ `original_filename`.
- `test_cases.input`, `test_cases.output` lưu **đường dẫn file**, không phải nội dung/byte dữ liệu.
- Đường dẫn tương đối dưới thư mục server quản lý, tên file do server tạo. Client không được chỉ định đường dẫn tùy ý.
- File rỗng có thể là test hợp lệ; đường dẫn tới file vẫn phải có giá trị.
- File và database phải lưu bền vững; sao lưu/khôi phục cần bao gồm cả hai.
- Nếu worker nằm trong container/máy khác, phải thiết kế quyền đọc và cách truy cập file; cách triển khai cụ thể chưa chốt.
- Không xác nhận nhận bài trước khi file được lưu bền vững và transaction DB thành công.
- Lưu DB thất bại: không báo nhận thành công; file mồ côi được dọn sau, tránh xóa file request đang xử lý.

## 8. Sửa test, version và chấm lại

**Chỉ giữ bộ test hiện tại. Không thêm version vào `test_cases`, không giữ bộ test cũ để chấm lịch sử.**

Khi `PUT` thay bộ test của problem:

1. Ghi file test mới vào đường dẫn mới, không ghi đè file đang dùng.
2. Khóa problem và mở transaction thay bộ test.
3. Xóa test cũ, thêm bộ test mới không rỗng.
4. Tăng `problems.version` **một lần cho cả lần lưu**, không tăng theo từng test.
5. Vô hiệu hóa các lượt/công việc chấm version cũ.
6. Tạo lượt chấm và công việc cho **mọi bài nộp của problem**, kể cả phiên đã kết thúc.
7. Commit; dọn file cũ an toàn sau đó.

- `judge_runs.problem_version` ghi version của problem dùng để chấm.
- Kết quả hiện tại phải thỏa **`judge_runs.problem_version = problems.version`**. Không lấy `MAX(version)` trong bảng kết quả vì có thể vô tình dùng kết quả cũ khi version mới chưa xong.
- Truy vấn còn phải lọc đúng sinh viên, phiên, problem và bài nộp; `problem_id + version` không tự xác định được chủ bài.
- Worker chụp bản sao nhất quán của test, version và giới hạn trước khi chạy. Không đọc lẫn test cũ/mới.
- Kết quả worker version cũ hoàn thành muộn phải bị loại bỏ.
- FK từ kết quả từng test tới test dùng `ON DELETE CASCADE`; xóa test cũ xóa luôn kết quả chi tiết cũ. Không cam kết còn dữ liệu phúc khảo của bộ test cũ.
- Run cũ có thể còn hàng tổng hợp nhưng bị `obsolete`, không được dùng làm kết quả hiện tại.
- FE cần biểu thị đang chờ/chấm lại, không trình bày kết quả cũ như kết quả hiện hành đã hoàn tất. Câu chữ API/FE cụ thể chưa chốt.
- Cho phép sửa đề; không áp dụng quy tắc khóa toàn bộ đề khi bắt đầu phiên.

**Điểm còn cần xác nhận khi triển khai:** chú thích SQL đề xuất tăng version/chấm lại khi đổi giới hạn thời gian hoặc bộ nhớ. Quy tắc tăng version khi thay test đã được chốt rõ; trường hợp đổi giới hạn chưa được thảo luận riêng. Không âm thầm suy ra phạm vi chấm lại khác.

## 9. Danh sách bảng và cột hiện tại

Chi tiết kiểu dữ liệu/constraint/index xem `database/schema-design.sql`.

| Bảng | Cột |
|---|---|
| `teachers` | `id`, `fullname`, `code`, `password_hash`, `created_at`, `updated_at` |
| `students` | `id`, `fullname`, `code`, `password_hash`, `created_at`, `updated_at` |
| `exams` | `id`, `teacher_id`, `title`, `description`, `created_at`, `updated_at` |
| `problems` | `id`, `exam_id`, `code`, `title`, `statement`, `time_limit_ms`, `memory_limit_mb`, `version`, `created_at`, `updated_at` |
| `test_cases` | `id`, `problem_id`, `input`, `output` |
| `exam_sessions` | `id`, `exam_id`, `name`, `starts_at`, `ends_at`, `created_at`, `updated_at` |
| `exam_participants` | `id`, `exam_session_id`, `student_id`, `status`, `joined_at`, `finished_at` |
| `refresh_tokens` | `id`, `teacher_id`, `student_id`, `token_hash`, `expires_at` |
| `submissions` | `id`, `exam_participant_id`, `problem_id`, `source_path`, `received_at` |
| `judge_runs` | `id`, `submission_id`, `problem_version`, `status`, `verdict`, `passed_tests`, `total_tests`, `compiler_info`, `compile_output`, `system_error`, `created_at`, `started_at`, `finished_at` |
| `submission_test_results` | `id`, `judge_run_id`, `test_case_id`, `verdict`, `execution_time_ms`, `memory_used_kb` |
| `judge_jobs` | `id`, `judge_run_id`, `status`, `attempt_count`, `available_at`, `worker_id`, `lease_token`, `lease_expires_at`, `last_error`, `created_at`, `updated_at` |

- PK dùng `bigint GENERATED ALWAYS AS IDENTITY`, thời gian dùng `timestamptz`.
- `updated_at` do backend cập nhật; chưa có trigger tự động.
- `problems` không có `position`; query sắp theo `code`. Đây là sắp xếp chuỗi, không tự bảo đảm `1, 2, 10` theo số.
- `test_cases` không có `position`; có thể dùng `id` để có thứ tự ổn định.
- Không có `auth_sessions`; đã thay bằng `refresh_tokens`.
- Không có bảng tổng điểm; kết quả AC được truy vấn từ lượt chấm hiện hành.
- Một run cho mỗi `(submission_id, problem_version)`; retry hạ tầng dùng lại run đó, không tạo thêm run trùng version.
- Một job cho mỗi run; nhận bài phải tạo submission + run + job cùng transaction.

## 10. Transaction, hàng đợi và phục hồi

### Nhận bài và kết thúc đồng thời

- `received_at` do server ghi ngay khi nhận đủ file, không dùng thời gian client và không lấy giờ sau khi chờ khóa để thay thế.
- Khóa participant để phân thứ tự nhận bài và kết thúc. Thao tác giữ khóa và commit trước quyết định, không theo thứ tự click trên FE.
- Nếu kết thúc đã được ghi nhận trước thì từ chối nộp; nếu nộp đã được ghi nhận trước thì tiếp tục chấm.
- File nhận đủ trước hạn có thể ghi DB sau hạn nếu participant chưa kết thúc lúc ghi nhận.
- Backend kiểm tra bài thuộc đúng đề của phiên, danh tính từ JWT và các điều kiện thời gian/trạng thái.

### Khóa và worker

- Thứ tự khóa thiết kế: **problem → exam_session → exam_participant → judge_run → judge_job**; chỉ khóa hàng cần dùng, nhiều hàng cùng bảng theo ID tăng dần.
- Worker tìm ứng viên queued, khóa theo thứ tự, dùng `FOR UPDATE SKIP LOCKED` khi lấy job và kiểm tra lại điều kiện trước khi nhận. Không khóa job rồi quay lại khóa problem/run.
- Không giữ transaction/khóa DB suốt thời gian biên dịch và chạy code.
- Job được cấp `worker_id`, `lease_token`, `lease_expires_at`; tăng `attempt_count`.
- Chỉ worker có lease còn hợp lệ và version hiện hành được ghi kết quả; worker cũ không được ghi đè sau khi job đã giao lại.
- Ghi kết quả từng test, tổng hợp run và hoàn tất job cùng transaction, kiểm tra lại version/lease dưới khóa.
- Khi retry, dọn/reset dữ liệu lần thử trước để không trộn kết quả.
- Worker chết/server khởi động lại: phục hồi job quá hạn lease. Job version cũ phải obsolete, không đưa lại vào hàng đợi hiện hành.
- Retry lỗi hạ tầng có backoff và giới hạn số lần; giá trị cụ thể chưa chốt. Hết retry thì báo lỗi hạ tầng, không quy thành lỗi chương trình sinh viên.

### Trạng thái hàng đợi và kết quả

| Dữ liệu | Trạng thái |
|---|---|
| `judge_runs.status` | `queued`, `judging`, `completed`, `system_error`, `obsolete` |
| `judge_jobs.status` | `queued`, `running`, `done`, `failed`, `obsolete` |

- Verdict chỉ có khi run `completed`. CE ở cấp run, không phải kết quả từng test.
- Run completed cần có test count và thời điểm hoàn tất hợp lệ.
- `0 <= passed_tests <= total_tests`; AC phải đúng toàn bộ test, CE có 0 test đúng; WA/TLE/MLE/RE phải có test chưa đúng.
- Job `running` phải có đủ trường worker/lease; khi rời running thì xóa các trường này.
- Backend còn phải bảo đảm test thuộc đúng problem của submission; FK hiện tại không tự bảo đảm điều này.

### Refresh token

- Thứ tự khóa riêng cho auth: **account → refresh_token**.
- Đọc token để xác định account, khóa account rồi khóa/đọc lại token; kiểm tra tồn tại/hết hạn trước khi thay token.
- Login, refresh và đổi mật khẩu phải đồng bộ để không cấp refresh token dựa trên thông tin trước khi đổi mật khẩu.

## 11. An toàn chạy code và truyền trạng thái

- Code sinh viên phải được biên dịch/chạy trong sandbox, không chạy trực tiếp với quyền web server.
- Giới hạn cả biên dịch lẫn thực thi: thời gian/CPU, bộ nhớ, tiến trình và output.
- Giới hạn thời gian/bộ nhớ chạy test nằm ở problem. Giới hạn biên dịch, tiến trình, output và cơ chế sandbox cụ thể chưa chọn.
- Compile output và lỗi nội bộ cần kiểm soát dung lượng/nội dung trước khi trả; không lộ test ẩn hoặc thông tin hạ tầng nhạy cảm.
- WebSocket cập nhật trạng thái; REST trả trạng thái hiện tại khi tải lại trang hoặc mất kết nối. Kết nối WebSocket không phải nguồn lưu kết quả duy nhất.

## 12. Xóa dữ liệu và giữ lịch sử

- Chặn xóa đề/problem đã có bài nộp; không xóa bài nộp qua chức năng thông thường.
- FK RESTRICT bảo vệ các quan hệ lịch sử. Xóa dữ liệu cha chưa có lịch sử vẫn cần quy trình xử lý dữ liệu con/file phù hợp.
- Không đổi liên kết lịch sử để chuyển problem/session sang đề khác, participant sang sinh viên/phiên khác hoặc submission sang chủ/problem khác.
- Ngoại lệ chủ động đã chốt: thay test xóa test và kết quả test cũ; token đăng nhập được xóa khi thu hồi.

## 13. Phạm vi chưa làm và các quyết định còn mở

Ngoài phạm vi bản đầu:

- Java; bài nhiều file/tương tác; chương trình tự đọc/ghi file dữ liệu; custom checker.
- Phát hiện giống code/gian lận, theo dõi rời tab.
- Triển khai Internet hoặc hệ thống phân tán nhiều máy.

Chưa chốt:

- HTTP framework, thư viện DB/WebSocket, công cụ migration, phiên bản Go/PostgreSQL/compiler.
- API chi tiết. `/api/v1` và các nhóm endpoint từng được đề xuất, chưa phải hợp đồng cố định.
- Cơ chế cấp tài khoản, cách sinh viên tìm/tham gia phiên, mã truy cập riêng nếu cần sau này.
- JWT TTL/thuật toán/cách lưu token, giới hạn file, cấu hình sandbox, số worker/lease/retry.
- Quy tắc chọn verdict tổng hợp khi một bài có nhiều loại test lỗi; không tự bịa thứ tự ưu tiên.
- Công nghệ FE, số thành viên, điều kiện mạng demo và kết quả đo phần cứng.
- Quy tắc chấm lại khi đổi giới hạn thời gian/bộ nhớ như đã ghi ở mục 8.

## 14. Tài liệu môn học và thực nghiệm

Nguồn trong repo ban đầu: `Instruction.md`, `README.md`, `Submission.md`, `Topics.md`.

- Networked Auto-Grader được nêu trong `Topics.md`.
- Cần trình bày kiến trúc mạng, message format, quản lý kết nối, concurrency, xử lý lỗi và Novelty/Contributions.
- Báo cáo theo cấu trúc technical paper; số liệu phải đo thực tế.
- Hướng thực nghiệm đã đề xuất: so sánh một worker với nhiều worker, đo thời gian chờ, thông lượng, thời gian chấm và tài nguyên. Chưa thực hiện, chưa có số liệu.
- `Submission.md` ghi hạn **23:59 ngày 31/10/2026**. Tài liệu có hướng bàn giao Google Drive và USB khác nhau, cần xác nhận giảng viên; không tự chọn thay người dùng.

Nếu tài liệu này được chép sang repo backend riêng, cần mang theo `database/schema-design.sql`; các tài liệu môn học có thể vẫn nằm ở repo ban đầu. Không khẳng định file tồn tại ở repo mới nếu chưa kiểm tra.

## 15. Công việc tiếp theo

Tại lúc bàn giao, người dùng chỉ yêu cầu tạo tài liệu này. **Chưa được tự chuyển sang bước triển khai.**

Khi có yêu cầu mới: đọc tài liệu này và SQL, kiểm tra trạng thái repo thực tế, xác định đúng bước được giao. Các bước dự kiến sau thiết kế là tạo/kiểm chứng migration PostgreSQL, chốt API rồi triển khai luồng tối thiểu; phải duyệt từng bước, không tự làm đồng loạt.
