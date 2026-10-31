package judge

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hoangdaulan/lan-lab-backend/internal/events"
)

// ListenForWork wakes the worker on every judge event (a new submission,
// rejudge, or a job released for retry) so queued jobs are claimed without
// waiting for the poll interval. It reconnects on errors; while
// disconnected the worker still polls.
func (w *Worker) ListenForWork(ctx context.Context, databaseURL string, log *slog.Logger) {
	ListenEvents(ctx, databaseURL, w.Wake, log)
}

// ListenEvents calls onEvent for every notification on the judge events
// channel, reconnecting with backoff until ctx ends. Callers must keep a
// polling fallback for the time it is disconnected.
func ListenEvents(ctx context.Context, databaseURL string, onEvent func(), log *slog.Logger) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := func() error {
			conn, err := pgx.Connect(ctx, databaseURL)
			if err != nil {
				return err
			}
			defer conn.Close(context.Background())
			if _, err := conn.Exec(ctx, "LISTEN "+events.Channel); err != nil {
				return err
			}
			backoff = time.Second
			for {
				if _, err := conn.WaitForNotification(ctx); err != nil {
					return err
				}
				onEvent()
			}
		}()
		if ctx.Err() != nil {
			return
		}
		log.Warn("work listener disconnected, polling only until reconnect", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 10*time.Second)
	}
}
