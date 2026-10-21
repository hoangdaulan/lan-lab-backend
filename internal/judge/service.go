// Package judge claims queued judge jobs, compiles and runs C++17 submissions
// in a sandbox and records verdicts (BACKEND_CONTEXT.md §10, §11). The API
// side of this package (handler.go) serves judge status to clients.
package judge

// Repository is the persistence boundary for the judge queue. Methods (claim
// job with FOR UPDATE SKIP LOCKED, renew lease, finish run, recover expired
// leases) are added in the judge step; the pgx implementation lives in
// repository.go.
type Repository interface{}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}
