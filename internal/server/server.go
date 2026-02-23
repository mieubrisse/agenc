package server

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

// Server is the AgenC HTTP server that manages missions and background loops.
type Server struct {
	agencDirpath string
	socketPath   string
	logger       *log.Logger
	httpServer   *http.Server
	listener     net.Listener
	db           *database.DB
}

// NewServer creates a new Server instance.
func NewServer(agencDirpath string, socketPath string, logger *log.Logger) *Server {
	return &Server{
		agencDirpath: agencDirpath,
		socketPath:   socketPath,
		logger:       logger,
	}
}

// Run starts the HTTP server on the unix socket and blocks until ctx is cancelled.
// It performs graceful shutdown when the context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	// Open the database
	dbFilepath := config.GetDatabaseFilepath(s.agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open database")
	}
	s.db = db
	defer s.db.Close()

	// Clean up stale socket file from a previous run
	os.Remove(s.socketPath)

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to listen on unix socket '%s'", s.socketPath)
	}
	s.listener = listener

	// Restrict socket permissions
	if err := os.Chmod(s.socketPath, 0600); err != nil {
		listener.Close()
		return stacktrace.Propagate(err, "failed to set socket permissions")
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Handler: mux,
	}

	s.logger.Printf("Server listening on %s", s.socketPath)

	var wg sync.WaitGroup

	// Start HTTP server in a goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.httpServer.Serve(listener); err != http.ErrServerClosed {
			s.logger.Printf("HTTP server error: %v", err)
		}
	}()

	// Wait for context cancellation, then gracefully shut down
	<-ctx.Done()
	s.logger.Println("Server shutting down...")

	if err := s.httpServer.Shutdown(context.Background()); err != nil {
		s.logger.Printf("HTTP server shutdown error: %v", err)
	}

	wg.Wait()
	os.Remove(s.socketPath)
	s.logger.Println("Server stopped")

	return nil
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /missions", s.handleListMissions)
	mux.HandleFunc("GET /missions/{id}", s.handleGetMission)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
