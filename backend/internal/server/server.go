package server

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/hyperbolic2346/gatehouse/internal/api"
	"github.com/hyperbolic2346/gatehouse/internal/db"
	"github.com/hyperbolic2346/gatehouse/internal/frigate"
	"github.com/hyperbolic2346/gatehouse/internal/gate"
	"github.com/hyperbolic2346/gatehouse/internal/ws"
)

// Config holds the configuration for the HTTP server.
type Config struct {
	Port        int
	FrontendDir string
	JWTSecret   string
	FrigateURL  string
	MQTTBroker  string
	DBPath      string
}

// Server is the main HTTP server for the gatehouse application.
type Server struct {
	cfg        Config
	db         *db.DB
	hub        *ws.Hub
	frigate    *frigate.Client
	gateCtrl   *gate.Controller
	httpSrv    *http.Server
}

// New creates a new Server. The caller must supply an already-opened DB,
// a running WebSocket hub, a Frigate client, and a gate controller.
func New(cfg Config, database *db.DB, hub *ws.Hub, frigateClient *frigate.Client, gateCtrl *gate.Controller) *Server {
	return &Server{
		cfg:      cfg,
		db:       database,
		hub:      hub,
		frigate:  frigateClient,
		gateCtrl: gateCtrl,
	}
}

// Start builds the route tree and starts listening. It blocks until the
// server is shut down.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	authMW := api.AuthMiddleware(s.cfg.JWTSecret, s.db)

	authHandler := &api.AuthHandler{DB: s.db, JWTSecret: s.cfg.JWTSecret}
	eventsHandler := &api.EventsHandler{Frigate: s.frigate}
	gatesHandler := &api.GatesHandler{Controller: s.gateCtrl, DB: s.db}
	usersHandler := &api.UsersHandler{DB: s.db}

	// --- Public routes ---
	mux.HandleFunc("POST /api/login", authHandler.Login)

	// --- Authenticated routes ---
	mux.Handle("POST /api/logout", authMW(http.HandlerFunc(authHandler.Logout)))
	mux.Handle("GET /api/me", authMW(http.HandlerFunc(authHandler.Me)))

	// Events
	mux.Handle("GET /api/events", authMW(http.HandlerFunc(eventsHandler.List)))
	mux.Handle("DELETE /api/events/{id}", authMW(api.AdminOnly(http.HandlerFunc(eventsHandler.Delete))))
	mux.Handle("GET /api/events/{id}/thumbnail.jpg", authMW(http.HandlerFunc(eventsHandler.Thumbnail)))
	mux.Handle("GET /api/events/{id}/clip.mp4", authMW(http.HandlerFunc(eventsHandler.Clip)))

	// WebRTC
	mux.Handle("POST /api/webrtc/offer", authMW(http.HandlerFunc(eventsHandler.WebRTCOffer)))

	// Gates
	mux.Handle("GET /api/gates", authMW(http.HandlerFunc(gatesHandler.Status)))
	mux.Handle("POST /api/gates/{id}/open", authMW(http.HandlerFunc(gatesHandler.Open)))
	mux.Handle("POST /api/gates/{id}/hold", authMW(http.HandlerFunc(gatesHandler.Hold)))
	mux.Handle("POST /api/gates/{id}/release", authMW(http.HandlerFunc(gatesHandler.Release)))

	// Users (admin only)
	mux.Handle("GET /api/users", authMW(api.AdminOnly(http.HandlerFunc(usersHandler.List))))
	mux.Handle("POST /api/users", authMW(api.AdminOnly(http.HandlerFunc(usersHandler.Create))))
	mux.Handle("PUT /api/users/{id}", authMW(api.AdminOnly(http.HandlerFunc(usersHandler.Update))))
	mux.Handle("DELETE /api/users/{id}", authMW(api.AdminOnly(http.HandlerFunc(usersHandler.Delete))))

	// WebSocket
	mux.Handle("GET /api/ws", authMW(http.HandlerFunc(s.hub.HandleWebSocket)))

	// Static file serving with SPA fallback
	mux.Handle("/", spaHandler(s.cfg.FrontendDir))

	handler := corsMiddleware(mux)

	addr := fmt.Sprintf(":%d", s.cfg.Port)
	s.httpSrv = &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	log.Printf("HTTP server listening on %s", addr)
	return s.httpSrv.ListenAndServe()
}

// Shutdown gracefully shuts down the HTTP server, waiting for active
// connections to finish until the context is cancelled.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}

// spaHandler returns an http.Handler that serves static files from dir.
// If the requested file does not exist on disk the handler falls back to
// serving index.html so that client-side routing works.
func spaHandler(dir string) http.Handler {
	fileServer := http.FileServer(http.Dir(dir))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clean the path to prevent directory traversal.
		path := filepath.Clean(r.URL.Path)
		if path == "." {
			path = "/"
		}

		// Check if the file exists on disk.
		fullPath := filepath.Join(dir, path)
		info, err := os.Stat(fullPath)
		if err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		// If the path is a directory, check for index.html inside it.
		if err == nil && info.IsDir() {
			indexPath := filepath.Join(fullPath, "index.html")
			if _, err := os.Stat(indexPath); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// For paths that look like static assets (have a file extension),
		// return 404 rather than the SPA fallback.
		if ext := filepath.Ext(path); ext != "" && !isHTMLExt(ext) {
			// Check if it exists in the filesystem at all.
			if _, err := fs.Stat(os.DirFS(dir), strings.TrimPrefix(path, "/")); err != nil {
				http.NotFound(w, r)
				return
			}
		}

		// SPA fallback: serve index.html for all other routes.
		indexFile := filepath.Join(dir, "index.html")
		if _, err := os.Stat(indexFile); err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, indexFile)
	})
}

// isHTMLExt returns true for extensions that should get SPA fallback.
func isHTMLExt(ext string) bool {
	ext = strings.ToLower(ext)
	return ext == ".html" || ext == ".htm"
}

// corsMiddleware adds CORS headers for development (localhost:5173).
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "http://localhost:5173" || origin == "http://127.0.0.1:5173" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
