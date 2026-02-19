package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	WorkerID            string
	ControlPlaneBaseURL string
	MaxParallelTasks    int
	HeartbeatInterval   time.Duration
	PollInterval        time.Duration
}

func FromEnv() Config {
	workerID := getenv("DAEF_WORKER_ID", "worker-local")
	baseURL := getenv("DAEF_CONTROL_PLANE_URL", "http://localhost:8080")
	maxTasks := getenvInt("DAEF_MAX_PARALLEL_TASKS", 2)
	hbSec := getenvInt("DAEF_HEARTBEAT_SECONDS", 5)
	pollMs := getenvInt("DAEF_POLL_MILLIS", 1500)

	return Config{
		WorkerID:            workerID,
		ControlPlaneBaseURL: baseURL,
		MaxParallelTasks:    maxTasks,
		HeartbeatInterval:   time.Duration(hbSec) * time.Second,
		PollInterval:        time.Duration(pollMs) * time.Millisecond,
	}
}

func getenv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func getenvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
