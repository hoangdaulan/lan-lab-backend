package submission

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hoangdaulan/lan-lab-backend/internal/apperr"
	"github.com/hoangdaulan/lan-lab-backend/internal/auth"
	"github.com/hoangdaulan/lan-lab-backend/internal/httpx"
)

type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// RegisterStudent adds student-only routes.
func (h *Handler) RegisterStudent(r chi.Router) {
	r.Post("/sessions/{id}/problems/{pid}/submissions", h.submit)
}

// RegisterTeacher adds teacher-only routes.
func (h *Handler) RegisterTeacher(r chi.Router) {
	r.Get("/submissions/{id}/tests", h.tests)
}

// RegisterShared adds routes for both roles; the service filters by owner.
func (h *Handler) RegisterShared(r chi.Router) {
	r.Get("/sessions/{id}/submissions", h.list)
	r.Get("/submissions/{id}", h.get)
	r.Get("/submissions/{id}/source", h.source)
}

func viewer(r *http.Request) Viewer {
	p, _ := auth.PrincipalFrom(r.Context())
	return Viewer{ID: p.ID, IsTeacher: p.Role == auth.RoleTeacher}
}

// multipartOverhead allows for headers and boundaries around the file part.
const multipartOverhead = 16 << 10

func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	sessionID, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	problemID, err := httpx.IDParam(r, "pid")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	filename, content, err := readSourcePart(w, r)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	// received_at: the moment the whole file is in hand (§10).
	receivedAt := h.svc.now()

	student, _ := auth.PrincipalFrom(r.Context())
	sub, err := h.svc.Submit(r.Context(), student.ID, sessionID, problemID, filename, content, receivedAt)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, toResponse(sub, false))
}

// readSourcePart streams the multipart field "file", reading at most
// MaxSourceBytes+1 bytes so oversize files are detected without buffering
// them.
func readSourcePart(w http.ResponseWriter, r *http.Request) (string, []byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxSourceBytes+multipartOverhead)
	mr, err := r.MultipartReader()
	if err != nil {
		return "", nil, apperr.Invalid("multipart/form-data with field \"file\" is required")
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return "", nil, apperr.Invalid("multipart field \"file\" is required")
		}
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return "", nil, apperr.TooLarge("the file is larger than %d KB", MaxSourceBytes>>10)
		}
		if err != nil {
			return "", nil, apperr.Invalid("invalid multipart body")
		}
		if part.FormName() != "file" {
			part.Close()
			continue
		}
		content, err := io.ReadAll(io.LimitReader(part, MaxSourceBytes+1))
		part.Close()
		if errors.As(err, &tooBig) {
			return "", nil, apperr.TooLarge("the file is larger than %d KB", MaxSourceBytes>>10)
		}
		if err != nil {
			return "", nil, apperr.Invalid("could not read the uploaded file")
		}
		if err := ValidateUpload(part.FileName(), len(content)); err != nil {
			return "", nil, err
		}
		return part.FileName(), content, nil
	}
}

type resultResponse struct {
	RunID          int64      `json:"run_id"`
	ProblemVersion int        `json:"problem_version"`
	Status         string     `json:"status"`
	Verdict        *string    `json:"verdict"`
	PassedTests    int        `json:"passed_tests"`
	TotalTests     int        `json:"total_tests"`
	CompilerInfo   *string    `json:"compiler_info,omitempty"`
	CompileOutput  *string    `json:"compile_output,omitempty"`
	SystemError    *string    `json:"system_error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	StartedAt      *time.Time `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
}

type submissionResponse struct {
	ID              int64           `json:"id"`
	SessionID       int64           `json:"session_id"`
	ProblemID       int64           `json:"problem_id"`
	ProblemCode     string          `json:"problem_code"`
	StudentID       int64           `json:"student_id"`
	StudentCode     string          `json:"student_code"`
	StudentFullname string          `json:"student_fullname"`
	ReceivedAt      time.Time       `json:"received_at"`
	ProblemVersion  int             `json:"problem_version"`
	Result          *resultResponse `json:"result"`
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// toResponse renders a submission. detail adds compile output; system
// errors are only shown to teachers (BACKEND_CONTEXT.md §11).
func toResponse(s Submission, detail bool, teacher ...bool) submissionResponse {
	out := submissionResponse{ID: s.ID, SessionID: s.SessionID, ProblemID: s.ProblemID, ProblemCode: s.ProblemCode,
		StudentID: s.StudentID, StudentCode: s.StudentCode, StudentFullname: s.StudentFullname,
		ReceivedAt: s.ReceivedAt.UTC(), ProblemVersion: s.ProblemVersion}
	if s.Result != nil {
		res := s.Result
		out.Result = &resultResponse{RunID: res.RunID, ProblemVersion: res.ProblemVersion, Status: res.Status,
			Verdict: res.Verdict, PassedTests: res.PassedTests, TotalTests: res.TotalTests,
			CreatedAt: res.CreatedAt.UTC(), StartedAt: utcPtr(res.StartedAt), FinishedAt: utcPtr(res.FinishedAt)}
		if detail {
			out.Result.CompilerInfo, out.Result.CompileOutput = res.CompilerInfo, res.CompileOutput
			if len(teacher) > 0 && teacher[0] {
				out.Result.SystemError = res.SystemError
			}
		}
	}
	return out
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	sessionID, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	var f Filter
	for key, dst := range map[string]*int64{"student_id": &f.StudentID, "problem_id": &f.ProblemID} {
		if v := r.URL.Query().Get(key); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n <= 0 {
				httpx.WriteError(w, h.log, apperr.Invalid("invalid %s", key))
				return
			}
			*dst = n
		}
	}
	subs, err := h.svc.List(r.Context(), viewer(r), sessionID, f)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	items := make([]submissionResponse, 0, len(subs))
	for _, s := range subs {
		items = append(items, toResponse(s, false))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	v := viewer(r)
	sub, err := h.svc.Get(r.Context(), v, id)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toResponse(sub, true, v.IsTeacher))
}

func (h *Handler) source(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	sub, content, err := h.svc.Source(r.Context(), viewer(r), id)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="submission-`+strconv.FormatInt(sub.ID, 10)+`.cpp"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(content)
}

type testResultResponse struct {
	TestCaseID      int64  `json:"test_case_id"`
	Verdict         string `json:"verdict"`
	ExecutionTimeMs int    `json:"execution_time_ms"`
	MemoryUsedKB    *int64 `json:"memory_used_kb"`
}

func (h *Handler) tests(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	teacher, _ := auth.PrincipalFrom(r.Context())
	sub, results, err := h.svc.TestResults(r.Context(), teacher.ID, id)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	items := make([]testResultResponse, 0, len(results))
	for _, t := range results {
		items = append(items, testResultResponse(t))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"submission": toResponse(sub, true, true), "items": items})
}
