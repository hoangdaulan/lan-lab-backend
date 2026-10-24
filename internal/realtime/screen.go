package realtime

import (
	"encoding/json"
	"errors"
	"sort"

	"github.com/hoangdaulan/lan-lab-backend/internal/auth"
)

// Screen sharing (phase 16): a student in a session shares their whole
// screen; the session's teacher sees who is sharing and can watch one
// student live. Video goes peer to peer over the LAN (WebRTC, no STUN/TURN);
// the hub only tracks sharing state and relays signaling between exactly
// one teacher connection and one student connection of the same session.
//
//	student → server  screen.state   {"sharing": bool}
//	teacher → server  screen.view    {"student_id"}         start watching
//	either  → server  webrtc.signal  {"view_id", "kind": "offer"|"answer"|"ice", "payload"}
//	either  → server  screen.stop    {"view_id"}
//	server → teacher  screen.list    {"session_id", "students": [{"student_id", "sharing"}]}
//	server → student  screen.view    {"view_id", "teacher_id"}  create an offer for this viewer
//	server → teacher  screen.view.started {"view_id", "student_id"}
//	server → peer     webrtc.signal  as sent, with "view_id"
//	server → both     screen.stop    {"view_id", "reason"}

type screenStateData struct {
	Sharing bool `json:"sharing"`
}

type screenViewData struct {
	StudentID int64 `json:"student_id"`
}

type signalData struct {
	ViewID  int64           `json:"view_id"`
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

type viewIDData struct {
	ViewID int64 `json:"view_id"`
}

// view pairs the watching teacher connection with the shared student one.
type view struct {
	id      int64
	teacher *Client
	student *Client
}

// ScreenStudent is one entry of screen.list.
type ScreenStudent struct {
	StudentID int64 `json:"student_id"`
	Sharing   bool  `json:"sharing"`
}

var (
	errNotSubscribed = errors.New("subscribe to the session first")
	errNotTeacher    = errors.New("only teachers can view screens")
	errNotStudent    = errors.New("only students share their screen")
	errNotSharing    = errors.New("the student is not sharing their screen")
	errNoView        = errors.New("unknown view")
)

const maxViewsPerTeacher = 4

// setScreen records a student connection's sharing state; turning sharing
// off ends every view of that connection.
func (h *Hub) setScreen(c *Client, sharing bool) error {
	if c.role != auth.RoleStudent {
		return errNotStudent
	}
	if c.session == 0 {
		return errNotSubscribed
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	c.sharing = sharing
	if !sharing {
		h.endViewsLocked(c, "student stopped sharing")
	}
	h.pushScreenListLocked(c.session)
	return nil
}

// startView lets a teacher watch a sharing student of the session the
// teacher is subscribed to (subscribe already checked the teacher owns it).
func (h *Hub) startView(t *Client, studentID int64) (int64, error) {
	if t.role != auth.RoleTeacher {
		return 0, errNotTeacher
	}
	if t.session == 0 {
		return 0, errNotSubscribed
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var target *Client
	for c := range h.clients {
		// The most recently registered sharing connection wins (a student
		// may have reopened the page).
		if c.role == auth.RoleStudent && c.userID == studentID && c.session == t.session && c.sharing &&
			(target == nil || c.seq > target.seq) {
			target = c
		}
	}
	if target == nil {
		return 0, errNotSharing
	}
	n := 0
	for _, v := range h.views {
		if v.teacher == t {
			if v.student == target { // already watching: restart cleanly
				h.endViewLocked(v, "restarted")
				continue
			}
			n++
		}
	}
	if n >= maxViewsPerTeacher {
		return 0, errors.New("too many screens open; close one first")
	}
	h.nextView++
	v := &view{id: h.nextView, teacher: t, student: target}
	h.views[v.id] = v
	h.sendLocked(target, encode("screen.view", map[string]int64{"view_id": v.id, "teacher_id": t.userID}))
	h.sendLocked(t, encode("screen.view.started", map[string]int64{"view_id": v.id, "student_id": studentID}))
	return v.id, nil
}

// relaySignal forwards an offer/answer/ICE candidate to the other end of
// the view; only the two connections of the view may use it.
func (h *Hub) relaySignal(from *Client, d signalData) error {
	switch d.Kind {
	case "offer", "answer", "ice":
	default:
		return errors.New("kind must be offer, answer or ice")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	v := h.views[d.ViewID]
	if v == nil {
		return errNoView
	}
	var to *Client
	switch from {
	case v.teacher:
		to = v.student
	case v.student:
		to = v.teacher
	default:
		return errNoView
	}
	h.sendLocked(to, encode("webrtc.signal", d))
	return nil
}

// stopView ends a view at the request of either end.
func (h *Hub) stopView(from *Client, viewID int64) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	v := h.views[viewID]
	if v == nil || (v.teacher != from && v.student != from) {
		return errNoView
	}
	h.endViewLocked(v, "stopped")
	return nil
}

// endViewsLocked ends every view that c takes part in (c left, changed
// session or stopped sharing). Caller holds h.mu.
func (h *Hub) endViewsLocked(c *Client, reason string) {
	for _, v := range h.views {
		if v.teacher == c || v.student == c {
			h.endViewLocked(v, reason)
		}
	}
}

func (h *Hub) endViewLocked(v *view, reason string) {
	delete(h.views, v.id)
	msg := encode("screen.stop", map[string]any{"view_id": v.id, "reason": reason})
	for _, c := range []*Client{v.teacher, v.student} {
		if _, ok := h.clients[c]; ok {
			h.sendLocked(c, msg)
		}
	}
}

// screenListLocked: students connected to the session, sharing or not
// (a student with several tabs counts as sharing if any tab shares).
func (h *Hub) screenListLocked(sessionID int64) []ScreenStudent {
	byID := map[int64]bool{}
	for c := range h.clients {
		if c.role == auth.RoleStudent && c.session == sessionID {
			byID[c.userID] = byID[c.userID] || c.sharing
		}
	}
	out := make([]ScreenStudent, 0, len(byID))
	for id, sharing := range byID {
		out = append(out, ScreenStudent{StudentID: id, Sharing: sharing})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StudentID < out[j].StudentID })
	return out
}

// pushScreenListLocked sends screen.list to the teachers viewing the session.
func (h *Hub) pushScreenListLocked(sessionID int64) {
	if sessionID == 0 {
		return
	}
	var msg []byte
	for c := range h.clients {
		if c.role == auth.RoleTeacher && c.session == sessionID {
			if msg == nil {
				msg = encode("screen.list", map[string]any{"session_id": sessionID, "students": h.screenListLocked(sessionID)})
			}
			h.sendLocked(c, msg)
		}
	}
}
