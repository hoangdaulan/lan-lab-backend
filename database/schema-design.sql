-- BAN THIET KE DE DUYET. Chua phai migration da trien khai.
-- PostgreSQL; moi thoi diem la timestamptz; thoi gian hop le: starts_at <= received_at < ends_at.
-- Code tai khoan va code bai phan biet hoa/thuong; backend chuan hoa truoc khi luu.
-- Duong dan la duong dan tuong doi do server sinh, khong nhan duong dan tuy y tu client.
-- updated_at duoc backend cap nhat, khong co trigger tu dong trong ban thiet ke nay.

BEGIN;

CREATE TABLE teachers (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    fullname varchar(200) NOT NULL CHECK (btrim(fullname) <> ''),
    code varchar(50) NOT NULL UNIQUE CHECK (btrim(code) <> ''),
    password_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE students (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    fullname varchar(200) NOT NULL CHECK (btrim(fullname) <> ''),
    code varchar(50) NOT NULL UNIQUE CHECK (btrim(code) <> ''),
    password_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE exams (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    teacher_id bigint NOT NULL REFERENCES teachers(id) ON DELETE RESTRICT,
    title varchar(255) NOT NULL CHECK (btrim(title) <> ''),
    description text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE problems (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_id bigint NOT NULL REFERENCES exams(id) ON DELETE RESTRICT,
    code varchar(20) NOT NULL CHECK (btrim(code) <> ''),
    title varchar(255) NOT NULL CHECK (btrim(title) <> ''),
    statement text NOT NULL DEFAULT '',
    time_limit_ms integer NOT NULL CHECK (time_limit_ms > 0),
    memory_limit_mb integer NOT NULL CHECK (memory_limit_mb > 0),
    version integer NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (exam_id, code)
);

CREATE TABLE test_cases (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    problem_id bigint NOT NULL REFERENCES problems(id) ON DELETE RESTRICT,
    input text NOT NULL CHECK (btrim(input) <> ''),
    output text NOT NULL CHECK (btrim(output) <> '')
);

CREATE TABLE exam_sessions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_id bigint NOT NULL REFERENCES exams(id) ON DELETE RESTRICT,
    name varchar(255) NOT NULL CHECK (btrim(name) <> ''),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (ends_at > starts_at)
);

CREATE TABLE exam_participants (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_session_id bigint NOT NULL REFERENCES exam_sessions(id) ON DELETE RESTRICT,
    student_id bigint NOT NULL REFERENCES students(id) ON DELETE RESTRICT,
    status varchar(20) NOT NULL DEFAULT 'in_progress'
        CHECK (status IN ('in_progress', 'finished')),
    joined_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at timestamptz,
    UNIQUE (exam_session_id, student_id),
    CHECK (
        (status = 'in_progress' AND finished_at IS NULL)
        OR (status = 'finished' AND finished_at IS NOT NULL AND finished_at >= joined_at)
    )
);

-- Access token JWT khong luu trong DB. Refresh token la chuoi ngau nhien;
-- chi luu hash, khong luu token goc. Loai tai khoan duoc xac dinh khi dang nhap.
CREATE TABLE refresh_tokens (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    teacher_id bigint REFERENCES teachers(id) ON DELETE CASCADE,
    student_id bigint REFERENCES students(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    CHECK ((teacher_id IS NOT NULL) <> (student_id IS NOT NULL))
);

CREATE TABLE submissions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_participant_id bigint NOT NULL REFERENCES exam_participants(id) ON DELETE RESTRICT,
    problem_id bigint NOT NULL REFERENCES problems(id) ON DELETE RESTRICT,
    source_path text NOT NULL CHECK (btrim(source_path) <> ''),
    received_at timestamptz NOT NULL
);

CREATE TABLE judge_runs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    submission_id bigint NOT NULL REFERENCES submissions(id) ON DELETE RESTRICT,
    problem_version integer NOT NULL CHECK (problem_version >= 1),
    status varchar(20) NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'judging', 'completed', 'system_error', 'obsolete')),
    verdict varchar(10) CHECK (verdict IN ('AC', 'WA', 'TLE', 'MLE', 'CE', 'RE')),
    passed_tests integer NOT NULL DEFAULT 0,
    total_tests integer NOT NULL DEFAULT 0,
    compiler_info text,
    compile_output text,
    system_error text,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at timestamptz,
    finished_at timestamptz,
    UNIQUE (submission_id, problem_version),
    CHECK (passed_tests >= 0 AND total_tests >= passed_tests),
    CHECK (status <> 'completed' OR
        (verdict IS NOT NULL AND total_tests > 0 AND finished_at IS NOT NULL)),
    CHECK (verdict IS NULL OR status = 'completed'),
    CHECK (verdict IS DISTINCT FROM 'AC' OR passed_tests = total_tests),
    CHECK (verdict IS DISTINCT FROM 'CE' OR passed_tests = 0),
    CHECK (verdict IS NULL OR verdict IN ('AC', 'CE') OR passed_tests < total_tests),
    CHECK (started_at IS NULL OR started_at >= created_at),
    CHECK (finished_at IS NULL OR (started_at IS NOT NULL AND finished_at >= started_at))
);

CREATE TABLE submission_test_results (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    judge_run_id bigint NOT NULL REFERENCES judge_runs(id) ON DELETE RESTRICT,
    test_case_id bigint NOT NULL REFERENCES test_cases(id) ON DELETE CASCADE,
    verdict varchar(10) NOT NULL CHECK (verdict IN ('AC', 'WA', 'TLE', 'MLE', 'RE')),
    execution_time_ms integer NOT NULL CHECK (execution_time_ms >= 0),
    memory_used_kb bigint CHECK (memory_used_kb >= 0),
    UNIQUE (judge_run_id, test_case_id)
);

CREATE TABLE judge_jobs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    judge_run_id bigint NOT NULL UNIQUE REFERENCES judge_runs(id) ON DELETE RESTRICT,
    status varchar(20) NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'done', 'failed', 'obsolete')),
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    available_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    worker_id varchar(100),
    lease_token uuid,
    lease_expires_at timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (
        (status = 'running' AND worker_id IS NOT NULL
            AND lease_token IS NOT NULL AND lease_expires_at IS NOT NULL)
        OR (status <> 'running' AND worker_id IS NULL
            AND lease_token IS NULL AND lease_expires_at IS NULL)
    )
);

-- UNIQUE/PRIMARY KEY da tu tao index. Cac index bo sung theo truy van du kien:
CREATE INDEX exams_teacher_idx ON exams (teacher_id);
CREATE INDEX test_cases_problem_idx ON test_cases (problem_id, id);
CREATE INDEX exam_sessions_exam_idx ON exam_sessions (exam_id);
CREATE INDEX participants_student_idx ON exam_participants (student_id, exam_session_id);
CREATE INDEX refresh_tokens_teacher_idx ON refresh_tokens (teacher_id) WHERE teacher_id IS NOT NULL;
CREATE INDEX refresh_tokens_student_idx ON refresh_tokens (student_id) WHERE student_id IS NOT NULL;
CREATE INDEX refresh_tokens_expiry_idx ON refresh_tokens (expires_at);
CREATE INDEX submissions_participant_history_idx
    ON submissions (exam_participant_id, received_at DESC, id DESC);
CREATE INDEX submissions_participant_problem_idx
    ON submissions (exam_participant_id, problem_id);
CREATE INDEX submissions_problem_idx ON submissions (problem_id, id);
CREATE INDEX test_results_test_case_idx ON submission_test_results (test_case_id);
CREATE INDEX jobs_queue_idx ON judge_jobs (available_at, id) WHERE status = 'queued';
CREATE INDEX jobs_expired_lease_idx ON judge_jobs (lease_expires_at, id) WHERE status = 'running';

COMMIT;

-- CAC BAT BIEN LIEN BANG / QUY TAC TRANSACTION CAN BACKEND THUC HIEN:
-- CHECK/FK o tren khong tu bao dam cac quy tac duoi day.
-- Khong cho API doi chu so huu cua du lieu lich su: exam_id cua problem/session,
-- student_id/session_id cua participant, participant_id/problem_id cua submission.
-- Cac transaction lien quan dung cung thu tu khoa: problem -> exam_session
-- -> exam_participant -> judge_run -> judge_job (chi khoa nhung hang can thiet).
-- Khi khoa nhieu hang cung bang, khoa theo id tang dan.
--
-- 1. NHAN BAI:
--    Server ghi received_at ngay khi nhan du file; khong dung timestamp tu client
--    hay CURRENT_TIMESTAMP cua transaction duoc mo sau khi cho khoa.
--    Luu file ben vung truoc khi xac nhan thanh cong. Trong transaction:
--    - Khoa problem/session de khong doi version/khung gio trong luc kiem tra.
--    - Khoa participant FOR UPDATE (ket thuc lam bai cung khoa hang nay).
--    - Kiem tra student tu JWT dung participant; status = in_progress;
--      session.exam_id = problem.exam_id;
--      starts_at <= received_at < ends_at; bo test khong rong.
--    - Tao submission + judge_run tai version hien tai + judge_job; COMMIT.
--    Chi tra thanh cong sau COMMIT. File duoc nhan truoc han van duoc chap nhan
--    neu transaction xu ly sau han, mien participant chua ket thuc khi ghi nhan.
--    Hai thao tac nop/ket thuc duoc phan thu tu bang khoa participant;
--    thao tac giu khoa va commit truoc quyet dinh, khong theo thu tu click tren FE.
--    Loi DB: khong xac nhan nhan bai; don file mo coi sau, co khoang dem de
--    khong xoa file cua request dang xu ly. DB va file can duoc sao luu cung nhau.
--
-- 2. KET THUC LAM BAI:
--    Khoa participant FOR UPDATE; chuyen in_progress -> finished va gan finished_at.
--    Yeu cau lap lai tra trang thai finished; khong cho mo lai bang API sinh vien.
--    Lich su/ket qua van duoc doc; bai da nhan van tiep tuc cham.
--
-- 3. THAY BO TEST:
--    Ghi file test moi vao cac duong dan moi, khong ghi de file dang duoc doc.
--    Khoa problem FOR UPDATE. Trong CUNG transaction:
--    - Xoa test cu (CASCADE xoa ket qua tung test cu), chen bo test moi khong rong.
--    - Tang version dung mot lan, danh dau runs/jobs cu la obsolete.
--      Khi doi run thanh obsolete, xoa verdict de thoa CHECK;
--      khi doi job thanh obsolete, xoa worker_id/lease_token/lease_expires_at.
--    - Tao run + job cho MOI submission cua problem, ke ca phien da ket thuc.
--    - COMMIT; chi sau do cho phep don file test cu mot cach an toan.
--    Worker tao ban sao rieng cua test, version va gioi han trong mot snapshot
--    nhat quan (giu khoa SHARE problem trong luc copy, roi tha khoa truoc khi chay).
--    Khong giu khoa DB trong suot thoi gian bien dich/chay code.
--    De xuat: doi gioi han thoi gian/bo nho cung tang version va cham lai;
--    sua ten/noi dung/code khong tu dong cham lai.
--
-- 4. WORKER / PHUC HOI:
--    Doc ung vien queued khong khoa; trong transaction ngan, khoa problem SHARE,
--    run FOR UPDATE, roi job FOR UPDATE SKIP LOCKED; kiem tra lai status,
--    available_at va version truoc khi nhan. Neu job khong con phu hop thi bo qua.
--    Khong khoa job truoc roi khoa run/problem, tranh dao thu tu voi thay test.
--    Gan running, tang attempt_count, cap lease_token moi va han lease;
--    dong thoi chuyen run sang judging va gan started_at khi bat dau cham.
--    Gia han/ghi ket qua chi khi token trung, lease chua het va version con hien hanh.
--    Khi ket thuc, khoa problem truoc run/job, kiem tra lai version va lease.
--    Ghi tat ca test results + tong hop run + job done trong cung transaction;
--    rang buoc backend: test phai thuoc problem cua submission, so test phai dung.
--    Khong cong bo ket qua tung phan truoc COMMIT. Bai 0 test khong duoc AC.
--    Worker cu mat lease hoac version cu phai bo ket qua, khong chen test results.
--    Lease het han: dua job ve queued va xoa lease; run ve queued, verdict NULL.
--    Phuc hoi cung khoa problem -> run -> job va kiem tra lai lease/version;
--    version cu phai obsolete, khong dua ve queued. Reset du lieu lan thu truoc:
--    passed_tests, total_tests, compiler_info, compile_output, system_error,
--    started_at, finished_at va ket qua tung test truoc khi cham lai.
--    Retry loi ha tang voi backoff va gioi han so lan thu trong cau hinh worker.
--    Thu lai cung run can xoa/reset ket qua lan thu truoc trong transaction.
--    Het so lan thu: run system_error, job failed; khong quy thanh WA/RE.
--    Chay phuc hoi dinh ky va khi khoi dong; gioi han dong thoi bang so worker.
--    Moi luot cham dung compiler C++17 da cong bo/co dinh trong cau hinh;
--    ghi compiler_info thuc te. Cau hinh sandbox gioi han ca bien dich va thuc thi:
--    CPU/thoi gian, bo nho, tien trinh va output; khong chay bang quyen web server.
--    So output theo token, khong tra input/output test an hoac system_error cho SV.
--
-- 5. REFRESH TOKEN:
--    Refresh: doc token de tim account (chua khoa), khoa account truoc, roi khoa
--    va doc lai token; kiem tra con ton tai/con han, xoa cu va chen moi trong cung
--    transaction; chi mot request duoc su dung token cu thanh cong.
--    Dang xuat: xoa token tuong ung. Doi mat khau: khoa account va xoa tat ca
--    refresh token cua account trong cung transaction. Login/refresh cung phai
--    dong bo tren account de khong cap token dua tren thong tin truoc khi doi mat khau.
--    Thu tu khoa auth: account -> refresh_token. Access JWT cu song den het han.
--
-- 6. HIEN THI / XOA:
--    Chi dung judge_runs.problem_version = problems.version.
--    Mot problem AC neu co it nhat mot run completed/AC cua participant tai version do.
--    Mau so la tong problems cua exam; khong co diem so hay bang tong diem.
--    Loi ha tang o lan nop khac khong lam mat AC cung version.
--    Khong cho xoa submission; FK RESTRICT giu lich su khi xoa participant/problem/exam.
--    Xoa problem chua co submission: xoa test va file theo quy trinh rieng.
--    File test cu chi don khi khong con worker dang sao chep/su dung chung;
--    ban sao rieng cua worker duoc don sau khi cham xong/huy.
