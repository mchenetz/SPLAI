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

func TestLoadFromEnvDefaultsToDenyWhenNoPolicyFile(t *testing.T) {
	t.Setenv("SPLAI_POLICY_MODE", "")
	t.Setenv("SPLAI_POLICY_FILE", "")
	engine, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	if engine.IsNoop() {
		t.Fatalf("expected non-noop engine in default enforce mode")
	}
	d := engine.EvaluateSubmit(SubmitInput{Tenant: "tenant-a", JobType: "chat"})
	if d.Allowed {
		t.Fatalf("expected default deny decision")
	}
	if d.ReasonCode != "default_deny" {
		t.Fatalf("unexpected reason code: %s", d.ReasonCode)
	}
}

func TestLoadFromEnvAllowAllMode(t *testing.T) {
	t.Setenv("SPLAI_POLICY_MODE", "allow_all")
	t.Setenv("SPLAI_POLICY_FILE", "")
	engine, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	if !engine.IsNoop() {
		t.Fatalf("expected noop allow-all engine")
	}
	d := engine.EvaluateSubmit(SubmitInput{Tenant: "tenant-a", JobType: "chat"})
	if !d.Allowed {
		t.Fatalf("expected allow decision in allow_all mode")
	}
}
