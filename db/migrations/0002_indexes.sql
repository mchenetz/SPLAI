CREATE INDEX IF NOT EXISTS idx_tasks_job_status ON tasks (job_id, status);
CREATE INDEX IF NOT EXISTS idx_workers_last_heartbeat ON workers (last_heartbeat);
