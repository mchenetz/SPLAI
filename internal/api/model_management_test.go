package api

import (
	"net/http/httptest"
	"testing"

	"github.com/example/splai/internal/planner"
	"github.com/example/splai/internal/scheduler"
	"github.com/example/splai/pkg/splaiapi"
)

func TestInjectModelInstallTasks(t *testing.T) {
	dag := planner.DAG{
		DAGID: "dag-1",
		Tasks: []planner.Task{
			{TaskID: "t1", Type: "llm_inference", Inputs: map[string]string{"prompt": "hi"}},
			{TaskID: "t2", Type: "aggregation", Inputs: map[string]string{"op": "agg"}, Dependencies: []string{"t1"}},
		},
	}
	out := injectModelInstallTasks(dag, "meta-llama/Llama-3-8B-Instruct", true)
	if len(out.Tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(out.Tasks))
	}
	if out.Tasks[0].Type != "model_download" {
		t.Fatalf("expected first task model_download, got %s", out.Tasks[0].Type)
	}
	var llm planner.Task
	for _, task := range out.Tasks {
		if task.TaskID == "t1" {
			llm = task
			break
		}
	}
	if llm.Inputs["_install_model_if_missing"] != "true" {
		t.Fatalf("expected llm install fallback flag")
	}
	if !containsString(llm.Dependencies, out.Tasks[0].TaskID) {
		t.Fatalf("expected llm task to depend on model install task")
	}
}

func TestModelPrefetchTargetsSpecificWorker(t *testing.T) {
	srv := NewServer(planner.NewCompiler(), scheduler.NewInMemoryEngine())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	doJSON(t, "POST", ts.URL+"/v1/workers/register", splaiapi.RegisterWorkerRequest{
		WorkerID: "worker-a",
		CPU:      8,
		Memory:   "16Gi",
		GPU:      false,
		Models:   []string{"llama3-8b-q4"},
		Tools:    []string{"bash"},
	}, nil)
	doJSON(t, "POST", ts.URL+"/v1/workers/register", splaiapi.RegisterWorkerRequest{
		WorkerID: "worker-b",
		CPU:      8,
		Memory:   "16Gi",
		GPU:      false,
		Models:   []string{"llama3-8b-q4"},
		Tools:    []string{"bash"},
	}, nil)

	onlyMissing := true
	var prefetchResp splaiapi.PrefetchModelResponse
	doJSON(t, "POST", ts.URL+"/v1/admin/models/prefetch", splaiapi.PrefetchModelRequest{
		Model:       "meta-llama/Llama-3-8B-Instruct",
		Source:      "huggingface",
		Workers:     []string{"worker-b"},
		OnlyMissing: &onlyMissing,
	}, &prefetchResp)
	if prefetchResp.JobID == "" {
		t.Fatalf("expected prefetch job id")
	}
	if len(prefetchResp.TargetedWorkers) != 1 || prefetchResp.TargetedWorkers[0] != "worker-b" {
		t.Fatalf("unexpected targeted workers: %+v", prefetchResp.TargetedWorkers)
	}

	var aAssignments splaiapi.PollAssignmentsResponse
	doJSON(t, "GET", ts.URL+"/v1/workers/worker-a/assignments?max_tasks=5", nil, &aAssignments)
	if len(aAssignments.Assignments) != 0 {
		t.Fatalf("expected worker-a to get no assignments, got %d", len(aAssignments.Assignments))
	}

	var bAssignments splaiapi.PollAssignmentsResponse
	doJSON(t, "GET", ts.URL+"/v1/workers/worker-b/assignments?max_tasks=5", nil, &bAssignments)
	if len(bAssignments.Assignments) != 1 {
		t.Fatalf("expected worker-b to get one assignment, got %d", len(bAssignments.Assignments))
	}
	if bAssignments.Assignments[0].Type != "model_download" {
		t.Fatalf("expected model_download assignment, got %s", bAssignments.Assignments[0].Type)
	}
}
