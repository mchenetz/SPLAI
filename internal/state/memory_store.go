package state

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

type MemoryStore struct {
	mu      sync.Mutex
	jobs    map[string]JobRecord
	tasks   map[string]map[string]TaskRecord
	workers map[string]WorkerRecord
	audits  []AuditEventRecord
	nextID  int64
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		jobs:    make(map[string]JobRecord),
		tasks:   make(map[string]map[string]TaskRecord),
		workers: make(map[string]WorkerRecord),
		audits:  make([]AuditEventRecord, 0, 128),
		nextID:  1,
	}
}

func (m *MemoryStore) CreateJobWithTasks(_ context.Context, job JobRecord, tasks []TaskRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	m.jobs[job.ID] = job
	m.tasks[job.ID] = make(map[string]TaskRecord, len(tasks))
	for _, task := range tasks {
		t := task
		if t.CreatedAt.IsZero() {
			t.CreatedAt = now
		}
		t.UpdatedAt = now
		m.tasks[job.ID][t.TaskID] = t
	}
	return nil
}

func (m *MemoryStore) GetJob(_ context.Context, jobID string) (JobRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[jobID]
	return job, ok, nil
}

func (m *MemoryStore) UpdateJob(_ context.Context, job JobRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job.UpdatedAt = time.Now().UTC()
	m.jobs[job.ID] = job
	return nil
}

func (m *MemoryStore) CountJobsByTenantStatus(_ context.Context, tenant, status string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, j := range m.jobs {
		if tenant != "" && j.Tenant != tenant {
			continue
		}
		if status != "" && j.Status != status {
			continue
		}
		count++
	}
	return count, nil
}

func (m *MemoryStore) ListTasksByJob(_ context.Context, jobID string) ([]TaskRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	byID := m.tasks[jobID]
	out := make([]TaskRecord, 0, len(byID))
	for _, task := range byID {
		out = append(out, task)
	}
	return out, nil
}

func (m *MemoryStore) GetTask(_ context.Context, jobID, taskID string) (TaskRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	byID, ok := m.tasks[jobID]
	if !ok {
		return TaskRecord{}, false, nil
	}
	task, ok := byID[taskID]
	return task, ok, nil
}

func (m *MemoryStore) UpdateTask(_ context.Context, task TaskRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tasks[task.JobID]; !ok {
		m.tasks[task.JobID] = map[string]TaskRecord{}
	}
	task.UpdatedAt = time.Now().UTC()
	m.tasks[task.JobID][task.TaskID] = task
	return nil
}

func (m *MemoryStore) CountTasksByTenantStatus(_ context.Context, tenant, status string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for jobID, byID := range m.tasks {
		job, ok := m.jobs[jobID]
		if !ok {
			continue
		}
		if tenant != "" && job.Tenant != tenant {
			continue
		}
		for _, t := range byID {
			if status != "" && t.Status != status {
				continue
			}
			count++
		}
	}
	return count, nil
}

func (m *MemoryStore) ListExpiredLeasedTasks(_ context.Context, now time.Time) ([]TaskRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]TaskRecord, 0)
	for _, byID := range m.tasks {
		for _, task := range byID {
			if task.Status != "Running" || task.LeaseID == "" {
				continue
			}
			if task.LeaseExpires.IsZero() || !task.LeaseExpires.Before(now) {
				continue
			}
			out = append(out, task)
		}
	}
	return out, nil
}

func (m *MemoryStore) UpsertWorker(_ context.Context, worker WorkerRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if worker.LastHeartbeat.IsZero() {
		worker.LastHeartbeat = time.Now().UTC()
	}
	m.workers[worker.ID] = worker
	return nil
}

func (m *MemoryStore) GetWorker(_ context.Context, workerID string) (WorkerRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.workers[workerID]
	return w, ok, nil
}

func (m *MemoryStore) ListWorkers(_ context.Context) ([]WorkerRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]WorkerRecord, 0, len(m.workers))
	for _, w := range m.workers {
		out = append(out, w)
	}
	return out, nil
}

func (m *MemoryStore) UpdateWorkerHeartbeat(_ context.Context, workerID string, queueDepth, runningTasks int, cpuUtil, memUtil float64, health string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.workers[workerID]
	w.QueueDepth = queueDepth
	w.RunningTasks = runningTasks
	w.CPUUtil = cpuUtil
	w.MemoryUtil = memUtil
	w.Health = health
	if w.Health == "" {
		w.Health = "healthy"
	}
	w.LastHeartbeat = time.Now().UTC()
	m.workers[workerID] = w
	return nil
}

func (m *MemoryStore) ExtendWorkerLeases(_ context.Context, workerID string, now time.Time, leaseDuration time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for jobID, byID := range m.tasks {
		for taskID, task := range byID {
			if task.Status != "Running" || task.WorkerID != workerID || task.LeaseID == "" {
				continue
			}
			task.LeaseExpires = now.Add(leaseDuration)
			task.UpdatedAt = now
			byID[taskID] = task
		}
		m.tasks[jobID] = byID
	}
	return nil
}

func (m *MemoryStore) ListTasksByWorkerStatus(_ context.Context, workerID, status string) ([]TaskRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]TaskRecord, 0, 16)
	for _, byID := range m.tasks {
		for _, t := range byID {
			if workerID != "" && t.WorkerID != workerID {
				continue
			}
			if status != "" && t.Status != status {
				continue
			}
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *MemoryStore) CountTasksByWorkerStatus(_ context.Context, workerID, status string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, byID := range m.tasks {
		for _, t := range byID {
			if workerID != "" && t.WorkerID != workerID {
				continue
			}
			if status != "" && t.Status != status {
				continue
			}
			n++
		}
	}
	return n, nil
}

func (m *MemoryStore) AppendAuditEvent(_ context.Context, event AuditEventRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if len(m.audits) > 0 {
		event.PrevHash = m.audits[len(m.audits)-1].EventHash
	}
	event.EventHash = computeAuditHash(event)
	event.ID = m.nextID
	m.nextID++
	m.audits = append(m.audits, event)
	return nil
}

func (m *MemoryStore) ListAuditEvents(_ context.Context, query AuditQuery) ([]AuditEventRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	limit := query.Limit
	offset := query.Offset
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	filtered := make([]AuditEventRecord, 0, len(m.audits))
	for _, a := range m.audits {
		if query.Action != "" && a.Action != query.Action {
			continue
		}
		if query.Actor != "" && a.Actor != query.Actor {
			continue
		}
		if query.Tenant != "" && a.Tenant != query.Tenant {
			continue
		}
		if query.Result != "" && a.Result != query.Result {
			continue
		}
		if !query.From.IsZero() && a.CreatedAt.Before(query.From) {
			continue
		}
		if !query.To.IsZero() && a.CreatedAt.After(query.To) {
			continue
		}
		filtered = append(filtered, a)
	}
	if offset > len(filtered) {
		offset = len(filtered)
	}
	items := filtered[offset:]
	if limit < len(items) {
		items = items[:limit]
	}
	out := make([]AuditEventRecord, 0, len(items))
	// Newest first for operator-facing endpoint.
	for i := len(items) - 1; i >= 0; i-- {
		out = append(out, items[i])
	}
	return out, nil
}

func computeAuditHash(event AuditEventRecord) string {
	payload := map[string]any{
		"action":       event.Action,
		"actor":        event.Actor,
		"tenant":       event.Tenant,
		"remote_addr":  event.RemoteAddr,
		"resource":     event.Resource,
		"payload_hash": event.PayloadHash,
		"prev_hash":    event.PrevHash,
		"requested":    event.Requested,
		"result":       event.Result,
		"details":      event.Details,
		"created_at":   event.CreatedAt.UnixNano(),
	}
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
