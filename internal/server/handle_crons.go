package server

import (
	"encoding/json"
	"net/http"
	"sort"
)

// CronInfo represents a cron job in API responses.
type CronInfo struct {
	Name     string `json:"name"`
	ID       string `json:"id"`
	Schedule string `json:"schedule"`
	Repo     string `json:"repo,omitempty"`
}

func (s *Server) handleListCrons(w http.ResponseWriter, r *http.Request) error {
	cfg := s.getConfig()

	crons := make([]CronInfo, 0, len(cfg.Crons))
	for name, cronCfg := range cfg.Crons {
		crons = append(crons, CronInfo{
			Name:     name,
			ID:       cronCfg.ID,
			Schedule: cronCfg.Schedule,
			Repo:     cronCfg.Repo,
		})
	}

	sort.Slice(crons, func(i, j int) bool {
		return crons[i].Name < crons[j].Name
	})

	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(crons)
}
