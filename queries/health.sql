-- Placeholder so `sqlc generate` has at least one query. Feature queries go in
-- one file per feature (auth.sql, exam.sql, ...).

-- name: Ping :one
SELECT 1::int AS ok;
