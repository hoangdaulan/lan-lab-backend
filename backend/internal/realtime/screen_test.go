package realtime

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/hoangdaulan/lan-lab-backend/internal/auth"
)

func setup(t *testing.T) (*Hub, map[string]*Client) {
	t.Helper()
	h := NewHub(&fakeLoader{}, quiet)
	cs := map[string]*Client{}
	add := func(name string, id int64, role auth.Role, session int64) {
		c, _ := newClient(id, role, 0)
		h.register(c)
		h.subscribe(c, session)
		cs[name] = c
	}
	add("teacher", 100, auth.RoleTeacher, 5)
	add("otherTeacher", 101, auth.RoleTeacher, 6)
	add("alice", 10, auth.RoleStudent, 5)
	add("bob", 11, auth.RoleStudent, 5)
	add("carol", 12, auth.RoleStudent, 6) // another session
	for _, c := range cs {
		drain(c)
	}
	return h, cs
}

func lastList(t *testing.T, es []envelope) []ScreenStudent {
	t.Helper()
	for i := len(es) - 1; i >= 0; i-- {
		if es[i].Type == "screen.list" {
			b, _ := json.Marshal(es[i].Data)
			var d struct {
				Students []ScreenStudent `json:"students"`
			}
			_ = json.Unmarshal(b, &d)
			return d.Students
		}
	}
	t.Fatalf("no screen.list in %v", types(es))
	return nil
}

func TestScreenListFollowsSharingAndPresence(t *testing.T) {
	h, cs := setup(t)
	if err := h.setScreen(cs["alice"], true); err != nil {
		t.Fatal(err)
	}
	got := lastList(t, drain(cs["teacher"]))
	want := []ScreenStudent{{StudentID: 10, Sharing: true}, {StudentID: 11, Sharing: false}}
	if !slices.Equal(got, want) {
		t.Errorf("list = %v, want %v", got, want)
	}
	if es := drain(cs["otherTeacher"]); len(es) != 0 {
		t.Errorf("teacher of another session got %v", types(es))
	}
	if err := h.setScreen(cs["teacher"], true); !errors.Is(err, errNotStudent) {
		t.Errorf("teacher screen.state: %v", err)
	}

	h.unregister(cs["bob"])
	got = lastList(t, drain(cs["teacher"]))
	if !slices.Equal(got, []ScreenStudent{{StudentID: 10, Sharing: true}}) {
		t.Errorf("after bob left: %v", got)
	}

	// A teacher who subscribes later gets the current list on request.
	late, _ := newClient(100, auth.RoleTeacher, 0)
	h.register(late)
	h.subscribe(late, 5)
	h.sendScreenList(late)
	if got := lastList(t, drain(late)); len(got) != 1 || !got[0].Sharing {
		t.Errorf("late teacher list = %v", got)
	}
}

func TestScreenViewSignalingIsPairwise(t *testing.T) {
	h, cs := setup(t)
	teacher, alice, bob := cs["teacher"], cs["alice"], cs["bob"]

	if _, err := h.startView(teacher, 10); !errors.Is(err, errNotSharing) {
		t.Fatalf("view before sharing: %v", err)
	}
	_ = h.setScreen(alice, true)
	if _, err := h.startView(alice, 11); !errors.Is(err, errNotTeacher) {
		t.Errorf("student starting a view: %v", err)
	}
	if _, err := h.startView(cs["otherTeacher"], 10); !errors.Is(err, errNotSharing) {
		t.Errorf("teacher of another session: %v", err)
	}
	drain(teacher)
	drain(alice)

	id, err := h.startView(teacher, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := types(drain(alice)); !slices.Equal(got, []string{"screen.view"}) {
		t.Errorf("alice got %v", got)
	}
	if got := types(drain(teacher)); !slices.Equal(got, []string{"screen.view.started"}) {
		t.Errorf("teacher got %v", got)
	}

	// Offer from the student reaches only the teacher, answer only the student.
	if err := h.relaySignal(alice, signalData{ViewID: id, Kind: "offer", Payload: json.RawMessage(`{"sdp":"x"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := h.relaySignal(teacher, signalData{ViewID: id, Kind: "answer", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if got := types(drain(teacher)); !slices.Equal(got, []string{"webrtc.signal"}) {
		t.Errorf("teacher got %v", got)
	}
	if got := types(drain(alice)); !slices.Equal(got, []string{"webrtc.signal"}) {
		t.Errorf("alice got %v", got)
	}
	// Outsiders cannot inject into the view.
	if err := h.relaySignal(bob, signalData{ViewID: id, Kind: "ice"}); !errors.Is(err, errNoView) {
		t.Errorf("bob relaying: %v", err)
	}
	if err := h.relaySignal(cs["otherTeacher"], signalData{ViewID: id, Kind: "ice"}); !errors.Is(err, errNoView) {
		t.Errorf("other teacher relaying: %v", err)
	}
	if err := h.relaySignal(alice, signalData{ViewID: id, Kind: "bogus"}); err == nil {
		t.Error("unknown kind accepted")
	}
	if len(drain(bob)) != 0 {
		t.Error("bob received signaling")
	}

	// Stopping sharing ends the view on both sides.
	_ = h.setScreen(alice, false)
	if got := types(drain(alice)); !slices.Contains(got, "screen.stop") {
		t.Errorf("alice got %v", got)
	}
	if got := types(drain(teacher)); !slices.Contains(got, "screen.stop") || !slices.Contains(got, "screen.list") {
		t.Errorf("teacher got %v", got)
	}
	if err := h.relaySignal(alice, signalData{ViewID: id, Kind: "ice"}); !errors.Is(err, errNoView) {
		t.Errorf("signal after stop: %v", err)
	}
}

func TestScreenViewEndsWhenEitherSideLeaves(t *testing.T) {
	h, cs := setup(t)
	teacher, alice := cs["teacher"], cs["alice"]
	_ = h.setScreen(alice, true)
	id, _ := h.startView(teacher, 10)
	drain(alice)

	h.unregister(teacher)
	if got := types(drain(alice)); !slices.Equal(got, []string{"screen.stop"}) {
		t.Errorf("alice got %v", got)
	}
	if err := h.stopView(alice, id); !errors.Is(err, errNoView) {
		t.Errorf("view still exists: %v", err)
	}

	// Student switching session also ends their views and sharing.
	t2, _ := newClient(100, auth.RoleTeacher, 0)
	h.register(t2)
	h.subscribe(t2, 5)
	id, _ = h.startView(t2, 10)
	drain(t2)
	h.subscribe(alice, 6)
	if got := types(drain(t2)); !slices.Contains(got, "screen.stop") {
		t.Errorf("teacher got %v", got)
	}
	if alice.sharing {
		t.Error("sharing kept across sessions")
	}
	if err := h.stopView(t2, id); !errors.Is(err, errNoView) {
		t.Errorf("stop after end: %v", err)
	}
}

func TestScreenViewLimitPerTeacher(t *testing.T) {
	h := NewHub(&fakeLoader{}, quiet)
	teacher, _ := newClient(100, auth.RoleTeacher, 0)
	h.register(teacher)
	h.subscribe(teacher, 5)
	for i := int64(1); i <= maxViewsPerTeacher+1; i++ {
		s, _ := newClient(i, auth.RoleStudent, 0)
		h.register(s)
		h.subscribe(s, 5)
		_ = h.setScreen(s, true)
		_, err := h.startView(teacher, i)
		if i <= maxViewsPerTeacher && err != nil {
			t.Fatalf("view %d: %v", i, err)
		}
		if i > maxViewsPerTeacher && err == nil {
			t.Fatal("limit not enforced")
		}
	}
	// Re-viewing the same student replaces the view instead of counting twice.
	if _, err := h.startView(teacher, 1); err != nil {
		t.Errorf("re-view: %v", err)
	}
}
