package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/mieubrisse/stacktrace"
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

func (c *Client) decodeError(resp *http.Response) error {
	var errResp errorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		return stacktrace.NewError("server returned status %d", resp.StatusCode)
	}
	return fmt.Errorf("%s", errResp.Message)
}
