package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/splai/internal/planner"
	"github.com/example/splai/internal/scheduler"
	"github.com/example/splai/pkg/splaiapi"
)

func TestJobTasksEndpointHandler(t *testing.T) {
	srv := NewServer(planner.NewCompiler(), scheduler.NewInMemoryEngine())
	h := srv.Handler()

	mustReqJSON(t, h, http.MethodPost, "/v1/workers/register", splaiapi.RegisterWorkerRequest{
		WorkerID: "worker-inline-1",
		CPU:      4,
		Memory:   "8Gi",
		GPU:      false,
		Models:   []string{"llama3-8b-q4"},
		Tools:    []string{"bash"},
	}, nil)

	var submitResp splaiapi.SubmitJobResponse
	mustReqJSON(t, h, http.MethodPost, "/v1/jobs", splaiapi.SubmitJobRequest{
		Type:     "chat",
		Input:    "simple prompt",
		Policy:   "enterprise-default",
		Priority: "interactive",
	}, &submitResp)
	if submitResp.JobID == "" {
		t.Fatalf("expected job id")
	}

	var assignments splaiapi.PollAssignmentsResponse
	mustReqJSON(t, h, http.MethodGet, "/v1/workers/worker-inline-1/assignments?max_tasks=1", nil, &assignments)
	if len(assignments.Assignments) != 1 {
		t.Fatalf("expected 1 assignment got %d", len(assignments.Assignments))
	}
	a := assignments.Assignments[0]

	var tasksBefore splaiapi.JobTasksResponse
	mustReqJSON(t, h, http.MethodGet, "/v1/jobs/"+submitResp.JobID+"/tasks", nil, &tasksBefore)
	if tasksBefore.Total != 1 || tasksBefore.Returned != 1 {
		t.Fatalf("expected total/returned to be 1, got total=%d returned=%d", tasksBefore.Total, tasksBefore.Returned)
	}
	if len(tasksBefore.Tasks) != 1 {
		t.Fatalf("expected 1 task got %d", len(tasksBefore.Tasks))
	}
	before := tasksBefore.Tasks[0]
	if before.Status != scheduler.JobRunning {
		t.Fatalf("expected running status got %s", before.Status)
	}
	if before.Attempt != 1 {
		t.Fatalf("expected attempt=1 got %d", before.Attempt)
	}
	if before.WorkerID != "worker-inline-1" {
		t.Fatalf("expected worker id worker-inline-1 got %s", before.WorkerID)
	}
	if before.LeaseID == "" || before.LeaseExpires == "" {
		t.Fatalf("expected lease fields to be populated, got %+v", before)
	}

	mustReqJSON(t, h, http.MethodPost, "/v1/tasks/report", splaiapi.ReportTaskResultRequest{
		WorkerID:          "worker-inline-1",
		JobID:             a.JobID,
		TaskID:            a.TaskID,
		LeaseID:           a.LeaseID,
		IdempotencyKey:    fmt.Sprintf("worker-inline-1:%s:%s:%d", a.JobID, a.TaskID, a.Attempt),
		Status:            scheduler.JobCompleted,
		OutputArtifactURI: fmt.Sprintf("artifact://%s/%s/output.json", a.JobID, a.TaskID),
		DurationMillis:    2,
	}, nil)

	var tasksAfter splaiapi.JobTasksResponse
	mustReqJSON(t, h, http.MethodGet, "/v1/jobs/"+submitResp.JobID+"/tasks", nil, &tasksAfter)
	if tasksAfter.Total != 1 || tasksAfter.Returned != 1 {
		t.Fatalf("expected total/returned to be 1 after completion, got total=%d returned=%d", tasksAfter.Total, tasksAfter.Returned)
	}
	after := tasksAfter.Tasks[0]
	if after.Status != scheduler.JobCompleted {
		t.Fatalf("expected completed status got %s", after.Status)
	}
	if after.LeaseID != "" {
		t.Fatalf("expected lease id cleared got %s", after.LeaseID)
	}
	if after.OutputURI == "" {
		t.Fatalf("expected output uri set")
	}
}

func TestJobTasksEndpointFilteringAndPagination(t *testing.T) {
	srv := NewServer(planner.NewCompiler(), scheduler.NewInMemoryEngine())
	h := srv.Handler()

	mustReqJSON(t, h, http.MethodPost, "/v1/workers/register", splaiapi.RegisterWorkerRequest{
		WorkerID: "worker-inline-2",
		CPU:      4,
		Memory:   "8Gi",
		GPU:      false,
		Models:   []string{"llama3-8b-q4"},
		Tools:    []string{"bash"},
	}, nil)

	var submitResp splaiapi.SubmitJobResponse
	mustReqJSON(t, h, http.MethodPost, "/v1/jobs", splaiapi.SubmitJobRequest{
		Type:     "chat",
		Input:    "Analyze 500 support tickets and produce root causes.",
		Policy:   "enterprise-default",
		Priority: "interactive",
	}, &submitResp)

	var assignments splaiapi.PollAssignmentsResponse
	mustReqJSON(t, h, http.MethodGet, "/v1/workers/worker-inline-2/assignments?max_tasks=1", nil, &assignments)
	if len(assignments.Assignments) != 1 {
		t.Fatalf("expected one running assignment, got %d", len(assignments.Assignments))
	}

	var all splaiapi.JobTasksResponse
	mustReqJSON(t, h, http.MethodGet, "/v1/jobs/"+submitResp.JobID+"/tasks", nil, &all)
	if all.Total != 3 || all.Returned != 3 {
		t.Fatalf("expected total/returned 3, got total=%d returned=%d", all.Total, all.Returned)
	}

	var queued splaiapi.JobTasksResponse
	mustReqJSON(t, h, http.MethodGet, "/v1/jobs/"+submitResp.JobID+"/tasks?status=Queued", nil, &queued)
	if queued.Total != 2 || queued.Returned != 2 {
		t.Fatalf("expected queued total/returned 2, got total=%d returned=%d", queued.Total, queued.Returned)
	}
	for _, tsk := range queued.Tasks {
		if tsk.Status != scheduler.JobQueued {
			t.Fatalf("expected only queued tasks, found %s", tsk.Status)
		}
	}

	var runningByWorker splaiapi.JobTasksResponse
	mustReqJSON(t, h, http.MethodGet, "/v1/jobs/"+submitResp.JobID+"/tasks?worker_id=worker-inline-2", nil, &runningByWorker)
	if runningByWorker.Total != 1 || runningByWorker.Returned != 1 {
		t.Fatalf("expected one task for worker filter, got total=%d returned=%d", runningByWorker.Total, runningByWorker.Returned)
	}
	if runningByWorker.Tasks[0].Status != scheduler.JobRunning {
		t.Fatalf("expected running task for worker filter, got %s", runningByWorker.Tasks[0].Status)
	}

	var paged splaiapi.JobTasksResponse
	mustReqJSON(t, h, http.MethodGet, "/v1/jobs/"+submitResp.JobID+"/tasks?limit=1&offset=1", nil, &paged)
	if paged.Total != 3 || paged.Returned != 1 || paged.Limit != 1 || paged.Offset != 1 {
		t.Fatalf("unexpected pagination response: %+v", paged)
	}

	w := reqJSON(t, h, http.MethodGet, "/v1/jobs/"+submitResp.JobID+"/tasks?limit=abc", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid limit, got %d", w.Code)
	}
}

func mustReqJSON(t *testing.T, h http.Handler, method, path string, reqBody any, respBody any) {
	t.Helper()
	var body []byte
	if reqBody != nil {
		var err error
		body, err = json.Marshal(reqBody)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
	}
	w := reqJSON(t, h, method, path, body)
	if w.Code >= 300 {
		t.Fatalf("request %s %s failed: status=%d body=%s", method, path, w.Code, w.Body.String())
	}
	if respBody != nil {
		if err := json.NewDecoder(w.Body).Decode(respBody); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
}

func reqJSON(t *testing.T, h http.Handler, method, path string, reqBody any) *httptest.ResponseRecorder {
	t.Helper()
	var body []byte
	switch v := reqBody.(type) {
	case nil:
		body = nil
	case []byte:
		body = v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		body = b
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}
