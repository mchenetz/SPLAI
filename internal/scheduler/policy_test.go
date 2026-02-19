package scheduler

import (
	"context"
	"testing"

	"github.com/example/splai/internal/planner"
	"github.com/example/splai/internal/policy"
	"github.com/example/splai/internal/state"
	"github.com/example/splai/pkg/splaiapi"
)

func TestPolicyDenyAtSubmit(t *testing.T) {
	store := state.NewMemoryStore()
	queue := state.NewMemoryQueue()
	p := policy.NewFromConfig(policy.Config{
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
	})
	e := NewEngine(store, queue, Options{QueueBackend: "memory", PolicyEngine: p})
	dag := planner.NewCompiler().Compile("job-1", "chat", "hello")

	err := e.AddJob("job-1", "tenant-a", "chat", "hello", "enterprise-default", "interactive", "confidential", "external_api", "", dag)
	if err == nil {
		t.Fatalf("expected policy deny error")
	}

	events, err := store.ListAuditEvents(context.Background(), state.AuditQuery{Limit: 20})
	if err != nil {
		t.Fatalf("list audits: %v", err)
	}
	if len(events) == 0 || events[0].Action != "policy_check_submit" || events[0].Result != "deny" {
		t.Fatalf("expected policy_check_submit deny audit, got %#v", events)
	}
}

func TestPolicyDenyAtAssignment(t *testing.T) {
	store := state.NewMemoryStore()
	queue := state.NewMemoryQueue()
	p := policy.NewFromConfig(policy.Config{
		DefaultAction: "allow",
		Rules: []policy.Rule{
			{
				Name:   "deny-locality-local",
				Effect: "deny",
				Reason: "locality_forbidden",
				Match: policy.RuleMatch{
					TaskType:       "llm_inference",
					WorkerLocality: "local",
				},
			},
		},
	})
	e := NewEngine(store, queue, Options{QueueBackend: "memory", PolicyEngine: p})
	if err := e.RegisterWorker(splaiapi.RegisterWorkerRequest{
		WorkerID: "w1",
		CPU:      8,
		Memory:   "16Gi",
		Locality: "local",
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	dag := planner.NewCompiler().Compile("job-2", "chat", "hello")
	if err := e.AddJob("job-2", "tenant-a", "chat", "hello", "enterprise-default", "interactive", "internal", "local_model", "", dag); err != nil {
		t.Fatalf("add job: %v", err)
	}
	assignments, err := e.PollAssignments("w1", 1)
	if err != nil {
		t.Fatalf("poll assignments: %v", err)
	}
	if len(assignments) != 0 {
		t.Fatalf("expected no assignments due to policy deny")
	}
	job, ok, err := e.GetJob("job-2")
	if err != nil || !ok {
		t.Fatalf("get job failed: %v", err)
	}
	if job.Status != JobFailed {
		t.Fatalf("expected failed job status after assignment deny, got %s", job.Status)
	}
	events, err := store.ListAuditEvents(context.Background(), state.AuditQuery{Limit: 20})
	if err != nil {
		t.Fatalf("list audits: %v", err)
	}
	found := false
	for _, ev := range events {
		if ev.Action == "policy_check_assignment" && ev.Result == "deny" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected policy_check_assignment deny audit event")
	}
}
