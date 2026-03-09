package server

import (
	"context"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"

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

	// cachedConfig holds the most recent parsed AgencConfig, updated via fsnotify.
	// Reads are lock-free via atomic.Pointer; only the config watcher goroutine writes.
	cachedConfig atomic.Pointer[config.AgencConfig]

	// Repo update worker
	repoUpdateCh chan repoUpdateRequest

	// Session summarizer: generates auto_summary from first user prompt via Haiku
	sessionSummaryCh   chan summaryRequest
	summarizedSessions *sync.Map

	// stashInProgress is set while a stash push or pop is running.
	// Mutating mission endpoints return 503 while this is true.
	stashInProgress atomic.Bool

	// loopHealth tracks the status of each background loop goroutine.
	// Values are "running", "stopped", or "crashed".
	loopHealth sync.Map
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

// getConfig returns the cached AgencConfig. Returns an empty config if the
// cache has not been populated yet (should not happen after startup).
func (s *Server) getConfig() *config.AgencConfig {
	cfg := s.cachedConfig.Load()
	if cfg == nil {
		return &config.AgencConfig{}
	}
	return cfg
}

// runLoop runs a named background loop function with panic recovery and health tracking.
// On normal return, the loop is marked "stopped". On panic, it is marked "crashed"
// and the panic is logged — the loop is NOT restarted.
func (s *Server) runLoop(name string, wg *sync.WaitGroup, ctx context.Context, fn func(ctx context.Context)) {
	wg.Add(1)
	s.loopHealth.Store(name, "running")
	defer func() {
		if r := recover(); r != nil {
			s.logger.Printf("PANIC in background loop %q: %v", name, r)
			s.loopHealth.Store(name, "crashed")
		} else {
			s.loopHealth.Store(name, "stopped")
		}
		wg.Done()
	}()
	fn(ctx)
}

// Run starts the HTTP server on the unix socket and blocks until ctx is cancelled.
// It performs graceful shutdown when the context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	// Acquire singleton lock — only one server process may run at a time.
	lockFilepath := config.GetServerLockFilepath(s.agencDirpath)
	lockFile, err := tryAcquireServerLock(lockFilepath)
	if err != nil {
		if err == ErrServerLocked {
			s.logger.Println("Another server is already running, exiting")
			return nil
		}
		return stacktrace.Propagate(err, "failed to acquire server lock")
	}
	defer lockFile.Close()

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

	// Reconcile tmux pane IDs with actual pool state
	s.reconcilePaneIDs()

	// Verify launchctl is available (required for cron scheduling)
	if err := launchd.VerifyLaunchctlAvailable(); err != nil {
		s.logger.Printf("Warning: %v - cron scheduling will not work", err)
	}

	// Load config and perform initial cron sync on startup
	s.loadConfigOnStartup()

	// Initialize session summarizer channel and deduplication map
	s.initSessionSummarizer()

	var wg sync.WaitGroup

	// Start HTTP server in a goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.httpServer.Serve(listener); err != http.ErrServerClosed {
			s.logger.Printf("HTTP server error: %v", err)
		}
	}()

	// Start background loops with panic recovery
	go s.runLoop("repo-update-worker", &wg, ctx, s.runRepoUpdateWorker)
	go s.runLoop("repo-update-loop", &wg, ctx, s.runRepoUpdateLoop)
	go s.runLoop("config-auto-commit", &wg, ctx, s.runConfigAutoCommitLoop)
	go s.runLoop("config-watcher", &wg, ctx, s.runConfigWatcherLoop)
	go s.runLoop("keybindings-writer", &wg, ctx, s.runKeybindingsWriterLoop)
	go s.runLoop("idle-timeout", &wg, ctx, s.runIdleTimeoutLoop)
	go s.runLoop("session-scanner", &wg, ctx, s.runSessionScannerLoop)
	go s.runLoop("session-summarizer", &wg, ctx, s.runSessionSummarizerWorker)

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

// loadConfigOnStartup reads the config, caches it, and performs initial cron sync.
func (s *Server) loadConfigOnStartup() {
	cfg, _, err := config.ReadAgencConfig(s.agencDirpath)
	if err != nil {
		s.logger.Printf("Failed to read config on startup: %v", err)
		return
	}

	s.cachedConfig.Store(cfg)

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
	mux.Handle("GET /server/logs", appHandler(s.requestLogger, s.handleServerLogs))
	mux.Handle("GET /missions", appHandler(s.requestLogger, s.handleListMissions))
	mux.Handle("POST /missions", appHandler(s.requestLogger, s.stashGuard(s.handleCreateMission)))
	mux.Handle("GET /missions/{id}", appHandler(s.requestLogger, s.handleGetMission))
	mux.Handle("POST /missions/{id}/attach", appHandler(s.requestLogger, s.stashGuard(s.handleAttachMission)))
	mux.Handle("POST /missions/{id}/detach", appHandler(s.requestLogger, s.stashGuard(s.handleDetachMission)))
	mux.Handle("POST /missions/{id}/send-keys", appHandler(s.requestLogger, s.handleSendKeys))
	mux.Handle("POST /missions/{id}/stop", appHandler(s.requestLogger, s.stashGuard(s.handleStopMission)))
	mux.Handle("DELETE /missions/{id}", appHandler(s.requestLogger, s.stashGuard(s.handleDeleteMission)))
	mux.Handle("POST /missions/{id}/reload", appHandler(s.requestLogger, s.stashGuard(s.handleReloadMission)))
	mux.Handle("POST /missions/{id}/archive", appHandler(s.requestLogger, s.stashGuard(s.handleArchiveMission)))
	mux.Handle("POST /missions/{id}/unarchive", appHandler(s.requestLogger, s.stashGuard(s.handleUnarchiveMission)))
	mux.Handle("POST /missions/{id}/heartbeat", appHandler(s.requestLogger, s.handleHeartbeat))
	mux.Handle("POST /missions/{id}/prompt", appHandler(s.requestLogger, s.handleRecordPrompt))
	mux.Handle("PATCH /missions/{id}", appHandler(s.requestLogger, s.stashGuard(s.handleUpdateMission)))
	mux.Handle("GET /sessions", appHandler(s.requestLogger, s.handleListSessions))
	mux.Handle("GET /sessions/{id}", appHandler(s.requestLogger, s.handleGetSession))
	mux.Handle("PATCH /sessions/{id}", appHandler(s.requestLogger, s.handleUpdateSession))
	mux.Handle("GET /repos", appHandler(s.requestLogger, s.handleListRepos))
	mux.Handle("POST /repos", appHandler(s.requestLogger, s.handleAddRepo))
	mux.Handle("DELETE /repos/", appHandler(s.requestLogger, s.handleRemoveRepo))
	// Push-event uses a catch-all prefix since repo names contain slashes
	mux.Handle("POST /repos/", appHandler(s.requestLogger, s.handlePushEvent))

	// Stash endpoints — push and pop are wrapped in stashGuard so they cannot
	// race with each other (e.g., pop arriving while push's background goroutine
	// is still stopping wrappers).
	mux.Handle("GET /stash", appHandler(s.requestLogger, s.handleListStashes))
	mux.Handle("POST /stash/push", appHandler(s.requestLogger, s.stashGuard(s.handlePushStash)))
	mux.Handle("POST /stash/pop", appHandler(s.requestLogger, s.stashGuard(s.handlePopStash)))

	// Claude-modifications config file endpoints
	mux.Handle("GET /config/claude-md", appHandler(s.requestLogger, s.handleGetClaudeMd))
	mux.Handle("PUT /config/claude-md", appHandler(s.requestLogger, s.handleUpdateClaudeMd))
	mux.Handle("GET /config/settings-json", appHandler(s.requestLogger, s.handleGetSettingsJson))
	mux.Handle("PUT /config/settings-json", appHandler(s.requestLogger, s.handleUpdateSettingsJson))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) error {
	loops := make(map[string]string)
	s.loopHealth.Range(func(key, value any) bool {
		loops[key.(string)] = value.(string)
		return true
	})

	status := "ok"
	for _, loopStatus := range loops {
		if loopStatus == "crashed" {
			status = "degraded"
			break
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  status,
		"version": version.Version,
		"loops":   loops,
	})
	return nil
}
