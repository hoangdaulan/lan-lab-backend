package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/hoangdaulan/lan-lab-backend/internal/apperr"
	"github.com/hoangdaulan/lan-lab-backend/internal/auth"
)

// Tunables (variables so tests can shorten them).
var (
	AuthTimeout  = 5 * time.Second
	PingInterval = 20 * time.Second
	WriteTimeout = 5 * time.Second
)

const (
	maxMessage       = 64 << 10 // WebRTC session descriptions are a few KB
	closeAuth        = websocket.StatusCode(4001)
	maxConnsPerUser  = 8
	tokenExpiredText = "token expired"
)

// TokenVerifier is satisfied by *auth.Service.
type TokenVerifier interface {
	VerifyAccessTokenExpiry(token string) (auth.Principal, time.Time, error)
}

type Handler struct {
	hub     *Hub
	tokens  TokenVerifier
	origins []string
	log     *slog.Logger
}

// NewHandler: origins are extra allowed Origin host patterns (for a front-end
// dev server); same-origin requests are always allowed.
func NewHandler(hub *Hub, tokens TokenVerifier, origins []string, log *slog.Logger) *Handler {
	return &Handler{hub: hub, tokens: tokens, origins: origins, log: log}
}

type authData struct {
	Token string `json:"token"`
}

type subscribeData struct {
	SessionID int64 `json:"session_id"`
}

type inbound struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// ServeHTTP upgrades GET /api/v1/ws. Authentication happens in the first
// message, so the token never appears in URLs or access logs.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Accept rejects cross-origin requests (403) unless the Origin host
	// matches the request host or one of the configured patterns.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.origins})
	if err != nil {
		h.log.Warn("websocket upgrade rejected", "remote", r.RemoteAddr, "origin", r.Header.Get("Origin"), "err", err)
		return
	}
	conn.SetReadLimit(maxMessage)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p, exp, err := h.authenticate(ctx, conn)
	if err != nil {
		if !errors.Is(err, errAuthTimeout) {
			conn.Close(closeAuth, err.Error())
		}
		return
	}
	if h.userConnections(p.ID) >= maxConnsPerUser {
		conn.Close(websocket.StatusPolicyViolation, "too many connections")
		return
	}

	c := &Client{userID: p.ID, role: p.Role, send: make(chan []byte, sendBuffer)}
	kicked := make(chan string, 1)
	c.kick = func(reason string) {
		select {
		case kicked <- reason:
		default:
		}
		cancel()
	}
	h.hub.register(c)
	defer h.hub.unregister(c)
	log := h.log.With("user_id", p.ID, "role", p.Role, "remote", r.RemoteAddr)
	log.Info("websocket connected")

	c.send <- encode("auth.ok", map[string]any{"user_id": p.ID, "role": p.Role, "expires_at": exp.UTC()})

	expiry := make(chan time.Time, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.writeLoop(ctx, conn, c, exp, expiry, kicked, cancel)
	}()
	h.readLoop(ctx, conn, c, p, expiry)
	cancel()
	<-done
	log.Info("websocket disconnected")
}

func (h *Handler) userConnections(userID int64) int {
	h.hub.mu.Lock()
	defer h.hub.mu.Unlock()
	n := 0
	for c := range h.hub.clients {
		if c.userID == userID {
			n++
		}
	}
	return n
}

var (
	errAuthRequired = errors.New("authentication required")
	errAuthTimeout  = errors.New("authentication timed out") // connection already closed
)

func (h *Handler) authenticate(ctx context.Context, conn *websocket.Conn) (auth.Principal, time.Time, error) {
	// A timer closes with 4001 when no auth arrives in time. (Cancelling the
	// Read context instead would make the library drop the connection
	// without a close frame, so the client could not tell why.)
	timer := time.AfterFunc(AuthTimeout, func() { conn.Close(closeAuth, errAuthRequired.Error()) })
	_, b, err := conn.Read(ctx)
	if !timer.Stop() {
		return auth.Principal{}, time.Time{}, errAuthTimeout
	}
	if err != nil {
		return auth.Principal{}, time.Time{}, errAuthRequired
	}
	var m inbound
	var d authData
	if json.Unmarshal(b, &m) != nil || m.Type != "auth" || json.Unmarshal(m.Data, &d) != nil || d.Token == "" {
		return auth.Principal{}, time.Time{}, errAuthRequired
	}
	p, exp, err := h.tokens.VerifyAccessTokenExpiry(d.Token)
	if err != nil {
		return auth.Principal{}, time.Time{}, errors.New("invalid or expired token")
	}
	return p, exp, nil
}

func (h *Handler) readLoop(ctx context.Context, conn *websocket.Conn, c *Client, p auth.Principal, expiry chan<- time.Time) {
	reply := func(typ string, data any) {
		select {
		case c.send <- encode(typ, data):
		default:
		}
	}
	for {
		_, b, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var m inbound
		if err := json.Unmarshal(b, &m); err != nil {
			reply("error", map[string]string{"message": "invalid JSON"})
			continue
		}
		switch m.Type {
		case "auth":
			var d authData
			np, exp, err := func() (auth.Principal, time.Time, error) {
				if err := json.Unmarshal(m.Data, &d); err != nil {
					return auth.Principal{}, time.Time{}, err
				}
				return h.tokens.VerifyAccessTokenExpiry(d.Token)
			}()
			if err != nil || np != p {
				reply("error", map[string]string{"message": "invalid token for this connection"})
				continue
			}
			select {
			case expiry <- exp:
			default:
			}
			reply("auth.ok", map[string]any{"user_id": p.ID, "role": p.Role, "expires_at": exp.UTC()})
		case "subscribe":
			var d subscribeData
			if err := json.Unmarshal(m.Data, &d); err != nil || d.SessionID <= 0 {
				reply("error", map[string]string{"message": "subscribe needs session_id"})
				continue
			}
			if err := h.authorizeSubscribe(ctx, p, d.SessionID); err != nil {
				reply("error", map[string]string{"message": err.Error()})
				continue
			}
			h.hub.subscribe(c, d.SessionID)
			reply("subscribed", map[string]int64{"session_id": d.SessionID})
			h.hub.sendScreenList(c)
		case "screen.state":
			var d screenStateData
			if err := json.Unmarshal(m.Data, &d); err != nil {
				reply("error", map[string]string{"message": "screen.state needs sharing"})
				continue
			}
			if err := h.hub.setScreen(c, d.Sharing); err != nil {
				reply("error", map[string]string{"message": err.Error()})
			}
		case "screen.view":
			var d screenViewData
			if err := json.Unmarshal(m.Data, &d); err != nil || d.StudentID <= 0 {
				reply("error", map[string]string{"message": "screen.view needs student_id"})
				continue
			}
			if _, err := h.hub.startView(c, d.StudentID); err != nil {
				reply("error", map[string]string{"message": err.Error()})
			}
		case "webrtc.signal":
			var d signalData
			if err := json.Unmarshal(m.Data, &d); err != nil {
				reply("error", map[string]string{"message": "invalid webrtc.signal"})
				continue
			}
			if err := h.hub.relaySignal(c, d); err != nil {
				reply("error", map[string]string{"message": err.Error()})
			}
		case "screen.stop":
			var d viewIDData
			if err := json.Unmarshal(m.Data, &d); err != nil {
				reply("error", map[string]string{"message": "screen.stop needs view_id"})
				continue
			}
			if err := h.hub.stopView(c, d.ViewID); err != nil {
				reply("error", map[string]string{"message": err.Error()})
			}
		default:
			reply("error", map[string]string{"message": "unknown message type " + m.Type})
		}
	}
}

// authorizeSubscribe: teachers only for sessions of their exams; students
// for any existing session (they see every session, BACKEND_CONTEXT.md §17,
// and only ever receive their own results).
func (h *Handler) authorizeSubscribe(ctx context.Context, p auth.Principal, sessionID int64) error {
	si, err := h.hub.loader.SessionInfo(ctx, sessionID)
	if err != nil {
		if errors.Is(err, apperr.ErrNotFound) {
			return errors.New("session not found")
		}
		return errors.New("cannot load session")
	}
	if p.Role == auth.RoleTeacher && si.TeacherID != p.ID {
		return errors.New("session not found")
	}
	return nil
}

// writeLoop sends queued messages, pings to detect dead peers, and closes
// when the token expires (unless a new auth extended it).
func (h *Handler) writeLoop(ctx context.Context, conn *websocket.Conn, c *Client, exp time.Time,
	expiry <-chan time.Time, kicked <-chan string, cancel context.CancelFunc) {
	ping := time.NewTicker(PingInterval)
	defer ping.Stop()
	expire := time.NewTimer(time.Until(exp))
	defer expire.Stop()
	for {
		select {
		case reason := <-kicked:
			conn.Close(websocket.StatusPolicyViolation, reason)
			return
		case <-ctx.Done():
			select {
			case reason := <-kicked:
				conn.Close(websocket.StatusPolicyViolation, reason)
			default:
				conn.Close(websocket.StatusNormalClosure, "")
			}
			return
		case msg := <-c.send:
			wctx, wcancel := context.WithTimeout(ctx, WriteTimeout)
			err := conn.Write(wctx, websocket.MessageText, msg)
			wcancel()
			if err != nil {
				cancel()
				return
			}
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, WriteTimeout)
			err := conn.Ping(pctx)
			pcancel()
			if err != nil { // no pong: the peer is gone (network lost)
				cancel()
				conn.CloseNow()
				return
			}
		case t := <-expiry:
			if !expire.Stop() {
				select {
				case <-expire.C:
				default:
				}
			}
			expire.Reset(time.Until(t))
		case <-expire.C:
			conn.Close(closeAuth, tokenExpiredText)
			cancel()
			return
		}
	}
}
