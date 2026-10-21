// Package auth handles teacher/student login, JWT access tokens and refresh
// token rotation (BACKEND_CONTEXT.md §4).
package auth

// Repository is the persistence boundary for auth. Methods are added with the
// login/refresh feature; the pgx implementation lives in repository.go.
type Repository interface{}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}
