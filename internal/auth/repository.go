package auth

import "github.com/jackc/pgx/v5/pgxpool"

type pgRepository struct {
	pool *pgxpool.Pool
}

var _ Repository = (*pgRepository)(nil)

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{pool: pool}
}
