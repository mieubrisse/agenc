package server

import (
	"bufio"
	"net/http"
	"os"

	"github.com/odyssey/agenc/internal/config"
)

const defaultTailLines = 200

func (s *Server) handleServerLogs(w http.ResponseWriter, r *http.Request) error {
	source := r.URL.Query().Get("source")
	if source == "" {
		source = "server"
	}

	var logFilepath string
	switch source {
	case "server":
		logFilepath = config.GetServerLogFilepath(s.agencDirpath)
	case "requests":
		logFilepath = config.GetServerRequestsLogFilepath(s.agencDirpath)
	default:
		return newHTTPErrorf(http.StatusBadRequest, "invalid source %q: must be \"server\" or \"requests\"", source)
	}

	if _, err := os.Stat(logFilepath); os.IsNotExist(err) {
		return newHTTPError(http.StatusNotFound, "log file does not exist yet")
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

// readTailLines returns the last n lines of a file.
func readTailLines(filepath string, n int) ([]string, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}
