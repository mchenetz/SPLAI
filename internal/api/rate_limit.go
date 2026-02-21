package api

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type submitLimiter struct {
	mu           sync.Mutex
	perTenantMax int
	globalMax    int
	window       time.Duration
	tenants      map[string][]int64
	global       []int64
}

func newSubmitLimiterFromEnv() *submitLimiter {
	perTenant := getenvIntRL("SPLAI_SUBMIT_RATE_LIMIT_PER_MIN", 1000)
	global := getenvIntRL("SPLAI_SUBMIT_GLOBAL_RATE_LIMIT_PER_MIN", 5000)
	if perTenant < 0 {
		perTenant = 0
	}
	if global < 0 {
		global = 0
	}
	return &submitLimiter{
		perTenantMax: perTenant,
		globalMax:    global,
		window:       time.Minute,
		tenants:      map[string][]int64{},
		global:       make([]int64, 0, 1024),
	}
}

func (l *submitLimiter) allow(tenant string, now time.Time) bool {
	if l == nil || (l.perTenantMax == 0 && l.globalMax == 0) {
		return true
	}
	ts := now.UTC().Unix()
	cutoff := ts - int64(l.window.Seconds())
	if tenant == "" {
		tenant = "default"
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	l.global = trimCutoff(l.global, cutoff)
	if l.globalMax > 0 && len(l.global) >= l.globalMax {
		return false
	}

	history := trimCutoff(l.tenants[tenant], cutoff)
	if l.perTenantMax > 0 && len(history) >= l.perTenantMax {
		l.tenants[tenant] = history
		return false
	}

	history = append(history, ts)
	l.tenants[tenant] = history
	l.global = append(l.global, ts)
	return true
}

func trimCutoff(in []int64, cutoff int64) []int64 {
	if len(in) == 0 {
		return in
	}
	i := 0
	for i < len(in) && in[i] <= cutoff {
		i++
	}
	if i == 0 {
		return in
	}
	out := make([]int64, len(in)-i)
	copy(out, in[i:])
	return out
}

func getenvIntRL(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}
