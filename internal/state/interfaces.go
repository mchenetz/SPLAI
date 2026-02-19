package state

import (
	"context"
	"time"
)

type Store interface {
	CreateJobWithTasks(ctx context.Context, job JobRecord, tasks []TaskRecord) error
	GetJob(ctx context.Context, jobID string) (JobRecord, bool, error)
	UpdateJob(ctx context.Context, job JobRecord) error
	ListTasksByJob(ctx context.Context, jobID string) ([]TaskRecord, error)
	GetTask(ctx context.Context, jobID, taskID string) (TaskRecord, bool, error)
	UpdateTask(ctx context.Context, task TaskRecord) error
	ListExpiredLeasedTasks(ctx context.Context, now time.Time) ([]TaskRecord, error)
	UpsertWorker(ctx context.Context, worker WorkerRecord) error
	GetWorker(ctx context.Context, workerID string) (WorkerRecord, bool, error)
	UpdateWorkerHeartbeat(ctx context.Context, workerID string, queueDepth, runningTasks int, cpuUtil, memUtil float64, health string) error
	ExtendWorkerLeases(ctx context.Context, workerID string, now time.Time, leaseDuration time.Duration) error
	AppendAuditEvent(ctx context.Context, event AuditEventRecord) error
	ListAuditEvents(ctx context.Context, query AuditQuery) ([]AuditEventRecord, error)
}

type Queue interface {
	Enqueue(ctx context.Context, ref TaskRef) error
	EnqueueMany(ctx context.Context, refs []TaskRef) error
	Claim(ctx context.Context, max int, consumer string, visibilityTimeout time.Duration) ([]QueueClaim, error)
	Ack(ctx context.Context, claims []QueueClaim) error
	Nack(ctx context.Context, claims []QueueClaim, reason string) error
	RequeueExpired(ctx context.Context, now time.Time, max int) (int, error)
	ListDeadLetters(ctx context.Context, limit int) ([]TaskRef, error)
	RequeueDeadLetters(ctx context.Context, refs []TaskRef) (int, error)
}
