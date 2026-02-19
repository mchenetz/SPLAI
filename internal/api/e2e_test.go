package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/splai/internal/planner"
	"github.com/example/splai/internal/scheduler"
	"github.com/example/splai/pkg/splaiapi"
)

func TestEndToEndJobLifecycle(t *testing.T) {
	srv := NewServer(planner.NewCompiler(), scheduler.NewInMemoryEngine())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	register := splaiapi.RegisterWorkerRequest{
		WorkerID: "worker-test-1",
		CPU:      8,
		Memory:   "16Gi",
		GPU:      false,
		Models:   []string{"llama3-8b-q4"},
		Tools:    []string{"bash"},
	}
	doJSON(t, http.MethodPost, ts.URL+"/v1/workers/register", register, nil)

	submit := splaiapi.SubmitJobRequest{
		Type:     "chat",
		Input:    "Analyze 500 support tickets and produce root causes.",
		Policy:   "enterprise-default",
		Priority: "interactive",
	}
	var submitResp splaiapi.SubmitJobResponse
	doJSON(t, http.MethodPost, ts.URL+"/v1/jobs", submit, &submitResp)
	if submitResp.JobID == "" {
		t.Fatalf("expected job id")
	}

	completed := false
	for i := 0; i < 20; i++ {
		var assignments splaiapi.PollAssignmentsResponse
		doJSON(t, http.MethodGet, ts.URL+"/v1/workers/worker-test-1/assignments?max_tasks=2", nil, &assignments)

		for _, a := range assignments.Assignments {
			report := splaiapi.ReportTaskResultRequest{
				WorkerID:          "worker-test-1",
				JobID:             a.JobID,
				TaskID:            a.TaskID,
				LeaseID:           a.LeaseID,
				IdempotencyKey:    fmt.Sprintf("worker-test-1:%s:%s:%d", a.JobID, a.TaskID, a.Attempt),
				Status:            scheduler.JobCompleted,
				OutputArtifactURI: fmt.Sprintf("artifact://%s/%s/output.json", a.JobID, a.TaskID),
				DurationMillis:    5,
			}
			doJSON(t, http.MethodPost, ts.URL+"/v1/tasks/report", report, nil)
		}

		var job splaiapi.JobStatusResponse
		doJSON(t, http.MethodGet, ts.URL+"/v1/jobs/"+submitResp.JobID, nil, &job)
		if job.Status == scheduler.JobCompleted {
			completed = true
			if job.ResultArtifactURI == "" {
				t.Fatalf("expected result artifact uri")
			}
			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	if !completed {
		t.Fatalf("expected job to complete")
	}
}

func TestJobTasksEndpoint(t *testing.T) {
	srv := NewServer(planner.NewCompiler(), scheduler.NewInMemoryEngine())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	register := splaiapi.RegisterWorkerRequest{
		WorkerID: "worker-test-2",
		CPU:      8,
		Memory:   "16Gi",
		GPU:      false,
		Models:   []string{"llama3-8b-q4"},
		Tools:    []string{"bash"},
	}
	doJSON(t, http.MethodPost, ts.URL+"/v1/workers/register", register, nil)

	submit := splaiapi.SubmitJobRequest{
		Type:     "chat",
		Input:    "simple prompt",
		Policy:   "enterprise-default",
		Priority: "interactive",
	}
	var submitResp splaiapi.SubmitJobResponse
	doJSON(t, http.MethodPost, ts.URL+"/v1/jobs", submit, &submitResp)

	var assignments splaiapi.PollAssignmentsResponse
	doJSON(t, http.MethodGet, ts.URL+"/v1/workers/worker-test-2/assignments?max_tasks=1", nil, &assignments)
	if len(assignments.Assignments) != 1 {
		t.Fatalf("expected 1 assignment, got %d", len(assignments.Assignments))
	}
	a := assignments.Assignments[0]

	var tasksBefore splaiapi.JobTasksResponse
	doJSON(t, http.MethodGet, ts.URL+"/v1/jobs/"+submitResp.JobID+"/tasks", nil, &tasksBefore)
	if len(tasksBefore.Tasks) != 1 {
		t.Fatalf("expected 1 task in tasks endpoint, got %d", len(tasksBefore.Tasks))
	}
	if tasksBefore.Tasks[0].Status != scheduler.JobRunning {
		t.Fatalf("expected running task status, got %s", tasksBefore.Tasks[0].Status)
	}
	if tasksBefore.Tasks[0].Attempt != 1 {
		t.Fatalf("expected attempt 1, got %d", tasksBefore.Tasks[0].Attempt)
	}
	if tasksBefore.Tasks[0].WorkerID != "worker-test-2" {
		t.Fatalf("expected worker id worker-test-2, got %s", tasksBefore.Tasks[0].WorkerID)
	}
	if tasksBefore.Tasks[0].LeaseID == "" {
		t.Fatalf("expected lease id to be populated")
	}

	report := splaiapi.ReportTaskResultRequest{
		WorkerID:          "worker-test-2",
		JobID:             a.JobID,
		TaskID:            a.TaskID,
		LeaseID:           a.LeaseID,
		IdempotencyKey:    fmt.Sprintf("worker-test-2:%s:%s:%d", a.JobID, a.TaskID, a.Attempt),
		Status:            scheduler.JobCompleted,
		OutputArtifactURI: fmt.Sprintf("artifact://%s/%s/output.json", a.JobID, a.TaskID),
		DurationMillis:    3,
	}
	doJSON(t, http.MethodPost, ts.URL+"/v1/tasks/report", report, nil)

	var tasksAfter splaiapi.JobTasksResponse
	doJSON(t, http.MethodGet, ts.URL+"/v1/jobs/"+submitResp.JobID+"/tasks", nil, &tasksAfter)
	if len(tasksAfter.Tasks) != 1 {
		t.Fatalf("expected 1 task after report, got %d", len(tasksAfter.Tasks))
	}
	if tasksAfter.Tasks[0].Status != scheduler.JobCompleted {
		t.Fatalf("expected completed task status, got %s", tasksAfter.Tasks[0].Status)
	}
	if tasksAfter.Tasks[0].LeaseID != "" {
		t.Fatalf("expected lease id to be cleared after completion")
	}
	if tasksAfter.Tasks[0].OutputURI == "" {
		t.Fatalf("expected output uri after completion")
	}
}

func doJSON(t *testing.T, method, url string, reqBody any, respBody any) {
	t.Helper()
	var body []byte
	if reqBody != nil {
		var err error
		body, err = json.Marshal(reqBody)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
	}

	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		t.Fatalf("request %s %s failed with status %s", method, url, resp.Status)
	}
	if respBody != nil {
		if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
}
