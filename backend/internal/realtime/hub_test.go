package realtime

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoangdaulan/lan-lab-backend/internal/auth"
	"github.com/hoangdaulan/lan-lab-backend/internal/board"
	"github.com/hoangdaulan/lan-lab-backend/internal/events"
	"github.com/hoangdaulan/lan-lab-backend/internal/submission"
)

type fakeLoader struct {
	subs     map[int64]submission.Submission
	byProb   map[int64][]int64
	sessions map[int64]board.SessionInfo
	boards   atomic.Int32
}

func (f *fakeLoader) Submission(_ context.Context, id int64) (submission.Submission, error) {
	return f.subs[id], nil
}
func (f *fakeLoader) SubmissionIDsOfProblem(_ context.Context, id int64) ([]int64, error) {
	return f.byProb[id], nil
}
func (f *fakeLoader) SessionInfo(_ context.Context, id int64) (board.SessionInfo, error) {
	return f.sessions[id], nil
}
func (f *fakeLoader) TeacherBoard(_ context.Context, id int64) (board.Summary, error) {
	f.boards.Add(1)
	return board.Summary{SessionID: id}, nil
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func newClient(userID int64, role auth.Role, session int64) (*Client, *atomic.Value) {
	reason := &atomic.Value{}
	c := &Client{userID: userID, role: role, session: session, send: make(chan []byte, sendBuffer)}
	c.kick = func(r string) { reason.Store(r) }
	return c, reason
}

func drain(c *Client) []envelope {
	var out []envelope
	for {
		select {
		case b := <-c.send:
			var e envelope
			_ = json.Unmarshal(b, &e)
			out = append(out, e)
		default:
			return out
		}
	}
}

func types(es []envelope) []string {
	var t []string
	for _, e := range es {
		t = append(t, e.Type)
	}
	return t
}

func TestRunUpdatedRouting(t *testing.T) {
	f := &fakeLoader{subs: map[int64]submission.Submission{
		1: {ID: 1, StudentID: 10, TeacherID: 100, SessionID: 5},
		2: {ID: 2, StudentID: 11, TeacherID: 100, SessionID: 5},
	}, byProb: map[int64][]int64{9: {1, 2}}}
	h := NewHub(f, quiet)
	owner, _ := newClient(10, auth.RoleStudent, 0)        // not subscribed: still gets own results
	ownerTab2, _ := newClient(10, auth.RoleStudent, 5)    // second tab
	other, _ := newClient(11, auth.RoleStudent, 5)        // another student
	teacher, _ := newClient(100, auth.RoleTeacher, 5)     // owning teacher viewing session 5
	teacherElse, _ := newClient(100, auth.RoleTeacher, 6) // same teacher, other session
	otherTeacher, _ := newClient(200, auth.RoleTeacher, 5)
	for _, c := range []*Client{owner, ownerTab2, other, teacher, teacherElse, otherTeacher} {
		h.register(c)
	}

	h.HandleEvent(context.Background(), events.Event{Kind: events.KindSubmission, ID: 1})
	for name, c := range map[string]*Client{"owner": owner, "owner tab 2": ownerTab2, "teacher": teacher} {
		if got := types(drain(c)); len(got) != 1 || got[0] != "run.updated" {
			t.Errorf("%s got %v, want one run.updated", name, got)
		}
	}
	for name, c := range map[string]*Client{"other student": other, "teacher other session": teacherElse, "other teacher": otherTeacher} {
		if got := drain(c); len(got) != 0 {
			t.Errorf("%s got %v, want nothing", name, types(got))
		}
	}

	// Problem rejudge fans out to every affected student.
	h.HandleEvent(context.Background(), events.Event{Kind: events.KindProblem, ID: 9})
	if n := len(drain(owner)); n != 1 {
		t.Errorf("owner got %d, want 1", n)
	}
	if n := len(drain(other)); n != 1 {
		t.Errorf("other student got %d, want 1 (own submission 2)", n)
	}
	if n := len(drain(teacher)); n != 2 {
		t.Errorf("teacher got %d, want 2", n)
	}
}

func TestSlowClientIsKicked(t *testing.T) {
	f := &fakeLoader{subs: map[int64]submission.Submission{1: {ID: 1, StudentID: 10, SessionID: 5}}}
	h := NewHub(f, quiet)
	slow, reason := newClient(10, auth.RoleStudent, 5)
	h.register(slow)
	for i := 0; i < sendBuffer+1; i++ {
		h.HandleEvent(context.Background(), events.Event{Kind: events.KindSubmission, ID: 1})
	}
	time.Sleep(10 * time.Millisecond) // kick runs in a goroutine
	if reason.Load() != "too slow" {
		t.Errorf("kick reason = %v", reason.Load())
	}
	if h.ClientCount() != 0 {
		t.Error("slow client still registered")
	}
}

func TestPresenceAnnouncementsAndSummaries(t *testing.T) {
	f := &fakeLoader{}
	h := NewHub(f, quiet)
	s1, _ := newClient(10, auth.RoleStudent, 0)
	s2, _ := newClient(11, auth.RoleStudent, 0)
	teacher, _ := newClient(100, auth.RoleTeacher, 0)
	for _, c := range []*Client{s1, s2, teacher} {
		h.register(c)
	}
	h.subscribe(s1, 5)
	h.subscribe(s2, 6)
	h.subscribe(teacher, 5)
	if got := h.OnlineStudents(5); len(got) != 1 || !got[10] {
		t.Errorf("online(5) = %v", got)
	}

	h.Announce(board.Announcement{ID: 1, SessionID: 5, Text: "Còn 10 phút"})
	if got := types(drain(s1)); len(got) != 1 || got[0] != "announcement" {
		t.Errorf("s1 got %v", got)
	}
	if got := drain(s2); len(got) != 0 {
		t.Errorf("student of another session got %v", types(got))
	}
	if got := types(drain(teacher)); len(got) != 1 || got[0] != "announcement" {
		t.Errorf("teacher got %v", got)
	}

	// Presence changes mark the board dirty; only viewed sessions rebuild.
	ctx, cancel := context.WithCancel(context.Background())
	go h.RunSummaries(ctx)
	time.Sleep(summaryInterval + 150*time.Millisecond)
	cancel()
	if got := types(drain(teacher)); len(got) != 1 || got[0] != "summary.updated" {
		t.Errorf("teacher got %v, want one summary.updated", got)
	}
	if f.boards.Load() != 1 {
		t.Errorf("boards built = %d, want 1 (session 6 has no teacher)", f.boards.Load())
	}
	h.unregister(s1)
	if got := h.OnlineStudents(5); len(got) != 0 {
		t.Errorf("online after disconnect = %v", got)
	}
}

func TestEventPayloads(t *testing.T) {
	for _, bad := range []string{"", "x:1", "s:", "s:0", "s:-3", "s1", "ss:1"} {
		if _, err := events.Parse(bad); err == nil {
			t.Errorf("Parse(%q) accepted", bad)
		}
	}
	if e, err := events.Parse("p:42"); err != nil || e.Kind != events.KindProblem || e.ID != 42 {
		t.Errorf("Parse(p:42) = %+v, %v", e, err)
	}
}
