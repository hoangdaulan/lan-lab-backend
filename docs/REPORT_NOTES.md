# Tài liệu cho báo cáo (dàn ý + số liệu)

Dàn ý theo cấu trúc paper ở `Instruction.md` mục 8. Đây là **ghi chú và số liệu đã kiểm chứng** để nhóm viết báo cáo bằng lời của mình (báo cáo được kiểm tra Compilatio, similarity ≤ 20 %; mọi ý lấy từ tài liệu ngoài phải trích dẫn). Số liệu: `docs/results/RESULTS.md` (sinh từ `docs/results/runs.jsonl`). Quyết định thiết kế và lý do: `BACKEND_CONTEXT.md`.

## 8.1 Title

Gợi ý: *LAN Auto-Grader: chấm bài lập trình C++ thời gian thực qua mạng LAN với hàng đợi bền vững và WebSocket*.

## 8.2 Abstract (ý chính)

- Problem: ~50 SV nộp bài C++ gần như cùng lúc trong phòng máy; cần chấm tự động, an toàn, trả kết quả ngay, không mất bài khi có sự cố.
- Approach: API Go (REST + WebSocket) + hàng đợi bền vững trong PostgreSQL (lease, retry, phục hồi) + N worker chấm trong sandbox `setrlimit`; sự kiện `LISTEN/NOTIFY` vừa đánh thức worker vừa đẩy kết quả qua WebSocket; chấm lại theo version khi đổi test/giới hạn.
- Results (máy i5-12400F, 50 bài cùng lúc): 11,3 → 42,2 bài/s khi 1 → 8 slot; NOTIFY giảm e2e p50 32 %; WebSocket giảm request 2,4–4,7 lần và transaction DB ~2,6 lần so với polling; restart toàn stack giữa chừng không mất bài nào.
- Contribution: bảng ở mục 8.14.

## 8.3 Keywords

online judge; WebSocket; message queue; PostgreSQL LISTEN/NOTIFY; sandbox; concurrency.

## 8.4 Introduction (ý)

- Bối cảnh: thi lập trình trên máy tính trong LAN; LAN **không** phải cơ chế chống gian lận.
- Vấn đề: tải dồn (cả lớp nộp lúc sắp hết giờ), kết quả chậm, rủi ro chạy code lạ, sửa test giữa chừng.
- Mục tiêu và phạm vi: README mục 2.3–2.4.
- Đóng góp: mục 8.14.

## 8.5 Background / Related Work (ý — cần tự tìm và trích nguồn)

- Hệ thống chấm phổ biến dùng sandbox dựa trên cgroup/namespace (ví dụ công cụ *isolate* dùng trong CMS cho IOI); dự án này chọn `setrlimit` + namespace mạng để không phụ thuộc công cụ ngoài (đổi lại đo bộ nhớ kém chính xác hơn).
- Hàng đợi công việc trên PostgreSQL bằng `SELECT ... FOR UPDATE SKIP LOCKED` (tài liệu PostgreSQL) thay cho message broker riêng.
- WebSocket (RFC 6455) so với HTTP polling cho thông báo thời gian thực.
- Hạn chế của cách thường gặp: chấm tuần tự; polling; mất việc khi tiến trình chấm chết.

## 8.6 Problem Definition

- Input: file `.cpp` ≤ 64 KB, nộp trong khung `starts_at <= received_at < ends_at` (giờ server).
- Output: verdict AC/WA/TLE/MLE/RE/CE + `passed/total` (dừng ở test sai đầu tiên), đẩy tới đúng SV; bảng `x/N AC` cho GV.
- Ràng buộc: ~50 SV, một máy chủ trong LAN, test là file trên đĩa GV có thể sửa bất cứ lúc nào.
- Giả định / yêu cầu: không mất bài đã báo nhận; không lộ test; không có điểm số (chỉ đếm AC).

## 8.7 Proposed Approach

- Tách nhận bài (API) khỏi chấm (worker) bằng hàng đợi bền vững trong cùng DB → transaction "nhận bài + tạo job" nguyên tử.
- Lease + heartbeat + kiểm tra token/version khi ghi kết quả → nhiều worker song song, không chấm trùng, phục hồi khi worker chết.
- Version của problem: đổi test/giới hạn → tăng version + xếp hàng lại mọi bài trong cùng transaction; kết quả version cũ bị loại.
- `pg_notify` trong transaction → sự kiện chỉ phát sau COMMIT; một nguồn sự kiện cho cả worker (đánh thức) và API (đẩy WebSocket).
- Thứ tự khóa cố định problem → session → participant → run → job để tránh deadlock.

## 8.8 System Architecture

- Sơ đồ: README mục 4.1 (mermaid). Thành phần: README mục 4.2.
- Triển khai: Docker Compose; mạng `web` (API ra LAN) và `db` (internal).

## 8.9 Network Communication Design

- Protocol, message format, sequence: README mục 5; chi tiết đầy đủ `docs/api-v1.md` ("Chi tiết WebSocket").
- Kết nối: REST (HTTP/1.1, keep-alive), WebSocket nâng cấp từ HTTP, xác thực bằng message đầu, ping/pong 20 s, mã đóng 4001/1008/1000; PostgreSQL: pool + một kết nối `LISTEN` mỗi tiến trình.
- Xử lý lỗi: README mục 5.5.

## 8.10 Implementation

- Module: `internal/{auth,student,exam,problem,submission,board}` (handler → service → repository), `internal/judge` (hàng đợi, worker), `internal/cppjudge` + `internal/sandbox`, `internal/realtime` (hub), `internal/events`.
- Concurrency: goroutine mỗi kết nối WebSocket (đọc/ghi), hub có mutex + kênh gửi có bộ đệm (client chậm bị ngắt); worker N goroutine chấm + goroutine heartbeat + goroutine phục hồi lease; DB: `FOR UPDATE SKIP LOCKED`, khóa hàng theo thứ tự.
- Cấu trúc dữ liệu: bảng `judge_runs`/`judge_jobs` với unique index một phần "một run hiện hành mỗi (bài, version)".

## 8.11 Experimental Setup

- Phần cứng/OS/phần mềm: `docs/results/RESULTS.md` mục "Môi trường".
- Dữ liệu: 50 tài khoản mới mỗi lần, 1 problem 15 test, chương trình a+b.
- Kịch bản: E1 số worker, E2 cách worker nhận job, E3 cách client nhận kết quả, E4 khởi động lại khi đang chấm; mỗi cấu hình 3 lần (E4: 1 lần).
- Công cụ: `cmd/loadtest`, `scripts/experiment.sh`, `scripts/results.py`.

## 8.12 Results (trích từ `docs/results/RESULTS.md`)

| Thí nghiệm | Kết quả chính |
|---|---|
| E1: 1 / 2 / 4 / 8 slot | 11,3 / 21,5 / 35,1 / 42,2 bài/s; e2e p50 2333 / 1249 / 797 / 722 ms |
| E1: 2 worker × 4 | 41,0 bài/s (≈ 1 × 8) |
| E2: polling 1 s → NOTIFY | chờ hàng đợi p50 1203 → 764 ms; e2e p50 1362 → 923 ms |
| E3: WebSocket vs polling 1 s / 250 ms | request 50 vs 118 / 237; transaction DB 250 vs 689 / 626; e2e p50 925 vs 1097 / 1094 ms; byte 134 KB vs 118 / 214 KB |
| Đăng nhập + nộp đồng loạt | đăng nhập p95 < 300 ms; nộp p95 < 100 ms; 0 lỗi HTTP / 31 lần |
| E4: restart toàn stack | 50/50 AC; 1 bài phục hồi sau lease (67 s) |
| WebSocket 50 client (test tích hợp) | thông báo tới 50 client: p50 0,68 ms; commit → client: p50 2,6 ms, max 10,7 ms |

## 8.13 Discussion

README mục 15. Thêm: vì sao byte WebSocket không thấp hơn polling 1 s (3 trạng thái/bài, bài chấm nhanh); vì sao bão hòa ở 8 slot (CPU); đánh đổi độ dài lease (phát hiện worker chết nhanh vs nguy cơ chấm trùng khi mạng chậm).

## 8.14 Novelty and Contributions

| Contribution | Description | Evidence |
|---|---|---|
| Hàng đợi bền vững + nhiều worker có lease | Không chấm trùng, không mất bài, retry lỗi hạ tầng | E1, E4; test `TestTwoWorkersJudgeEachJobExactlyOnce`, `TestExpiredLeaseRecoveredAndOldWorkerDropped`, `TestNoDeadlockRequeueVsWorkers` |
| Sự kiện NOTIFY đánh thức worker | Bỏ thời gian chờ chu kỳ polling | E2 (−32 % e2e p50) |
| Đẩy kết quả qua WebSocket với protocol riêng | Kết quả tới trình duyệt ngay sau COMMIT; REST bù khi nối lại | E3; test tích hợp 50 client; `TestListenerReconnectSendsResync` |
| Chấm lại theo version | Sửa test/giới hạn → chấm lại tự động, kết quả cũ không bị hiện như hiện hành | Test tích hợp + smoke `2/2 AC@v2 → 1/2 WA@v3` |
| Sandbox không cần công cụ ngoài | uid riêng, namespace mạng rỗng, `no_new_privs`, `setrlimit` | `make test-sandbox` (17 test, có kiểm tra đột biến) |

## 8.15 Limitations

README mục 16.

## 8.16 Future Work

README mục 17.

## 8.17 Conclusion (ý)

Bài toán → giải pháp (queue + worker + WebSocket + version) → số liệu chính → đóng góp.

## 8.18 References

README mục 19 (RFC 6455, RFC 9110, RFC 7519, tài liệu PostgreSQL, man-pages Linux, bcrypt). Bổ sung nguồn cho mục Related Work khi viết.

## 8.19 Appendix (gợi ý)

- Schema: `database/schema-design.sql`.
- Protocol đầy đủ: `docs/api-v1.md`.
- Cấu hình: `.env.example`, `docker-compose.yml`.
- Kết quả thô: `docs/results/runs.jsonl`.
- Kế hoạch + checklist đã kiểm chứng: `docs/IMPLEMENTATION_PLAN.md`.
