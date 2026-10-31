package board

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// WriteCSV writes the board as CSV for Excel: UTF-8 with BOM (Vietnamese
// names display correctly), comma separated, CRLF. One row per student:
// code, name, status, one column per problem (AC / "10/15 TLE" / pending /
// empty), then "x/N AC".
func WriteCSV(w io.Writer, sum Summary) error {
	if _, err := io.WriteString(w, string(rune(0xFEFF))); err != nil { // UTF-8 BOM
		return err
	}
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	header := []string{"student_code", "fullname", "status"}
	for _, p := range sum.Problems {
		header = append(header, p.Code)
	}
	header = append(header, "ac")
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, r := range sum.Rows {
		rec := []string{safeCell(r.StudentCode), safeCell(r.Fullname), r.Status}
		for _, c := range r.Cells {
			rec = append(rec, cellText(c))
		}
		rec = append(rec, fmt.Sprintf("%d/%d AC", r.ACCount, sum.ProblemsTotal))
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// cellText: "AC" for a solved problem, otherwise the newest submission's
// current result ("10/15 TLE"), "pending" while judging, "" if untouched.
func cellText(c Cell) string {
	switch c.State {
	case CellAC:
		return "AC"
	case CellNone:
		return ""
	case CellPending:
		return "pending"
	}
	if l := c.Latest; l != nil && l.Status != nil {
		if *l.Status == "system_error" {
			return "system_error"
		}
		if l.Verdict != nil {
			return fmt.Sprintf("%d/%d %s", l.PassedTests, l.TotalTests, *l.Verdict)
		}
	}
	return "attempted"
}

// safeCell stops spreadsheet formula injection from user-entered text
// (a name starting with = + - @ would otherwise be evaluated by Excel).
func safeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// QueueStats is GET /admin/queue.
type QueueStats struct {
	Jobs           map[string]int `json:"jobs"`             // judge_jobs by status
	ActiveWorkers  []string       `json:"active_workers"`   // worker_id with a live lease
	OldestQueuedMs int64          `json:"oldest_queued_ms"` // age of the oldest claimable job
	// Over runs started in the last 10 minutes:
	RecentRuns     int       `json:"recent_runs"`
	AvgQueueWaitMs float64   `json:"avg_queue_wait_ms"` // created_at -> started_at
	AvgJudgeMs     float64   `json:"avg_judge_ms"`      // started_at -> finished_at (completed runs)
	GeneratedAt    time.Time `json:"generated_at"`
}

func (r *Repository) QueueStats(ctx context.Context) (QueueStats, error) {
	st := QueueStats{Jobs: map[string]int{"queued": 0, "running": 0, "done": 0, "failed": 0, "obsolete": 0},
		ActiveWorkers: []string{}}
	rows, err := r.pool.Query(ctx, `SELECT status, count(*) FROM judge_jobs GROUP BY status`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			rows.Close()
			return st, err
		}
		st.Jobs[s] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return st, err
	}
	rows, err = r.pool.Query(ctx, `SELECT DISTINCT worker_id FROM judge_jobs
		WHERE status = 'running' AND lease_expires_at > clock_timestamp() ORDER BY 1`)
	if err != nil {
		return st, err
	}
	if st.ActiveWorkers, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return st, err
	}
	if st.ActiveWorkers == nil {
		st.ActiveWorkers = []string{}
	}
	if err := r.pool.QueryRow(ctx, `SELECT COALESCE(EXTRACT(EPOCH FROM clock_timestamp() - min(created_at)) * 1000, 0)::bigint
		FROM judge_jobs WHERE status = 'queued' AND available_at <= clock_timestamp()`).Scan(&st.OldestQueuedMs); err != nil {
		return st, err
	}
	err = r.pool.QueryRow(ctx, `SELECT count(*),
		COALESCE(avg(EXTRACT(EPOCH FROM started_at - created_at) * 1000), 0),
		COALESCE(avg(EXTRACT(EPOCH FROM finished_at - started_at) * 1000) FILTER (WHERE status = 'completed'), 0)
		FROM judge_runs WHERE started_at > clock_timestamp() - interval '10 minutes'`).
		Scan(&st.RecentRuns, &st.AvgQueueWaitMs, &st.AvgJudgeMs)
	st.GeneratedAt = time.Now().UTC()
	return st, err
}
