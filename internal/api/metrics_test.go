package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/daef/internal/observability"
	"github.com/example/daef/internal/planner"
	"github.com/example/daef/internal/scheduler"
)

func TestMetricsPrometheusEndpoint(t *testing.T) {
	observability.Default.Reset()
	observability.Default.IncCounter("queue_claimed_total", map[string]string{"queue_backend": "memory", "worker_id": "w1"}, 1)

	srv := NewServer(planner.NewCompiler(), scheduler.NewInMemoryEngine())
	req := httptest.NewRequest(http.MethodGet, "/v1/metrics/prometheus", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Fatalf("unexpected content type: %s", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "queue_claimed_total") {
		t.Fatalf("expected prometheus metric in body: %s", body)
	}
}
