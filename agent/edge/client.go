package edge

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
	baseURL    string
	httpClient *http.Client
}

type Health struct {
	Status        string `json:"status"`
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

type TrafficRecord struct {
	User          string
	UploadBytes   uint64
	DownloadBytes uint64
}

type TrafficSnapshot struct {
	UploadBytes   uint64          `json:"upload_bytes"`
	DownloadBytes uint64          `json:"download_bytes"`
	Users         []TrafficRecord `json:"users"`
}

type trafficRecordJSON struct {
	User          string `json:"user"`
	UploadBytes   uint64 `json:"upload_bytes"`
	DownloadBytes uint64 `json:"download_bytes"`
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	var health Health
	if err := c.getJSON(ctx, "/health", &health); err != nil {
		return Health{}, err
	}
	return health, nil
}

func (c *Client) Reload(ctx context.Context) error {
	return c.postForm(ctx, "/reload", nil, nil)
}

func (c *Client) RecordTraffic(ctx context.Context, record TrafficRecord) error {
	values := url.Values{}
	values.Set("user", record.User)
	values.Set("upload", fmt.Sprintf("%d", record.UploadBytes))
	values.Set("download", fmt.Sprintf("%d", record.DownloadBytes))
	return c.postForm(ctx, "/traffic", values, nil)
}

func (c *Client) DrainTraffic(ctx context.Context) (TrafficSnapshot, error) {
	var raw struct {
		UploadBytes   uint64              `json:"upload_bytes"`
		DownloadBytes uint64              `json:"download_bytes"`
		Users         []trafficRecordJSON `json:"users"`
	}
	if err := c.postForm(ctx, "/traffic/drain", nil, &raw); err != nil {
		return TrafficSnapshot{}, err
	}

	snapshot := TrafficSnapshot{
		UploadBytes:   raw.UploadBytes,
		DownloadBytes: raw.DownloadBytes,
		Users:         make([]TrafficRecord, 0, len(raw.Users)),
	}
	for _, user := range raw.Users {
		snapshot.Users = append(snapshot.Users, TrafficRecord{
			User:          user.User,
			UploadBytes:   user.UploadBytes,
			DownloadBytes: user.DownloadBytes,
		})
	}
	return snapshot, nil
}

func (c *Client) getJSON(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(path), nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeResponse(resp, target)
}

func (c *Client) postForm(ctx context.Context, path string, form url.Values, target any) error {
	body := ""
	if form != nil {
		body = form.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(path), bytes.NewBufferString(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeResponse(resp, target)
}

func (c *Client) url(path string) string {
	return c.baseURL + path
}

func decodeResponse(resp *http.Response, target any) error {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("keli-edge status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if target == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode keli-edge response failed: %w", err)
	}
	return nil
}
