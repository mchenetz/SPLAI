package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/splai/internal/planner"
	"github.com/example/splai/internal/scheduler"
	"github.com/example/splai/pkg/splaiapi"
)

func TestAuditEndpointFiltersAndCSV(t *testing.T) {
	t.Setenv("SPLAI_API_TOKENS", "operator-token:operator|metrics")
	srv := NewServer(planner.NewCompiler(), scheduler.NewInMemoryEngine())
	h := srv.Handler()

	body := []byte(`{"tasks":[{"job_id":"job-1","task_id":"t1"}],"dry_run":true}`)
	w := reqWithToken(t, h, http.MethodPost, "/v1/admin/queue/dead-letter", "operator-token", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for dry-run requeue, got %d body=%s", w.Code, w.Body.String())
	}
	var rr splaiapi.RequeueDeadLettersResponse
	if err := json.NewDecoder(w.Body).Decode(&rr); err != nil {
		t.Fatalf("decode requeue response: %v", err)
	}
	if !rr.DryRun {
		t.Fatalf("expected dry_run response")
	}

	w = reqWithToken(t, h, http.MethodGet, "/v1/admin/audit?action=dead_letter_requeue&result=dry_run&limit=10", "operator-token", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for filtered audit, got %d body=%s", w.Code, w.Body.String())
	}
	var audits splaiapi.ListAuditEventsResponse
	if err := json.NewDecoder(w.Body).Decode(&audits); err != nil {
		t.Fatalf("decode audit response: %v", err)
	}
	if audits.Returned == 0 {
		t.Fatalf("expected filtered audit events")
	}
	if audits.Events[0].Action != "dead_letter_requeue" {
		t.Fatalf("unexpected action %s", audits.Events[0].Action)
	}
	if audits.Events[0].Result != "dry_run" {
		t.Fatalf("unexpected result %s", audits.Events[0].Result)
	}
	if audits.Events[0].EventHash == "" {
		t.Fatalf("expected event hash in audit record")
	}

	w = reqWithToken(t, h, http.MethodGet, "/v1/admin/audit?format=csv&limit=5", "operator-token", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for csv audit export, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("unexpected csv content type: %s", w.Header().Get("Content-Type"))
	}
	csvBody := w.Body.String()
	if !strings.Contains(csvBody, "action,actor,tenant") {
		t.Fatalf("expected csv header in response, got: %s", csvBody)
	}
	if !strings.Contains(csvBody, "dead_letter_requeue") {
		t.Fatalf("expected audit row in csv output")
	}
}

func TestDeadLetterRequeueSafetyControls(t *testing.T) {
	t.Setenv("SPLAI_API_TOKENS", "operator-token:operator")
	t.Setenv("SPLAI_ADMIN_REQUEUE_MAX_BATCH", "1")
	t.Setenv("SPLAI_ADMIN_REQUEUE_RATE_LIMIT_PER_MIN", "1")
	t.Setenv("SPLAI_ADMIN_REQUEUE_CONFIRM_THRESHOLD", "1")
	t.Setenv("SPLAI_ADMIN_REQUEUE_CONFIRM_TOKEN", "confirm-me")
	srv := NewServer(planner.NewCompiler(), scheduler.NewInMemoryEngine())
	h := srv.Handler()

	// Missing confirm token for threshold hit.
	body := []byte(`{"tasks":[{"job_id":"job-1","task_id":"t1"}]}`)
	w := reqWithToken(t, h, http.MethodPost, "/v1/admin/queue/dead-letter", "operator-token", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 missing confirm token, got %d", w.Code)
	}

	// With confirm token should pass.
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/queue/dead-letter", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer operator-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-SPLAI-Confirm", "confirm-me")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with confirm token, got %d body=%s", w.Code, w.Body.String())
	}

	// Second non-dry-run requeue should hit rate limit.
	req = httptest.NewRequest(http.MethodPost, "/v1/admin/queue/dead-letter", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer operator-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-SPLAI-Confirm", "confirm-me")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for rate limit, got %d", w.Code)
	}

	// Batch size guard.
	largeBody := []byte(`{"tasks":[{"job_id":"job-1","task_id":"a"},{"job_id":"job-1","task_id":"b"}]}`)
	req = httptest.NewRequest(http.MethodPost, "/v1/admin/queue/dead-letter", strings.NewReader(string(largeBody)))
	req.Header.Set("Authorization", "Bearer operator-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-SPLAI-Confirm", "confirm-me")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for max batch size, got %d", w.Code)
	}
}
