# Hướng dẫn nộp bài Project

## 1. Nộp báo cáo trên Compilatio

Mỗi nhóm **chỉ nộp 01 file báo cáo** trên Compilatio.

Để thuận tiện cho việc phân biệt các nhóm, **tên file bắt buộc** theo format:

`NhomXX_TenProject_Report.docx`

Trong đó:

* `XX`: số nhóm, ví dụ `01`, `02`, `03`, ...
* `TenProject`: tên viết ngắn gọn của project, không dấu.
* File báo cáo **nên nộp ở định dạng `.docx`** để hệ thống xử lý và nhận diện nội dung ổn định hơn.

**Ví dụ:**

```text
Nhom01_NetworkedAutoGrader_Report.docx
Nhom02_OpticalNetworkControl_Report.docx
Nhom03_MultiplayerChess_Report.docx
```

Báo cáo phải được kiểm tra trên Compilatio và đáp ứng yêu cầu về mức độ tương đồng theo quy định của môn học.

---

## 2. Nộp Source Code

Mỗi nhóm chuẩn bị **01 thư mục Source Code hoàn chỉnh**, có thể chạy được và chứa đầy đủ các thành phần cần thiết.

Tên thư mục:

`NhomXX_TenProject_Source`

Ví dụ:

```text
Nhom01_NetworkedAutoGrader_Source/
Nhom02_OpticalNetworkControl_Source/
```

Source code cần bao gồm:

* Source code.
* Configuration files.
* Database/schema hoặc dữ liệu mẫu nếu cần.
* Dependencies hoặc file hướng dẫn cài đặt.
* `README.md` hướng dẫn build/run/test.
* Các tài nguyên cần thiết để chạy project.

Không nộp source code chỉ để **tham khảo hoặc không thể chạy được**.

---

## 3. Kiểm tra và bàn giao cuối cùng

Sau khi hoàn thành, mỗi nhóm chuẩn bị:

```text
NhomXX_TenProject/
├── Source/
└── Report/
```

Trong đó:

* `Source/`: toàn bộ source code và tài liệu cần thiết để chạy project.
* `Report/`: báo cáo cuối cùng đã nộp trên Compilatio.

**Lớp trưởng sẽ thu thập toàn bộ Source Code và Report của các nhóm vào 01 USB** để bàn giao cho giảng viên.

Trước khi bàn giao, lớp trưởng/phụ trách nhóm cần phối hợp kiểm tra:

* Đúng tên nhóm.
* Đúng project.
* Source code đầy đủ.
* Project có thể build/run theo hướng dẫn.
* Báo cáo đúng phiên bản cuối cùng.
* Các thành viên thực sự tham gia và hiểu phần việc của mình.
* Không sử dụng source code của nhóm khác hoặc tài liệu không được phép.
* Không có tình trạng **nộp nhầm, nộp thiếu, nộp file rỗng, nộp project không chạy được hoặc khai báo sai về sản phẩm**.

Việc kiểm tra của lớp trưởng **không thay thế việc đánh giá của giảng viên**. Giảng viên có thể yêu cầu nhóm chạy demo, giải thích source code hoặc kiểm tra lại project khi cần.

---

## 4. Deadline

**Hạn cuối: 23:59 – 31/10/2026.**

Sau thời hạn trên, mọi thay đổi đối với source code hoặc báo cáo phải được giảng viên xác nhận.
