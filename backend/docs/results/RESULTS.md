# Kết quả thực nghiệm (phase 14)

Sinh tự động bởi `scripts/results.py` từ `docs/results/runs.jsonl`. Mỗi dòng là **trung vị** của các lần lặp (cột n).
Mỗi lần: 50 SV đăng nhập + mở kết nối, 50 bài nộp gửi **cùng lúc**, 15 test/bài, chương trình C++ a+b (AC).

Thời gian tính bằng ms. `e2e` = từ lúc SV gửi bài tới lúc client biết kết quả; `chờ hàng đợi`/`chấm` đo ở server (DB); `makespan` = bài đầu gửi → kết quả cuối; `thông lượng` = bài/giây.

## E1. Số worker (slot chấm song song)

| cấu hình | n | e2e p50 | e2e p95 | chờ hàng đợi p50 | chấm p50 | makespan | thông lượng | CPU đỉnh worker % | RAM đỉnh worker MiB | CPU đỉnh API % | RAM đỉnh API MiB | lỗi HTTP | verdict |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `workers-c1` | 3 | 2333 | 4257 | 2215 | 85 | 4431 | 11.3 | 106 | 24 | 340 | 38 | 0 | {'AC': 150} |
| `workers-c2` | 3 | 1249 | 2238 | 1118 | 88 | 2328 | 21.5 | 213 | 43 | 340 | 39 | 0 | {'AC': 150} |
| `workers-c4` | 3 | 797 | 1373 | 651 | 103 | 1424 | 35.1 | 305 | 43 | 326 | 41 | 0 | {'AC': 150} |
| `workers-c8` | 3 | 722 | 1133 | 514 | 164 | 1184 | 42.2 | 0 | 8 | 330 | 41 | 0 | {'AC': 150} |
| `workers-2x4` | 3 | 731 | 1178 | 527 | 171 | 1220 | 41.0 | 337 | 59 | 341 | 40 | 0 | {'AC': 150} |

## E2. Cách worker nhận job: polling 1 s vs NOTIFY

| cấu hình | n | e2e p50 | e2e p95 | chờ hàng đợi p50 | chấm p50 | makespan | thông lượng | lỗi HTTP | verdict |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `pickup-poll1s` | 3 | 1362 | 1979 | 1203 | 118 | 2100 | 23.8 | 0 | {'AC': 150} |
| `pickup-notify` | 3 | 923 | 1633 | 764 | 122 | 1662 | 30.1 | 0 | {'AC': 150} |

## E3. Cách client nhận kết quả: WebSocket vs polling

| cấu hình | n | e2e p50 | e2e p95 | request sau khi nộp | byte sau khi nộp | message WS | transaction DB | tuple DB đọc | lỗi HTTP | verdict |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `delivery-ws` | 3 | 925 | 1649 | 50 | 134467 | 250 | 250 | 43395 | 0 | {'AC': 150} |
| `delivery-poll1s` | 3 | 1097 | 2093 | 118 | 117836 | 0 | 689 | 99972 | 0 | {'AC': 150} |
| `delivery-poll250ms` | 3 | 1094 | 1842 | 237 | 213831 | 0 | 626 | 91969 | 0 | {'AC': 150} |

## Đăng nhập và nhận bài khi cả lớp thao tác cùng lúc

| cấu hình | n | đăng nhập p50 | đăng nhập p95 | nộp 202 p50 | nộp 202 p95 | lỗi HTTP | verdict |
|---|---:|---:|---:|---:|---:|---:|---|
| `workers-c1` | 3 | 247 | 262 | 81 | 92 | 0 | {'AC': 150} |
| `workers-c2` | 3 | 240 | 261 | 83 | 93 | 0 | {'AC': 150} |
| `workers-c4` | 3 | 240 | 258 | 83 | 96 | 0 | {'AC': 150} |
| `workers-c8` | 3 | 252 | 271 | 80 | 94 | 0 | {'AC': 150} |
| `workers-2x4` | 3 | 252 | 268 | 81 | 93 | 0 | {'AC': 150} |
| `delivery-ws` | 3 | 265 | 297 | 78 | 89 | 0 | {'AC': 150} |
| `delivery-poll1s` | 3 | 249 | 272 | 77 | 87 | 0 | {'AC': 150} |
| `delivery-poll250ms` | 3 | 243 | 266 | 75 | 85 | 0 | {'AC': 150} |

## E4. Khởi động lại toàn bộ stack khi đang chấm

| cấu hình | n | e2e p50 | e2e p95 | makespan | thông lượng | lỗi HTTP | verdict |
|---|---:|---:|---:|---:|---:|---:|---|
| `restart-midway` | 1 | 4090 | 6089 | 68091 | 0.7 | 0 | {'AC': 50} |

## Môi trường và giới hạn của phép đo

- Một máy: Intel i5-12400F (6 nhân/12 luồng), 15 GiB RAM, Ubuntu 24.04, Docker 29.8; PostgreSQL 16, API, worker chạy bằng Docker Compose. **Không phải** laptop i3 dự kiến dùng khi thi — cần đo lại trên máy thật.
- Client giả lập (`cmd/loadtest`) chạy **trên cùng máy**, nối qua loopback: độ trễ mạng LAN thật (Wi-Fi/switch) **không** có trong số liệu.
- Bài nộp là chương trình a+b nhỏ: thời gian chấm chủ yếu là biên dịch `-static`. Bài nặng hơn (TLE, nhiều test lớn) sẽ làm hàng đợi dài hơn.
- CPU/RAM lấy mẫu bằng `docker stats` (~1 mẫu/giây); lần chạy ngắn hơn ~1,5 s có thể không bắt được đỉnh (ví dụ dòng `workers-c8` ghi 0). CPU % tính theo 1 luồng = 100 %.
- Byte = thân request/response + ước lượng 400 byte header mỗi lượt HTTP; với WebSocket = độ dài message + 4 byte header frame. Byte và request tính cho phần **sau khi nộp** (gồm cả 50 request nộp bài).

## Đọc kết quả

- **E1 – thêm slot chấm:** thông lượng tăng gần tuyến tính 1 → 2 → 4 slot (11 → 21,5 → 35 bài/s), sau đó bão hòa ở 8 slot (~42 bài/s) do CPU: thời gian chấm mỗi bài tăng 85 → 164 ms vì các bài tranh CPU. 2 container × 4 slot cho kết quả tương đương 1 container × 8 slot → giới hạn là CPU của máy, không phải cách chia container.
- **E2 – worker được báo bằng NOTIFY:** so với worker chỉ polling 1 s, thời gian chờ hàng đợi p50 giảm ~37 % (1203 → 764 ms), e2e p50 giảm ~32 % (1362 → 923 ms), thông lượng tăng ~26 %.
- **E3 – WebSocket vs polling:** WebSocket cần **ít request hơn 2,4–4,7 lần** (50 so với 118/237) và **ít transaction DB hơn ~2,6 lần** (250 so với 626–689), e2e p50 thấp hơn ~16 %. Byte thì **không ít hơn** polling 1 s trong thí nghiệm này: WebSocket đẩy 3 trạng thái/bài (queued, judging, completed) còn bài a+b chấm xong chỉ sau 1–2 lượt poll. Khi bài chấm lâu hơn (hàng đợi dài), số lượt poll tăng theo thời gian chờ còn số message WebSocket giữ nguyên.
- **Đăng nhập cả lớp cùng lúc:** p95 < 300 ms; CPU của API lên ~340 % do bcrypt (cost 10) cho 50 lần đăng nhập đồng thời. Nộp bài (lưu file + transaction) p95 < 100 ms. Không có lỗi HTTP nào trong 31 lần chạy.
- **E4 – khởi động lại toàn bộ stack khi đang chấm:** cả 50 bài vẫn được chấm (50 AC). 49 bài xong ở lần thử 1; 1 bài đang chấm dở lúc khởi động lại được phục hồi khi lease 60 s hết hạn và chấm lại ở lần thử 2 (chờ 67 s) — đó là nguyên nhân makespan 68 s.

