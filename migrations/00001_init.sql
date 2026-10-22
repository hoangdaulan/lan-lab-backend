-- +goose Up
-- Ban sao DDL cua database/schema-design.sql (ban thiet ke da duyet), boc cho goose.
-- goose tu chay migration trong transaction nen bo BEGIN/COMMIT cua ban goc.
-- Cac quy tac transaction/khoa (khong phai DDL) xem chu thich cuoi database/schema-design.sql.
-- Sua truc tiep ngay 22/10/2026 (gop teachers/students thanh users) vi chua co du lieu that;
-- DB dev duoc tao lai tu dau.
--
-- PostgreSQL; moi thoi diem la timestamptz; thoi gian hop le: starts_at <= received_at < ends_at.
-- Code tai khoan va code bai phan biet hoa/thuong; backend chuan hoa truoc khi luu.
-- Duong dan la duong dan tuong doi do server sinh, khong nhan duong dan tuy y tu client.
-- updated_at duoc backend cap nhat, khong co trigger tu dong trong ban thiet ke nay.

-- Giao vien va sinh vien chung mot bang (quyet dinh 22/10/2026, thay hai bang
-- teachers/students). code duy nhat tren toan bo tai khoan; role lay tu bang nay
-- khi dang nhap. FK tro toi users khong tu kiem tra role: exams.teacher_id phai
-- la role = 'teacher', exam_participants.student_id phai la role = 'student' --
-- tang service bao dam (khong dung trigger).
CREATE TABLE users (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code varchar(50) NOT NULL UNIQUE CHECK (btrim(code) <> ''),
    fullname varchar(200) NOT NULL CHECK (btrim(fullname) <> ''),
    role varchar(10) NOT NULL CHECK (role IN ('teacher', 'student')),
    password_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE exams (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    teacher_id bigint NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
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
    student_id bigint NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
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
-- chi luu hash, khong luu token goc.
CREATE TABLE refresh_tokens (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL
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
CREATE INDEX users_role_code_idx ON users (role, code);
CREATE INDEX refresh_tokens_user_idx ON refresh_tokens (user_id);
CREATE INDEX refresh_tokens_expiry_idx ON refresh_tokens (expires_at);
CREATE INDEX submissions_participant_history_idx
    ON submissions (exam_participant_id, received_at DESC, id DESC);
CREATE INDEX submissions_participant_problem_idx
    ON submissions (exam_participant_id, problem_id);
CREATE INDEX submissions_problem_idx ON submissions (problem_id, id);
CREATE INDEX test_results_test_case_idx ON submission_test_results (test_case_id);
CREATE INDEX jobs_queue_idx ON judge_jobs (available_at, id) WHERE status = 'queued';
CREATE INDEX jobs_expired_lease_idx ON judge_jobs (lease_expires_at, id) WHERE status = 'running';

-- +goose Down
DROP TABLE judge_jobs;
DROP TABLE submission_test_results;
DROP TABLE judge_runs;
DROP TABLE submissions;
DROP TABLE refresh_tokens;
DROP TABLE exam_participants;
DROP TABLE exam_sessions;
DROP TABLE test_cases;
DROP TABLE problems;
DROP TABLE exams;
DROP TABLE users;
