package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/daef/internal/planner"
	"github.com/example/daef/internal/scheduler"
	"github.com/example/daef/pkg/daefapi"
)

func TestAuthScopesForSensitiveEndpoints(t *testing.T) {
	t.Setenv("DAEF_API_TOKENS", "operator-token:operator|metrics,metrics-token:metrics,tenant-a-token:tenant:tenant-a")
	srv := NewServer(planner.NewCompiler(), scheduler.NewInMemoryEngine())
	h := srv.Handler()

	w := reqJSON(t, h, http.MethodGet, "/v1/metrics", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", w.Code)
	}

	w = reqWithToken(t, h, http.MethodGet, "/v1/metrics", "metrics-token", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for metrics token on /v1/metrics, got %d", w.Code)
	}

	w = reqWithToken(t, h, http.MethodGet, "/v1/admin/queue/dead-letter", "metrics-token", nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for metrics-only token on admin endpoint, got %d", w.Code)
	}

	w = reqWithToken(t, h, http.MethodGet, "/v1/admin/queue/dead-letter", "operator-token", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for operator token on admin endpoint, got %d", w.Code)
	}

	body := []byte(`{"tasks":[{"job_id":"job-1","task_id":"t1"}]}`)
	w = reqWithToken(t, h, http.MethodPost, "/v1/admin/queue/dead-letter", "operator-token", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for operator requeue request, got %d body=%s", w.Code, w.Body.String())
	}

	w = reqWithToken(t, h, http.MethodGet, "/v1/admin/audit?limit=10", "operator-token", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for operator audit list request, got %d body=%s", w.Code, w.Body.String())
	}
	var audits daefapi.ListAuditEventsResponse
	if err := json.NewDecoder(w.Body).Decode(&audits); err != nil {
		t.Fatalf("decode audit response: %v", err)
	}
	if audits.Returned == 0 {
		t.Fatalf("expected at least one audit event")
	}

	w = reqWithToken(t, h, http.MethodGet, "/v1/admin/audit?limit=10", "metrics-token", nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for metrics-only token on audit endpoint, got %d", w.Code)
	}

	// Tenant enforcement for job submission.
	jobBody := []byte(`{"type":"chat","input":"hello","policy":"enterprise-default","priority":"interactive","tenant":"tenant-a"}`)
	w = reqWithToken(t, h, http.MethodPost, "/v1/jobs", "tenant-a-token", jobBody)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected tenant token to submit tenant-a job, got %d body=%s", w.Code, w.Body.String())
	}
	jobBody = []byte(`{"type":"chat","input":"hello","policy":"enterprise-default","priority":"interactive","tenant":"tenant-b"}`)
	w = reqWithToken(t, h, http.MethodPost, "/v1/jobs", "tenant-a-token", jobBody)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected tenant token forbidden for tenant-b job, got %d", w.Code)
	}
}

func reqWithToken(t *testing.T, h http.Handler, method, path, token string, reqBody []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(reqBody))
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}
