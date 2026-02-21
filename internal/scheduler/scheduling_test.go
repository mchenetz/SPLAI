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

func TestReportTaskResultRetrySuccessKeepsJobRunningUntilAllTasksDone(t *testing.T) {
	e := NewEngine(state.NewMemoryStore(), state.NewMemoryQueue(), Options{QueueBackend: "memory"})
	if err := e.RegisterWorker(splaiapi.RegisterWorkerRequest{
		WorkerID: "worker-retry-fix",
		CPU:      8,
		Memory:   "16Gi",
		Models:   []string{"m-a"},
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	dag := planner.DAG{
		DAGID: "dag-retry-fix",
		Tasks: []planner.Task{
			{TaskID: "t1", Type: "llm_inference", Inputs: map[string]string{"prompt": "first"}, TimeoutSec: 30, MaxRetries: 1},
			{TaskID: "t2", Type: "llm_inference", Inputs: map[string]string{"prompt": "second"}, TimeoutSec: 30, MaxRetries: 1},
		},
	}
	if err := e.AddJob("job-retry-fix", "tenant-a", "chat", "retry", "enterprise-default", "interactive", "internal", "m-a", "", dag); err != nil {
		t.Fatalf("add job: %v", err)
	}

	first, err := e.PollAssignments("worker-retry-fix", 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("expected first assignment, err=%v len=%d", err, len(first))
	}
	firstTaskID := first[0].TaskID

	if err := e.ReportTaskResult(splaiapi.ReportTaskResultRequest{
		WorkerID:       "worker-retry-fix",
		JobID:          first[0].JobID,
		TaskID:         firstTaskID,
		LeaseID:        first[0].LeaseID,
		IdempotencyKey: "retry-fail-1",
		Status:         JobFailed,
		Error:          "transient",
	}); err != nil {
		t.Fatalf("report failed: %v", err)
	}

	time.Sleep(1100 * time.Millisecond)
	second, err := e.PollAssignments("worker-retry-fix", 1)
	if err != nil || len(second) != 1 {
		t.Fatalf("expected retry assignment, err=%v len=%d", err, len(second))
	}
	if second[0].TaskID != firstTaskID {
		t.Fatalf("expected retry on %s, got %s", firstTaskID, second[0].TaskID)
	}

	if err := e.ReportTaskResult(splaiapi.ReportTaskResultRequest{
		WorkerID:          "worker-retry-fix",
		JobID:             second[0].JobID,
		TaskID:            second[0].TaskID,
		LeaseID:           second[0].LeaseID,
		IdempotencyKey:    "retry-ok-1",
		Status:            JobCompleted,
		OutputArtifactURI: "artifact://job-retry-fix/t1/output.json",
		DurationMillis:    1,
	}); err != nil {
		t.Fatalf("report completed retry: %v", err)
	}

	job, ok, err := e.GetJob("job-retry-fix")
	if err != nil || !ok {
		t.Fatalf("get job: ok=%v err=%v", ok, err)
	}
	if job.Status != JobRunning {
		t.Fatalf("expected running status after retry success with remaining work, got %s", job.Status)
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

func TestLLMInferenceRespectsAdvertisedBackendCapability(t *testing.T) {
	e := NewEngine(state.NewMemoryStore(), state.NewMemoryQueue(), Options{QueueBackend: "memory"})
	if err := e.RegisterWorker(splaiapi.RegisterWorkerRequest{
		WorkerID: "worker-ollama",
		CPU:      8,
		Memory:   "16Gi",
		Backends: []string{"ollama"},
		Models:   []string{"m-a"},
	}); err != nil {
		t.Fatalf("register worker-ollama: %v", err)
	}
	if err := e.RegisterWorker(splaiapi.RegisterWorkerRequest{
		WorkerID: "worker-vllm",
		CPU:      8,
		Memory:   "16Gi",
		Backends: []string{"vllm"},
		Models:   []string{"m-a"},
	}); err != nil {
		t.Fatalf("register worker-vllm: %v", err)
	}

	dag := planner.DAG{
		DAGID: "dag-backend-capability",
		Tasks: []planner.Task{
			{
				TaskID:     "t1",
				Type:       "llm_inference",
				Inputs:     map[string]string{"prompt": "hi", "backend": "vllm", "model": "m-a"},
				TimeoutSec: 30,
				MaxRetries: 1,
			},
		},
	}
	if err := e.AddJob("job-backend-capability", "tenant-a", "chat", "hi", "enterprise-default", "interactive", "internal", "m-a", "", dag); err != nil {
		t.Fatalf("add job: %v", err)
	}

	a, err := e.PollAssignments("worker-ollama", 1)
	if err != nil {
		t.Fatalf("poll worker-ollama: %v", err)
	}
	if len(a) != 0 {
		t.Fatalf("expected no assignment for non-matching backend worker")
	}
	b, err := e.PollAssignments("worker-vllm", 1)
	if err != nil {
		t.Fatalf("poll worker-vllm: %v", err)
	}
	if len(b) != 1 {
		t.Fatalf("expected one assignment for matching backend worker, got %d", len(b))
	}
}

func TestLLMInferenceRespectsAdvertisedModelInventory(t *testing.T) {
	e := NewEngine(state.NewMemoryStore(), state.NewMemoryQueue(), Options{QueueBackend: "memory"})
	if err := e.RegisterWorker(splaiapi.RegisterWorkerRequest{
		WorkerID: "worker-model-a",
		CPU:      8,
		Memory:   "16Gi",
		Backends: []string{"ollama"},
		Models:   []string{"m-a"},
	}); err != nil {
		t.Fatalf("register worker-model-a: %v", err)
	}
	if err := e.RegisterWorker(splaiapi.RegisterWorkerRequest{
		WorkerID: "worker-model-b",
		CPU:      8,
		Memory:   "16Gi",
		Backends: []string{"ollama"},
		Models:   []string{"m-b"},
	}); err != nil {
		t.Fatalf("register worker-model-b: %v", err)
	}

	dag := planner.DAG{
		DAGID: "dag-model-capability",
		Tasks: []planner.Task{
			{
				TaskID:     "t1",
				Type:       "llm_inference",
				Inputs:     map[string]string{"prompt": "hi", "backend": "ollama", "model": "m-b"},
				TimeoutSec: 30,
				MaxRetries: 1,
			},
		},
	}
	if err := e.AddJob("job-model-capability", "tenant-a", "chat", "hi", "enterprise-default", "interactive", "internal", "m-b", "", dag); err != nil {
		t.Fatalf("add job: %v", err)
	}

	a, err := e.PollAssignments("worker-model-a", 1)
	if err != nil {
		t.Fatalf("poll worker-model-a: %v", err)
	}
	if len(a) != 0 {
		t.Fatalf("expected no assignment for non-matching model worker")
	}
	b, err := e.PollAssignments("worker-model-b", 1)
	if err != nil {
		t.Fatalf("poll worker-model-b: %v", err)
	}
	if len(b) != 1 {
		t.Fatalf("expected one assignment for matching model worker, got %d", len(b))
	}
}
