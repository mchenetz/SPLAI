package scheduler

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/example/splai/internal/observability"
	"github.com/example/splai/internal/planner"
	"github.com/example/splai/internal/policy"
	"github.com/example/splai/internal/state"
	"github.com/example/splai/pkg/splaiapi"
	"go.opentelemetry.io/otel/attribute"
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
	PolicyEngine  *policy.Engine
	ScoreWeights  ScoreWeights
	Preempt       bool
}

type ScoreWeights struct {
	CapabilityMatch   float64
	ModelWarmCache    float64
	LocalityScore     float64
	QueuePenalty      float64
	LatencyPrediction float64
	FairnessPenalty   float64
	WaitAgeBonus      float64
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
	policy        *policy.Engine
	weights       ScoreWeights
	preempt       bool
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
	p := opts.PolicyEngine
	if p == nil {
		p = policy.NewAllowAll()
	}
	w := opts.ScoreWeights
	if w.CapabilityMatch == 0 {
		w.CapabilityMatch = 1.0
	}
	if w.ModelWarmCache == 0 {
		w.ModelWarmCache = 1.5
	}
	if w.LocalityScore == 0 {
		w.LocalityScore = 1.0
	}
	if w.QueuePenalty == 0 {
		w.QueuePenalty = 0.15
	}
	if w.LatencyPrediction == 0 {
		w.LatencyPrediction = 1.0
	}
	if w.FairnessPenalty == 0 {
		w.FairnessPenalty = 0.05
	}
	if w.WaitAgeBonus == 0 {
		w.WaitAgeBonus = 0.05
	}
	preempt := opts.Preempt
	if !opts.Preempt {
		preempt = true
	}
	return &Engine{store: store, queue: queue, leaseDuration: leaseDuration, queueBackend: queueBackend, policy: p, weights: w, preempt: preempt}
}

func NewInMemoryEngine() *Engine {
	return NewEngine(state.NewMemoryStore(), state.NewMemoryQueue(), Options{QueueBackend: "memory"})
}

func (e *Engine) AddJob(id, tenant, reqType, input, policyName, priority, dataClassification, model, networkIsolation string, dag planner.DAG) error {
	ctx := context.Background()
	ctx, span := observability.StartSpan(ctx, "scheduler.add_job",
		attribute.String("job.id", id),
		attribute.String("tenant", tenant),
		attribute.String("job.type", reqType),
	)
	defer span.End()
	now := time.Now().UTC()
	if !e.policy.IsNoop() {
		runningJobs, err := e.store.CountJobsByTenantStatus(ctx, tenant, JobRunning)
		if err != nil {
			return err
		}
		decision := e.policy.EvaluateSubmit(policy.SubmitInput{
			Tenant:             tenant,
			JobType:            reqType,
			Priority:           priority,
			Model:              model,
			DataClassification: dataClassification,
			RunningJobs:        runningJobs,
		})
		e.appendPolicyAudit(tenant, "policy_check_submit", decision, fmt.Sprintf("job_id=%s job_type=%s", id, reqType))
		if !decision.Allowed {
			return fmt.Errorf("policy denied submit: %s", decision.ReasonCode)
		}
	}
	job := state.JobRecord{
		ID:        id,
		Tenant:    tenant,
		Type:      reqType,
		Input:     input,
		Policy:    policyName,
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
		if rec.Inputs == nil {
			rec.Inputs = map[string]string{}
		}
		rec.Inputs["_priority"] = priority
		if dataClassification != "" {
			rec.Inputs["_data_classification"] = dataClassification
		}
		if model != "" {
			rec.Inputs["model"] = model
		}
		if networkIsolation != "" {
			rec.Inputs["_network_isolation"] = networkIsolation
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

func (e *Engine) ListWorkers() ([]state.WorkerRecord, error) {
	return e.store.ListWorkers(context.Background())
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

func (e *Engine) RegisterWorker(req splaiapi.RegisterWorkerRequest) error {
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

func (e *Engine) Heartbeat(workerID string, req splaiapi.HeartbeatRequest) error {
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

func (e *Engine) PollAssignments(workerID string, max int) ([]splaiapi.Assignment, error) {
	ctx := context.Background()
	ctx, span := observability.StartSpan(ctx, "scheduler.poll_assignments",
		attribute.String("worker.id", workerID),
		attribute.Int("max", max),
	)
	defer span.End()
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

	searchWindow := max * 32
	if searchWindow < max {
		searchWindow = max
	}
	claims, err := e.queue.Claim(ctx, searchWindow, workerID, e.leaseDuration)
	if err != nil {
		return nil, err
	}
	if len(claims) == 0 {
		return nil, nil
	}

	readyAssignments := make([]splaiapi.Assignment, 0, len(claims))
	ackClaims := make([]state.QueueClaim, 0, len(claims))
	nackClaims := make([]state.QueueClaim, 0)
	nackErrorClaims := make([]state.QueueClaim, 0)
	for _, claim := range claims {
		if len(readyAssignments) >= max {
			nackClaims = append(nackClaims, claim)
			continue
		}
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
	span.SetAttributes(attribute.Int("assignments.count", len(readyAssignments)))
	return readyAssignments, nil
}

func (e *Engine) tryAssign(ctx context.Context, workerID string, ref state.TaskRef) (splaiapi.Assignment, bool, error) {
	job, ok, err := e.store.GetJob(ctx, ref.JobID)
	if err != nil || !ok {
		return splaiapi.Assignment{}, false, err
	}
	if job.Status != JobRunning {
		return splaiapi.Assignment{}, false, nil
	}
	task, ok, err := e.store.GetTask(ctx, ref.JobID, ref.TaskID)
	if err != nil || !ok {
		return splaiapi.Assignment{}, false, err
	}
	if task.Status != JobQueued {
		return splaiapi.Assignment{}, false, nil
	}
	ready, err := e.dependenciesCompleted(ctx, ref.JobID, task.Dependencies)
	if err != nil {
		return splaiapi.Assignment{}, false, err
	}
	if !ready {
		return splaiapi.Assignment{}, false, nil
	}
	bestWorkerID, _, err := e.bestWorkerForTask(ctx, job, task)
	if err != nil {
		return splaiapi.Assignment{}, false, err
	}
	if bestWorkerID != "" && bestWorkerID != workerID {
		return splaiapi.Assignment{}, false, nil
	}
	worker, ok, err := e.store.GetWorker(ctx, workerID)
	if err != nil || !ok {
		return splaiapi.Assignment{}, false, err
	}
	if e.preempt && e.workerAtCapacity(worker) {
		preempted, err := e.tryPreemptLowerPriority(ctx, worker, task)
		if err != nil {
			return splaiapi.Assignment{}, false, err
		}
		if !preempted {
			return splaiapi.Assignment{}, false, nil
		}
	}
	if !e.policy.IsNoop() {
		runningTasks, err := e.store.CountTasksByTenantStatus(ctx, job.Tenant, JobRunning)
		if err != nil {
			return splaiapi.Assignment{}, false, err
		}
		decision := e.policy.EvaluateAssignment(policy.AssignmentInput{
			Tenant:             job.Tenant,
			TaskType:           task.Type,
			Model:              task.Inputs["model"],
			DataClassification: task.Inputs["_data_classification"],
			NetworkIsolation:   task.Inputs["_network_isolation"],
			WorkerLocality:     worker.Locality,
			WorkerGPU:          worker.GPU,
			RunningTasks:       runningTasks,
		})
		e.appendPolicyAudit(job.Tenant, "policy_check_assignment", decision, fmt.Sprintf("job_id=%s task_id=%s worker_id=%s", task.JobID, task.TaskID, workerID))
		if !decision.Allowed {
			task.Status = JobFailed
			task.Error = "policy denied assignment: " + decision.ReasonCode
			task.UpdatedAt = time.Now().UTC()
			if err := e.store.UpdateTask(ctx, task); err != nil {
				return splaiapi.Assignment{}, false, err
			}
			job.Status = JobFailed
			job.Message = "policy denied assignment: " + decision.ReasonCode
			job.UpdatedAt = time.Now().UTC()
			if err := e.store.UpdateJob(ctx, job); err != nil {
				return splaiapi.Assignment{}, false, err
			}
			return splaiapi.Assignment{}, false, nil
		}
	}

	now := time.Now().UTC()
	assignmentInputs := make(map[string]string, len(task.Inputs)+len(task.Dependencies))
	for k, v := range task.Inputs {
		assignmentInputs[k] = v
	}
	for _, dep := range task.Dependencies {
		depTask, ok, err := e.store.GetTask(ctx, task.JobID, dep)
		if err != nil {
			return splaiapi.Assignment{}, false, err
		}
		if !ok {
			return splaiapi.Assignment{}, false, fmt.Errorf("dependency %s not found", dep)
		}
		assignmentInputs["dep:"+dep+":output_uri"] = depTask.OutputURI
	}
	task.Status = JobRunning
	task.WorkerID = workerID
	task.Attempt++
	task.LeaseID = newLeaseID(workerID, task.TaskID, task.Attempt, now)
	task.LeaseExpires = now.Add(e.leaseDuration)
	task.UpdatedAt = now
	if err := e.store.UpdateTask(ctx, task); err != nil {
		return splaiapi.Assignment{}, false, err
	}

	job.UpdatedAt = now
	if err := e.store.UpdateJob(ctx, job); err != nil {
		return splaiapi.Assignment{}, false, err
	}

	return splaiapi.Assignment{
		JobID:   task.JobID,
		TaskID:  task.TaskID,
		Type:    task.Type,
		Inputs:  assignmentInputs,
		Attempt: task.Attempt,
		LeaseID: task.LeaseID,
	}, true, nil
}

func (e *Engine) bestWorkerForTask(ctx context.Context, job state.JobRecord, task state.TaskRecord) (string, float64, error) {
	workers, err := e.store.ListWorkers(ctx)
	if err != nil {
		return "", 0, err
	}
	targetWorker := strings.TrimSpace(task.Inputs["_target_worker"])
	tenantRunning, _ := e.store.CountTasksByTenantStatus(ctx, job.Tenant, JobRunning)
	bestID := ""
	bestScore := math.Inf(-1)
	bestTie := uint32(0)
	taskKey := task.JobID + "|" + task.TaskID
	for _, w := range workers {
		if targetWorker != "" && w.ID != targetWorker {
			continue
		}
		if strings.EqualFold(w.Health, "unhealthy") {
			continue
		}
		score := e.computeWorkerScore(task, w, tenantRunning, w.RunningTasks)
		if score > bestScore {
			bestScore = score
			bestID = w.ID
			bestTie = tieHash(taskKey, w.ID)
			continue
		}
		if math.Abs(score-bestScore) < 0.0001 && tieHash(taskKey, w.ID) > bestTie {
			bestScore = score
			bestID = w.ID
			bestTie = tieHash(taskKey, w.ID)
		}
	}
	return bestID, bestScore, nil
}

func (e *Engine) computeWorkerScore(task state.TaskRecord, w state.WorkerRecord, tenantRunning, liveRunning int) float64 {
	capabilityMatch := 0.0
	if task.Type == "llm_inference" {
		capabilityMatch += e.weights.CapabilityMatch
	}
	if task.Type == "tool_execution" && contains(w.Tools, "bash") {
		capabilityMatch += e.weights.CapabilityMatch
	}
	if task.Type == "embedding" {
		capabilityMatch += e.weights.CapabilityMatch * 0.8
	}
	modelWarmCache := 0.0
	if model := strings.TrimSpace(task.Inputs["model"]); model != "" && contains(w.Models, model) {
		modelWarmCache = e.weights.ModelWarmCache
	}
	localityScore := 0.0
	if want := strings.TrimSpace(task.Inputs["_data_locality"]); want != "" && want == strings.TrimSpace(w.Locality) {
		localityScore = e.weights.LocalityScore
	}
	queuePenalty := float64(w.QueueDepth+liveRunning) * e.weights.QueuePenalty
	latencyPrediction := ((w.CPUUtil + w.MemoryUtil) / 100.0) * e.weights.LatencyPrediction
	fairnessPenalty := float64(tenantRunning) * e.weights.FairnessPenalty
	waitAgeBonus := 0.0
	if !task.CreatedAt.IsZero() {
		waitAgeBonus = time.Since(task.CreatedAt).Seconds() / 60.0 * e.weights.WaitAgeBonus
	}
	return capabilityMatch + modelWarmCache + localityScore - queuePenalty - latencyPrediction - fairnessPenalty + waitAgeBonus
}

func (e *Engine) workerAtCapacity(w state.WorkerRecord) bool {
	cap := workerCapacity(w)
	if cap <= 0 {
		return false
	}
	return w.RunningTasks >= cap
}

func workerCapacity(w state.WorkerRecord) int {
	if w.CPU <= 0 {
		return 1
	}
	c := w.CPU / 4
	if c < 1 {
		c = 1
	}
	if w.GPU && c < 2 {
		c = 2
	}
	if c > 16 {
		c = 16
	}
	return c
}

func (e *Engine) tryPreemptLowerPriority(ctx context.Context, worker state.WorkerRecord, incoming state.TaskRecord) (bool, error) {
	incomingRank := priorityRank(incoming.Inputs["_priority"])
	if incomingRank <= 0 {
		return false, nil
	}
	running, err := e.store.ListTasksByWorkerStatus(ctx, worker.ID, JobRunning)
	if err != nil {
		return false, err
	}
	if len(running) == 0 {
		return false, nil
	}
	sort.Slice(running, func(i, j int) bool {
		return running[i].UpdatedAt.Before(running[j].UpdatedAt)
	})
	for _, t := range running {
		if priorityRank(t.Inputs["_priority"]) >= incomingRank {
			continue
		}
		t.Status = JobQueued
		t.WorkerID = ""
		t.LeaseID = ""
		t.LeaseExpires = time.Time{}
		t.Error = "preempted for higher-priority task"
		t.UpdatedAt = time.Now().UTC()
		if err := e.store.UpdateTask(ctx, t); err != nil {
			return false, err
		}
		if err := e.queue.Enqueue(ctx, state.TaskRef{JobID: t.JobID, TaskID: t.TaskID}); err != nil {
			return false, err
		}
		_ = e.store.AppendAuditEvent(ctx, state.AuditEventRecord{
			Action:    "task_preempted",
			Actor:     "system/scheduler",
			Resource:  "tasks",
			Requested: 1,
			Result:    "ok",
			Details:   fmt.Sprintf("preempted_job=%s preempted_task=%s worker=%s", t.JobID, t.TaskID, worker.ID),
			CreatedAt: time.Now().UTC(),
		})
		return true, nil
	}
	return false, nil
}

func priorityRank(priority string) int {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "interactive", "high":
		return 3
	case "standard", "medium":
		return 2
	case "batch", "low":
		return 1
	default:
		return 2
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(strings.TrimSpace(x), strings.TrimSpace(v)) {
			return true
		}
	}
	return false
}

func tieHash(taskKey, workerID string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(taskKey))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(workerID))
	return h.Sum32()
}

func (e *Engine) ReportTaskResult(req splaiapi.ReportTaskResultRequest) error {
	ctx := context.Background()
	ctx, span := observability.StartSpan(ctx, "scheduler.report_task_result",
		attribute.String("job.id", req.JobID),
		attribute.String("task.id", req.TaskID),
		attribute.String("worker.id", req.WorkerID),
		attribute.String("status", req.Status),
	)
	defer span.End()
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
	if task.Status == JobCompleted && strings.EqualFold(task.Type, "model_download") && strings.TrimSpace(req.WorkerID) != "" {
		model := strings.TrimSpace(task.Inputs["model"])
		if model != "" {
			worker, ok, err := e.store.GetWorker(ctx, req.WorkerID)
			if err != nil {
				return err
			}
			if ok && !contains(worker.Models, model) {
				worker.Models = append(worker.Models, model)
				if err := e.store.UpsertWorker(ctx, worker); err != nil {
					return err
				}
			}
		}
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

func (e *Engine) appendPolicyAudit(tenant, action string, d policy.Decision, details string) {
	result := "allow"
	if !d.Allowed {
		result = "deny"
	}
	msg := fmt.Sprintf("reason=%s rule=%s details=%s", d.ReasonCode, d.Rule, details)
	if d.Message != "" {
		msg = d.Message + " (" + msg + ")"
	}
	_ = e.store.AppendAuditEvent(context.Background(), state.AuditEventRecord{
		Action:    action,
		Actor:     "system/policy",
		Tenant:    tenant,
		Resource:  "policy",
		Result:    result,
		Details:   msg,
		CreatedAt: time.Now().UTC(),
	})
}
