package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/sleep"
)

func (s *Server) handleListSleepWindows(w http.ResponseWriter, r *http.Request) error {
	cfg := s.getConfig()
	windows := extractWindows(cfg)
	writeJSON(w, http.StatusOK, windows)
	return nil
}

func (s *Server) handleAddSleepWindow(w http.ResponseWriter, r *http.Request) error {
	var window sleep.WindowDef
	if err := json.NewDecoder(r.Body).Decode(&window); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if err := sleep.ValidateWindow(window); err != nil {
		return newHTTPErrorf(http.StatusBadRequest, "%s", err.Error())
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

	if cfg.SleepMode == nil {
		cfg.SleepMode = &config.SleepModeConfig{}
	}
	cfg.SleepMode.Windows = append(cfg.SleepMode.Windows, window)

	if err := config.WriteAgencConfig(s.agencDirpath, cfg, cm); err != nil {
		return err
	}

	s.cachedConfig.Store(cfg)

	writeJSON(w, http.StatusCreated, cfg.SleepMode.Windows)
	return nil
}

func (s *Server) handleRemoveSleepWindow(w http.ResponseWriter, r *http.Request) error {
	indexStr := r.PathValue("index")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		return newHTTPErrorf(http.StatusBadRequest, "invalid index %q: must be a number", indexStr)
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

	var windows []sleep.WindowDef
	if cfg.SleepMode != nil {
		windows = cfg.SleepMode.Windows
	}

	if index < 0 || index >= len(windows) {
		return newHTTPErrorf(http.StatusNotFound, "window index %d out of bounds (have %d windows)", index, len(windows))
	}

	windows = append(windows[:index], windows[index+1:]...)

	if len(windows) == 0 {
		cfg.SleepMode = nil
	} else {
		cfg.SleepMode.Windows = windows
	}

	if err := config.WriteAgencConfig(s.agencDirpath, cfg, cm); err != nil {
		return err
	}

	s.cachedConfig.Store(cfg)

	writeJSON(w, http.StatusOK, extractWindows(cfg))
	return nil
}

// extractWindows returns the sleep windows from cfg, or an empty slice if none.
func extractWindows(cfg *config.AgencConfig) []sleep.WindowDef {
	if cfg.SleepMode == nil || len(cfg.SleepMode.Windows) == 0 {
		return []sleep.WindowDef{}
	}
	return cfg.SleepMode.Windows
}
