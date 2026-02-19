// Code generated from proto/daef/v1/*.proto. DO NOT EDIT.

package daefv1

type Metadata struct {
	RequestID string
	Tenant    string
	TraceID   string
}

type ResourceRequest struct {
	CPU    uint32
	Memory string
	GPU    bool
}

type Constraint struct {
	DataLocality string
}

type Task struct {
	TaskID         string
	Type           string
	Inputs         map[string]string
	Resources      *ResourceRequest
	Constraints    *Constraint
	TimeoutSeconds uint32
	MaxRetries     uint32
}

type Dependency struct {
	FromTaskID string
	ToTaskID   string
}

type DAG struct {
	DAGID        string
	Tasks        []*Task
	Dependencies []*Dependency
}

type Empty struct{}

type SubmitJobRequest struct {
	Metadata *Metadata
	Type     string
	Input    string
	Policy   string
	Priority string
}

type SubmitJobResponse struct {
	JobID string
}

type GetJobStatusRequest struct {
	JobID string
}

type GetJobStatusResponse struct {
	JobID             string
	Phase             string
	Message           string
	ResultArtifactURI string
}

type CancelJobRequest struct {
	JobID  string
	Reason string
}

type CancelJobResponse struct {
	Accepted bool
}

type CompileJobRequest struct {
	JobID  string
	Mode   string
	Input  string
	Policy string
}

type CompileJobResponse struct {
	DAG *DAG
}

type EnqueueDAGRequest struct {
	JobID string
	DAG   *DAG
}

type EnqueueDAGResponse struct {
	Accepted bool
}

type PollAssignmentsRequest struct {
	WorkerID string
	MaxTasks uint32
}

type Assignment struct {
	JobID string
	Task  *Task
}

type PollAssignmentsResponse struct {
	Assignments []*Assignment
}

type ReportTaskResultRequest struct {
	WorkerID          string
	JobID             string
	TaskID            string
	Phase             string
	OutputArtifactURI string
	Error             string
	Attempts          uint32
}

type RegisterWorkerRequest struct {
	WorkerID string
	CPU      uint32
	Memory   string
	GPU      bool
	Models   []string
	Tools    []string
	Locality string
}

type RegisterWorkerResponse struct {
	Accepted                 bool
	HeartbeatIntervalSeconds uint32
}

type HeartbeatRequest struct {
	WorkerID          string
	QueueDepth        uint32
	RunningTasks      uint32
	CPUUtilization    float32
	MemoryUtilization float32
	Health            string
}

type HeartbeatResponse struct {
	Accepted        bool
	ControlMessages []string
}

type JobService interface {
	SubmitJob(*SubmitJobRequest) (*SubmitJobResponse, error)
	GetJobStatus(*GetJobStatusRequest) (*GetJobStatusResponse, error)
	CancelJob(*CancelJobRequest) (*CancelJobResponse, error)
}

type PlannerService interface {
	CompileJob(*CompileJobRequest) (*CompileJobResponse, error)
}

type SchedulerService interface {
	EnqueueDAG(*EnqueueDAGRequest) (*EnqueueDAGResponse, error)
	PollAssignments(*PollAssignmentsRequest) (*PollAssignmentsResponse, error)
	ReportTaskResult(*ReportTaskResultRequest) (*Empty, error)
}

type WorkerService interface {
	RegisterWorker(*RegisterWorkerRequest) (*RegisterWorkerResponse, error)
	Heartbeat(*HeartbeatRequest) (*HeartbeatResponse, error)
}
