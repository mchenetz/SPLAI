package scheduler

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/example/splai/internal/state"
	"github.com/example/splai/pkg/splaiapi"
)

func TestNFRUtilizationProof(t *testing.T) {
	if os.Getenv("SPLAI_RUN_NFR_PROOF") != "1" {
		t.Skip("set SPLAI_RUN_NFR_PROOF=1 to run scale utilization proof")
	}
	workers := envIntDefault("SPLAI_NFR_WORKERS", 1000)
	jobs := envIntDefault("SPLAI_NFR_JOBS", workers)
	target := envFloatDefault("SPLAI_NFR_TARGET_UTILIZATION", 0.70)

	e := NewEngine(state.NewMemoryStore(), state.NewMemoryQueue(), Options{QueueBackend: "memory"})
	for i := 0; i < workers; i++ {
		if err := e.RegisterWorker(splaiapi.RegisterWorkerRequest{
			WorkerID: fmt.Sprintf("w-%d", i),
			CPU:      8,
			Memory:   "16Gi",
			Models:   []string{"llama3-8b-q4"},
			Tools:    []string{"bash"},
			Locality: "cluster-a",
		}); err != nil {
			t.Fatalf("register worker: %v", err)
		}
	}
	seen := map[string]struct{}{}
	ctx := context.Background()
	job := state.JobRecord{ID: "nfr-job", Tenant: "tenant-a", Type: "chat", Priority: "interactive"}
	for i := 0; i < jobs; i++ {
		task := state.TaskRecord{
			JobID:  "nfr-job",
			TaskID: fmt.Sprintf("t-%d", i),
			Type:   "llm_inference",
			Inputs: map[string]string{
				"_priority": "interactive",
				"model":     "llama3-8b-q4",
			},
		}
		workerID, _, err := e.bestWorkerForTask(ctx, job, task)
		if err != nil {
			t.Fatalf("bestWorkerForTask: %v", err)
		}
		if workerID != "" {
			seen[workerID] = struct{}{}
		}
	}
	assignedWorkers := len(seen)
	util := float64(assignedWorkers) / float64(workers)
	t.Logf("workers=%d jobs=%d assigned_workers=%d utilization=%.4f target=%.4f", workers, jobs, assignedWorkers, util, target)
	if util < target {
		t.Fatalf("utilization %.4f is below target %.4f", util, target)
	}
}

func envIntDefault(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func envFloatDefault(key string, fallback float64) float64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return v
}
