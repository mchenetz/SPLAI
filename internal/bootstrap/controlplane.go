package bootstrap

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/example/daef/internal/scheduler"
	"github.com/example/daef/internal/state"
)

func NewEngineFromEnv() (*scheduler.Engine, error) {
	storeType := getenv("DAEF_STORE", "memory")
	queueType := getenv("DAEF_QUEUE", "memory")

	store, err := newStore(storeType)
	if err != nil {
		return nil, err
	}
	queue, err := newQueue(queueType)
	if err != nil {
		return nil, err
	}
	leaseSeconds := getenvInt("DAEF_LEASE_SECONDS", 15)
	return scheduler.NewEngine(store, queue, scheduler.Options{
		LeaseDuration: time.Duration(leaseSeconds) * time.Second,
		QueueBackend:  queueType,
	}), nil
}

func newStore(kind string) (state.Store, error) {
	switch kind {
	case "memory":
		return state.NewMemoryStore(), nil
	case "postgres":
		dsn := os.Getenv("DAEF_POSTGRES_DSN")
		if dsn == "" {
			return nil, fmt.Errorf("DAEF_POSTGRES_DSN is required when DAEF_STORE=postgres")
		}
		return state.NewPostgresStore(dsn)
	default:
		return nil, fmt.Errorf("unsupported DAEF_STORE value %q", kind)
	}
}

func newQueue(kind string) (state.Queue, error) {
	switch kind {
	case "memory":
		return state.NewMemoryQueue(), nil
	case "redis":
		addr := getenv("DAEF_REDIS_ADDR", "127.0.0.1:6379")
		db := getenvInt("DAEF_REDIS_DB", 0)
		password := os.Getenv("DAEF_REDIS_PASSWORD")
		key := getenv("DAEF_REDIS_KEY", "daef:tasks")
		deadLetterMax := getenvInt("DAEF_REDIS_DEADLETTER_MAX", 5)
		return state.NewRedisQueue(state.RedisQueueConfig{
			Addr:          addr,
			Password:      password,
			DB:            db,
			Key:           key,
			Timeout:       3 * time.Second,
			DeadLetterMax: deadLetterMax,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported DAEF_QUEUE value %q", kind)
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
