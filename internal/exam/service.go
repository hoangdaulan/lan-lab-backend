// Package exam manages exams, exam sessions and student participation
// (BACKEND_CONTEXT.md §5).
package exam

// Repository is the persistence boundary for exams. Methods are added with the
// exam feature; the pgx implementation lives in repository.go.
type Repository interface{}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}
