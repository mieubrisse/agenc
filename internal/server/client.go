package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/database"
)

// Client is an HTTP client that connects to the AgenC server via unix socket.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new client that connects to the server at the given socket path.
func NewClient(socketPath string) *Client {
	return &Client{
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return net.DialTimeout("unix", socketPath, 5*time.Second)
				},
			},
			Timeout: 30 * time.Second,
		},
		// The host doesn't matter for unix sockets, but HTTP requires one
		baseURL: "http://agenc",
	}
}

// Get sends a GET request and decodes the response into result.
func (c *Client) Get(path string, result any) error {
	resp, err := c.httpClient.Get(c.baseURL + path)
	if err != nil {
		return stacktrace.Propagate(err, "failed to connect to server")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.decodeError(resp)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return stacktrace.Propagate(err, "failed to decode server response")
		}
	}

	return nil
}

// Post sends a POST request with a JSON body and decodes the response into result.
func (c *Client) Post(path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		pr, pw := io.Pipe()
		go func() {
			pw.CloseWithError(json.NewEncoder(pw).Encode(body))
		}()
		bodyReader = pr
	}

	resp, err := c.httpClient.Post(c.baseURL+path, "application/json", bodyReader)
	if err != nil {
		return stacktrace.Propagate(err, "failed to connect to server")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.decodeError(resp)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return stacktrace.Propagate(err, "failed to decode server response")
		}
	}

	return nil
}

// Delete sends a DELETE request.
func (c *Client) Delete(path string) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return stacktrace.Propagate(err, "failed to create request")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return stacktrace.Propagate(err, "failed to connect to server")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.decodeError(resp)
	}

	return nil
}

// Patch sends a PATCH request with a JSON body and decodes the response into result.
func (c *Client) Patch(path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		pr, pw := io.Pipe()
		go func() {
			pw.CloseWithError(json.NewEncoder(pw).Encode(body))
		}()
		bodyReader = pr
	}

	req, err := http.NewRequest(http.MethodPatch, c.baseURL+path, bodyReader)
	if err != nil {
		return stacktrace.Propagate(err, "failed to create request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return stacktrace.Propagate(err, "failed to connect to server")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.decodeError(resp)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return stacktrace.Propagate(err, "failed to decode server response")
		}
	}

	return nil
}

// ============================================================================
// High-level mission API methods
// ============================================================================

// ListMissions fetches all missions from the server.
func (c *Client) ListMissions(includeArchived bool, cronID string) ([]*database.Mission, error) {
	path := "/missions"
	var params []string
	if includeArchived {
		params = append(params, "include_archived=true")
	}
	if cronID != "" {
		params = append(params, "cron_id="+cronID)
	}
	if len(params) > 0 {
		path += "?" + strings.Join(params, "&")
	}

	var responses []MissionResponse
	if err := c.Get(path, &responses); err != nil {
		return nil, err
	}

	missions := make([]*database.Mission, len(responses))
	for i := range responses {
		missions[i] = responses[i].ToMission()
	}
	return missions, nil
}

// GetMission fetches a single mission by ID (supports short ID resolution).
func (c *Client) GetMission(id string) (*database.Mission, error) {
	var resp MissionResponse
	if err := c.Get("/missions/"+id, &resp); err != nil {
		return nil, err
	}
	return resp.ToMission(), nil
}

// ResolveMissionID resolves a short ID or full UUID to the full mission ID.
func (c *Client) ResolveMissionID(id string) (string, error) {
	mission, err := c.GetMission(id)
	if err != nil {
		return "", err
	}
	return mission.ID, nil
}

// StopMission stops a mission's wrapper process via the server.
func (c *Client) StopMission(id string) error {
	return c.Post("/missions/"+id+"/stop", nil, nil)
}

// DeleteMission permanently removes a mission via the server.
func (c *Client) DeleteMission(id string) error {
	return c.Delete("/missions/" + id)
}

// ArchiveMission stops and archives a mission via the server.
func (c *Client) ArchiveMission(id string) error {
	return c.Post("/missions/"+id+"/archive", nil, nil)
}

// UnarchiveMission sets a mission back to active via the server.
func (c *Client) UnarchiveMission(id string) error {
	return c.Post("/missions/"+id+"/unarchive", nil, nil)
}

// UpdateMission updates specific fields on a mission via the server.
func (c *Client) UpdateMission(id string, update UpdateMissionRequest) error {
	return c.Patch("/missions/"+id, update, nil)
}

// Heartbeat updates a mission's last_heartbeat timestamp.
func (c *Client) Heartbeat(id string) error {
	return c.Post("/missions/"+id+"/heartbeat", nil, nil)
}

// RecordPrompt updates last_active and increments prompt_count for a mission.
func (c *Client) RecordPrompt(id string) error {
	return c.Post("/missions/"+id+"/prompt", nil, nil)
}

// ReloadMission reloads a mission's wrapper via the server.
func (c *Client) ReloadMission(id string, tmuxSession string) error {
	body := map[string]string{}
	if tmuxSession != "" {
		body["tmux_session"] = tmuxSession
	}
	return c.Post("/missions/"+id+"/reload", body, nil)
}

// AttachMission ensures the mission's wrapper is running in the pool and links
// the pool window into the given tmux session.
func (c *Client) AttachMission(id string, tmuxSession string) error {
	body := AttachRequest{TmuxSession: tmuxSession}
	return c.Post("/missions/"+id+"/attach", body, nil)
}

// CreateMission creates a new mission via the server.
func (c *Client) CreateMission(req CreateMissionRequest) (*database.Mission, error) {
	var resp MissionResponse
	if err := c.Post("/missions", req, &resp); err != nil {
		return nil, err
	}
	return resp.ToMission(), nil
}

func (c *Client) decodeError(resp *http.Response) error {
	var errResp errorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		return stacktrace.NewError("server returned status %d", resp.StatusCode)
	}
	return fmt.Errorf("%s", errResp.Message)
}
