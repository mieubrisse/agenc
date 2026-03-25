package server

import (
	"net/http"
	"os"

	"github.com/odyssey/agenc/internal/config"
)

func (s *Server) handleCronLogs(w http.ResponseWriter, r *http.Request) error {
	cronID := r.PathValue("id")

	logFilepath := config.GetCronLogFilepath(s.agencDirpath, cronID)
	if _, err := os.Stat(logFilepath); os.IsNotExist(err) {
		return newHTTPError(http.StatusNotFound, "no logs found — cron may not have run yet")
	}

	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "tail"
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	switch mode {
	case "all":
		file, err := os.Open(logFilepath)
		if err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to open log file: %v", err)
		}
		defer file.Close()
		buf := make([]byte, 32*1024)
		for {
			n, readErr := file.Read(buf)
			if n > 0 {
				w.Write(buf[:n])
			}
			if readErr != nil {
				break
			}
		}
	case "tail":
		lines, err := readTailLines(logFilepath, defaultTailLines)
		if err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to read log file: %v", err)
		}
		for i, line := range lines {
			if i > 0 {
				w.Write([]byte("\n"))
			}
			w.Write([]byte(line))
		}
		if len(lines) > 0 {
			w.Write([]byte("\n"))
		}
	default:
		return newHTTPErrorf(http.StatusBadRequest, "invalid mode %q: must be \"tail\" or \"all\"", mode)
	}

	return nil
}
