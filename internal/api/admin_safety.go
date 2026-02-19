package api

import (
	"os"
	"strconv"
	"sync"
	"time"
)

type adminSafety struct {
	maxBatch          int
	rateLimitPerMin   int
	confirmThreshold  int
	confirmToken      string
	mu                sync.Mutex
	recentRequeueUnix []int64
}

func newAdminSafetyFromEnv() *adminSafety {
	return &adminSafety{
		maxBatch:         getenvInt("SPLAI_ADMIN_REQUEUE_MAX_BATCH", 100),
		rateLimitPerMin:  getenvInt("SPLAI_ADMIN_REQUEUE_RATE_LIMIT_PER_MIN", 30),
		confirmThreshold: getenvInt("SPLAI_ADMIN_REQUEUE_CONFIRM_THRESHOLD", 20),
		confirmToken:     os.Getenv("SPLAI_ADMIN_REQUEUE_CONFIRM_TOKEN"),
	}
}

func (a *adminSafety) allowRequeue(now time.Time) bool {
	if a.rateLimitPerMin <= 0 {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	cutoff := now.Add(-time.Minute).Unix()
	kept := a.recentRequeueUnix[:0]
	for _, ts := range a.recentRequeueUnix {
		if ts >= cutoff {
			kept = append(kept, ts)
		}
	}
	a.recentRequeueUnix = kept
	if len(a.recentRequeueUnix) >= a.rateLimitPerMin {
		return false
	}
	a.recentRequeueUnix = append(a.recentRequeueUnix, now.Unix())
	return true
}

func getenvInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}
