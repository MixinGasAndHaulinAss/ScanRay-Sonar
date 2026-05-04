// Package api wires the HTTP/websocket surface of sonar-api: routing,
// middleware, JSON handlers, OpenAPI/static asset serving, and the
// websocket hubs for agents and UI clients.
package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"

	"github.com/NCLGISA/ScanRay-Sonar/internal/auth"
	"github.com/NCLGISA/ScanRay-Sonar/internal/config"
	scrypto "github.com/NCLGISA/ScanRay-Sonar/internal/crypto"
	"github.com/NCLGISA/ScanRay-Sonar/internal/db"
	"github.com/NCLGISA/ScanRay-Sonar/internal/geoip"
)

// Server is the long-lived HTTP/WS service. Construct once via New and
// then call ListenAndServe.
type Server struct {
	cfg    *config.Config
	log    *slog.Logger
	pool   *pgxpool.Pool
	store  *db.Store
	iss    *auth.Issuer
	sealer *scrypto.Sealer
	nats   *nats.Conn
	geo    *geoip.Reader

	agentHub *Hub
	uiHub    *Hub

	openAPISpec []byte
	webFS       fs.FS
	probeFS     fs.FS
}

// Deps bundles the dependencies a Server needs. Tests wire these up
// directly; main.go does the same with concrete implementations.
type Deps struct {
	Config      *config.Config
	Logger      *slog.Logger
	Pool        *pgxpool.Pool
	Store       *db.Store
	Issuer      *auth.Issuer
	Sealer      *scrypto.Sealer
	NATS        *nats.Conn
	OpenAPISpec []byte
	WebFS       fs.FS
	// ProbeFS holds cross-compiled probe binaries laid out as
	// "linux/amd64/sonar-probe", "linux/arm64/sonar-probe", etc. May be nil
	// in dev builds; the /probe/download endpoint returns 404 when so.
	ProbeFS fs.FS
	// Geo is the optional MaxMind GeoLite2 reader used to enrich
	// agent telemetry with country/city/ASN data. May be nil; in
	// that case GeoIP-driven UI (world map, network topology
	// provider labels) gracefully degrades.
	Geo *geoip.Reader
}

func New(d Deps) *Server {
	return &Server{
		cfg:         d.Config,
		log:         d.Logger,
		pool:        d.Pool,
		store:       d.Store,
		iss:         d.Issuer,
		sealer:      d.Sealer,
		nats:        d.NATS,
		geo:         d.Geo,
		agentHub:    NewHub(d.Logger.With(slog.String("hub", "agent"))),
		uiHub:       NewHub(d.Logger.With(slog.String("hub", "ui"))),
		openAPISpec: d.OpenAPISpec,
		webFS:       d.WebFS,
		probeFS:     d.ProbeFS,
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

		// Probe enrollment is unauthenticated at the bearer-token layer —
		// the request body carries the single-use enrollment token issued
		// by an operator. Probe binary download is also unauthenticated
		// because the install one-liner runs before any JWT exists.
		r.Post("/agents/enroll", s.handleAgentEnroll)
		r.Get("/probe/download/{os}/{arch}", s.handleProbeDownload)
		r.Get("/probe/install.sh", s.handleProbeInstallScript)
		r.Get("/probe/install.ps1", s.handleProbeInstallScriptPS1)

		// Authenticated.
		r.Group(func(r chi.Router) {
			r.Use(s.authRequired)
			r.Get("/auth/me", s.handleMe)

			r.Get("/sites", s.handleListSites)
			r.With(requireRole(auth.RoleSuperAdmin)).Post("/sites", s.handleCreateSite)
			r.With(requireRole(auth.RoleSuperAdmin)).Patch("/sites/{id}", s.handleUpdateSite)
			r.With(requireRole(auth.RoleSuperAdmin)).Delete("/sites/{id}", s.handleDeleteSite)

			r.With(requireRole(auth.RoleSuperAdmin)).Get("/users", s.handleListUsers)
			r.With(requireRole(auth.RoleSuperAdmin)).Post("/users", s.handleCreateUser)
			r.With(requireRole(auth.RoleSuperAdmin)).Patch("/users/{id}", s.handleUpdateUser)
			r.With(requireRole(auth.RoleSuperAdmin)).Delete("/users/{id}", s.handleDeleteUser)

			r.Get("/agents", s.handleListAgents)
			// Overview aggregation endpoints. Mounted under
			// /agents/overview so chi resolves them before
			// /agents/{id} (which would otherwise match
			// /agents/overview as id=overview).
			r.Get("/agents/overview/devices-averages", s.handleOverviewDevicesAverages)
			r.Get("/agents/overview/devices-performance", s.handleOverviewDevicesPerformance)
			r.Get("/agents/overview/network-latency", s.handleOverviewNetworkLatency)
			r.Get("/agents/overview/network-performance", s.handleOverviewNetworkPerformance)
			r.Get("/agents/overview/applications-performance", s.handleOverviewApplicationsPerformance)
			r.Get("/agents/overview/user-experience", s.handleOverviewUserExperience)
			// chi matches static segments before wildcards, so the
			// enrollment-tokens routes below win over /agents/{id}
			// even though both look like "/agents/<something>".
			r.Get("/agents/{id}", s.handleGetAgent)
			r.Get("/agents/{id}/metrics", s.handleAgentMetrics)
			r.Get("/agents/{id}/network", s.handleAgentNetwork)
			r.Get("/agents/{id}/latency", s.handleAgentLatency)
			r.Get("/agents/{id}/network-graph", s.handleAgentNetworkGraph)
			r.With(requireRole(auth.RoleSiteAdmin)).Patch("/agents/{id}", s.handleUpdateAgent)
			r.With(requireRole(auth.RoleSiteAdmin)).Delete("/agents/{id}", s.handleDeleteAgent)
			r.With(requireRole(auth.RoleSiteAdmin)).Get("/agents/enrollment-tokens", s.handleListEnrollmentTokens)
			r.With(requireRole(auth.RoleSiteAdmin)).Post("/agents/enrollment-tokens", s.handleCreateEnrollmentToken)
			r.With(requireRole(auth.RoleSiteAdmin)).Delete("/agents/enrollment-tokens/{id}", s.handleRevokeEnrollmentToken)

			r.Get("/appliances", s.handleListAppliances)
			r.With(requireRole(auth.RoleSiteAdmin)).Post("/appliances", s.handleCreateAppliance)
			r.Get("/appliances/{id}", s.handleGetAppliance)
			r.Get("/appliances/{id}/metrics", s.handleApplianceMetrics)
			r.Get("/appliances/{id}/interfaces/{ifIndex}/metrics", s.handleApplianceIfaceMetrics)
			r.With(requireRole(auth.RoleSiteAdmin)).Patch("/appliances/{id}", s.handleUpdateAppliance)
			r.With(requireRole(auth.RoleSiteAdmin)).Delete("/appliances/{id}", s.handleDeleteAppliance)

			r.Get("/topology", s.handleTopology)
		})
	})

	// Agent ingest websocket. Authentication is in-protocol via a
	// ?token=<agent-jwt> query parameter (browsers and CLI clients
	// alike can attach query strings, while Authorization headers
	// require a custom client because most websocket libraries don't
	// expose them on Upgrade).
	r.HandleFunc("/agent/ws", s.handleAgentWS)

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
//
// We deliberately do NOT use http.FileServer here because it
// "canonicalizes" requests for /index.html into a 301 to ./ and treats
// directory paths the same way, which produces redirect loops when the
// router below it (chi) is also routing /. Reading the asset and
// streaming it via http.ServeContent skips that behavior entirely.
//
// Cache strategy:
//   - Vite emits content-hashed filenames under /assets/ (e.g.
//     assets/index-If7J8Sj9.js). Those are safe to cache forever:
//     a new bundle gets a new filename, so a stale URL can never
//     refer to outdated content. We send "immutable" so browsers
//     and Cloudflare both pin them.
//   - index.html and any other root-level file have stable URLs
//     and DO change every deploy. Without explicit headers, both
//     Cloudflare and browsers apply heuristic caching, which is
//     what causes "I deployed but the UI didn't change" — the
//     user's index.html still references the previous bundle.
//     We force "no-cache, must-revalidate" + a content ETag so
//     every request is conditional and a redeploy is reflected on
//     the next page load.
func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	if s.webFS == nil {
		writeErr(w, http.StatusNotFound, "not_found", "no embedded web build")
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" || name == "." {
		name = "index.html"
	}

	data, info, err := readAsset(s.webFS, name)
	if err != nil {
		// SPA fallback: anything we can't resolve becomes index.html so
		// the React Router takes over on the client.
		data, info, err = readAsset(s.webFS, "index.html")
		if err != nil {
			writeErr(w, http.StatusNotFound, "not_found", "no index.html in embedded build")
			return
		}
		name = "index.html"
	}

	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	}
	sum := sha256.Sum256(data)
	w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:16])+`"`)

	http.ServeContent(w, r, name, info.ModTime(), bytes.NewReader(data))
}

// readAsset opens a file in the embedded FS and returns its bytes plus
// FileInfo. Directories are treated as not-found so the SPA fallback
// path can take over.
func readAsset(efs fs.FS, name string) ([]byte, fs.FileInfo, error) {
	f, err := efs.Open(name)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if info.IsDir() {
		return nil, nil, fs.ErrNotExist
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, nil, err
	}
	return data, info, nil
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
