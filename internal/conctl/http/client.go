package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Client wraps net/http.Client with base URL, timeout, and verbose logging.
type Client struct {
	base       string
	hc         *http.Client
	verbose    bool
	adminToken string
}

// NewClient creates a configured HTTP client.
func NewClient(baseURL, timeout string, verbose bool) (*Client, error) {
	d, err := time.ParseDuration(timeout)
	if err != nil {
		return nil, fmt.Errorf("invalid timeout %q: %w", timeout, err)
	}
	return &Client{
		base:       strings.TrimRight(baseURL, "/"),
		hc:         &http.Client{Timeout: d},
		verbose:    verbose,
		adminToken: strings.TrimSpace(os.Getenv("CONCTL_ADMIN_API_TOKEN")),
	}, nil
}

// GetJSON issues a GET request and decodes the JSON response into dst.
func (c *Client) GetJSON(ctx context.Context, path string, query url.Values, dst interface{}) error {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	return c.do(req, dst)
}

// PostJSON issues a POST with a JSON body and decodes the response.
func (c *Client) PostJSON(ctx context.Context, path string, body interface{}, dst interface{}) error {
	return c.doJSON(ctx, http.MethodPost, path, body, dst)
}

// PutJSON issues a PUT with a JSON body and decodes the response.
func (c *Client) PutJSON(ctx context.Context, path string, body interface{}, dst interface{}) error {
	return c.doJSON(ctx, http.MethodPut, path, body, dst)
}

// PatchJSON issues a PATCH with a JSON body and decodes the response.
func (c *Client) PatchJSON(ctx context.Context, path string, body interface{}, dst interface{}) error {
	return c.doJSON(ctx, http.MethodPatch, path, body, dst)
}

// doJSON is the shared implementation for POST/PUT/PATCH with JSON bodies.
func (c *Client) doJSON(ctx context.Context, method, path string, body interface{}, dst interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = strings.NewReader(string(data))
	}
	u := c.base + path
	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	return c.do(req, dst)
}

// DeleteJSON issues a DELETE and decodes the response.
func (c *Client) DeleteJSON(ctx context.Context, path string, dst interface{}) error {
	u := c.base + path
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	return c.do(req, dst)
}

// PostForm issues a POST with form-encoded body and decodes the response.
func (c *Client) PostForm(ctx context.Context, path string, form url.Values, dst interface{}) error {
	u := c.base + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return c.do(req, dst)
}

// GetRaw issues a GET and returns the raw response body as bytes.
func (c *Client) GetRaw(ctx context.Context, path string, query url.Values) ([]byte, error) {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	return c.doRaw(req)
}

func (c *Client) do(req *http.Request, dst interface{}) error {
	c.applyAdminToken(req)
	start := time.Now()
	resp, err := c.hc.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return &APIError{StatusCode: 0, Message: err.Error()}
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close

	if c.verbose {
		fmt.Fprintf(os.Stderr, "[conctl] %s %s → %d (%s)\n",
			req.Method, req.URL.Path, resp.StatusCode, elapsed.Round(time.Millisecond))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &APIError{StatusCode: resp.StatusCode, Message: "read body: " + err.Error()}
	}

	if resp.StatusCode >= 400 {
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    strings.TrimSpace(string(body)),
		}
	}

	if dst != nil {
		if err := json.Unmarshal(body, dst); err != nil {
			return fmt.Errorf("decode response: %w (body: %s)", err, truncate(string(body), 200))
		}
	}
	return nil
}

func (c *Client) doRaw(req *http.Request) ([]byte, error) {
	c.applyAdminToken(req)
	start := time.Now()
	resp, err := c.hc.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return nil, &APIError{StatusCode: 0, Message: err.Error()}
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close

	if c.verbose {
		fmt.Fprintf(os.Stderr, "[conctl] %s %s → %d (%s)\n",
			req.Method, req.URL.Path, resp.StatusCode, elapsed.Round(time.Millisecond))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &APIError{StatusCode: resp.StatusCode, Message: "read body: " + err.Error()}
	}

	if resp.StatusCode >= 400 {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    strings.TrimSpace(string(body)),
		}
	}
	return body, nil
}

func (c *Client) applyAdminToken(req *http.Request) {
	if c == nil || req == nil || strings.TrimSpace(c.adminToken) == "" {
		return
	}
	if req.URL == nil || (req.URL.Path != "/api/admin" && !strings.HasPrefix(req.URL.Path, "/api/admin/")) {
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.adminToken)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
