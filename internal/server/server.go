package server

import (
	"context"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/launchd"
	"github.com/odyssey/agenc/internal/version"
)

// Server is the AgenC HTTP server that manages missions and background loops.
type Server struct {
	agencDirpath  string
	socketPath    string
	logger        *log.Logger
	requestLogger *slog.Logger
	httpServer    *http.Server
	listener      net.Listener
	db            *database.DB

	// Background loop state (formerly in the Daemon struct)
	repoUpdateCycleCount int
	cronSyncer           *CronSyncer

	// Repo update worker
	repoUpdateCh chan repoUpdateRequest
}

// NewServer creates a new Server instance.
func NewServer(agencDirpath string, socketPath string, logger *log.Logger) *Server {
	return &Server{
		agencDirpath: agencDirpath,
		socketPath:   socketPath,
		logger:       logger,
		cronSyncer:   NewCronSyncer(agencDirpath),
		repoUpdateCh: make(chan repoUpdateRequest, repoUpdateChannelSize),
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

	// Open structured request log
	requestsLogFilepath := config.GetServerRequestsLogFilepath(s.agencDirpath)
	requestsLogFile, err := os.OpenFile(requestsLogFilepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open requests log file")
	}
	defer requestsLogFile.Close()
	s.requestLogger = slog.New(slog.NewJSONHandler(requestsLogFile, nil))

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

	// Ensure the tmux pool session exists for mission windows
	if err := s.ensurePoolSession(); err != nil {
		s.logger.Printf("Warning: failed to create tmux pool session: %v", err)
	}

	// Verify launchctl is available (required for cron scheduling)
	if err := launchd.VerifyLaunchctlAvailable(); err != nil {
		s.logger.Printf("Warning: %v - cron scheduling will not work", err)
	}

	// Initial cron sync on startup
	s.syncCronsOnStartup()

	var wg sync.WaitGroup

	// Start HTTP server in a goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.httpServer.Serve(listener); err != http.ErrServerClosed {
			s.logger.Printf("HTTP server error: %v", err)
		}
	}()

	// Start background loops
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.runRepoUpdateWorker(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.runRepoUpdateLoop(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.runConfigAutoCommitLoop(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.runConfigWatcherLoop(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.runKeybindingsWriterLoop(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.runMissionSummarizerLoop(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.runIdleTimeoutLoop(ctx)
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

// syncCronsOnStartup performs an initial sync of cron jobs to launchd on server startup.
func (s *Server) syncCronsOnStartup() {
	cfg, _, err := config.ReadAgencConfig(s.agencDirpath)
	if err != nil {
		s.logger.Printf("Failed to read config on startup: %v", err)
		return
	}

	if len(cfg.Crons) == 0 {
		s.logger.Println("Cron syncer: no cron jobs configured")
		return
	}

	if err := s.cronSyncer.SyncCronsToLaunchd(cfg.Crons, s.logger); err != nil {
		s.logger.Printf("Failed to sync crons on startup: %v", err)
	}
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.Handle("GET /health", appHandler(s.requestLogger, s.handleHealth))
	mux.Handle("GET /missions", appHandler(s.requestLogger, s.handleListMissions))
	mux.Handle("POST /missions", appHandler(s.requestLogger, s.handleCreateMission))
	mux.Handle("GET /missions/{id}", appHandler(s.requestLogger, s.handleGetMission))
	mux.Handle("POST /missions/{id}/attach", appHandler(s.requestLogger, s.handleAttachMission))
	mux.Handle("POST /missions/{id}/detach", appHandler(s.requestLogger, s.handleDetachMission))
	mux.Handle("POST /missions/{id}/stop", appHandler(s.requestLogger, s.handleStopMission))
	mux.Handle("DELETE /missions/{id}", appHandler(s.requestLogger, s.handleDeleteMission))
	mux.Handle("POST /missions/{id}/reload", appHandler(s.requestLogger, s.handleReloadMission))
	mux.Handle("POST /missions/{id}/archive", appHandler(s.requestLogger, s.handleArchiveMission))
	mux.Handle("POST /missions/{id}/unarchive", appHandler(s.requestLogger, s.handleUnarchiveMission))
	mux.Handle("POST /missions/{id}/heartbeat", appHandler(s.requestLogger, s.handleHeartbeat))
	mux.Handle("POST /missions/{id}/prompt", appHandler(s.requestLogger, s.handleRecordPrompt))
	mux.Handle("PATCH /missions/{id}", appHandler(s.requestLogger, s.handleUpdateMission))
	// Push-event uses a catch-all prefix since repo names contain slashes
	mux.Handle("POST /repos/", appHandler(s.requestLogger, s.handlePushEvent))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": version.Version,
	})
	return nil
}
