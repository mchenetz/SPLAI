package models

import "testing"

func TestRouteSelectsReasoningRule(t *testing.T) {
	r := &Router{cfg: Config{
		DefaultBackend: "ollama",
		DefaultModel:   "llama3-8b-q4",
		Rules: []Rule{
			{
				Name:             "reasoning-gpu",
				WhenReasoning:    boolPtr(true),
				UseBackend:       "vllm",
				UseModel:         "llama3-70b",
				WhenLatencyClass: "interactive",
			},
		},
	}}
	d := r.Route(RouteInput{LatencyClass: "interactive", ReasoningRequired: true})
	if d.Backend != "vllm" || d.Model != "llama3-70b" || d.Rule != "reasoning-gpu" {
		t.Fatalf("unexpected route decision: %#v", d)
	}
}

func boolPtr(v bool) *bool { return &v }
