package realtime

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hoangdaulan/lan-lab-backend/internal/events"
)

// Listener keeps one dedicated PostgreSQL connection on LISTEN judge_events
// and feeds notifications to the hub. When the connection drops, events in
// between are lost, so after reconnecting it sends resync to every client
// (they reload over REST) — the polling fallback of BACKEND_CONTEXT.md §19.
type Listener struct {
	databaseURL string
	hub         *Hub
	log         *slog.Logger
}

func NewListener(databaseURL string, hub *Hub, log *slog.Logger) *Listener {
	return &Listener{databaseURL: databaseURL, hub: hub, log: log}
}

func (l *Listener) Run(ctx context.Context) {
	backoff := time.Second
	first := true
	for ctx.Err() == nil {
		err := l.listen(ctx, first)
		if ctx.Err() != nil {
			return
		}
		l.log.Warn("event listener disconnected, reconnecting", "err", err, "in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 10*time.Second)
		first = false
	}
}

func (l *Listener) listen(ctx context.Context, first bool) error {
	conn, err := pgx.Connect(ctx, l.databaseURL)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(ctx, "LISTEN "+events.Channel); err != nil {
		return err
	}
	l.log.Info("listening for judge events", "channel", events.Channel)
	if !first {
		l.hub.Resync()
	}
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		e, err := events.Parse(n.Payload)
		if err != nil {
			l.log.Warn("ignore notification", "err", err)
			continue
		}
		l.hub.HandleEvent(ctx, e)
	}
}
