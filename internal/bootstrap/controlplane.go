package bootstrap

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/example/splai/internal/policy"
	"github.com/example/splai/internal/scheduler"
	"github.com/example/splai/internal/state"
)

func NewEngineFromEnv() (*scheduler.Engine, error) {
	storeType := getenv("SPLAI_STORE", "memory")
	queueType := getenv("SPLAI_QUEUE", "memory")

	store, err := newStore(storeType)
	if err != nil {
		return nil, err
	}
	queue, err := newQueue(queueType)
	if err != nil {
		return nil, err
	}
	policyEngine, err := policy.LoadFromEnv()
	if err != nil {
		return nil, err
	}
	leaseSeconds := getenvInt("SPLAI_LEASE_SECONDS", 15)
	return scheduler.NewEngine(store, queue, scheduler.Options{
		LeaseDuration: time.Duration(leaseSeconds) * time.Second,
		QueueBackend:  queueType,
		PolicyEngine:  policyEngine,
		Preempt:       getenvBool("SPLAI_SCHEDULER_PREEMPT", true),
		ScoreWeights: scheduler.ScoreWeights{
			CapabilityMatch:   getenvFloat("SPLAI_SCHED_WEIGHT_CAPABILITY_MATCH", 1.0),
			ModelWarmCache:    getenvFloat("SPLAI_SCHED_WEIGHT_MODEL_WARM_CACHE", 1.5),
			LocalityScore:     getenvFloat("SPLAI_SCHED_WEIGHT_LOCALITY", 1.0),
			QueuePenalty:      getenvFloat("SPLAI_SCHED_WEIGHT_QUEUE_PENALTY", 0.15),
			LatencyPrediction: getenvFloat("SPLAI_SCHED_WEIGHT_LATENCY", 1.0),
			FairnessPenalty:   getenvFloat("SPLAI_SCHED_WEIGHT_FAIRNESS", 0.05),
			WaitAgeBonus:      getenvFloat("SPLAI_SCHED_WEIGHT_WAIT_AGE", 0.05),
		},
	}), nil
}

func newStore(kind string) (state.Store, error) {
	switch kind {
	case "memory":
		return state.NewMemoryStore(), nil
	case "postgres":
		dsn := os.Getenv("SPLAI_POSTGRES_DSN")
		if dsn == "" {
			return nil, fmt.Errorf("SPLAI_POSTGRES_DSN is required when SPLAI_STORE=postgres")
		}
		return state.NewPostgresStore(dsn)
	default:
		return nil, fmt.Errorf("unsupported SPLAI_STORE value %q", kind)
	}
}

func newQueue(kind string) (state.Queue, error) {
	switch kind {
	case "memory":
		return state.NewMemoryQueue(), nil
	case "redis":
		addr := getenv("SPLAI_REDIS_ADDR", "127.0.0.1:6379")
		db := getenvInt("SPLAI_REDIS_DB", 0)
		password := os.Getenv("SPLAI_REDIS_PASSWORD")
		key := getenv("SPLAI_REDIS_KEY", "splai:tasks")
		deadLetterMax := getenvInt("SPLAI_REDIS_DEADLETTER_MAX", 5)
		return state.NewRedisQueue(state.RedisQueueConfig{
			Addr:          addr,
			Password:      password,
			DB:            db,
			Key:           key,
			Timeout:       3 * time.Second,
			DeadLetterMax: deadLetterMax,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported SPLAI_QUEUE value %q", kind)
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

func getenvFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(v, 64)
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
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes":
		return true
	case "0", "false", "no":
		return false
	default:
		return fallback
	}
}
