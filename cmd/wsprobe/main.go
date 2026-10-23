// Command wsprobe is a WebSocket client for demos and smoke tests: it logs
// in over REST, opens /api/v1/ws, subscribes to a session and prints every
// message as one JSON line (with a receive timestamp) until -duration ends.
//
//	go run ./cmd/wsprobe -base http://192.168.1.9:8080 -code B21DCCN001 -password ... -session 3
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
)

func main() {
	base := flag.String("base", "http://localhost:8080", "API base URL")
	code := flag.String("code", "", "account code")
	password := flag.String("password", "", "password (or WSPROBE_PASSWORD)")
	session := flag.Int64("session", 0, "session to subscribe to (0 = none)")
	duration := flag.Duration("duration", 30*time.Second, "how long to listen")
	flag.Parse()
	if *password == "" {
		*password = os.Getenv("WSPROBE_PASSWORD")
	}
	if err := run(*base, *code, *password, *session, *duration); err != nil {
		fmt.Fprintln(os.Stderr, "wsprobe:", err)
		os.Exit(1)
	}
}

func run(base, code, password string, session int64, d time.Duration) error {
	body, _ := json.Marshal(map[string]string{"code": code, "password": password})
	resp, err := http.Post(base+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var login struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&login); err != nil || login.AccessToken == "" {
		return fmt.Errorf("login failed: %s %s", resp.Status, login.Error)
	}

	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(base, "http") + "/api/v1/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	send := func(typ string, data any) error {
		b, _ := json.Marshal(map[string]any{"type": typ, "data": data})
		return conn.Write(ctx, websocket.MessageText, b)
	}
	if err := send("auth", map[string]string{"token": login.AccessToken}); err != nil {
		return err
	}
	if session > 0 {
		if err := send("subscribe", map[string]int64{"session_id": session}); err != nil {
			return err
		}
	}
	for {
		_, b, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				conn.Close(websocket.StatusNormalClosure, "")
				return nil
			}
			return fmt.Errorf("closed: %v (status %d)", err, websocket.CloseStatus(err))
		}
		fmt.Printf("%s %s\n", time.Now().UTC().Format("15:04:05.000"), b)
	}
}
