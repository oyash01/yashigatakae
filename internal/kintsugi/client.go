package kintsugi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to a kintsugi relay. baseURL ends in /kintsugi (no trailing slash).
type Client struct {
	BaseURL    string
	APIKey     string
	HTTP       *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/kintsugi") {
		baseURL += "/kintsugi"
	}
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) auth(req *http.Request) {
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
}

// PostCheckpoint uploads a (typically encrypted) blob.
func (c *Client) PostCheckpoint(ctx context.Context, sid, machine string, body []byte) error {
	url := fmt.Sprintf("%s/sessions/%s/checkpoints", c.BaseURL, sid)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Yashi-Machine", machine)
	c.auth(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("post checkpoint: HTTP %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// GetLatest downloads the most recent checkpoint blob across all machines.
func (c *Client) GetLatest(ctx context.Context, sid string) ([]byte, error) {
	url := fmt.Sprintf("%s/sessions/%s/checkpoints/latest", c.BaseURL, sid)
	return c.getBlob(ctx, url)
}

// GetCheckpoint downloads a specific checkpoint by timestamp prefix.
func (c *Client) GetCheckpoint(ctx context.Context, sid, ts string) ([]byte, error) {
	url := fmt.Sprintf("%s/sessions/%s/checkpoints/%s", c.BaseURL, sid, ts)
	return c.getBlob(ctx, url)
}

func (c *Client) getBlob(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	c.auth(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("not found")
	}
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get: HTTP %d: %s", resp.StatusCode, string(b))
	}
	return io.ReadAll(resp.Body)
}

// CheckpointInfo mirrors the relay's checkpoint listing.
type CheckpointInfo struct {
	TS      string `json:"ts"`
	Machine string `json:"machine"`
	Size    int64  `json:"size"`
	File    string `json:"file"`
}

// ListSessions returns the session IDs known to the relay.
func (c *Client) ListSessions(ctx context.Context) ([]string, error) {
	url := c.BaseURL + "/sessions"
	body, err := c.getJSON(ctx, url)
	if err != nil {
		return nil, err
	}
	var out []string
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListCheckpoints returns checkpoint metadata for one session.
func (c *Client) ListCheckpoints(ctx context.Context, sid string) ([]CheckpointInfo, error) {
	url := fmt.Sprintf("%s/sessions/%s/checkpoints", c.BaseURL, sid)
	body, err := c.getJSON(ctx, url)
	if err != nil {
		return nil, err
	}
	var out []CheckpointInfo
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) getJSON(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	c.auth(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get json: HTTP %d: %s", resp.StatusCode, string(b))
	}
	return io.ReadAll(resp.Body)
}
