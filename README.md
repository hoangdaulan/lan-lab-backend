# LAN Judge — hệ thống thi và chấm bài C++ qua mạng LAN

Hệ thống cho phòng máy: giáo viên tạo đề, phiên thi, bộ test; sinh viên đăng nhập bằng mã sinh viên, làm bài C++17 ngay trên trình duyệt, nộp và thấy kết quả chấm (`15/15 AC`, `9/10 TLE`…) theo thời gian thực. Giáo viên theo dõi bảng kết quả trực tiếp, xem màn hình sinh viên, gửi thông báo và chấm lại khi sửa test.

Gồm ba thành phần chạy bằng Docker Compose trên một máy chủ trong LAN:

- **api** (Go): REST + WebSocket + giao diện web, cổng `8080` (HTTP) và `8443` (HTTPS).
- **worker** (Go): biên dịch và chạy bài trong sandbox (không mạng, giới hạn thời gian/bộ nhớ).
- **postgres** (PostgreSQL 16): dữ liệu và hàng đợi chấm.

Mã nguồn bài nộp và file test nằm trong thư mục `backend/data` trên máy chủ.

## Mục lục

1. [Yêu cầu](#1-yêu-cầu)
2. [Cài đặt](#2-cài-đặt)
3. [Mạng LAN và tường lửa](#3-mạng-lan-và-tường-lửa)
4. [Hướng dẫn cho giáo viên](#4-hướng-dẫn-cho-giáo-viên)
5. [Hướng dẫn cho sinh viên](#5-hướng-dẫn-cho-sinh-viên)
6. [Vận hành thường ngày](#6-vận-hành-thường-ngày)
7. [Xử lý sự cố](#7-xử-lý-sự-cố)
8. [Tài liệu thêm](#8-tài-liệu-thêm)

---

## 1. Yêu cầu

Máy chủ (máy của giáo viên hoặc một máy trong phòng):

- Linux (đã thử trên Ubuntu), Docker Engine và Docker Compose v2 (`docker compose version`). Windows (qua WSL2) và macOS chạy được bằng Docker Desktop, xem [mục 2.5](#25-chạy-máy-chủ-trên-windows-hoặc-macos).
- `git`, `make`.
- Không bắt buộc: Node.js 20+ (chỉ khi sửa giao diện), Go 1.26 (chỉ khi chạy công cụ dòng lệnh như `lannet`, `wsprobe`).

Máy sinh viên/giáo viên: Chrome, Edge hoặc Firefox bản mới, cùng mạng LAN với máy chủ.

## 2. Cài đặt

### 2.1. Lấy mã nguồn

```bash
git clone https://github.com/hoangdaulan/lan-lab
cd lan-lab/backend
```

Mọi lệnh `make` và `docker compose` bên dưới chạy trong thư mục `backend/`.

### 2.2. Tạo file `.env`

```bash
cp .env.example .env
```

Mở `.env` và sửa ít nhất các dòng sau (file này đã nằm trong `.gitignore`, không commit):

| Biến | Ý nghĩa | Gợi ý |
| --- | --- | --- |
| `POSTGRES_PASSWORD` | Mật khẩu PostgreSQL | Chuỗi ngẫu nhiên, ví dụ `openssl rand -hex 16` |
| `JWT_SECRET` | Khóa ký token đăng nhập | `openssl rand -hex 32` |
| `TLS_HOSTS` | IP LAN của máy chủ, ghi vào chứng chỉ HTTPS | Ví dụ `192.168.1.9` (xem `ip -4 addr`) |
| `APP_UID`, `APP_GID` | Chủ sở hữu thư mục `data/` | Đặt bằng `id -u` và `id -g` để sửa file test không cần `sudo` |
| `DISPATCH_TOKEN` | Khóa chung giữa api và worker | `openssl rand -hex 16` |
| `WORKER_CONCURRENCY` | Số bài chấm song song | Mặc định `2`; tăng nếu máy nhiều nhân |

Các biến khác (`API_PORT=8080`, `API_HTTPS_PORT=8443`, `DB_PORT=5433`) giữ nguyên nếu không trùng cổng.

> Nếu giữ `APP_UID=10001`, cần cấp quyền thư mục dữ liệu: `sudo chown -R 10001:10001 data`.

### 2.3. Khởi động, tạo bảng và tài khoản giáo viên đầu tiên

```bash
make up
```

Lệnh này tạo thư mục `data/`, build image và chạy `postgres`, `api`, `worker` ở nền. Lần đầu mất vài phút vì phải tải image và cài g++.

```bash
make migrate
```

Tạo các bảng trong cơ sở dữ liệu (chạy lại sau mỗi lần cập nhật mã nguồn; an toàn khi chạy nhiều lần).

```bash
SEED_TEACHER_CODE=GV01 SEED_TEACHER_FULLNAME="Nguyễn Văn A" SEED_TEACHER_PASSWORD='<mật-khẩu-của-bạn>' make seed
```

Tạo tài khoản giáo viên đầu tiên. Thay `GV01`, họ tên và mật khẩu theo ý bạn; truyền mật khẩu trên dòng lệnh như trên, đừng lưu vào `.env`.

Kiểm tra các container đang chạy:

```bash
docker compose ps
```

```bash
docker compose logs -f api
```

### 2.4. Mở giao diện web

- Trên trình duyệt: `https://<IP-máy-chủ>:8443/` (ví dụ `https://192.168.1.9:8443/`).
- Chứng chỉ là tự ký nên trình duyệt sẽ cảnh báo lần đầu: chọn **Nâng cao → Tiếp tục truy cập**. Mỗi máy chỉ cần làm một lần.
- `http://<IP>:8080` sẽ tự chuyển sang HTTPS. Phải dùng HTTPS vì trình duyệt chỉ cho chia sẻ màn hình trên kết nối bảo mật.
- Swagger UI (thử API): `http://<IP>:8080/swagger/index.html`.

Chứng chỉ được tạo trong `data/tls` khi api khởi động. Nếu đổi IP máy chủ, sửa `TLS_HOSTS` trong `.env` rồi chạy lại `make up`.

### 2.5. Chạy máy chủ trên Windows hoặc macOS

Hệ thống được phát triển và kiểm thử trên Linux; với kỳ thi thật nên dùng máy chủ Linux. Trên Windows và macOS, Docker Desktop chạy các container trong một máy ảo Linux nên vẫn chạy được, với các khác biệt dưới đây (chưa kiểm thử đầy đủ trên hai hệ này).

#### Windows (qua WSL2)

1. Cài [Docker Desktop](https://www.docker.com/products/docker-desktop/), bật tùy chọn **Use the WSL 2 based engine**.
2. Cài Ubuntu cho WSL (PowerShell quyền quản trị), khởi động lại máy nếu được yêu cầu:

   ```powershell
   wsl --install -d Ubuntu
   ```

3. Trong Docker Desktop: **Settings → Resources → WSL integration**, bật cho Ubuntu.
4. Mở terminal **Ubuntu** và cài công cụ:

   ```bash
   sudo apt update && sudo apt install -y git make
   ```

5. Clone vào thư mục của WSL (ví dụ `~/lan-lab`), **không** clone vào `C:\` hay `/mnt/c/...`: ổ NTFS không giữ quyền file Linux, mà worker cần quyền đó để chặn bài sinh viên đọc file đáp án.

   ```bash
   cd ~ && git clone https://github.com/hoangdaulan/lan-lab && cd lan-lab/backend
   ```

6. Làm tiếp như mục 2.2–2.4, tất cả lệnh chạy trong terminal Ubuntu. `TLS_HOSTS` là IP LAN của **máy Windows** (xem bằng `ipconfig` trong PowerShell), không phải IP của WSL.
7. Mở cổng cho máy khác trong LAN bằng Windows Defender Firewall (PowerShell quyền quản trị), thay cho `ufw` ở mục 3:

   ```powershell
   New-NetFirewallRule -DisplayName "LAN Judge" -Direction Inbound -Protocol TCP -LocalPort 8080,8443 -Action Allow
   ```

#### macOS

1. Cài [Docker Desktop](https://www.docker.com/products/docker-desktop/) (bản cho Apple Silicon hoặc Intel tùy máy) và mở nó lên.
2. Cài `git` và `make`:

   ```bash
   xcode-select --install
   ```

3. Làm như mục 2.1–2.4. Trong `.env`:
   - `APP_UID`/`APP_GID` đặt bằng kết quả của `id -u` và `id -g` (thường là `501` và `20`).
   - `TLS_HOSTS` là IP LAN của máy Mac, xem bằng `ipconfig getifaddr en0` (Wi-Fi) hoặc trong **System Settings → Network**.
4. Nếu bật tường lửa macOS (**System Settings → Network → Firewall**), cho phép Docker nhận kết nối đến khi được hỏi, để máy khác truy cập được cổng 8080/8443.

Lưu ý: chia sẻ thư mục của Docker Desktop trên macOS không đảm bảo quyền file giống Linux, nên việc chặn bài sinh viên đọc thư mục `data/` có thể không chặt bằng trên máy chủ Linux.

#### Giới hạn chung trên Windows và macOS

- Tính năng UDP ở mục 3 (`docker-compose.lan.yml`, broadcast 9999 và multicast 9998) cần mạng host thật, mà Docker Desktop không có, nên gần như chắc chắn gói UDP không ra được LAN. Giao diện web, chấm bài, WebSocket, thông báo và xem màn hình vẫn hoạt động bình thường qua cổng 8080/8443.
- Thời gian chạy bài trong máy ảo có thể chậm hơn chạy trên Linux thật; nếu thấy TLE bất thường, nới giới hạn thời gian của bài.

## 3. Mạng LAN và tường lửa

Mở các cổng cho máy khác trong phòng truy cập (Ubuntu dùng `ufw`; Windows/macOS xem mục 2.5):

```bash
sudo ufw allow 8080/tcp
```

```bash
sudo ufw allow 8443/tcp
```

Như vậy là đủ để dùng giao diện web.

### Tính năng UDP (không bắt buộc)

Hệ thống có thêm hai tính năng UDP viết tay (chi tiết trong `backend/docs/net-protocols.md`):

- **Tìm máy chủ bằng broadcast**, cổng UDP `9999`: máy sinh viên gửi gói dò, máy chủ trả lời địa chỉ của nó.
- **Phát thông báo qua multicast**, nhóm `239.255.42.99:9998`: thông báo của giáo viên được phát thêm qua UDP multicast.

Gói broadcast/multicast không đi qua mạng NAT của Docker, nên muốn dùng thì api phải chạy ở chế độ mạng host bằng file `docker-compose.lan.yml`:

```bash
sudo ufw allow 9999/udp
```

```bash
sudo ufw allow 9998/udp
```

```bash
docker compose -f docker-compose.yml -f docker-compose.lan.yml up -d --build
```

Có thể đặt thêm trong `.env`: `ADVERTISE_HOST=<IP-máy-chủ>` (mặc định lấy IP đầu tiên trong `TLS_HOSTS`). Ở chế độ này `DISCOVERY_PORT` mặc định là `9999` và `ANNOUNCE_MULTICAST_ADDR` mặc định là `239.255.42.99:9998`.

Kiểm tra từ một máy trong LAN (cần Go):

```bash
go run ./cmd/lannet discover
```

```bash
go run ./cmd/lannet announcements
```

## 4. Hướng dẫn cho giáo viên

### 4.1. Đăng nhập

1. Mở `https://<IP>:8443/`.
2. Nhập **mã tài khoản** (ví dụ `GV01` đã tạo bằng `make seed`) và mật khẩu.
3. Thanh điều hướng có ba mục: **Đề thi**, **Sinh viên**, **Hàng đợi chấm**. Đổi mật khẩu ở menu tài khoản góc trên (mục **Đổi mật khẩu**).

### 4.2. Thêm sinh viên

Vào **Sinh viên**.

**Thêm từng người:** điền **MSV**, **Họ tên**, **Ngày sinh** (dạng `dd/mm/yyyy`) rồi bấm **Thêm**.

**Nhập từ file CSV:** chọn file rồi bấm **Nhập CSV**. File cần dòng tiêu đề đúng như sau (mã hóa UTF-8):

```csv
full_name,code,date_of_birth
Nguyễn Văn An,SV001,05/09/2004
Trần Thị Bình,SV002,12/01/2004
```

- Ngày sinh nên viết `dd/mm/yyyy`; hệ thống cũng nhận `d/m/yyyy` và `yyyy-mm-dd`.
- Nếu có dòng lỗi (kể cả thiếu ngày sinh) thì **không dòng nào được lưu**, và trang sẽ liệt kê dòng lỗi để sửa.
- MSV đã tồn tại sẽ bị bỏ qua (không ghi đè).
- File mẫu: `backend/examples/students-import.csv`.

**Mật khẩu ban đầu** của sinh viên là ngày sinh dạng `ddmmyyyy` (ví dụ `05/09/2004` → `05092004`). Sinh viên quên mật khẩu thì tìm theo MSV/họ tên và bấm **Đặt lại mật khẩu** để đưa về ngày sinh.

### 4.3. Tạo đề thi và bài tập

1. Vào **Đề thi**, nhập **Tên đề**, **Mô tả**, bấm **Tạo đề thi**. Trang chi tiết đề sẽ mở ra.
2. Ở mục **Bài tập**, bấm **+ Thêm bài** và điền:
   - **Mã bài** (ví dụ `A`), **Tên bài**, **Đề bài** (văn bản).
   - **Giới hạn thời gian (ms)** (mặc định 1000) và **Giới hạn bộ nhớ (MB)** (mặc định 256).
3. Sau khi lưu, cột **Thư mục test trên máy chủ** hiện đường dẫn dạng `./data/problems/<id>/tests`.

### 4.4. Thêm file test

Test là các cặp file `N.in` / `N.out` đặt trực tiếp trên máy chủ, trong thư mục của bài (tính từ `backend/`):

```text
backend/data/problems/<id>/tests/
├── 1.in
├── 1.out
├── 2.in
├── 2.out
└── ...
```

Ví dụ chép bộ test từ máy chủ:

```bash
cp ~/bo-test-bai-A/*.in ~/bo-test-bai-A/*.out data/problems/1/tests/
```

Quy tắc:

- Mỗi `N.in` phải có `N.out` tương ứng và ngược lại; không dùng thư mục con hay symlink.
- Kết quả so khớp theo từng token (bỏ qua khác biệt khoảng trắng/xuống dòng).
- Chép xong, quay lại trang đề, bấm **Quét test & chấm lại** ở dòng của bài. Cột **Test** sẽ hiện số test đang dùng; nút **Test** cho xem danh sách.

Nếu thư mục sai quy tắc, hệ thống báo lỗi và **không thay đổi gì**.

### 4.5. Tạo phiên thi

1. Trong trang đề, mục **Phiên thi**, bấm **+ Thêm phiên**.
2. Đặt **Tên phiên** (ví dụ `Ca 1`), giờ **Bắt đầu** và **Kết thúc**. Máy chủ dùng giờ của nó để mở/đóng nộp bài, nên hãy đảm bảo giờ máy chủ đúng.
3. Bấm **Mở bảng** để vào trang điều hành phiên.

Sinh viên chỉ vào làm bài được trong khung giờ của phiên.

### 4.6. Điều hành phiên thi

Trang phiên có năm tab và nút **Xuất CSV** (tải bảng điểm, mở được bằng Excel).

**Bảng trực tiếp:** mỗi dòng là một sinh viên, mỗi cột là một bài. Ô hiện `AC (số lần nộp)`, `đang chấm…` hoặc kết quả gần nhất (ví dụ `9/10 TLE`); cột cuối là số bài AC. Bảng tự cập nhật qua WebSocket, chấm xanh cạnh MSV nghĩa là sinh viên đang mở trang. Bấm vào một ô để xem các bài nộp của sinh viên đó cho bài đó.

**Màn hình SV:** xem trực tiếp màn hình sinh viên.

1. Chọn kích thước lưới **1 / 4 / 9 / 15** ô. Lưới càng lớn thì mỗi màn hình càng ít khung hình để máy giáo viên không bị quá tải:

   | Lưới | Khung/giây mỗi màn hình |
   | ---: | ---: |
   | 1 | 15 |
   | 4 | 10 |
   | 9 | 5 |
   | 15 | 3 |

2. Bấm **Xem màn hình** ở dòng sinh viên đang chia sẻ để đưa vào lưới (hoặc **Xem tất cả** để mở các màn hình đang chia sẻ cho tới khi đầy lưới; **Đóng tất cả** để xóa lưới). Lưới đầy thì phải đóng bớt ô hoặc chọn lưới lớn hơn.
3. Nhấp đúp một ô để phóng to (15 khung/giây). Bấm **Đóng** để bỏ một ô.

Video đi thẳng từ máy sinh viên tới máy giáo viên trong LAN (WebRTC), máy chủ chỉ chuyển tín hiệu kết nối. Sinh viên tắt chia sẻ thì trang làm bài của họ bị khóa và giáo viên thấy trạng thái "Chưa/đã tắt chia sẻ".

**Bài nộp:** danh sách mọi bài nộp, lọc theo sinh viên và bài. Bấm một dòng để xem chi tiết: mã nguồn, thông báo biên dịch, kết quả từng test (thời gian, bộ nhớ). Nút **Chấm lại bài này** để chấm lại riêng một bài.

**Người tham gia:** ai đang làm, ai đã kết thúc, giờ vào và giờ kết thúc.

**Thông báo:** nhập nội dung (ví dụ "Còn 10 phút") và bấm **Gửi tới cả phiên**. Sinh viên đang mở trang thấy ngay. Thông báo chỉ lưu trong bộ nhớ của api, sẽ mất khi khởi động lại api.

### 4.7. Chấm lại

- **Sửa test:** sửa/thêm/xóa file trong `data/problems/<id>/tests/`, rồi bấm **Quét test & chấm lại**. Bài tăng version và **mọi bài đã nộp** của bài đó được chấm lại theo test mới.
- **Đổi giới hạn thời gian/bộ nhớ:** bấm **Sửa** ở dòng bài, lưu lại; hệ thống tự chấm lại mọi bài đã nộp của bài đó.
- **Một bài nộp:** mở chi tiết bài nộp, bấm **Chấm lại bài này**.

Có thể chấm lại cả khi phiên đang diễn ra; bảng trực tiếp tự cập nhật khi có kết quả mới.

### 4.8. Hàng đợi chấm

Trang **Hàng đợi chấm** cho biết số bài đang chờ, đang chấm, đã xong, worker đang giữ bài, bài chờ lâu nhất và thời gian chờ/chấm trung bình trong 10 phút gần nhất. Trang tự làm mới mỗi 3 giây.

### 4.9. Xóa

- Xóa bài: chỉ được khi bài chưa có bài nộp.
- Xóa phiên: chỉ được khi phiên chưa có bài nộp.
- Xóa đề: chỉ được khi đề không còn bài và phiên.

## 5. Hướng dẫn cho sinh viên

1. **Mở trang:** `https://<IP-máy-chủ>:8443/` (giáo viên sẽ cho địa chỉ). Lần đầu chấp nhận cảnh báo chứng chỉ.
2. **Đăng nhập:** mã tài khoản là **MSV**, mật khẩu ban đầu là **ngày sinh dạng `ddmmyyyy`** (sinh ngày 05/09/2004 thì mật khẩu là `05092004`). Nên đổi mật khẩu ở menu tài khoản → **Đổi mật khẩu**.
3. **Chọn phiên thi:** trang **Phiên thi** liệt kê phiên Đang diễn ra / Sắp diễn ra / Đã kết thúc. Bấm **Vào làm bài** ở phiên đang diễn ra (lần sau là **Tiếp tục làm bài**).
4. **Chia sẻ màn hình (bắt buộc):** bấm **Chia sẻ toàn màn hình**, chọn **Toàn bộ màn hình** (Entire screen, không chọn cửa sổ hay tab) rồi bấm **Chia sẻ**. Nếu dừng chia sẻ, trang làm bài bị khóa cho tới khi chia sẻ lại.
5. **Làm bài:**
   - Chọn bài ở thanh bên hoặc dùng **Bài trước** / **Bài tiếp theo**. Đề bài hiện bên trái.
   - Viết code C++17 trong ô soạn thảo (bản nháp tự lưu trên trình duyệt), hoặc bấm **Tải lên .cpp** để chọn file. Mã nguồn tối đa 64 KB, chỉ một file `.cpp`.
   - Bấm **Nộp bài** hoặc nhấn `Ctrl + Enter` (`⌘ + Enter` trên Mac).
6. **Xem kết quả:** kết quả hiện ngay khi chấm xong, không cần tải lại trang: `Đang chờ` → `Đang chấm` → ví dụ `15/15 AC`. Ý nghĩa:
   - **AC**: đúng tất cả test. **WA**: sai đáp án. **TLE**: quá thời gian. **MLE**: quá bộ nhớ. **RE**: lỗi khi chạy. **CE**: lỗi biên dịch (có thông báo của trình biên dịch).
   - Hệ thống dừng ở test sai đầu tiên, nên `9/10 TLE` nghĩa là qua 9 test rồi test thứ 10 bị quá giờ.
   - Có thể nộp nhiều lần; danh sách bài nộp của bài nằm ở vùng kết quả, bấm **Xem mã nguồn** để xem lại.
7. **Thông báo:** thông báo của giáo viên hiện trên đầu trang làm bài; bấm **Tất cả thông báo** để xem lại.
8. **Kết thúc:** bấm **Kết thúc** khi làm xong. Sau khi kết thúc **không nộp thêm được nữa**. Hết giờ phiên thì hệ thống cũng ngừng nhận bài.

Phím tắt và hướng dẫn: nút **Hướng dẫn và phím tắt** (biểu tượng dấu hỏi) trên thanh trên cùng.

## 6. Vận hành thường ngày

Chạy trong `backend/`:

| Việc | Lệnh |
| --- | --- |
| Khởi động / cập nhật sau khi `git pull` | `make up` rồi `make migrate` |
| Dừng hệ thống (dữ liệu vẫn còn) | `make down` |
| Xem log | `docker compose logs -f api worker` |
| Sao lưu DB và `data/` vào `backups/<thời-điểm>/` | `make backup` |
| Kiểm tra bản sao lưu (không đụng dữ liệu thật) | `scripts/restore.sh backups/<thời-điểm> --verify` |
| Khôi phục bản sao lưu (ghi đè DB và `data/`) | `scripts/restore.sh backups/<thời-điểm> --yes` |
| Chạy unit test | `make test` (cần Go) |
| Kiểm tra sandbox chấm bài | `make test-sandbox` |

Sửa giao diện (thư mục `web/src` ở gốc repo): chạy `npm install` rồi `npm run build` ở gốc repo, sau đó `make up` để build lại api (giao diện được nhúng vào binary).

## 7. Xử lý sự cố

- **Máy sinh viên không mở được trang:** kiểm tra cùng mạng (`ping <IP>`), đã mở cổng 8080/8443 trên `ufw`, và `docker compose ps` thấy `api` đang chạy.
- **Không chia sẻ màn hình được / báo "Trình duyệt không chia sẻ màn hình được":** phải mở bằng `https://<IP>:8443/`, không phải `http://`, trên Chrome/Edge/Firefox mới.
- **Trình duyệt báo chứng chỉ không khớp:** `TLS_HOSTS` chưa có IP hiện tại của máy chủ. Sửa `.env` rồi `make up`.
- **Bài nộp đứng ở "Đang chờ":** xem `docker compose logs worker` và trang **Hàng đợi chấm**. Bài không bị mất khi worker khởi động lại; nó sẽ được chấm lại.
- **Bài báo "Lỗi hệ thống" hoặc test không quét được:** kiểm tra quyền thư mục `data/` khớp với `APP_UID`/`APP_GID` trong `.env`, và các file `N.in`/`N.out` đủ cặp.
- **Lỗi `set POSTGRES_USER in .env` khi chạy `make`:** chưa tạo `.env` hoặc đang đứng sai thư mục; chạy lệnh trong `backend/`.

## 8. Tài liệu thêm

- `backend/BACKEND_CONTEXT.md`: thiết kế backend chi tiết.
- `backend/docs/api-v1.md`, Swagger UI tại `/swagger/index.html`: REST và WebSocket.
- `backend/docs/net-protocols.md`: giao thức TCP dispatch và UDP discovery/multicast.
- `backend/docs/DEMO.md`: kịch bản demo.
- `backend/examples/api-manual.http`: ví dụ gọi API.
