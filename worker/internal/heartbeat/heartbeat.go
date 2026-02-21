package heartbeat

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
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
	cpuUtil, memUtil := hostUtilization()
	payload := splaiapi.HeartbeatRequest{
		QueueDepth:    int(c.queueDepth.Load()),
		RunningTasks:  int(c.runningTasks.Load()),
		CPUUtil:       cpuUtil,
		MemoryUtil:    memUtil,
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

func hostUtilization() (float64, float64) {
	return cpuUtilizationPercent(), memoryUtilizationPercent()
}

func cpuUtilizationPercent() float64 {
	// Linux loadavg-based estimate normalized by CPU cores.
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		parts := strings.Fields(string(b))
		if len(parts) > 0 {
			if v, err := strconv.ParseFloat(parts[0], 64); err == nil {
				cpus := float64(runtime.NumCPU())
				if cpus <= 0 {
					cpus = 1
				}
				pct := (v / cpus) * 100.0
				if pct < 0 {
					pct = 0
				}
				if pct > 100 {
					pct = 100
				}
				return pct
			}
		}
	}
	// Portable process-based fallback.
	return 0
}

func memoryUtilizationPercent() float64 {
	// Linux host memory from /proc/meminfo.
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		var totalKB, availKB float64
		for _, line := range strings.Split(string(b), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			switch fields[0] {
			case "MemTotal:":
				totalKB, _ = strconv.ParseFloat(fields[1], 64)
			case "MemAvailable:":
				availKB, _ = strconv.ParseFloat(fields[1], 64)
			}
		}
		if totalKB > 0 && availKB >= 0 {
			used := ((totalKB - availKB) / totalKB) * 100.0
			if used < 0 {
				used = 0
			}
			if used > 100 {
				used = 100
			}
			return used
		}
	}
	return 0
}
