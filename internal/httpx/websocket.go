package httpx

import (
	"net/http"

	"github.com/coder/websocket"
)

// AcceptWebSocket upgrades a request for judge status push. WebSocket only
// notifies; REST stays the source of truth after reload/reconnect
// (BACKEND_CONTEXT.md §11). Origin checks and the message format are decided
// in the realtime step.
func AcceptWebSocket(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	return websocket.Accept(w, r, nil)
}
