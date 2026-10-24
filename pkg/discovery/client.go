package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	base string
	http *http.Client
}

// HTTPError retains the directory status so renewal can distinguish a lost
// connection from a rejected request or a conflicting registration.
type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("discovery: HTTP %d: %s", e.StatusCode, e.Message)
}

func NewClient(base string) (*Client, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("discovery: registry must be an HTTP(S) base URL")
	}
	return &Client{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) request(ctx context.Context, method, path string, value any, lease string, result any) error {
	var body io.Reader
	if value != nil {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if lease != "" {
		req.Header.Set("Anra-Lease", lease)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("discovery: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return &HTTPError{StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(message))}
	}
	if result != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(result)
	}
	return nil
}

func (c *Client) Register(ctx context.Context, record Record, lease string) (time.Duration, error) {
	var response leaseResponse
	err := c.request(ctx, http.MethodPut, AgentsPath+"/"+url.PathEscape(record.Name), registration{Record: record, Lease: lease}, "", &response)
	if err == nil && response.TTL < time.Second {
		return 0, fmt.Errorf("discovery: invalid lease TTL")
	}
	return response.TTL, err
}

func (c *Client) Unregister(ctx context.Context, name, lease string) error {
	return c.request(ctx, http.MethodDelete, AgentsPath+"/"+url.PathEscape(name), nil, lease, nil)
}

func (c *Client) Lookup(ctx context.Context, name string) (Record, error) {
	var result Record
	err := c.request(ctx, http.MethodGet, AgentsPath+"/"+url.PathEscape(name), nil, "", &result)
	if err == nil && result.Name != name {
		return Record{}, fmt.Errorf("discovery: agent name mismatch")
	}
	return result, err
}

func (c *Client) List(ctx context.Context) ([]Record, error) {
	var result []Record
	err := c.request(ctx, http.MethodGet, AgentsPath, nil, "", &result)
	return result, err
}
