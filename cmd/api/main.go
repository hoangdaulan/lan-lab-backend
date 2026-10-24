// Command api serves the REST/WebSocket API for teachers and students on the LAN.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
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
	"github.com/hoangdaulan/lan-lab-backend/internal/tlscert"
	"github.com/hoangdaulan/lan-lab-backend/web"
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

	web.Mount(router)

	// Plain HTTP keeps serving the API (scripts, tools on the server); a
	// browser opening the page from another machine is sent to HTTPS, where
	// screen sharing is allowed.
	httpHandler := http.Handler(router)
	var tlsCert, tlsKey string
	if cfg.HTTPSAddr != "" {
		hosts := tlscert.RequiredHosts(cfg.TLSHosts)
		var created bool
		tlsCert, tlsKey, created, err = tlscert.Ensure(cfg.TLSDir, hosts, tlscert.InterfaceIPs(), time.Now())
		if err != nil {
			return fmt.Errorf("tls certificate: %w", err)
		}
		log.Info("tls certificate", "file", tlsCert, "created", created, "hosts", hosts)
		httpHandler = redirectPageToHTTPS(router, cfg.HTTPSPublicPort)
	}

	servers := []*http.Server{{Addr: cfg.HTTPAddr, Handler: httpHandler, ReadHeaderTimeout: 10 * time.Second}}
	if cfg.HTTPSAddr != "" {
		servers = append(servers, &http.Server{Addr: cfg.HTTPSAddr, Handler: router, ReadHeaderTimeout: 10 * time.Second})
	}

	errCh := make(chan error, len(servers))
	for i, srv := range servers {
		go func() {
			var err error
			if i == 0 {
				log.Info("api listening", "addr", srv.Addr)
				err = srv.ListenAndServe()
			} else {
				log.Info("api listening (https)", "addr", srv.Addr)
				err = srv.ListenAndServeTLS(tlsCert, tlsKey)
			}
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
			}
		}()
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Info("api shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var errs []error
	for _, srv := range servers {
		errs = append(errs, srv.Shutdown(shutdownCtx))
	}
	return errors.Join(errs...)
}

// redirectPageToHTTPS sends browsers that open the web page over plain HTTP
// from another machine (Host is not localhost) to the HTTPS port. API calls
// and requests to localhost are served as is.
func redirectPageToHTTPS(next http.Handler, httpsPort string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(r.Host); err == nil {
			host = h
		}
		if r.Method == http.MethodGet && r.URL.Path == "/" && !isLocalHost(host) {
			http.Redirect(w, r, "https://"+net.JoinHostPort(host, httpsPort)+"/", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLocalHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
