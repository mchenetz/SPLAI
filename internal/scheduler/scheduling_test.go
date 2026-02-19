package scheduler

import (
	"testing"

	"github.com/example/splai/internal/planner"
	"github.com/example/splai/internal/state"
	"github.com/example/splai/pkg/splaiapi"
)

func TestBestWorkerSelectionUsesModelWarmCache(t *testing.T) {
	e := NewEngine(state.NewMemoryStore(), state.NewMemoryQueue(), Options{QueueBackend: "memory"})
	if err := e.RegisterWorker(splaiapi.RegisterWorkerRequest{
		WorkerID: "worker-a",
		CPU:      8,
		Memory:   "16Gi",
		Models:   []string{"m-a"},
	}); err != nil {
		t.Fatalf("register worker-a: %v", err)
	}
	if err := e.RegisterWorker(splaiapi.RegisterWorkerRequest{
		WorkerID: "worker-b",
		CPU:      8,
		Memory:   "16Gi",
		Models:   []string{"m-b"},
	}); err != nil {
		t.Fatalf("register worker-b: %v", err)
	}
	dag := planner.NewCompiler().CompileWithMode("job-1", "chat", "hello", "template")
	if err := e.AddJob("job-1", "tenant-a", "chat", "hello", "enterprise-default", "interactive", "internal", "m-b", "", dag); err != nil {
		t.Fatalf("add job: %v", err)
	}

	assn, err := e.PollAssignments("worker-a", 1)
	if err != nil {
		t.Fatalf("poll worker-a: %v", err)
	}
	if len(assn) != 0 {
		t.Fatalf("expected no assignment for non-best worker")
	}
	assn, err = e.PollAssignments("worker-b", 1)
	if err != nil {
		t.Fatalf("poll worker-b: %v", err)
	}
	if len(assn) != 1 {
		t.Fatalf("expected assignment for best worker")
	}
}

func TestPreemptionForHigherPriorityTask(t *testing.T) {
	e := NewEngine(state.NewMemoryStore(), state.NewMemoryQueue(), Options{QueueBackend: "memory"})
	if err := e.RegisterWorker(splaiapi.RegisterWorkerRequest{
		WorkerID: "worker-1",
		CPU:      4,
		Memory:   "8Gi",
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	batchDAG := planner.NewCompiler().CompileWithMode("job-batch", "chat", "batch", "template")
	if err := e.AddJob("job-batch", "tenant-a", "chat", "batch", "enterprise-default", "batch", "internal", "m-a", "", batchDAG); err != nil {
		t.Fatalf("add batch job: %v", err)
	}
	batchAssn, err := e.PollAssignments("worker-1", 1)
	if err != nil || len(batchAssn) != 1 {
		t.Fatalf("expected batch assignment, err=%v len=%d", err, len(batchAssn))
	}
	if err := e.Heartbeat("worker-1", splaiapi.HeartbeatRequest{RunningTasks: 1, QueueDepth: 0, CPUUtil: 5, MemoryUtil: 5, Health: "healthy"}); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	highDAG := planner.NewCompiler().CompileWithMode("job-high", "chat", "high", "template")
	if err := e.AddJob("job-high", "tenant-a", "chat", "high", "enterprise-default", "interactive", "internal", "m-a", "", highDAG); err != nil {
		t.Fatalf("add high job: %v", err)
	}
	highAssn, err := e.PollAssignments("worker-1", 1)
	if err != nil || len(highAssn) != 1 {
		t.Fatalf("expected high-priority assignment with preemption, err=%v len=%d", err, len(highAssn))
	}

	tasks, ok, err := e.GetJobTasks("job-batch")
	if err != nil || !ok {
		t.Fatalf("batch tasks read failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one batch task")
	}
	if tasks[0].Status != JobQueued {
		t.Fatalf("expected preempted batch task to be queued, got %s", tasks[0].Status)
	}
}
