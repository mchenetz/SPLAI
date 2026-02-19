package policy

import "testing"

func TestEvaluateSubmitQuotaAndDenyRule(t *testing.T) {
	engine := NewFromConfig(Config{
		DefaultAction: "allow",
		TenantQuotas: map[string]TenantQuota{
			"tenant-a": {MaxRunningJobs: 1},
		},
		Rules: []Rule{
			{
				Name:   "deny-confidential-external",
				Effect: "deny",
				Reason: "confidential_external_forbidden",
				Match: RuleMatch{
					DataClassification: "confidential",
					Model:              "external_api",
				},
			},
		},
	})

	d := engine.EvaluateSubmit(SubmitInput{
		Tenant:             "tenant-a",
		JobType:            "chat",
		Model:              "external_api",
		DataClassification: "confidential",
		RunningJobs:        0,
	})
	if d.Allowed {
		t.Fatalf("expected deny decision")
	}
	if d.ReasonCode != "confidential_external_forbidden" {
		t.Fatalf("unexpected reason code: %s", d.ReasonCode)
	}

	d = engine.EvaluateSubmit(SubmitInput{
		Tenant:      "tenant-a",
		JobType:     "chat",
		RunningJobs: 1,
	})
	if d.Allowed {
		t.Fatalf("expected quota deny decision")
	}
	if d.ReasonCode != "quota_running_jobs_exceeded" {
		t.Fatalf("unexpected quota reason code: %s", d.ReasonCode)
	}
}

func TestEvaluateAssignmentQuota(t *testing.T) {
	engine := NewFromConfig(Config{
		DefaultAction: "allow",
		TenantQuotas: map[string]TenantQuota{
			"tenant-a": {MaxRunningTasks: 2},
		},
	})
	d := engine.EvaluateAssignment(AssignmentInput{
		Tenant:       "tenant-a",
		TaskType:     "llm_inference",
		RunningTasks: 2,
	})
	if d.Allowed {
		t.Fatalf("expected running task quota deny")
	}
	if d.ReasonCode != "quota_running_tasks_exceeded" {
		t.Fatalf("unexpected reason code: %s", d.ReasonCode)
	}
}
