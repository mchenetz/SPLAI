package state

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPostgresStoreIntegrationAuditAndTasks(t *testing.T) {
	dsn := os.Getenv("DAEF_POSTGRES_DSN_INTEGRATION")
	if dsn == "" {
		t.Skip("set DAEF_POSTGRES_DSN_INTEGRATION to run Postgres integration tests")
	}
	store, err := NewPostgresStore(dsn)
	if err != nil {
		t.Fatalf("new postgres store: %v", err)
	}

	ctx := context.Background()
	jobID := "job-int-" + time.Now().UTC().Format("20060102150405")
	job := JobRecord{ID: jobID, Tenant: "tenant-int", Type: "chat", Input: "hello", Policy: "enterprise-default", Priority: "interactive", Status: "Queued"}
	task := TaskRecord{JobID: jobID, TaskID: "t1", Type: "llm_inference", Inputs: map[string]string{"prompt": "hello"}, Dependencies: []string{}, Status: "Queued", MaxRetries: 1, TimeoutSec: 30}
	if err := store.CreateJobWithTasks(ctx, job, []TaskRecord{task}); err != nil {
		t.Fatalf("create job: %v", err)
	}

	if err := store.AppendAuditEvent(ctx, AuditEventRecord{Action: "integration_test", Actor: "itest", Tenant: "tenant-int", Result: "ok", Requested: 1}); err != nil {
		t.Fatalf("append audit event: %v", err)
	}
	audits, err := store.ListAuditEvents(ctx, AuditQuery{Limit: 5, Action: "integration_test", Tenant: "tenant-int"})
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	if len(audits) == 0 {
		t.Fatalf("expected audit events")
	}
	if audits[0].EventHash == "" {
		t.Fatalf("expected event hash")
	}

	tasks, err := store.ListTasksByJob(ctx, jobID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].TaskID != "t1" {
		t.Fatalf("unexpected task rows: %+v", tasks)
	}
}
