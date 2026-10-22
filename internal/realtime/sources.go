package realtime

import (
	"context"

	"github.com/hoangdaulan/lan-lab-backend/internal/board"
	"github.com/hoangdaulan/lan-lab-backend/internal/submission"
)

// Sources implements Loader with the existing repositories.
type Sources struct {
	Submissions submission.Repository
	Board       *board.Service
}

func (s Sources) Submission(ctx context.Context, id int64) (submission.Submission, error) {
	return s.Submissions.Get(ctx, id)
}

func (s Sources) SubmissionIDsOfProblem(ctx context.Context, problemID int64) ([]int64, error) {
	return s.Board.Repo().SubmissionIDsOfProblem(ctx, problemID)
}

func (s Sources) SessionInfo(ctx context.Context, sessionID int64) (board.SessionInfo, error) {
	return s.Board.Repo().SessionInfo(ctx, sessionID)
}

func (s Sources) TeacherBoard(ctx context.Context, sessionID int64) (board.Summary, error) {
	return s.Board.SummaryForBroadcast(ctx, sessionID)
}
