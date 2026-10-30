// Package events carries "something changed" signals from the writers of
// judge state (API transactions and the worker) to the API's WebSocket hub
// through PostgreSQL NOTIFY/LISTEN (BACKEND_CONTEXT.md §19, phase 11).
//
// Notifications are sent with pg_notify inside the writing transaction, so
// PostgreSQL delivers them only after COMMIT: a client is never told about
// a result that was rolled back. Payloads only name what changed; the
// listener reloads the current state from the database before pushing it.
package events

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Channel is the LISTEN/NOTIFY channel name.
const Channel = "judge_events"

// Kind of a notification.
type Kind byte

const (
	// KindSubmission: the current run of one submission changed state.
	KindSubmission Kind = 's'
	// KindProblem: every submission of a problem was requeued (rejudge or
	// limit change); the listener fans out one update per submission.
	KindProblem Kind = 'p'
)

type Event struct {
	Kind Kind
	ID   int64
}

func (e Event) payload() string { return string(e.Kind) + ":" + strconv.FormatInt(e.ID, 10) }

// Parse decodes a notification payload ("s:12", "p:3").
func Parse(payload string) (Event, error) {
	k, id, ok := strings.Cut(payload, ":")
	if !ok || len(k) != 1 || (Kind(k[0]) != KindSubmission && Kind(k[0]) != KindProblem) {
		return Event{}, fmt.Errorf("bad event payload %q", payload)
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return Event{}, fmt.Errorf("bad event payload %q", payload)
	}
	return Event{Kind: Kind(k[0]), ID: n}, nil
}

// Submission queues a notification that submissionID's current run changed.
func Submission(ctx context.Context, tx pgx.Tx, submissionID int64) error {
	return notify(ctx, tx, Event{Kind: KindSubmission, ID: submissionID})
}

// Problem queues a notification that all submissions of problemID changed.
func Problem(ctx context.Context, tx pgx.Tx, problemID int64) error {
	return notify(ctx, tx, Event{Kind: KindProblem, ID: problemID})
}

// SubmissionOfRun notifies for the submission owning runID.
func SubmissionOfRun(ctx context.Context, tx pgx.Tx, runID int64) error {
	_, err := tx.Exec(ctx,
		`SELECT pg_notify($1, 's:' || submission_id) FROM judge_runs WHERE id = $2`, Channel, runID)
	return err
}

func notify(ctx context.Context, tx pgx.Tx, e Event) error {
	_, err := tx.Exec(ctx, `SELECT pg_notify($1, $2)`, Channel, e.payload())
	return err
}
