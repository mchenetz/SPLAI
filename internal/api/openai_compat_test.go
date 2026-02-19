package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/example/splai/internal/planner"
	"github.com/example/splai/internal/scheduler"
	"github.com/example/splai/pkg/splaiapi"
)

func TestOpenAICompatDisabledByDefault(t *testing.T) {
	srv := NewServer(planner.NewCompiler(), scheduler.NewInMemoryEngine())
	w := reqJSON(t, srv.Handler(), http.MethodPost, "/v1/chat/completions", []byte(`{}`))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when compat disabled, got %d", w.Code)
	}
}

func TestOpenAIChatCompletionsCompat(t *testing.T) {
	t.Setenv("SPLAI_OPENAI_COMPAT", "true")
	engine := scheduler.NewInMemoryEngine()
	if err := engine.RegisterWorker(splaiapi.RegisterWorkerRequest{
		WorkerID: "compat-worker-1",
		CPU:      4,
		Memory:   "8Gi",
		Models:   []string{"llama3-8b-q4"},
		Tools:    []string{"bash"},
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}
	stop := make(chan struct{})
	defer close(stop)
	go runInlineWorker(engine, "compat-worker-1", stop)

	srv := NewServer(planner.NewCompiler(), engine)
	body := []byte(`{"model":"llama3-8b-q4","messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"Say hi"}]}`)
	w := reqJSON(t, srv.Handler(), http.MethodPost, "/v1/chat/completions", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["object"] != "chat.completion" {
		t.Fatalf("unexpected object: %v", resp["object"])
	}
	choices, ok := resp["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatalf("expected choices in response")
	}
}

func TestOpenAIResponsesCompat(t *testing.T) {
	t.Setenv("SPLAI_OPENAI_COMPAT", "true")
	engine := scheduler.NewInMemoryEngine()
	if err := engine.RegisterWorker(splaiapi.RegisterWorkerRequest{
		WorkerID: "compat-worker-2",
		CPU:      4,
		Memory:   "8Gi",
		Models:   []string{"llama3-8b-q4"},
		Tools:    []string{"bash"},
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}
	stop := make(chan struct{})
	defer close(stop)
	go runInlineWorker(engine, "compat-worker-2", stop)

	srv := NewServer(planner.NewCompiler(), engine)
	body := []byte(`{"model":"llama3-8b-q4","input":"Summarize this."}`)
	w := reqJSON(t, srv.Handler(), http.MethodPost, "/v1/responses", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["object"] != "response" {
		t.Fatalf("unexpected object: %v", resp["object"])
	}
}

func runInlineWorker(engine *scheduler.Engine, workerID string, stop <-chan struct{}) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			assignments, err := engine.PollAssignments(workerID, 4)
			if err != nil {
				continue
			}
			for _, a := range assignments {
				_ = engine.ReportTaskResult(splaiapi.ReportTaskResultRequest{
					WorkerID:          workerID,
					JobID:             a.JobID,
					TaskID:            a.TaskID,
					LeaseID:           a.LeaseID,
					IdempotencyKey:    fmt.Sprintf("%s:%s:%s:%d", workerID, a.JobID, a.TaskID, a.Attempt),
					Status:            scheduler.JobCompleted,
					OutputArtifactURI: fmt.Sprintf("artifact://%s/%s/output.json", a.JobID, a.TaskID),
					DurationMillis:    1,
				})
			}
		}
	}
}
