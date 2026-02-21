package scheduler

import (
	"testing"
	"time"

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

func TestPriorityAssignmentsPreferInteractiveFirst(t *testing.T) {
	e := NewEngine(state.NewMemoryStore(), state.NewMemoryQueue(), Options{QueueBackend: "memory"})
	if err := e.RegisterWorker(splaiapi.RegisterWorkerRequest{
		WorkerID: "worker-priority",
		CPU:      8,
		Memory:   "16Gi",
		Models:   []string{"m-a"},
		Tools:    []string{"bash"},
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	dag := planner.DAG{
		DAGID: "dag-priority",
		Tasks: []planner.Task{
			{TaskID: "t1", Type: "llm_inference", Inputs: map[string]string{"prompt": "p"}, TimeoutSec: 30, MaxRetries: 1},
		},
	}
	if err := e.AddJob("job-batch-priority", "tenant-a", "chat", "batch", "enterprise-default", "batch", "internal", "m-a", "", dag); err != nil {
		t.Fatalf("add batch job: %v", err)
	}
	if err := e.AddJob("job-int-priority", "tenant-a", "chat", "interactive", "enterprise-default", "interactive", "internal", "m-a", "", dag); err != nil {
		t.Fatalf("add interactive job: %v", err)
	}

	assn, err := e.PollAssignments("worker-priority", 1)
	if err != nil {
		t.Fatalf("poll assignments: %v", err)
	}
	if len(assn) != 1 {
		t.Fatalf("expected one assignment, got %d", len(assn))
	}
	if assn[0].JobID != "job-int-priority" {
		t.Fatalf("expected interactive job first, got %s", assn[0].JobID)
	}
}

func TestTaskTimeoutRetriesWithBackoffAndThenFails(t *testing.T) {
	e := NewEngine(state.NewMemoryStore(), state.NewMemoryQueue(), Options{
		QueueBackend:  "memory",
		LeaseDuration: 10 * time.Second,
	})
	if err := e.RegisterWorker(splaiapi.RegisterWorkerRequest{
		WorkerID: "worker-timeout",
		CPU:      8,
		Memory:   "16Gi",
		Models:   []string{"m-a"},
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	dag := planner.DAG{
		DAGID: "dag-timeout",
		Tasks: []planner.Task{
			{TaskID: "t1", Type: "llm_inference", Inputs: map[string]string{"prompt": "slow"}, TimeoutSec: 1, MaxRetries: 1},
		},
	}
	if err := e.AddJob("job-timeout", "tenant-a", "chat", "slow", "enterprise-default", "interactive", "internal", "m-a", "", dag); err != nil {
		t.Fatalf("add timeout job: %v", err)
	}

	first, err := e.PollAssignments("worker-timeout", 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("expected first assignment, err=%v len=%d", err, len(first))
	}
	if first[0].Attempt != 1 {
		t.Fatalf("expected attempt 1, got %d", first[0].Attempt)
	}

	time.Sleep(1200 * time.Millisecond)
	none, err := e.PollAssignments("worker-timeout", 1)
	if err != nil {
		t.Fatalf("poll after timeout: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("expected backoff delay with no assignment, got %d", len(none))
	}

	time.Sleep(1100 * time.Millisecond)
	second, err := e.PollAssignments("worker-timeout", 1)
	if err != nil || len(second) != 1 {
		t.Fatalf("expected second assignment after backoff, err=%v len=%d", err, len(second))
	}
	if second[0].Attempt != 2 {
		t.Fatalf("expected attempt 2, got %d", second[0].Attempt)
	}

	time.Sleep(1200 * time.Millisecond)
	last, err := e.PollAssignments("worker-timeout", 1)
	if err != nil {
		t.Fatalf("poll after second timeout: %v", err)
	}
	if len(last) != 0 {
		t.Fatalf("expected no assignment after max retries exhausted")
	}

	job, ok, err := e.GetJob("job-timeout")
	if err != nil || !ok {
		t.Fatalf("expected timeout job record, err=%v ok=%v", err, ok)
	}
	if job.Status != JobFailed {
		t.Fatalf("expected job failed after retry exhaustion, got %s", job.Status)
	}
}

func TestArchiveJobFromTerminalState(t *testing.T) {
	e := NewEngine(state.NewMemoryStore(), state.NewMemoryQueue(), Options{QueueBackend: "memory"})
	if err := e.RegisterWorker(splaiapi.RegisterWorkerRequest{
		WorkerID: "worker-archive",
		CPU:      4,
		Memory:   "8Gi",
		Models:   []string{"m-a"},
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}
	dag := planner.DAG{
		DAGID: "dag-archive",
		Tasks: []planner.Task{
			{TaskID: "t1", Type: "llm_inference", Inputs: map[string]string{"prompt": "ok"}, TimeoutSec: 30, MaxRetries: 1},
		},
	}
	if err := e.AddJob("job-archive", "tenant-a", "chat", "ok", "enterprise-default", "interactive", "internal", "m-a", "", dag); err != nil {
		t.Fatalf("add job: %v", err)
	}
	assn, err := e.PollAssignments("worker-archive", 1)
	if err != nil || len(assn) != 1 {
		t.Fatalf("poll assignment failed: err=%v len=%d", err, len(assn))
	}
	if err := e.ReportTaskResult(splaiapi.ReportTaskResultRequest{
		WorkerID:          "worker-archive",
		JobID:             assn[0].JobID,
		TaskID:            assn[0].TaskID,
		LeaseID:           assn[0].LeaseID,
		IdempotencyKey:    "k-1",
		Status:            JobCompleted,
		OutputArtifactURI: "artifact://job-archive/t1/output.json",
		DurationMillis:    1,
	}); err != nil {
		t.Fatalf("report completed: %v", err)
	}

	ok, err := e.ArchiveJob("job-archive")
	if err != nil || !ok {
		t.Fatalf("archive job failed: ok=%v err=%v", ok, err)
	}
	job, found, err := e.GetJob("job-archive")
	if err != nil || !found {
		t.Fatalf("get job failed: found=%v err=%v", found, err)
	}
	if job.Status != JobArchived {
		t.Fatalf("expected archived status, got %s", job.Status)
	}
}
