package api

import (
	"net/http"
	"testing"

	"github.com/example/splai/internal/planner"
	"github.com/example/splai/internal/policy"
	"github.com/example/splai/internal/scheduler"
	"github.com/example/splai/internal/state"
)

func TestSubmitJobPolicyDenyReturnsForbidden(t *testing.T) {
	engine := scheduler.NewEngine(
		state.NewMemoryStore(),
		state.NewMemoryQueue(),
		scheduler.Options{
			QueueBackend: "memory",
			PolicyEngine: policy.NewFromConfig(policy.Config{
				DefaultAction: "allow",
				Rules: []policy.Rule{
					{
						Name:   "deny-confidential-external",
						Effect: "deny",
						Reason: "confidential_external_forbidden",
						Match: policy.RuleMatch{
							DataClassification: "confidential",
							Model:              "external_api",
						},
					},
				},
			}),
		},
	)
	srv := NewServer(planner.NewCompiler(), engine)
	h := srv.Handler()

	body := []byte(`{"type":"chat","input":"hello","policy":"enterprise-default","priority":"interactive","tenant":"tenant-a","model":"external_api","data_classification":"confidential"}`)
	w := reqJSON(t, h, http.MethodPost, "/v1/jobs", body)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for policy denied submit, got %d body=%s", w.Code, w.Body.String())
	}
}
