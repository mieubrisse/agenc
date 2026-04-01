package server

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/google/uuid"

	"github.com/odyssey/agenc/internal/config"
)

// CronInfo represents a cron job in API responses.
type CronInfo struct {
	Name        string `json:"name"`
	ID          string `json:"id"`
	Schedule    string `json:"schedule"`
	Prompt      string `json:"prompt"`
	Description string `json:"description,omitempty"`
	Repo        string `json:"repo,omitempty"`
	Enabled     bool   `json:"enabled"`
}

// CreateCronRequest is the request body for POST /crons.
type CreateCronRequest struct {
	Name        string `json:"name"`
	Schedule    string `json:"schedule"`
	Prompt      string `json:"prompt"`
	Description string `json:"description,omitempty"`
	Repo        string `json:"repo,omitempty"`
}

// UpdateCronRequest is the request body for PATCH /crons/{name}.
// Only non-nil fields are applied.
type UpdateCronRequest struct {
	Schedule    *string `json:"schedule,omitempty"`
	Prompt      *string `json:"prompt,omitempty"`
	Description *string `json:"description,omitempty"`
	Repo        *string `json:"repo,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
}

func cronInfoFromConfig(name string, cronCfg config.CronConfig) CronInfo {
	return CronInfo{
		Name:        name,
		ID:          cronCfg.ID,
		Schedule:    cronCfg.Schedule,
		Prompt:      cronCfg.Prompt,
		Description: cronCfg.Description,
		Repo:        cronCfg.Repo,
		Enabled:     cronCfg.IsEnabled(),
	}
}

func (s *Server) handleListCrons(w http.ResponseWriter, r *http.Request) error {
	cfg := s.getConfig()

	crons := make([]CronInfo, 0, len(cfg.Crons))
	for name, cronCfg := range cfg.Crons {
		crons = append(crons, cronInfoFromConfig(name, cronCfg))
	}

	sort.Slice(crons, func(i, j int) bool {
		return crons[i].Name < crons[j].Name
	})

	writeJSON(w, http.StatusOK, crons)
	return nil
}

func (s *Server) handleCreateCron(w http.ResponseWriter, r *http.Request) error {
	var req CreateCronRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if err := config.ValidateCronName(req.Name); err != nil {
		return newHTTPErrorf(http.StatusBadRequest, "%s", err.Error())
	}
	if err := config.ValidateCronSchedule(req.Schedule); err != nil {
		return newHTTPErrorf(http.StatusBadRequest, "%s", err.Error())
	}
	if req.Prompt == "" {
		return newHTTPError(http.StatusBadRequest, "prompt cannot be empty")
	}

	release, err := config.AcquireConfigLock(s.agencDirpath)
	if err != nil {
		return err
	}
	defer release()

	cfg, cm, err := config.ReadAgencConfig(s.agencDirpath)
	if err != nil {
		return err
	}

	if _, exists := cfg.Crons[req.Name]; exists {
		return newHTTPErrorf(http.StatusConflict, "cron job '%s' already exists", req.Name)
	}

	cronCfg := config.CronConfig{
		ID:          uuid.New().String(),
		Schedule:    req.Schedule,
		Prompt:      req.Prompt,
		Description: req.Description,
		Repo:        req.Repo,
	}

	if cfg.Crons == nil {
		cfg.Crons = make(map[string]config.CronConfig)
	}
	cfg.Crons[req.Name] = cronCfg

	if err := config.WriteAgencConfig(s.agencDirpath, cfg, cm); err != nil {
		return err
	}

	s.cachedConfig.Store(cfg)
	s.syncCronsAfterMutation(cfg)

	writeJSON(w, http.StatusCreated, cronInfoFromConfig(req.Name, cronCfg))
	return nil
}

func (s *Server) handleUpdateCron(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")

	var req UpdateCronRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid request body")
	}

	release, err := config.AcquireConfigLock(s.agencDirpath)
	if err != nil {
		return err
	}
	defer release()

	cfg, cm, err := config.ReadAgencConfig(s.agencDirpath)
	if err != nil {
		return err
	}

	cronCfg, exists := cfg.Crons[name]
	if !exists {
		return newHTTPErrorf(http.StatusNotFound, "cron job '%s' not found", name)
	}

	if req.Schedule != nil {
		if err := config.ValidateCronSchedule(*req.Schedule); err != nil {
			return newHTTPErrorf(http.StatusBadRequest, "%s", err.Error())
		}
		cronCfg.Schedule = *req.Schedule
	}
	if req.Prompt != nil {
		if *req.Prompt == "" {
			return newHTTPError(http.StatusBadRequest, "prompt cannot be empty")
		}
		cronCfg.Prompt = *req.Prompt
	}
	if req.Description != nil {
		cronCfg.Description = *req.Description
	}
	if req.Repo != nil {
		cronCfg.Repo = *req.Repo
	}
	if req.Enabled != nil {
		cronCfg.Enabled = req.Enabled
	}

	cfg.Crons[name] = cronCfg

	if err := config.WriteAgencConfig(s.agencDirpath, cfg, cm); err != nil {
		return err
	}

	s.cachedConfig.Store(cfg)
	s.syncCronsAfterMutation(cfg)

	writeJSON(w, http.StatusOK, cronInfoFromConfig(name, cronCfg))
	return nil
}

func (s *Server) handleDeleteCron(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")

	release, err := config.AcquireConfigLock(s.agencDirpath)
	if err != nil {
		return err
	}
	defer release()

	cfg, cm, err := config.ReadAgencConfig(s.agencDirpath)
	if err != nil {
		return err
	}

	if _, exists := cfg.Crons[name]; !exists {
		return newHTTPErrorf(http.StatusNotFound, "cron job '%s' not found", name)
	}

	delete(cfg.Crons, name)

	if err := config.WriteAgencConfig(s.agencDirpath, cfg, cm); err != nil {
		return err
	}

	s.cachedConfig.Store(cfg)
	s.syncCronsAfterMutation(cfg)

	w.WriteHeader(http.StatusNoContent)
	return nil
}

// syncCronsAfterMutation triggers a launchd cron sync after a cron mutation.
func (s *Server) syncCronsAfterMutation(cfg *config.AgencConfig) {
	if err := s.cronSyncer.SyncCronsToLaunchd(cfg.Crons, s.logger); err != nil {
		s.logger.Printf("Failed to sync crons after mutation: %v", err)
	}
}
