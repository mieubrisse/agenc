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
	"github.com/odyssey/agenc/internal/sleep"
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

// GetRaw sends a GET request and returns the raw response body as bytes.
// Unlike Get, it does not JSON-decode the response.
func (c *Client) GetRaw(path string) ([]byte, error) {
	resp, err := c.httpClient.Get(c.baseURL + path)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to connect to server")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, c.decodeError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to read response body")
	}

	return body, nil
}

// Put sends a PUT request with a JSON body and decodes the response into result.
func (c *Client) Put(path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		pr, pw := io.Pipe()
		go func() {
			pw.CloseWithError(json.NewEncoder(pw).Encode(body))
		}()
		bodyReader = pr
	}

	req, err := http.NewRequest(http.MethodPut, c.baseURL+path, bodyReader)
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

// ListMissionsRequest holds parameters for the ListMissions client call.
type ListMissionsRequest struct {
	IncludeArchived bool
	Source          string
	SourceID        string
	Since           *time.Time
	Until           *time.Time
}

// ListMissions fetches missions from the server with optional filtering.
func (c *Client) ListMissions(req ListMissionsRequest) ([]*database.Mission, error) {
	path := "/missions"
	var params []string
	if req.IncludeArchived {
		params = append(params, "include_archived=true")
	}
	if req.Source != "" {
		params = append(params, "source="+req.Source)
	}
	if req.SourceID != "" {
		params = append(params, "source_id="+req.SourceID)
	}
	if req.Since != nil {
		params = append(params, "since="+req.Since.UTC().Format(time.RFC3339))
	}
	if req.Until != nil {
		params = append(params, "until="+req.Until.UTC().Format(time.RFC3339))
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

// Heartbeat updates a mission's last_heartbeat timestamp. If paneID is
// non-empty, the server also stores it as the mission's current tmux pane.
// If lastUserPromptAt is non-empty, the server also updates that timestamp.
func (c *Client) Heartbeat(id string, paneID string, lastUserPromptAt string) error {
	body := map[string]string{}
	if paneID != "" {
		body["pane_id"] = paneID
	}
	if lastUserPromptAt != "" {
		body["last_user_prompt_at"] = lastUserPromptAt
	}
	return c.Post("/missions/"+id+"/heartbeat", body, nil)
}

// RecordPrompt increments prompt_count for a mission.
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
func (c *Client) AttachMission(id string, tmuxSession string, noFocus bool) error {
	body := AttachRequest{TmuxSession: tmuxSession, NoFocus: noFocus}
	return c.Post("/missions/"+id+"/attach", body, nil)
}

// DetachMission unlinks the mission's pool window from the given tmux session.
// The wrapper keeps running in the pool.
func (c *Client) DetachMission(id string, tmuxSession string) error {
	body := DetachRequest{TmuxSession: tmuxSession}
	return c.Post("/missions/"+id+"/detach", body, nil)
}

// SendKeys sends keystrokes to a running mission's tmux pane.
func (c *Client) SendKeys(id string, keys []string) error {
	body := SendKeysRequest{Keys: keys}
	return c.Post("/missions/"+id+"/send-keys", body, nil)
}

// CreateMission creates a new mission via the server.
func (c *Client) CreateMission(req CreateMissionRequest) (*database.Mission, error) {
	var resp MissionResponse
	if err := c.Post("/missions", req, &resp); err != nil {
		return nil, err
	}
	return resp.ToMission(), nil
}

// ListSessions fetches all sessions across all missions.
func (c *Client) ListSessions() ([]*database.Session, error) {
	var responses []SessionResponse
	if err := c.Get("/sessions", &responses); err != nil {
		return nil, err
	}

	return toSessionSlice(responses), nil
}

// ListMissionSessions fetches all sessions for a mission.
func (c *Client) ListMissionSessions(missionID string) ([]*database.Session, error) {
	var responses []SessionResponse
	if err := c.Get("/sessions?mission_id="+missionID, &responses); err != nil {
		return nil, err
	}

	return toSessionSlice(responses), nil
}

// ResolveSessionID resolves a short ID or full UUID to the full session ID.
func (c *Client) ResolveSessionID(id string) (string, error) {
	var resp SessionResponse
	if err := c.Get("/sessions/"+id, &resp); err != nil {
		return "", err
	}
	return resp.ID, nil
}

// UpdateSession updates fields on a session via the server.
func (c *Client) UpdateSession(sessionID string, req UpdateSessionRequest) error {
	return c.Patch("/sessions/"+sessionID, req, nil)
}

// ============================================================================
// High-level repo API methods
// ============================================================================

// ListRepos fetches all repos from the server.
func (c *Client) ListRepos() ([]RepoResponse, error) {
	var repos []RepoResponse
	if err := c.Get("/repos", &repos); err != nil {
		return nil, err
	}
	return repos, nil
}

// AddRepo adds a repo via the server.
func (c *Client) AddRepo(req AddRepoRequest) (*AddRepoResponse, error) {
	var resp AddRepoResponse
	if err := c.Post("/repos", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RemoveRepo removes a repo via the server.
func (c *Client) RemoveRepo(repoName string) error {
	return c.Delete("/repos/" + repoName)
}

// MoveRepo renames a repo in the library via the server.
func (c *Client) MoveRepo(oldName, newName string) error {
	req := MoveRepoRequest{NewName: newName}
	return c.Post("/repos/"+oldName+"/mv", req, nil)
}

// ============================================================================
// High-level claude-modifications API methods
// ============================================================================

// ClaudeModsFileResponse is the response from GET /config/claude-md
// and GET /config/settings-json.
type ClaudeModsFileResponse = claudeModsFileResponse

// ClaudeModsFileUpdateRequest is the request body for PUT /config/claude-md
// and PUT /config/settings-json.
type ClaudeModsFileUpdateRequest = claudeModsFileUpdateRequest

// ClaudeModsFileUpdateResponse is the response from successful PUT operations.
type ClaudeModsFileUpdateResponse = claudeModsFileUpdateResponse

// GetClaudeMd reads the AgenC-specific CLAUDE.md content and its content hash.
func (c *Client) GetClaudeMd() (*ClaudeModsFileResponse, error) {
	var resp ClaudeModsFileResponse
	if err := c.Get("/config/claude-md", &resp); err != nil {
		return nil, stacktrace.Propagate(err, "failed to get claude-md")
	}
	return &resp, nil
}

// UpdateClaudeMd writes new content to the AgenC-specific CLAUDE.md.
// Returns the new content hash on success.
func (c *Client) UpdateClaudeMd(content string, expectedHash string) (*ClaudeModsFileUpdateResponse, error) {
	var resp ClaudeModsFileUpdateResponse
	req := ClaudeModsFileUpdateRequest{
		Content:      content,
		ExpectedHash: expectedHash,
	}
	if err := c.Put("/config/claude-md", req, &resp); err != nil {
		return nil, err // Preserve HTTP error for conflict detection
	}
	return &resp, nil
}

// GetSettingsJson reads the AgenC-specific settings.json content and its content hash.
func (c *Client) GetSettingsJson() (*ClaudeModsFileResponse, error) {
	var resp ClaudeModsFileResponse
	if err := c.Get("/config/settings-json", &resp); err != nil {
		return nil, stacktrace.Propagate(err, "failed to get settings-json")
	}
	return &resp, nil
}

// UpdateSettingsJson writes new content to the AgenC-specific settings.json.
// Returns the new content hash on success.
func (c *Client) UpdateSettingsJson(content string, expectedHash string) (*ClaudeModsFileUpdateResponse, error) {
	var resp ClaudeModsFileUpdateResponse
	req := ClaudeModsFileUpdateRequest{
		Content:      content,
		ExpectedHash: expectedHash,
	}
	if err := c.Put("/config/settings-json", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ============================================================================
// High-level server API methods
// ============================================================================

// GetServerLogs fetches server log content from the server.
// source is "server" or "requests"; if all is true, returns the entire file.
func (c *Client) GetServerLogs(source string, all bool) ([]byte, error) {
	path := "/server/logs?source=" + source
	if all {
		path += "&mode=all"
	}
	return c.GetRaw(path)
}

// ============================================================================
// High-level cron API methods
// ============================================================================

// ListCrons fetches the list of configured cron jobs from the server.
func (c *Client) ListCrons() ([]CronInfo, error) {
	var crons []CronInfo
	if err := c.Get("/crons", &crons); err != nil {
		return nil, err
	}
	return crons, nil
}

// CreateCron creates a new cron job via the server.
func (c *Client) CreateCron(req CreateCronRequest) (*CronInfo, error) {
	var result CronInfo
	if err := c.Post("/crons", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateCron updates an existing cron job via the server.
func (c *Client) UpdateCron(name string, req UpdateCronRequest) (*CronInfo, error) {
	var result CronInfo
	if err := c.Patch("/crons/"+name, req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteCron removes a cron job via the server.
func (c *Client) DeleteCron(name string) error {
	return c.Delete("/crons/" + name)
}

// GetCronLogs fetches log content for a cron job by ID.
// If all is true, returns the entire log file; otherwise returns the last 200 lines.
func (c *Client) GetCronLogs(cronID string, all bool) ([]byte, error) {
	path := "/crons/" + cronID + "/logs"
	if all {
		path += "?mode=all"
	}
	return c.GetRaw(path)
}

// ============================================================================
// High-level stash API methods
// ============================================================================

// ListStashes fetches available stash files from the server.
func (c *Client) ListStashes() ([]StashListEntry, error) {
	var entries []StashListEntry
	if err := c.Get("/stash", &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// PushStash snapshots and stops all running missions.
// Returns (response, conflictResponse, error).
// If non-idle missions exist and force is false, response is nil and
// conflictResponse contains the non-idle missions.
func (c *Client) PushStash(force bool) (*StashPushResponse, *StashPushConflictResponse, error) {
	body := StashPushRequest{Force: force}

	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(json.NewEncoder(pw).Encode(body))
	}()

	resp, err := c.httpClient.Post(c.baseURL+"/stash/push", "application/json", pr)
	if err != nil {
		return nil, nil, stacktrace.Propagate(err, "failed to connect to server")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		var conflict StashPushConflictResponse
		if err := json.NewDecoder(resp.Body).Decode(&conflict); err != nil {
			return nil, nil, stacktrace.Propagate(err, "failed to decode conflict response")
		}
		return nil, &conflict, nil
	}

	if resp.StatusCode >= 400 {
		return nil, nil, c.decodeError(resp)
	}

	var pushResp StashPushResponse
	if err := json.NewDecoder(resp.Body).Decode(&pushResp); err != nil {
		return nil, nil, stacktrace.Propagate(err, "failed to decode push response")
	}
	return &pushResp, nil, nil
}

// PopStash restores missions from a stash. If stashID is empty, pops the
// most recent stash.
func (c *Client) PopStash(stashID string) (*StashPopResponse, error) {
	body := StashPopRequest{StashID: stashID}
	var resp StashPopResponse
	if err := c.Post("/stash/pop", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// HealthResponse represents the response from the /health endpoint.
type HealthResponse struct {
	Status  string            `json:"status"`
	Version string            `json:"version"`
	Loops   map[string]string `json:"loops"`
}

// GetHealth calls the /health endpoint and returns the server health status.
func (c *Client) GetHealth() (*HealthResponse, error) {
	var result HealthResponse
	if err := c.Get("/health", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ============================================================================
// High-level sleep API methods
// ============================================================================

// ListSleepWindows returns the current sleep mode windows.
func (c *Client) ListSleepWindows() ([]sleep.WindowDef, error) {
	var result []sleep.WindowDef
	if err := c.Get("/config/sleep/windows", &result); err != nil {
		return nil, err
	}
	return result, nil
}

// AddSleepWindow adds a new sleep window and returns the updated list.
func (c *Client) AddSleepWindow(window sleep.WindowDef) ([]sleep.WindowDef, error) {
	var result []sleep.WindowDef
	if err := c.Post("/config/sleep/windows", window, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// RemoveSleepWindow removes a sleep window by index.
func (c *Client) RemoveSleepWindow(index int) error {
	return c.Delete(fmt.Sprintf("/config/sleep/windows/%d", index))
}

func (c *Client) decodeError(resp *http.Response) error {
	var errResp errorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		return stacktrace.NewError("server returned status %d", resp.StatusCode)
	}
	return fmt.Errorf("%s", errResp.Message)
}
