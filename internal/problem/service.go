// Package problem manages problems, their test sets and versioning
// (BACKEND_CONTEXT.md §6, §8).
package problem

// Repository is the persistence boundary for problems. Methods are added with
// the problem feature; the pgx implementation lives in repository.go.
type Repository interface{}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}
