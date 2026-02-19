ALTER TABLE jobs ADD COLUMN IF NOT EXISTS tenant TEXT NOT NULL DEFAULT 'default';
CREATE INDEX IF NOT EXISTS idx_jobs_tenant_status_created_at ON jobs (tenant, status, created_at DESC);

ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS tenant TEXT NOT NULL DEFAULT 'default';
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS prev_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS event_hash TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_audit_events_tenant_created_at ON audit_events (tenant, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_actor_created_at ON audit_events (actor, created_at DESC);
