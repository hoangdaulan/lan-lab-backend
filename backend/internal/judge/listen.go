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
				w.Wake()
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
