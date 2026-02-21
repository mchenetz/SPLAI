package api

import (
	"net/http"
	"testing"

	"github.com/example/splai/internal/planner"
	"github.com/example/splai/internal/scheduler"
	"github.com/example/splai/pkg/splaiapi"
)

func TestSubmitRateLimitApplied(t *testing.T) {
	t.Setenv("SPLAI_SUBMIT_RATE_LIMIT_PER_MIN", "1")
	t.Setenv("SPLAI_SUBMIT_GLOBAL_RATE_LIMIT_PER_MIN", "10")
	srv := NewServer(planner.NewCompiler(), scheduler.NewInMemoryEngine())
	h := srv.Handler()

	mustReqJSON(t, h, http.MethodPost, "/v1/workers/register", splaiapi.RegisterWorkerRequest{
		WorkerID: "worker-rate-1",
		CPU:      4,
		Memory:   "8Gi",
		Models:   []string{"llama3-8b-q4"},
		Tools:    []string{"bash"},
	}, nil)

	mustReqJSON(t, h, http.MethodPost, "/v1/jobs", splaiapi.SubmitJobRequest{
		Type:     "chat",
		Input:    "first",
		Policy:   "enterprise-default",
		Priority: "interactive",
	}, nil)

	w := reqJSON(t, h, http.MethodPost, "/v1/jobs", splaiapi.SubmitJobRequest{
		Type:     "chat",
		Input:    "second",
		Policy:   "enterprise-default",
		Priority: "interactive",
	})
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for submit rate limit, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestArchiveEndpoint(t *testing.T) {
	srv := NewServer(planner.NewCompiler(), scheduler.NewInMemoryEngine())
	h := srv.Handler()

	mustReqJSON(t, h, http.MethodPost, "/v1/workers/register", splaiapi.RegisterWorkerRequest{
		WorkerID: "worker-archive-1",
		CPU:      4,
		Memory:   "8Gi",
		Models:   []string{"llama3-8b-q4"},
		Tools:    []string{"bash"},
	}, nil)

	var submitResp splaiapi.SubmitJobResponse
	mustReqJSON(t, h, http.MethodPost, "/v1/jobs", splaiapi.SubmitJobRequest{
		Type:     "chat",
		Input:    "archive me",
		Policy:   "enterprise-default",
		Priority: "interactive",
	}, &submitResp)

	var assignments splaiapi.PollAssignmentsResponse
	mustReqJSON(t, h, http.MethodGet, "/v1/workers/worker-archive-1/assignments?max_tasks=1", nil, &assignments)
	if len(assignments.Assignments) != 1 {
		t.Fatalf("expected one assignment")
	}
	a := assignments.Assignments[0]
	mustReqJSON(t, h, http.MethodPost, "/v1/tasks/report", splaiapi.ReportTaskResultRequest{
		WorkerID:          "worker-archive-1",
		JobID:             a.JobID,
		TaskID:            a.TaskID,
		LeaseID:           a.LeaseID,
		IdempotencyKey:    "archive-key-1",
		Status:            scheduler.JobCompleted,
		OutputArtifactURI: "artifact://archive/output.json",
		DurationMillis:    2,
	}, nil)

	mustReqJSON(t, h, http.MethodPost, "/v1/jobs/"+submitResp.JobID+"/archive", nil, nil)

	var job splaiapi.JobStatusResponse
	mustReqJSON(t, h, http.MethodGet, "/v1/jobs/"+submitResp.JobID, nil, &job)
	if job.Status != scheduler.JobArchived {
		t.Fatalf("expected archived status, got %s", job.Status)
	}
}
