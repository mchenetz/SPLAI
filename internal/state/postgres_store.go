package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/example/splai/db/migrations"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(dsn string) (*PostgresStore, error) {
	if !hasSQLDriver("pgx") {
		return nil, errors.New("pgx SQL driver is not linked; import github.com/jackc/pgx/v5/stdlib")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	store := &PostgresStore{db: db}
	if err := store.ensureSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func hasSQLDriver(name string) bool {
	for _, d := range sql.Drivers() {
		if d == name {
			return true
		}
	}
	return false
}

func (p *PostgresStore) ensureSchema(ctx context.Context) error {
	if _, err := p.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL)`); err != nil {
		return err
	}
	files, err := listMigrationFiles(migrations.Files)
	if err != nil {
		return err
	}
	for _, file := range files {
		applied, err := p.isMigrationApplied(ctx, file)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := p.applyMigration(ctx, file); err != nil {
			return err
		}
	}
	return nil
}

func (p *PostgresStore) isMigrationApplied(ctx context.Context, version string) (bool, error) {
	var exists bool
	err := p.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&exists)
	return exists, err
}

func (p *PostgresStore) applyMigration(ctx context.Context, file string) error {
	sqlBytes, err := migrations.Files.ReadFile(file)
	if err != nil {
		return err
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("apply migration %s: %w", file, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES ($1, $2)`, file, time.Now().UTC()); err != nil {
		return fmt.Errorf("record migration %s: %w", file, err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func listMigrationFiles(migFS fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(migFS, ".")
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
	return files, nil
}

func (p *PostgresStore) CreateJobWithTasks(ctx context.Context, job JobRecord, tasks []TaskRecord) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO jobs (id, tenant, type, input, policy, priority, status, message, result_artifact_uri, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		job.ID, job.Tenant, job.Type, job.Input, job.Policy, job.Priority, job.Status, job.Message, job.ResultArtifactURI, job.CreatedAt, job.UpdatedAt,
	); err != nil {
		return err
	}

	for _, t := range tasks {
		inputs, err := json.Marshal(t.Inputs)
		if err != nil {
			return err
		}
		deps, err := json.Marshal(t.Dependencies)
		if err != nil {
			return err
		}
		createdAt := t.CreatedAt
		if createdAt.IsZero() {
			createdAt = now
		}
		updatedAt := now
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO tasks (job_id, task_id, type, inputs_json, dependencies_json, status, worker_id, lease_id, lease_expires_at, attempt, max_retries, timeout_sec, output_uri, error_text, last_report_key, created_at, updated_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
			t.JobID, t.TaskID, t.Type, string(inputs), string(deps), t.Status, t.WorkerID, t.LeaseID, nullTime(t.LeaseExpires), t.Attempt, t.MaxRetries, t.TimeoutSec, t.OutputURI, t.Error, t.LastReportKey, createdAt, updatedAt,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (p *PostgresStore) GetJob(ctx context.Context, jobID string) (JobRecord, bool, error) {
	var j JobRecord
	err := p.db.QueryRowContext(ctx,
		`SELECT id, tenant, type, input, policy, priority, status, message, result_artifact_uri, created_at, updated_at
		 FROM jobs WHERE id = $1`, jobID,
	).Scan(&j.ID, &j.Tenant, &j.Type, &j.Input, &j.Policy, &j.Priority, &j.Status, &j.Message, &j.ResultArtifactURI, &j.CreatedAt, &j.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return JobRecord{}, false, nil
	}
	if err != nil {
		return JobRecord{}, false, err
	}
	return j, true, nil
}

func (p *PostgresStore) UpdateJob(ctx context.Context, job JobRecord) error {
	job.UpdatedAt = time.Now().UTC()
	res, err := p.db.ExecContext(ctx,
		`UPDATE jobs SET status=$2, message=$3, result_artifact_uri=$4, updated_at=$5 WHERE id=$1`,
		job.ID, job.Status, job.Message, job.ResultArtifactURI, job.UpdatedAt,
	)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("job %s not found", job.ID)
	}
	return nil
}

func (p *PostgresStore) CountJobsByTenantStatus(ctx context.Context, tenant, status string) (int, error) {
	var n int
	err := p.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM jobs WHERE tenant=$1 AND status=$2`, tenant, status).Scan(&n)
	return n, err
}

func (p *PostgresStore) ListTasksByJob(ctx context.Context, jobID string) ([]TaskRecord, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT job_id, task_id, type, inputs_json, dependencies_json, status, worker_id, lease_id, lease_expires_at, attempt, max_retries, timeout_sec, output_uri, error_text, last_report_key, created_at, updated_at
		 FROM tasks WHERE job_id=$1`, jobID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]TaskRecord, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (p *PostgresStore) GetTask(ctx context.Context, jobID, taskID string) (TaskRecord, bool, error) {
	row := p.db.QueryRowContext(ctx,
		`SELECT job_id, task_id, type, inputs_json, dependencies_json, status, worker_id, lease_id, lease_expires_at, attempt, max_retries, timeout_sec, output_uri, error_text, last_report_key, created_at, updated_at
		 FROM tasks WHERE job_id=$1 AND task_id=$2`, jobID, taskID,
	)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskRecord{}, false, nil
	}
	if err != nil {
		return TaskRecord{}, false, err
	}
	return t, true, nil
}

func (p *PostgresStore) UpdateTask(ctx context.Context, task TaskRecord) error {
	inputs, err := json.Marshal(task.Inputs)
	if err != nil {
		return err
	}
	deps, err := json.Marshal(task.Dependencies)
	if err != nil {
		return err
	}
	task.UpdatedAt = time.Now().UTC()
	res, err := p.db.ExecContext(ctx,
		`UPDATE tasks SET type=$3, inputs_json=$4, dependencies_json=$5, status=$6, worker_id=$7, lease_id=$8, lease_expires_at=$9, attempt=$10, max_retries=$11, timeout_sec=$12, output_uri=$13, error_text=$14, last_report_key=$15, updated_at=$16
		 WHERE job_id=$1 AND task_id=$2`,
		task.JobID, task.TaskID, task.Type, string(inputs), string(deps), task.Status, task.WorkerID, task.LeaseID, nullTime(task.LeaseExpires), task.Attempt, task.MaxRetries, task.TimeoutSec, task.OutputURI, task.Error, task.LastReportKey, task.UpdatedAt,
	)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("task %s/%s not found", task.JobID, task.TaskID)
	}
	return nil
}

func (p *PostgresStore) CountTasksByTenantStatus(ctx context.Context, tenant, status string) (int, error) {
	var n int
	err := p.db.QueryRowContext(ctx,
		`SELECT COUNT(1)
		 FROM tasks t
		 JOIN jobs j ON j.id = t.job_id
		 WHERE j.tenant=$1 AND t.status=$2`,
		tenant, status,
	).Scan(&n)
	return n, err
}

func (p *PostgresStore) ListExpiredLeasedTasks(ctx context.Context, now time.Time) ([]TaskRecord, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT job_id, task_id, type, inputs_json, dependencies_json, status, worker_id, lease_id, lease_expires_at, attempt, max_retries, timeout_sec, output_uri, error_text, last_report_key, created_at, updated_at
		 FROM tasks WHERE status IN ('Running','Assigned') AND lease_id <> '' AND lease_expires_at IS NOT NULL AND lease_expires_at < $1`, now,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TaskRecord, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (p *PostgresStore) UpsertWorker(ctx context.Context, worker WorkerRecord) error {
	models, err := json.Marshal(worker.Models)
	if err != nil {
		return err
	}
	tools, err := json.Marshal(worker.Tools)
	if err != nil {
		return err
	}
	if worker.LastHeartbeat.IsZero() {
		worker.LastHeartbeat = time.Now().UTC()
	}
	_, err = p.db.ExecContext(ctx,
		`INSERT INTO workers (id, cpu, memory, gpu, models_json, tools_json, locality, health, queue_depth, running_tasks, cpu_util, memory_util, last_heartbeat)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		 ON CONFLICT (id) DO UPDATE SET
		 cpu=EXCLUDED.cpu,
		 memory=EXCLUDED.memory,
		 gpu=EXCLUDED.gpu,
		 models_json=EXCLUDED.models_json,
		 tools_json=EXCLUDED.tools_json,
		 locality=EXCLUDED.locality,
		 health=EXCLUDED.health,
		 queue_depth=EXCLUDED.queue_depth,
		 running_tasks=EXCLUDED.running_tasks,
		 cpu_util=EXCLUDED.cpu_util,
		 memory_util=EXCLUDED.memory_util,
		 last_heartbeat=EXCLUDED.last_heartbeat`,
		worker.ID, worker.CPU, worker.Memory, worker.GPU, string(models), string(tools), worker.Locality, worker.Health, worker.QueueDepth, worker.RunningTasks, worker.CPUUtil, worker.MemoryUtil, worker.LastHeartbeat,
	)
	return err
}

func (p *PostgresStore) GetWorker(ctx context.Context, workerID string) (WorkerRecord, bool, error) {
	var w WorkerRecord
	var modelsJSON, toolsJSON string
	err := p.db.QueryRowContext(ctx,
		`SELECT id, cpu, memory, gpu, models_json, tools_json, locality, health, queue_depth, running_tasks, cpu_util, memory_util, last_heartbeat
		 FROM workers WHERE id = $1`, workerID,
	).Scan(&w.ID, &w.CPU, &w.Memory, &w.GPU, &modelsJSON, &toolsJSON, &w.Locality, &w.Health, &w.QueueDepth, &w.RunningTasks, &w.CPUUtil, &w.MemoryUtil, &w.LastHeartbeat)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkerRecord{}, false, nil
	}
	if err != nil {
		return WorkerRecord{}, false, err
	}
	if err := json.Unmarshal([]byte(modelsJSON), &w.Models); err != nil {
		return WorkerRecord{}, false, err
	}
	if err := json.Unmarshal([]byte(toolsJSON), &w.Tools); err != nil {
		return WorkerRecord{}, false, err
	}
	return w, true, nil
}

func (p *PostgresStore) ListWorkers(ctx context.Context) ([]WorkerRecord, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, cpu, memory, gpu, models_json, tools_json, locality, health, queue_depth, running_tasks, cpu_util, memory_util, last_heartbeat
		 FROM workers`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]WorkerRecord, 0, 64)
	for rows.Next() {
		var w WorkerRecord
		var modelsJSON, toolsJSON string
		if err := rows.Scan(&w.ID, &w.CPU, &w.Memory, &w.GPU, &modelsJSON, &toolsJSON, &w.Locality, &w.Health, &w.QueueDepth, &w.RunningTasks, &w.CPUUtil, &w.MemoryUtil, &w.LastHeartbeat); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(modelsJSON), &w.Models); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(toolsJSON), &w.Tools); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (p *PostgresStore) UpdateWorkerHeartbeat(ctx context.Context, workerID string, queueDepth, runningTasks int, cpuUtil, memUtil float64, health string) error {
	if health == "" {
		health = "healthy"
	}
	res, err := p.db.ExecContext(ctx,
		`UPDATE workers SET queue_depth=$2, running_tasks=$3, cpu_util=$4, memory_util=$5, health=$6, last_heartbeat=$7 WHERE id=$1`,
		workerID, queueDepth, runningTasks, cpuUtil, memUtil, health, time.Now().UTC(),
	)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("worker %s not found", workerID)
	}
	return nil
}

func (p *PostgresStore) ExtendWorkerLeases(ctx context.Context, workerID string, now time.Time, leaseDuration time.Duration) error {
	_, err := p.db.ExecContext(ctx,
		`UPDATE tasks SET lease_expires_at=$3, updated_at=$4
		 WHERE worker_id=$1 AND status='Running' AND lease_id <> '' AND (lease_expires_at IS NULL OR lease_expires_at > $2)`,
		workerID, now, now.Add(leaseDuration), now,
	)
	return err
}

func (p *PostgresStore) ListTasksByWorkerStatus(ctx context.Context, workerID, status string) ([]TaskRecord, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT job_id, task_id, type, inputs_json, dependencies_json, status, worker_id, lease_id, lease_expires_at, attempt, max_retries, timeout_sec, output_uri, error_text, last_report_key, created_at, updated_at
		 FROM tasks
		 WHERE worker_id=$1 AND status=$2`, workerID, status,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TaskRecord, 0, 32)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (p *PostgresStore) CountTasksByWorkerStatus(ctx context.Context, workerID, status string) (int, error) {
	var n int
	err := p.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM tasks WHERE worker_id=$1 AND status=$2`,
		workerID, status,
	).Scan(&n)
	return n, err
}

func (p *PostgresStore) AppendAuditEvent(ctx context.Context, event AuditEventRecord) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	prevHash := ""
	_ = p.db.QueryRowContext(ctx, `SELECT event_hash FROM audit_events ORDER BY id DESC LIMIT 1`).Scan(&prevHash)
	event.PrevHash = prevHash
	event.EventHash = computeAuditHash(event)
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO audit_events (action, actor, tenant, remote_addr, resource, payload_hash, prev_hash, event_hash, requested, result, details, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		event.Action, event.Actor, event.Tenant, event.RemoteAddr, event.Resource, event.PayloadHash, event.PrevHash, event.EventHash, event.Requested, event.Result, event.Details, event.CreatedAt,
	)
	return err
}

func (p *PostgresStore) ListAuditEvents(ctx context.Context, query AuditQuery) ([]AuditEventRecord, error) {
	limit := query.Limit
	offset := query.Offset
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	where := []string{"1=1"}
	args := make([]any, 0, 12)
	argi := 1
	add := func(clause string, v any) {
		where = append(where, fmt.Sprintf(clause, argi))
		args = append(args, v)
		argi++
	}
	if query.Action != "" {
		add("action=$%d", query.Action)
	}
	if query.Actor != "" {
		add("actor=$%d", query.Actor)
	}
	if query.Tenant != "" {
		add("tenant=$%d", query.Tenant)
	}
	if query.Result != "" {
		add("result=$%d", query.Result)
	}
	if !query.From.IsZero() {
		add("created_at >= $%d", query.From)
	}
	if !query.To.IsZero() {
		add("created_at <= $%d", query.To)
	}
	args = append(args, limit, offset)
	sqlQuery := fmt.Sprintf(
		`SELECT id, action, actor, tenant, remote_addr, resource, payload_hash, prev_hash, event_hash, requested, result, details, created_at
		 FROM audit_events
		 WHERE %s
		 ORDER BY created_at DESC, id DESC
		 LIMIT $%d OFFSET $%d`,
		strings.Join(where, " AND "), argi, argi+1,
	)
	rows, err := p.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AuditEventRecord, 0, limit)
	for rows.Next() {
		var a AuditEventRecord
		if err := rows.Scan(&a.ID, &a.Action, &a.Actor, &a.Tenant, &a.RemoteAddr, &a.Resource, &a.PayloadHash, &a.PrevHash, &a.EventHash, &a.Requested, &a.Result, &a.Details, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(s scanner) (TaskRecord, error) {
	var t TaskRecord
	var inputsJSON, depsJSON string
	var leaseExpires sql.NullTime
	if err := s.Scan(&t.JobID, &t.TaskID, &t.Type, &inputsJSON, &depsJSON, &t.Status, &t.WorkerID, &t.LeaseID, &leaseExpires, &t.Attempt, &t.MaxRetries, &t.TimeoutSec, &t.OutputURI, &t.Error, &t.LastReportKey, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return TaskRecord{}, err
	}
	if err := json.Unmarshal([]byte(inputsJSON), &t.Inputs); err != nil {
		return TaskRecord{}, err
	}
	if err := json.Unmarshal([]byte(depsJSON), &t.Dependencies); err != nil {
		return TaskRecord{}, err
	}
	if leaseExpires.Valid {
		t.LeaseExpires = leaseExpires.Time
	}
	return t, nil
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
