package scheduler

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/example/splai/internal/planner"
	"github.com/example/splai/internal/state"
	"github.com/example/splai/pkg/splaiapi"
)

func BenchmarkAssignmentPath(b *testing.B) {
	store := state.NewMemoryStore()
	queue := state.NewMemoryQueue()
	e := NewEngine(store, queue, Options{QueueBackend: "memory"})
	workers := envInt("SPLAI_BENCH_WORKERS", 50)
	for i := 0; i < workers; i++ {
		_ = e.RegisterWorker(splaiapi.RegisterWorkerRequest{
			WorkerID: fmt.Sprintf("w-%d", i),
			CPU:      16,
			Memory:   "32Gi",
			Models:   []string{"llama3-8b-q4"},
			Tools:    []string{"bash"},
			Locality: "cluster-a",
		})
	}
	p := planner.NewCompiler()
	for i := 0; i < b.N; i++ {
		jobID := fmt.Sprintf("job-%d", i)
		dag := p.CompileWithMode(jobID, "chat", "hello", "template")
		if err := e.AddJob(jobID, "tenant-a", "chat", "hello", "enterprise-default", "interactive", "internal", "llama3-8b-q4", "", dag); err != nil {
			b.Fatalf("add job: %v", err)
		}
		target := "w-0"
		if workers > 0 {
			target = fmt.Sprintf("w-%d", i%workers)
		}
		if _, err := e.PollAssignments(target, 1); err != nil {
			b.Fatalf("poll: %v", err)
		}
	}
}

func envInt(key string, fallback int) int {
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
