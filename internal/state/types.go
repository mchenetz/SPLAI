package state

import "time"

type JobRecord struct {
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

type TaskRecord struct {
	JobID         string
	TaskID        string
	Type          string
	Inputs        map[string]string
	Dependencies  []string
	Status        string
	WorkerID      string
	LeaseID       string
	LeaseExpires  time.Time
	Attempt       int
	MaxRetries    int
	TimeoutSec    int
	OutputURI     string
	Error         string
	LastReportKey string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type WorkerRecord struct {
	ID            string
	CPU           int
	Memory        string
	GPU           bool
	Models        []string
	Tools         []string
	Backends      []string
	Locality      string
	Health        string
	QueueDepth    int
	RunningTasks  int
	CPUUtil       float64
	MemoryUtil    float64
	LastHeartbeat time.Time
}

type TaskRef struct {
	JobID  string
	TaskID string
}

type QueueClaim struct {
	Ref       TaskRef
	Receipt   string
	ClaimedBy string
	ClaimedAt time.Time
	VisibleAt time.Time
}

type AuditEventRecord struct {
	ID          int64
	Action      string
	Actor       string
	Tenant      string
	RemoteAddr  string
	Resource    string
	PayloadHash string
	PrevHash    string
	EventHash   string
	Requested   int
	Result      string
	Details     string
	CreatedAt   time.Time
}

type AuditQuery struct {
	Limit  int
	Offset int
	Action string
	Actor  string
	Tenant string
	Result string
	From   time.Time
	To     time.Time
}
