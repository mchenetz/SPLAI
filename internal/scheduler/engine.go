package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/example/daef/internal/observability"
	"github.com/example/daef/internal/planner"
	"github.com/example/daef/internal/state"
	"github.com/example/daef/pkg/daefapi"
)

const (
	JobQueued    = "Queued"
	JobRunning   = "Running"
	JobCompleted = "Completed"
	JobFailed    = "Failed"
	JobCanceled  = "Canceled"
)

type Options struct {
	LeaseDuration time.Duration
	QueueBackend  string
}

type Job struct {
	ID                string
	Tenant            string
	Type              string
	Input             string
	Policy            string
	Priority          string
	Status            string
	Message           string
	ResultArtifactURI string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type TaskStatus struct {
	TaskID       string
	Type         string
	Status       string
	Attempt      int
	WorkerID     string
	LeaseID      string
	LeaseExpires time.Time
	OutputURI    string
	Error        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Engine struct {
	store         state.Store
	queue         state.Queue
	leaseDuration time.Duration
	queueBackend  string
}

func NewEngine(store state.Store, queue state.Queue, opts Options) *Engine {
	leaseDuration := opts.LeaseDuration
	if leaseDuration <= 0 {
		leaseDuration = 15 * time.Second
	}
	queueBackend := opts.QueueBackend
	if queueBackend == "" {
		queueBackend = "unknown"
	}
	return &Engine{store: store, queue: queue, leaseDuration: leaseDuration, queueBackend: queueBackend}
}

func NewInMemoryEngine() *Engine {
	return NewEngine(state.NewMemoryStore(), state.NewMemoryQueue(), Options{QueueBackend: "memory"})
}

func (e *Engine) AddJob(id, tenant, reqType, input, policy, priority string, dag planner.DAG) error {
	ctx := context.Background()
	now := time.Now().UTC()
	job := state.JobRecord{
		ID:        id,
		Tenant:    tenant,
		Type:      reqType,
		Input:     input,
		Policy:    policy,
		Priority:  priority,
		Status:    JobQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if len(dag.Tasks) > 0 {
		job.Status = JobRunning
	}

	tasks := make([]state.TaskRecord, 0, len(dag.Tasks))
	readyRefs := make([]state.TaskRef, 0, len(dag.Tasks))
	for _, t := range dag.Tasks {
		rec := state.TaskRecord{
			JobID:        id,
			TaskID:       t.TaskID,
			Type:         t.Type,
			Inputs:       t.Inputs,
			Dependencies: t.Dependencies,
			Status:       JobQueued,
			MaxRetries:   t.MaxRetries,
			TimeoutSec:   t.TimeoutSec,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		tasks = append(tasks, rec)
		if len(t.Dependencies) == 0 {
			readyRefs = append(readyRefs, state.TaskRef{JobID: id, TaskID: t.TaskID})
		}
	}

	if err := e.store.CreateJobWithTasks(ctx, job, tasks); err != nil {
		return err
	}
	if len(readyRefs) > 0 {
		return e.queue.EnqueueMany(ctx, readyRefs)
	}
	return nil
}

func (e *Engine) GetJob(jobID string) (*Job, bool, error) {
	rec, ok, err := e.store.GetJob(context.Background(), jobID)
	if err != nil || !ok {
		return nil, ok, err
	}
	return &Job{
		ID:                rec.ID,
		Tenant:            rec.Tenant,
		Type:              rec.Type,
		Input:             rec.Input,
		Policy:            rec.Policy,
		Priority:          rec.Priority,
		Status:            rec.Status,
		Message:           rec.Message,
		ResultArtifactURI: rec.ResultArtifactURI,
		CreatedAt:         rec.CreatedAt,
		UpdatedAt:         rec.UpdatedAt,
	}, true, nil
}

func (e *Engine) GetJobTasks(jobID string) ([]TaskStatus, bool, error) {
	ctx := context.Background()
	_, ok, err := e.store.GetJob(ctx, jobID)
	if err != nil || !ok {
		return nil, ok, err
	}
	tasks, err := e.store.ListTasksByJob(ctx, jobID)
	if err != nil {
		return nil, true, err
	}
	out := make([]TaskStatus, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, TaskStatus{
			TaskID:       t.TaskID,
			Type:         t.Type,
			Status:       t.Status,
			Attempt:      t.Attempt,
			WorkerID:     t.WorkerID,
			LeaseID:      t.LeaseID,
			LeaseExpires: t.LeaseExpires,
			OutputURI:    t.OutputURI,
			Error:        t.Error,
			CreatedAt:    t.CreatedAt,
			UpdatedAt:    t.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return out, true, nil
}

func (e *Engine) CancelJob(jobID string) (bool, error) {
	ctx := context.Background()
	job, ok, err := e.store.GetJob(ctx, jobID)
	if err != nil || !ok {
		return false, err
	}
	if isTerminal(job.Status) {
		return true, nil
	}
	job.Status = JobCanceled
	job.Message = "canceled by user"
	job.UpdatedAt = time.Now().UTC()
	if err := e.store.UpdateJob(ctx, job); err != nil {
		return false, err
	}
	tasks, err := e.store.ListTasksByJob(ctx, jobID)
	if err != nil {
		return false, err
	}
	for _, t := range tasks {
		if t.Status == JobQueued || t.Status == JobRunning {
			t.Status = JobCanceled
			t.WorkerID = ""
			t.LeaseID = ""
			t.LeaseExpires = time.Time{}
			if err := e.store.UpdateTask(ctx, t); err != nil {
				return false, err
			}
		}
	}
	return true, nil
}

func (e *Engine) RegisterWorker(req daefapi.RegisterWorkerRequest) error {
	return e.store.UpsertWorker(context.Background(), state.WorkerRecord{
		ID:            req.WorkerID,
		CPU:           req.CPU,
		Memory:        req.Memory,
		GPU:           req.GPU,
		Models:        req.Models,
		Tools:         req.Tools,
		Locality:      req.Locality,
		Health:        "healthy",
		LastHeartbeat: time.Now().UTC(),
	})
}

func (e *Engine) Heartbeat(workerID string, req daefapi.HeartbeatRequest) error {
	ctx := context.Background()
	_, ok, err := e.store.GetWorker(ctx, workerID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("worker not registered")
	}
	if err := e.store.UpdateWorkerHeartbeat(ctx, workerID, req.QueueDepth, req.RunningTasks, req.CPUUtil, req.MemoryUtil, req.Health); err != nil {
		return err
	}
	return e.store.ExtendWorkerLeases(ctx, workerID, time.Now().UTC(), e.leaseDuration)
}

func (e *Engine) PollAssignments(workerID string, max int) ([]daefapi.Assignment, error) {
	ctx := context.Background()
	_, ok, err := e.store.GetWorker(ctx, workerID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("worker not registered")
	}
	if max <= 0 {
		max = 1
	}
	_, err = e.queue.RequeueExpired(ctx, time.Now().UTC(), max*8)
	if err != nil {
		return nil, err
	}
	if err := e.requeueExpiredLeases(ctx, time.Now().UTC()); err != nil {
		return nil, err
	}

	claims, err := e.queue.Claim(ctx, max, workerID, e.leaseDuration)
	if err != nil {
		return nil, err
	}
	if len(claims) == 0 {
		return nil, nil
	}

	readyAssignments := make([]daefapi.Assignment, 0, len(claims))
	ackClaims := make([]state.QueueClaim, 0, len(claims))
	nackClaims := make([]state.QueueClaim, 0)
	nackErrorClaims := make([]state.QueueClaim, 0)
	for _, claim := range claims {
		assignment, ready, err := e.tryAssign(ctx, workerID, claim.Ref)
		if err != nil {
			observability.Default.IncCounter("scheduler_assignment_errors_total", map[string]string{"queue_backend": e.queueBackend, "worker_id": workerID}, 1)
			nackErrorClaims = append(nackErrorClaims, claim)
			continue
		}
		if !ready {
			nackClaims = append(nackClaims, claim)
			continue
		}
		readyAssignments = append(readyAssignments, assignment)
		ackClaims = append(ackClaims, claim)
	}
	if err := e.queue.Ack(ctx, ackClaims); err != nil {
		return nil, err
	}
	if err := e.queue.Nack(ctx, nackClaims, "not_ready"); err != nil {
		return nil, err
	}
	if err := e.queue.Nack(ctx, nackErrorClaims, "error"); err != nil {
		return nil, err
	}
	return readyAssignments, nil
}

func (e *Engine) tryAssign(ctx context.Context, workerID string, ref state.TaskRef) (daefapi.Assignment, bool, error) {
	job, ok, err := e.store.GetJob(ctx, ref.JobID)
	if err != nil || !ok {
		return daefapi.Assignment{}, false, err
	}
	if job.Status != JobRunning {
		return daefapi.Assignment{}, false, nil
	}
	task, ok, err := e.store.GetTask(ctx, ref.JobID, ref.TaskID)
	if err != nil || !ok {
		return daefapi.Assignment{}, false, err
	}
	if task.Status != JobQueued {
		return daefapi.Assignment{}, false, nil
	}
	ready, err := e.dependenciesCompleted(ctx, ref.JobID, task.Dependencies)
	if err != nil {
		return daefapi.Assignment{}, false, err
	}
	if !ready {
		return daefapi.Assignment{}, false, nil
	}

	now := time.Now().UTC()
	task.Status = JobRunning
	task.WorkerID = workerID
	task.Attempt++
	task.LeaseID = newLeaseID(workerID, task.TaskID, task.Attempt, now)
	task.LeaseExpires = now.Add(e.leaseDuration)
	task.UpdatedAt = now
	if err := e.store.UpdateTask(ctx, task); err != nil {
		return daefapi.Assignment{}, false, err
	}

	job.UpdatedAt = now
	if err := e.store.UpdateJob(ctx, job); err != nil {
		return daefapi.Assignment{}, false, err
	}

	return daefapi.Assignment{
		JobID:   task.JobID,
		TaskID:  task.TaskID,
		Type:    task.Type,
		Inputs:  task.Inputs,
		Attempt: task.Attempt,
		LeaseID: task.LeaseID,
	}, true, nil
}

func (e *Engine) ReportTaskResult(req daefapi.ReportTaskResultRequest) error {
	ctx := context.Background()
	job, ok, err := e.store.GetJob(ctx, req.JobID)
	if err != nil || !ok {
		return fmt.Errorf("job %s not found", req.JobID)
	}
	task, ok, err := e.store.GetTask(ctx, req.JobID, req.TaskID)
	if err != nil || !ok {
		return fmt.Errorf("task %s not found", req.TaskID)
	}
	if job.Status == JobCanceled {
		return nil
	}

	if req.IdempotencyKey != "" && task.LastReportKey == req.IdempotencyKey {
		return nil
	}
	if req.LeaseID != "" && task.LeaseID != "" && req.LeaseID != task.LeaseID {
		return fmt.Errorf("stale lease for task %s", req.TaskID)
	}

	task.OutputURI = req.OutputArtifactURI
	task.Error = req.Error
	task.LastReportKey = req.IdempotencyKey
	task.UpdatedAt = time.Now().UTC()
	task.LeaseID = ""
	task.LeaseExpires = time.Time{}

	if req.Status == JobCompleted {
		task.Status = JobCompleted
	} else if task.Attempt <= task.MaxRetries {
		task.Status = JobQueued
		task.WorkerID = ""
		if err := e.queue.Enqueue(ctx, state.TaskRef{JobID: task.JobID, TaskID: task.TaskID}); err != nil {
			return err
		}
		job.Message = fmt.Sprintf("retrying task %s", task.TaskID)
	} else {
		task.Status = JobFailed
	}

	if err := e.store.UpdateTask(ctx, task); err != nil {
		return err
	}

	allTasks, err := e.store.ListTasksByJob(ctx, req.JobID)
	if err != nil {
		return err
	}

	if task.Status == JobCompleted {
		refs := make([]state.TaskRef, 0)
		for _, t := range allTasks {
			if t.Status != JobQueued {
				continue
			}
			ready, err := e.dependenciesCompleted(ctx, t.JobID, t.Dependencies)
			if err != nil {
				return err
			}
			if ready {
				refs = append(refs, state.TaskRef{JobID: t.JobID, TaskID: t.TaskID})
			}
		}
		if len(refs) > 0 {
			if err := e.queue.EnqueueMany(ctx, refs); err != nil {
				return err
			}
		}
	}

	completed := 0
	failed := 0
	lastOutput := ""
	for _, t := range allTasks {
		switch t.Status {
		case JobCompleted:
			completed++
			if t.OutputURI != "" {
				lastOutput = t.OutputURI
			}
		case JobFailed:
			failed++
		}
	}

	if failed > 0 {
		job.Status = JobFailed
		if job.Message == "" {
			job.Message = "one or more tasks failed"
		}
	} else if completed == len(allTasks) && len(allTasks) > 0 {
		job.Status = JobCompleted
		job.Message = "all tasks completed"
		job.ResultArtifactURI = lastOutput
	}
	job.UpdatedAt = time.Now().UTC()
	return e.store.UpdateJob(ctx, job)
}

func (e *Engine) requeueExpiredLeases(ctx context.Context, now time.Time) error {
	expired, err := e.store.ListExpiredLeasedTasks(ctx, now)
	if err != nil {
		return err
	}
	refs := make([]state.TaskRef, 0, len(expired))
	for _, t := range expired {
		t.Status = JobQueued
		t.WorkerID = ""
		t.LeaseID = ""
		t.LeaseExpires = time.Time{}
		t.UpdatedAt = now
		if err := e.store.UpdateTask(ctx, t); err != nil {
			return err
		}
		refs = append(refs, state.TaskRef{JobID: t.JobID, TaskID: t.TaskID})
	}
	if len(refs) > 0 {
		if err := e.queue.EnqueueMany(ctx, refs); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) ListDeadLetters(limit int) ([]state.TaskRef, error) {
	return e.queue.ListDeadLetters(context.Background(), limit)
}

func (e *Engine) RequeueDeadLetters(refs []state.TaskRef) (int, error) {
	return e.queue.RequeueDeadLetters(context.Background(), refs)
}

func (e *Engine) AppendAuditEvent(event state.AuditEventRecord) error {
	return e.store.AppendAuditEvent(context.Background(), event)
}

func (e *Engine) ListAuditEvents(query state.AuditQuery) ([]state.AuditEventRecord, error) {
	return e.store.ListAuditEvents(context.Background(), query)
}

func (e *Engine) dependenciesCompleted(ctx context.Context, jobID string, deps []string) (bool, error) {
	for _, dep := range deps {
		t, ok, err := e.store.GetTask(ctx, jobID, dep)
		if err != nil {
			return false, err
		}
		if !ok || t.Status != JobCompleted {
			return false, nil
		}
	}
	return true, nil
}

func newLeaseID(workerID, taskID string, attempt int, now time.Time) string {
	return fmt.Sprintf("%s:%s:%d:%d", workerID, taskID, attempt, now.UnixNano())
}

func isTerminal(status string) bool {
	switch status {
	case JobCompleted, JobFailed, JobCanceled:
		return true
	default:
		return false
	}
}
