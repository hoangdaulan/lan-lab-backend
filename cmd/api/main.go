// Command api serves the REST/WebSocket API for teachers and students on the LAN.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hoangdaulan/lan-lab-backend/internal/auth"
	"github.com/hoangdaulan/lan-lab-backend/internal/board"
	"github.com/hoangdaulan/lan-lab-backend/internal/config"
	"github.com/hoangdaulan/lan-lab-backend/internal/db"
	"github.com/hoangdaulan/lan-lab-backend/internal/exam"
	"github.com/hoangdaulan/lan-lab-backend/internal/httpx"
	"github.com/hoangdaulan/lan-lab-backend/internal/problem"
	"github.com/hoangdaulan/lan-lab-backend/internal/realtime"
	"github.com/hoangdaulan/lan-lab-backend/internal/student"
	"github.com/hoangdaulan/lan-lab-backend/internal/submission"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("api exited", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.RequireAPI(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Feature wiring: repository -> service -> handler, routes under /api/v1
	// (docs/api-v1.md).
	authSvc := auth.NewService(auth.NewRepository(pool),
		auth.NewJWTSigner(cfg.JWTSecret, cfg.AccessTokenTTL), cfg.RefreshTokenTTL)
	authHandler := auth.NewHandler(authSvc, log)
	studentHandler := student.NewHandler(student.NewService(student.NewRepository(pool), authSvc), log)
	examHandler := exam.NewHandler(exam.NewService(exam.NewRepository(pool), nil), log)
	problemHandler := problem.NewHandler(problem.NewService(problem.NewRepository(pool), cfg.DataDir, nil, log), log)
	submissionRepo := submission.NewRepository(pool)
	submissionSvc := submission.NewService(submissionRepo, submission.NewStorage(cfg.DataDir), nil, log)
	submissionHandler := submission.NewHandler(submissionSvc, log)
	go submissionSvc.RunOrphanCleanup(ctx, 30*time.Minute)

	// Realtime: LISTEN judge_events -> hub -> WebSocket clients
	// (BACKEND_CONTEXT.md §19, phase 11).
	boardSvc := board.NewService(board.NewRepository(pool), board.NewAnnouncements())
	hub := realtime.NewHub(realtime.Sources{Submissions: submissionRepo, Board: boardSvc}, log)
	boardSvc.SetBroadcaster(hub)
	boardHandler := board.NewHandler(boardSvc, log)
	wsHandler := realtime.NewHandler(hub, authSvc, cfg.WSAllowedOrigins, log)
	go realtime.NewListener(cfg.DatabaseURL, hub, log).Run(ctx)
	go hub.RunSummaries(ctx)

	router := httpx.NewRouter(pool)
	router.Route("/api/v1", func(r chi.Router) {
		authHandler.Register(r)
		// Authenticated by its first message, not by the Bearer middleware.
		r.Get("/ws", wsHandler.ServeHTTP)

		r.Group(func(r chi.Router) {
			r.Use(auth.Authenticate(authSvc))
			examHandler.RegisterShared(r)
			submissionHandler.RegisterShared(r)
			boardHandler.RegisterShared(r)

			r.Group(func(r chi.Router) {
				r.Use(auth.RequireRole(auth.RoleTeacher))
				studentHandler.Register(r)
				examHandler.RegisterTeacher(r)
				problemHandler.RegisterTeacher(r)
				submissionHandler.RegisterTeacher(r)
				boardHandler.RegisterTeacher(r)
			})
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireRole(auth.RoleStudent))
				examHandler.RegisterStudent(r)
				problemHandler.RegisterStudent(r)
				submissionHandler.RegisterStudent(r)
				boardHandler.RegisterStudent(r)
			})
		})
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Info("api shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
