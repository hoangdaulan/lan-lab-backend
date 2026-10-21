// Package submission receives source files and creates submission + judge
// run + judge job in one transaction (BACKEND_CONTEXT.md §7, §10).
package submission

// Repository is the persistence boundary for submissions. Methods are added
// with the submission feature; the pgx implementation lives in repository.go.
type Repository interface{}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}
