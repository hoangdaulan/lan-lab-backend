// Package realtime pushes judge results, the teacher's live board and
// announcements over WebSocket (phase 11, BACKEND_CONTEXT.md §19).
//
// Protocol: JSON text messages {"type": string, "data": object}.
//
//	client → server
//	  auth       {"token": "<access JWT>"}   first message, within AuthTimeout;
//	                                         may be sent again to extend the
//	                                         connection with a fresh token
//	  subscribe  {"session_id": 12}          one session at a time
//	  screen.*, webrtc.signal                screen sharing, see screen.go
//	server → client
//	  auth.ok          {"user_id", "role", "expires_at"}
//	  subscribed       {"session_id"}
//	  run.updated      submission with its current result (as REST)
//	  summary.updated  teacher board (as GET /sessions/{id}/summary)
//	  announcement     {"id", "session_id", "text", "created_at"}
//	  resync           {}   events may have been missed: reload over REST
//	  screen.list, screen.view, screen.view.started, screen.stop,
//	  webrtc.signal    screen sharing, see screen.go
//	  error            {"message"}
//
// Close codes: 4001 authentication failed or token expired, 1008 too slow
// (send buffer full), 1001 server shutting down.
//
// WebSocket only notifies; REST stays the source of truth (§11). Clients
// reload state over REST after (re)connecting and on resync.
package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/hoangdaulan/lan-lab-backend/internal/auth"
	"github.com/hoangdaulan/lan-lab-backend/internal/board"
	"github.com/hoangdaulan/lan-lab-backend/internal/events"
	"github.com/hoangdaulan/lan-lab-backend/internal/submission"
)

// Loader reads current state for pushes.
type Loader interface {
	Submission(ctx context.Context, id int64) (submission.Submission, error)
	SubmissionIDsOfProblem(ctx context.Context, problemID int64) ([]int64, error)
	SessionInfo(ctx context.Context, sessionID int64) (board.SessionInfo, error)
	TeacherBoard(ctx context.Context, sessionID int64) (board.Summary, error)
}

type envelope struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

func encode(typ string, data any) []byte {
	b, _ := json.Marshal(envelope{Type: typ, Data: data})
	return b
}

const (
	sendBuffer      = 256
	summaryInterval = 300 * time.Millisecond // debounce for summary.updated
	summaryRefresh  = 5 * time.Second        // periodic refresh (queue length, safety net)
)

// Client is one authenticated WebSocket connection.
type Client struct {
	userID  int64
	role    auth.Role
	session int64 // subscribed session, 0 = none
	sharing bool  // student: screen shared on this connection (screen.go)
	seq     int64 // registration order
	send    chan []byte
	kick    func(reason string)
}

type Hub struct {
	loader Loader
	log    *slog.Logger

	mu       sync.Mutex
	clients  map[*Client]struct{}
	dirty    map[int64]bool
	views    map[int64]*view // screen views by id (screen.go)
	nextView int64
	nextSeq  int64
}

func NewHub(loader Loader, log *slog.Logger) *Hub {
	return &Hub{loader: loader, log: log, clients: map[*Client]struct{}{}, dirty: map[int64]bool{}, views: map[int64]*view{}}
}

func (h *Hub) register(c *Client) {
	h.mu.Lock()
	h.nextSeq++
	c.seq = h.nextSeq
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.endViewsLocked(c, "disconnected")
	if c.role == auth.RoleStudent && c.session != 0 {
		h.dirty[c.session] = true // presence changed
		h.pushScreenListLocked(c.session)
	}
	h.mu.Unlock()
}

func (h *Hub) subscribe(c *Client, sessionID int64) {
	h.mu.Lock()
	old := c.session
	if old != sessionID {
		// Views and sharing belong to one session.
		h.endViewsLocked(c, "changed session")
		c.sharing = false
	}
	c.session = sessionID
	if c.role == auth.RoleStudent {
		if old != 0 {
			h.dirty[old] = true
			if old != sessionID {
				h.pushScreenListLocked(old)
			}
		}
		h.pushScreenListLocked(sessionID)
	}
	h.dirty[sessionID] = true // presence or a new teacher view
	h.mu.Unlock()
}

// sendScreenList sends the session's screen.list to one teacher connection
// (right after it subscribes).
func (h *Hub) sendScreenList(c *Client) {
	if c.role != auth.RoleTeacher || c.session == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sendLocked(c, encode("screen.list", map[string]any{"session_id": c.session, "students": h.screenListLocked(c.session)}))
}

// sendLocked queues msg; a client that cannot keep up is disconnected
// instead of blocking the hub. Caller holds h.mu.
func (h *Hub) sendLocked(c *Client, msg []byte) {
	select {
	case c.send <- msg:
	default:
		delete(h.clients, c)
		go c.kick("too slow")
	}
}

// ClientCount returns the number of authenticated connections.
func (h *Hub) ClientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// OnlineStudents implements board.Broadcaster.
func (h *Hub) OnlineStudents(sessionID int64) map[int64]bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[int64]bool{}
	for c := range h.clients {
		if c.role == auth.RoleStudent && c.session == sessionID {
			out[c.userID] = true
		}
	}
	return out
}

// Announce implements board.Broadcaster: every client on the session.
func (h *Hub) Announce(a board.Announcement) {
	msg := encode("announcement", a)
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.session == a.SessionID {
			h.sendLocked(c, msg)
		}
	}
}

// Resync tells every client to reload over REST (events may have been lost).
func (h *Hub) Resync() {
	msg := encode("resync", struct{}{})
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		h.sendLocked(c, msg)
	}
}

// HandleEvent loads what changed and pushes it.
func (h *Hub) HandleEvent(ctx context.Context, e events.Event) {
	switch e.Kind {
	case events.KindSubmission:
		h.pushSubmission(ctx, e.ID)
	case events.KindProblem:
		ids, err := h.loader.SubmissionIDsOfProblem(ctx, e.ID)
		if err != nil {
			h.log.Error("load problem submissions", "problem_id", e.ID, "err", err)
			return
		}
		for _, id := range ids {
			h.pushSubmission(ctx, id)
		}
	}
}

// pushSubmission sends run.updated to the student who owns the submission
// (all their connections) and to the owning teacher's connections viewing
// that session, then marks the session's board dirty.
func (h *Hub) pushSubmission(ctx context.Context, id int64) {
	sub, err := h.loader.Submission(ctx, id)
	if err != nil {
		h.log.Error("load submission for push", "submission_id", id, "err", err)
		return
	}
	msg := encode("run.updated", submission.ToResponse(sub))
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		switch {
		case c.role == auth.RoleStudent && c.userID == sub.StudentID:
			h.sendLocked(c, msg)
		case c.role == auth.RoleTeacher && c.userID == sub.TeacherID && c.session == sub.SessionID:
			h.sendLocked(c, msg)
		}
	}
	h.dirty[sub.SessionID] = true
}

// RunSummaries pushes summary.updated to teachers: debounced after changes,
// and refreshed periodically (queue length moves with every session).
func (h *Hub) RunSummaries(ctx context.Context) {
	tick := time.NewTicker(summaryInterval)
	defer tick.Stop()
	lastRefresh := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		refresh := time.Since(lastRefresh) >= summaryRefresh
		if refresh {
			lastRefresh = time.Now()
		}
		for _, sessionID := range h.takeDirty(refresh) {
			sum, err := h.loader.TeacherBoard(ctx, sessionID)
			if err != nil {
				if ctx.Err() == nil {
					h.log.Error("build board", "session_id", sessionID, "err", err)
				}
				continue
			}
			msg := encode("summary.updated", sum)
			h.mu.Lock()
			for c := range h.clients {
				if c.role == auth.RoleTeacher && c.session == sessionID {
					h.sendLocked(c, msg)
				}
			}
			h.mu.Unlock()
		}
	}
}

// takeDirty returns sessions to rebuild that some teacher is viewing; with
// all=true every viewed session.
func (h *Hub) takeDirty(all bool) []int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	viewed := map[int64]bool{}
	for c := range h.clients {
		if c.role == auth.RoleTeacher && c.session != 0 {
			viewed[c.session] = true
		}
	}
	var out []int64
	for s := range viewed {
		if all || h.dirty[s] {
			out = append(out, s)
		}
	}
	h.dirty = map[int64]bool{}
	return out
}
