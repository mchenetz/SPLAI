package v1alpha1

import "time"

type ObjectMeta struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

type DAEFJob struct {
	APIVersion string        `json:"apiVersion"`
	Kind       string        `json:"kind"`
	Metadata   ObjectMeta    `json:"metadata"`
	Spec       DAEFJobSpec   `json:"spec"`
	Status     DAEFJobStatus `json:"status,omitempty"`
}

type DAEFJobSpec struct {
	Tenant         string           `json:"tenant"`
	Priority       string           `json:"priority"`
	Policy         string           `json:"policy"`
	TimeoutSeconds int              `json:"timeoutSeconds,omitempty"`
	Tasks          []DAEFTask       `json:"tasks,omitempty"`
	Dependencies   []DAEFDependency `json:"dependencies,omitempty"`
}

type DAEFTask struct {
	TaskID       string            `json:"taskId"`
	Type         string            `json:"type"`
	Inputs       map[string]string `json:"inputs,omitempty"`
	CPU          int               `json:"cpu,omitempty"`
	Memory       string            `json:"memory,omitempty"`
	GPU          bool              `json:"gpu,omitempty"`
	DataLocality string            `json:"dataLocality,omitempty"`
}

type DAEFDependency struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type DAEFJobStatus struct {
	Phase             string `json:"phase,omitempty"`
	TraceID           string `json:"traceId,omitempty"`
	TotalTasks        int    `json:"totalTasks,omitempty"`
	CompletedTasks    int    `json:"completedTasks,omitempty"`
	FailedTasks       int    `json:"failedTasks,omitempty"`
	ResultArtifactURI string `json:"resultArtifactURI,omitempty"`
	Message           string `json:"message,omitempty"`
}

type DAEFWorker struct {
	APIVersion string           `json:"apiVersion"`
	Kind       string           `json:"kind"`
	Metadata   ObjectMeta       `json:"metadata"`
	Spec       DAEFWorkerSpec   `json:"spec"`
	Status     DAEFWorkerStatus `json:"status,omitempty"`
}

type DAEFWorkerSpec struct {
	WorkerID string   `json:"workerId"`
	CPU      int      `json:"cpu"`
	Memory   string   `json:"memory"`
	GPU      bool     `json:"gpu"`
	Models   []string `json:"models,omitempty"`
	Tools    []string `json:"tools,omitempty"`
	Locality string   `json:"locality,omitempty"`
}

type DAEFWorkerStatus struct {
	Health        string    `json:"health,omitempty"`
	CPUPercent    float64   `json:"cpuPercent,omitempty"`
	MemoryPercent float64   `json:"memoryPercent,omitempty"`
	QueueDepth    int       `json:"queueDepth,omitempty"`
	RunningTasks  int       `json:"runningTasks,omitempty"`
	LastHeartbeat time.Time `json:"lastHeartbeat,omitempty"`
}

type DAEFPolicy struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   ObjectMeta     `json:"metadata"`
	Spec       DAEFPolicySpec `json:"spec"`
}

type DAEFPolicySpec struct {
	Deny               []DAEFDenyRule `json:"deny,omitempty"`
	AllowModels        []string       `json:"allowModels,omitempty"`
	MaxConcurrentTasks int            `json:"maxConcurrentTasks,omitempty"`
}

type DAEFDenyRule struct {
	Model string `json:"model"`
	When  string `json:"when"`
}

type DAEFModelRoute struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Metadata   ObjectMeta         `json:"metadata"`
	Spec       DAEFModelRouteSpec `json:"spec"`
}

type DAEFModelRouteSpec struct {
	Rules []DAEFRouteRule `json:"rules"`
}

type DAEFRouteRule struct {
	Name     string `json:"name"`
	If       string `json:"if,omitempty"`
	Backend  string `json:"backend"`
	Model    string `json:"model,omitempty"`
	Priority int    `json:"priority,omitempty"`
}
