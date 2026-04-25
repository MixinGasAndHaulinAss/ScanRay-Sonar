// Package api wires the HTTP/websocket surface of sonar-api: routing,
// middleware, JSON handlers, OpenAPI/static asset serving, and the
// websocket hubs for agents and UI clients.
package api

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"

	"github.com/NCLGISA/ScanRay-Sonar/internal/auth"
	"github.com/NCLGISA/ScanRay-Sonar/internal/config"
	"github.com/NCLGISA/ScanRay-Sonar/internal/db"
)

// Server is the long-lived HTTP/WS service. Construct once via New and
// then call ListenAndServe.
type Server struct {
	cfg   *config.Config
	log   *slog.Logger
	pool  *pgxpool.Pool
	store *db.Store
	iss   *auth.Issuer
	nats  *nats.Conn

	agentHub *Hub
	uiHub    *Hub

	openAPISpec []byte
	webFS       fs.FS
}

// Deps bundles the dependencies a Server needs. Tests wire these up
// directly; main.go does the same with concrete implementations.
type Deps struct {
	Config      *config.Config
	Logger      *slog.Logger
	Pool        *pgxpool.Pool
	Store       *db.Store
	Issuer      *auth.Issuer
	NATS        *nats.Conn
	OpenAPISpec []byte
	WebFS       fs.FS
}

func New(d Deps) *Server {
	return &Server{
		cfg:         d.Config,
		log:         d.Logger,
		pool:        d.Pool,
		store:       d.Store,
		iss:         d.Issuer,
		nats:        d.NATS,
		agentHub:    NewHub(d.Logger.With(slog.String("hub", "agent"))),
		uiHub:       NewHub(d.Logger.With(slog.String("hub", "ui"))),
		openAPISpec: d.OpenAPISpec,
		webFS:       d.WebFS,
	}
}

// Routes builds the chi router. Exposed for tests.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(slogRequestLogger(s.log))

	allowedOrigins := []string{s.cfg.PublicURL}
	if s.cfg.Env != "production" {
		allowedOrigins = append(allowedOrigins, "http://localhost:5173", "http://127.0.0.1:5173")
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Route("/api/v1", func(r chi.Router) {
		// Public.
		r.Get("/healthz", s.handleHealthz)
		r.Get("/version", s.handleVersion)
		r.Get("/openapi.yaml", s.handleOpenAPI)
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/refresh", s.handleRefresh)

		// Authenticated.
		r.Group(func(r chi.Router) {
			r.Use(s.authRequired)
			r.Get("/auth/me", s.handleMe)

			r.Get("/sites", s.handleListSites)
			r.With(requireRole(auth.RoleSuperAdmin)).Post("/sites", s.handleCreateSite)

			r.With(requireRole(auth.RoleSuperAdmin)).Get("/users", s.handleListUsers)
			r.With(requireRole(auth.RoleSuperAdmin)).Post("/users", s.handleCreateUser)

			r.Get("/agents", s.handleListAgents)
			r.Get("/appliances", s.handleListAppliances)
		})
	})

	// Agent ingest websocket. Authentication for this is handled
	// in-protocol once Phase 2 lands.
	r.HandleFunc("/agent/ws", func(w http.ResponseWriter, req *http.Request) {
		s.agentHub.Serve(w, req, "agent")
	})

	// UI live-updates websocket (authenticated).
	r.With(s.authRequired).HandleFunc("/ws", func(w http.ResponseWriter, req *http.Request) {
		s.uiHub.Serve(w, req, "ui")
	})

	// Static SPA fallback. Anything not under /api/, /agent/, or /ws
	// returns the embedded React build (with index.html for unknown paths).
	r.NotFound(s.serveSPA)

	return r
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(s.openAPISpec)
}

// serveSPA serves the embedded web/dist filesystem. SPA-style: any
// unknown path falls back to index.html so client-side routing works.
func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	if s.webFS == nil {
		writeErr(w, http.StatusNotFound, "not_found", "no embedded web build")
		return
	}
	clean := strings.TrimPrefix(r.URL.Path, "/")
	if clean == "" {
		clean = "index.html"
	}
	if _, err := fs.Stat(s.webFS, clean); err != nil {
		// SPA fallback to index.html for unknown paths.
		clean = "index.html"
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/" + clean
	http.FileServer(http.FS(s.webFS)).ServeHTTP(w, r2)
}

// ListenAndServe starts the HTTP server and blocks until ctx is done.
func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.cfg.BindAddr,
		Handler:           s.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		s.log.Info("listening", "addr", s.cfg.BindAddr, "env", s.cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		s.log.Info("shutdown requested")
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	case err := <-errCh:
		return err
	}
}

// slogRequestLogger emits a JSON line per request via slog.
func slogRequestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"dur_ms", time.Since(start).Milliseconds(),
				"req_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}
