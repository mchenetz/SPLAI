package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	WorkerID            string
	ControlPlaneBaseURL string
	APIToken            string
	MaxParallelTasks    int
	HeartbeatInterval   time.Duration
	PollInterval        time.Duration
	ArtifactRoot        string
	ModelCacheDir       string
	ArtifactBackend     string
	MinIOEndpoint       string
	MinIOAccessKey      string
	MinIOSecretKey      string
	MinIOBucket         string
	MinIOUseSSL         bool
}

func FromEnv() Config {
	workerID := getenv("SPLAI_WORKER_ID", "worker-local")
	baseURL := getenv("SPLAI_CONTROL_PLANE_URL", "http://localhost:8080")
	apiToken := getenv("SPLAI_API_TOKEN", "")
	maxTasks := getenvInt("SPLAI_MAX_PARALLEL_TASKS", 2)
	hbSec := getenvInt("SPLAI_HEARTBEAT_SECONDS", 5)
	pollMs := getenvInt("SPLAI_POLL_MILLIS", 1500)
	artifactRoot := getenv("SPLAI_ARTIFACT_ROOT", "/tmp/splai-artifacts")
	modelCacheDir := getenv("SPLAI_MODEL_CACHE_DIR", artifactRoot+"/models")
	artifactBackend := getenv("SPLAI_ARTIFACT_BACKEND", "local")
	minioEndpoint := getenv("SPLAI_MINIO_ENDPOINT", "")
	minioAccessKey := getenv("SPLAI_MINIO_ACCESS_KEY", "")
	minioSecretKey := getenv("SPLAI_MINIO_SECRET_KEY", "")
	minioBucket := getenv("SPLAI_MINIO_BUCKET", "splai-artifacts")
	minioUseSSL := getenvBool("SPLAI_MINIO_USE_SSL", false)

	return Config{
		WorkerID:            workerID,
		ControlPlaneBaseURL: baseURL,
		APIToken:            apiToken,
		MaxParallelTasks:    maxTasks,
		HeartbeatInterval:   time.Duration(hbSec) * time.Second,
		PollInterval:        time.Duration(pollMs) * time.Millisecond,
		ArtifactRoot:        artifactRoot,
		ModelCacheDir:       modelCacheDir,
		ArtifactBackend:     artifactBackend,
		MinIOEndpoint:       minioEndpoint,
		MinIOAccessKey:      minioAccessKey,
		MinIOSecretKey:      minioSecretKey,
		MinIOBucket:         minioBucket,
		MinIOUseSSL:         minioUseSSL,
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

func getenvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	switch v {
	case "1", "true", "TRUE", "yes", "YES":
		return true
	case "0", "false", "FALSE", "no", "NO":
		return false
	default:
		return fallback
	}
}
