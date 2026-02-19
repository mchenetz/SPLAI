package daefapi

import "time"

type SubmitJobRequest struct {
	Type     string `json:"type"`
	Input    string `json:"input"`
	Policy   string `json:"policy"`
	Priority string `json:"priority"`
	Tenant   string `json:"tenant,omitempty"`
}

type SubmitJobResponse struct {
	JobID string `json:"job_id"`
}

type JobStatusResponse struct {
	JobID             string `json:"job_id"`
	Status            string `json:"status"`
	Message           string `json:"message,omitempty"`
	ResultArtifactURI string `json:"result_artifact_uri,omitempty"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

type JobTaskStatus struct {
	TaskID       string `json:"task_id"`
	Type         string `json:"type"`
	Status       string `json:"status"`
	Attempt      int    `json:"attempt"`
	WorkerID     string `json:"worker_id,omitempty"`
	LeaseID      string `json:"lease_id,omitempty"`
	LeaseExpires string `json:"lease_expires,omitempty"`
	OutputURI    string `json:"output_artifact_uri,omitempty"`
	Error        string `json:"error,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type JobTasksResponse struct {
	JobID    string          `json:"job_id"`
	Total    int             `json:"total"`
	Returned int             `json:"returned"`
	Limit    int             `json:"limit,omitempty"`
	Offset   int             `json:"offset,omitempty"`
	Tasks    []JobTaskStatus `json:"tasks"`
}

type CancelJobResponse struct {
	Accepted bool `json:"accepted"`
}

type RegisterWorkerRequest struct {
	WorkerID string   `json:"worker_id"`
	CPU      int      `json:"cpu"`
	Memory   string   `json:"memory"`
	GPU      bool     `json:"gpu"`
	Models   []string `json:"models"`
	Tools    []string `json:"tools"`
	Locality string   `json:"locality,omitempty"`
}

type RegisterWorkerResponse struct {
	Accepted                 bool `json:"accepted"`
	HeartbeatIntervalSeconds int  `json:"heartbeat_interval_seconds"`
}

type HeartbeatRequest struct {
	QueueDepth    int     `json:"queue_depth"`
	RunningTasks  int     `json:"running_tasks"`
	CPUUtil       float64 `json:"cpu_utilization"`
	MemoryUtil    float64 `json:"memory_utilization"`
	Health        string  `json:"health"`
	TimestampUnix int64   `json:"timestamp_unix"`
}

type HeartbeatResponse struct {
	Accepted bool `json:"accepted"`
}

type Assignment struct {
	JobID   string            `json:"job_id"`
	TaskID  string            `json:"task_id"`
	Type    string            `json:"type"`
	Inputs  map[string]string `json:"inputs"`
	Attempt int               `json:"attempt"`
	LeaseID string            `json:"lease_id"`
}

type PollAssignmentsResponse struct {
	Assignments []Assignment `json:"assignments"`
}

type ReportTaskResultRequest struct {
	WorkerID          string `json:"worker_id"`
	JobID             string `json:"job_id"`
	TaskID            string `json:"task_id"`
	LeaseID           string `json:"lease_id,omitempty"`
	IdempotencyKey    string `json:"idempotency_key,omitempty"`
	Status            string `json:"status"`
	OutputArtifactURI string `json:"output_artifact_uri,omitempty"`
	Error             string `json:"error,omitempty"`
	DurationMillis    int64  `json:"duration_millis"`
}

type ReportTaskResultResponse struct {
	Accepted bool `json:"accepted"`
}

type DeadLetterTask struct {
	JobID  string `json:"job_id"`
	TaskID string `json:"task_id"`
}

type ListDeadLettersResponse struct {
	Tasks []DeadLetterTask `json:"tasks"`
}

type RequeueDeadLettersRequest struct {
	Tasks  []DeadLetterTask `json:"tasks"`
	DryRun bool             `json:"dry_run,omitempty"`
}

type RequeueDeadLettersResponse struct {
	DryRun    bool `json:"dry_run,omitempty"`
	Requested int  `json:"requested,omitempty"`
	Requeued  int  `json:"requeued"`
}

type AuditEvent struct {
	ID          int64  `json:"id"`
	Action      string `json:"action"`
	Actor       string `json:"actor"`
	Tenant      string `json:"tenant,omitempty"`
	RemoteAddr  string `json:"remote_addr,omitempty"`
	Resource    string `json:"resource,omitempty"`
	PayloadHash string `json:"payload_hash,omitempty"`
	PrevHash    string `json:"prev_hash,omitempty"`
	EventHash   string `json:"event_hash,omitempty"`
	Requested   int    `json:"requested"`
	Result      string `json:"result,omitempty"`
	Details     string `json:"details,omitempty"`
	CreatedAt   string `json:"created_at"`
}

type ListAuditEventsResponse struct {
	Returned int          `json:"returned"`
	Limit    int          `json:"limit"`
	Offset   int          `json:"offset"`
	Events   []AuditEvent `json:"events"`
}

func RFC3339Now() string {
	return time.Now().UTC().Format(time.RFC3339)
}
