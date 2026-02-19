package planner

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPProviderPlan(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"dag_id":"dag-x","tasks":[{"task_id":"p1","type":"llm_inference","inputs":{"op":"decompose"}}]}`))
	}))
	defer srv.Close()

	p := NewHTTPProvider(srv.URL, "")
	d, err := p.Plan("job-1", "chat", "hello")
	if err != nil {
		t.Fatalf("plan failed: %v", err)
	}
	if d.DAGID != "dag-x" || len(d.Tasks) != 1 || d.Tasks[0].TaskID != "p1" {
		t.Fatalf("unexpected dag: %#v", d)
	}
}
