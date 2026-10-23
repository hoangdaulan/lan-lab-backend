// Command seed creates the first teacher account from environment variables:
//
//	SEED_TEACHER_CODE, SEED_TEACHER_FULLNAME, SEED_TEACHER_PASSWORD
//
// It is idempotent: if the code already exists nothing is changed. Teachers
// are not created over HTTP (BACKEND_CONTEXT.md §17).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/hoangdaulan/lan-lab-backend/internal/auth"
	"github.com/hoangdaulan/lan-lab-backend/internal/config"
	"github.com/hoangdaulan/lan-lab-backend/internal/db"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("seed failed", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	code := os.Getenv("SEED_TEACHER_CODE")
	fullname := os.Getenv("SEED_TEACHER_FULLNAME")
	password := os.Getenv("SEED_TEACHER_PASSWORD")
	if code == "" || fullname == "" || password == "" {
		return errors.New("SEED_TEACHER_CODE, SEED_TEACHER_FULLNAME and SEED_TEACHER_PASSWORD are required")
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	// The signer is unused here; CreateTeacher only hashes and inserts.
	svc := auth.NewService(auth.NewRepository(pool), nil, cfg.RefreshTokenTTL)
	acct, err := svc.CreateTeacher(ctx, code, fullname, password)
	if errors.Is(err, auth.ErrAlreadyExists) {
		log.Info("teacher already exists, nothing changed", "code", auth.NormalizeCode(code))
		return nil
	}
	if err != nil {
		return fmt.Errorf("create teacher: %w", err)
	}
	log.Info("teacher created", "id", acct.ID, "code", acct.Code)
	return nil
}
