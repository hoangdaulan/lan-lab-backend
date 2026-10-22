// Package board builds the session result board ("x/N AC" per student ×
// problem, BACKEND_CONTEXT.md §6) and keeps teacher announcements. The
// WebSocket hub (internal/realtime) pushes both live; REST serves the
// initial state and resync after a reconnect.
package board

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hoangdaulan/lan-lab-backend/internal/apperr"
)

// Cell states.
const (
	CellAC        = "ac"        // a current run of this version is AC
	CellPending   = "pending"   // not AC yet, and a current run is queued/judging (or awaiting rejudge)
	CellAttempted = "attempted" // submitted, judged, no AC
	CellNone      = "none"      // no submission
)

type Problem struct {
	ID    int64  `json:"id"`
	Code  string `json:"code"`
	Title string `json:"title"`
}

// Latest is the current result of the newest submission in a cell.
type Latest struct {
	SubmissionID int64   `json:"submission_id"`
	Status       *string `json:"status"` // nil while awaiting a run at the current version
	Verdict      *string `json:"verdict"`
	PassedTests  int     `json:"passed_tests"`
	TotalTests   int     `json:"total_tests"`
}

type Cell struct {
	ProblemID   int64   `json:"problem_id"`
	State       string  `json:"state"`
	Submissions int     `json:"submissions"`
	Latest      *Latest `json:"latest"`
}

type Row struct {
	StudentID   int64  `json:"student_id"`
	StudentCode string `json:"student_code"`
	Fullname    string `json:"student_fullname"`
	Status      string `json:"status"` // participant status: in_progress / finished
	Online      bool   `json:"online"`
	ACCount     int    `json:"ac_count"`
	Cells       []Cell `json:"cells"`
}

type Queue struct {
	Queued  int `json:"queued"`
	Running int `json:"running"`
}

type Summary struct {
	SessionID     int64     `json:"session_id"`
	ProblemsTotal int       `json:"problems_total"`
	Problems      []Problem `json:"problems"`
	Rows          []Row     `json:"rows"`
	OnlineCount   int       `json:"online_count"` // students with an open WebSocket on this session
	Queue         Queue     `json:"queue"`        // all judge jobs, every session
	GeneratedAt   time.Time `json:"generated_at"`
}

// SessionInfo is what permission checks need.
type SessionInfo struct {
	ExamID    int64
	TeacherID int64
	StartsAt  time.Time
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) SessionInfo(ctx context.Context, sessionID int64) (SessionInfo, error) {
	var si SessionInfo
	err := r.pool.QueryRow(ctx,
		`SELECT s.exam_id, e.teacher_id, s.starts_at FROM exam_sessions s JOIN exams e ON e.id = s.exam_id WHERE s.id = $1`,
		sessionID).Scan(&si.ExamID, &si.TeacherID, &si.StartsAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionInfo{}, apperr.NotFound("session")
	}
	return si, err
}

// Summary builds the board. studentID > 0 limits rows to that student.
// Only current runs count: problem_version = problems.version and not
// obsolete (§8); a later wrong or failed submission never removes an AC of
// the same version.
func (r *Repository) Summary(ctx context.Context, sessionID, examID, studentID int64) (Summary, error) {
	sum := Summary{SessionID: sessionID, Problems: []Problem{}, Rows: []Row{}}
	rows, err := r.pool.Query(ctx, `SELECT id, code, title FROM problems WHERE exam_id = $1 ORDER BY code, id`, examID)
	if err != nil {
		return sum, err
	}
	sum.Problems, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Problem, error) {
		var p Problem
		return p, row.Scan(&p.ID, &p.Code, &p.Title)
	})
	if err != nil {
		return sum, err
	}
	sum.ProblemsTotal = len(sum.Problems)

	rows, err = r.pool.Query(ctx,
		`SELECT p.id, u.id, u.code, u.fullname, p.status FROM exam_participants p JOIN users u ON u.id = p.student_id
		 WHERE p.exam_session_id = $1 AND ($2 = 0 OR p.student_id = $2) ORDER BY u.code`, sessionID, studentID)
	if err != nil {
		return sum, err
	}
	rowByParticipant := map[int64]int{}
	for rows.Next() {
		var pid int64
		var row Row
		if err := rows.Scan(&pid, &row.StudentID, &row.StudentCode, &row.Fullname, &row.Status); err != nil {
			rows.Close()
			return sum, err
		}
		row.Cells = make([]Cell, len(sum.Problems))
		for i, p := range sum.Problems {
			row.Cells[i] = Cell{ProblemID: p.ID, State: CellNone}
		}
		rowByParticipant[pid] = len(sum.Rows)
		sum.Rows = append(sum.Rows, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return sum, err
	}
	colByProblem := map[int64]int{}
	for i, p := range sum.Problems {
		colByProblem[p.ID] = i
	}

	// Per cell: count, any current AC, any current run pending (or none yet).
	const current = `FROM submissions s
		JOIN exam_participants p ON p.id = s.exam_participant_id
		JOIN problems pr ON pr.id = s.problem_id
		LEFT JOIN judge_runs r ON r.submission_id = s.id AND r.problem_version = pr.version AND r.status <> 'obsolete'
		WHERE p.exam_session_id = $1 AND ($2 = 0 OR p.student_id = $2)`
	rows, err = r.pool.Query(ctx, `SELECT s.exam_participant_id, s.problem_id, count(*),
		bool_or(r.status = 'completed' AND r.verdict = 'AC'),
		bool_or(r.id IS NULL OR r.status IN ('queued', 'judging')) `+current+` GROUP BY 1, 2`, sessionID, studentID)
	if err != nil {
		return sum, err
	}
	for rows.Next() {
		var pid, problemID int64
		var n int
		var ac, pending bool
		if err := rows.Scan(&pid, &problemID, &n, &ac, &pending); err != nil {
			rows.Close()
			return sum, err
		}
		ri, okR := rowByParticipant[pid]
		ci, okC := colByProblem[problemID]
		if !okR || !okC {
			continue
		}
		c := &sum.Rows[ri].Cells[ci]
		c.Submissions = n
		switch {
		case ac:
			c.State = CellAC
			sum.Rows[ri].ACCount++
		case pending:
			c.State = CellPending
		default:
			c.State = CellAttempted
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return sum, err
	}

	// Newest submission per cell with its current result.
	rows, err = r.pool.Query(ctx, `SELECT DISTINCT ON (s.exam_participant_id, s.problem_id)
		s.exam_participant_id, s.problem_id, s.id, r.status, r.verdict, COALESCE(r.passed_tests, 0), COALESCE(r.total_tests, 0) `+
		current+` ORDER BY s.exam_participant_id, s.problem_id, s.received_at DESC, s.id DESC`, sessionID, studentID)
	if err != nil {
		return sum, err
	}
	for rows.Next() {
		var pid, problemID int64
		var l Latest
		if err := rows.Scan(&pid, &problemID, &l.SubmissionID, &l.Status, &l.Verdict, &l.PassedTests, &l.TotalTests); err != nil {
			rows.Close()
			return sum, err
		}
		if ri, ok := rowByParticipant[pid]; ok {
			if ci, ok := colByProblem[problemID]; ok {
				lc := l
				sum.Rows[ri].Cells[ci].Latest = &lc
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return sum, err
	}

	err = r.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status = 'queued'), count(*) FILTER (WHERE status = 'running')
		FROM judge_jobs WHERE status IN ('queued', 'running')`).Scan(&sum.Queue.Queued, &sum.Queue.Running)
	sum.GeneratedAt = time.Now().UTC()
	return sum, err
}

// SubmissionIDsOfProblem lists every submission of a problem (rejudge
// fan-out).
func (r *Repository) SubmissionIDsOfProblem(ctx context.Context, problemID int64) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT id FROM submissions WHERE problem_id = $1 ORDER BY id`, problemID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// IsParticipant reports whether the student joined the session.
func (r *Repository) IsParticipant(ctx context.Context, sessionID, studentID int64) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exam_participants WHERE exam_session_id = $1 AND student_id = $2)`,
		sessionID, studentID).Scan(&ok)
	return ok, err
}

// Announcement is a teacher message to everyone in a session.
type Announcement struct {
	ID        int64     `json:"id"`
	SessionID int64     `json:"session_id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

const (
	MaxAnnouncementLen = 1000 // characters
	keepAnnouncements  = 50   // per session
)

// Announcements keeps the latest announcements per session in memory: there
// is no table for them (adding one was not part of the approved schema), so
// they are lost when the API restarts. Clients fetch them over REST after
// (re)connecting.
type Announcements struct {
	mu     sync.Mutex
	nextID int64
	bySess map[int64][]Announcement
}

func NewAnnouncements() *Announcements { return &Announcements{bySess: map[int64][]Announcement{}} }

func (a *Announcements) Add(sessionID int64, text string, now time.Time) (Announcement, error) {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > MaxAnnouncementLen {
		return Announcement{}, apperr.Invalid("text must be 1-%d characters", MaxAnnouncementLen)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextID++
	an := Announcement{ID: a.nextID, SessionID: sessionID, Text: text, CreatedAt: now.UTC()}
	list := append(a.bySess[sessionID], an)
	if len(list) > keepAnnouncements {
		list = list[len(list)-keepAnnouncements:]
	}
	a.bySess[sessionID] = list
	return an, nil
}

// List returns a session's announcements, newest first.
func (a *Announcements) List(sessionID int64) []Announcement {
	a.mu.Lock()
	defer a.mu.Unlock()
	list := a.bySess[sessionID]
	out := make([]Announcement, len(list))
	for i := range list {
		out[i] = list[len(list)-1-i]
	}
	return out
}
