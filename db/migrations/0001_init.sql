CREATE TABLE IF NOT EXISTS jobs (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  input TEXT NOT NULL,
  policy TEXT NOT NULL,
  priority TEXT NOT NULL,
  status TEXT NOT NULL,
  message TEXT NOT NULL DEFAULT '',
  result_artifact_uri TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS tasks (
  job_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  type TEXT NOT NULL,
  inputs_json TEXT NOT NULL,
  dependencies_json TEXT NOT NULL,
  status TEXT NOT NULL,
  worker_id TEXT NOT NULL DEFAULT '',
  attempt INT NOT NULL DEFAULT 0,
  max_retries INT NOT NULL DEFAULT 0,
  timeout_sec INT NOT NULL DEFAULT 0,
  output_uri TEXT NOT NULL DEFAULT '',
  error_text TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (job_id, task_id),
  FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS workers (
  id TEXT PRIMARY KEY,
  cpu INT NOT NULL,
  memory TEXT NOT NULL,
  gpu BOOLEAN NOT NULL,
  models_json TEXT NOT NULL,
  tools_json TEXT NOT NULL,
  backends_json TEXT NOT NULL DEFAULT '[]',
  locality TEXT NOT NULL DEFAULT '',
  health TEXT NOT NULL DEFAULT 'healthy',
  queue_depth INT NOT NULL DEFAULT 0,
  running_tasks INT NOT NULL DEFAULT 0,
  cpu_util DOUBLE PRECISION NOT NULL DEFAULT 0,
  memory_util DOUBLE PRECISION NOT NULL DEFAULT 0,
  last_heartbeat TIMESTAMPTZ NOT NULL
);
