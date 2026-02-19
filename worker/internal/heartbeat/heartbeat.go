package heartbeat

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/example/splai/pkg/splaiapi"
)

type Client struct {
	baseURL      string
	workerID     string
	apiToken     string
	interval     time.Duration
	runningTasks atomic.Int64
	queueDepth   atomic.Int64
	httpClient   *http.Client
}

func New(baseURL, workerID, apiToken string, interval time.Duration) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		workerID:   workerID,
		apiToken:   strings.TrimSpace(apiToken),
		interval:   interval,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *Client) SetStats(running, queue int) {
	c.runningTasks.Store(int64(running))
	c.queueDepth.Store(int64(queue))
}

func (c *Client) Start(ctx context.Context) {
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.send(ctx); err != nil {
				log.Printf("heartbeat failed: %v", err)
			}
		}
	}
}

func (c *Client) send(ctx context.Context) error {
	payload := splaiapi.HeartbeatRequest{
		QueueDepth:    int(c.queueDepth.Load()),
		RunningTasks:  int(c.runningTasks.Load()),
		CPUUtil:       15.0,
		MemoryUtil:    20.0,
		Health:        "healthy",
		TimestampUnix: time.Now().Unix(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := c.baseURL + "/v1/workers/" + c.workerID + "/heartbeat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiToken != "" {
		req.Header.Set("X-SPLAI-Token", c.apiToken)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmtError(resp.Status)
	}
	return nil
}

func fmtError(status string) error {
	return &heartbeatError{status: status}
}

type heartbeatError struct {
	status string
}

func (e *heartbeatError) Error() string {
	return "heartbeat request failed: " + e.status
}
