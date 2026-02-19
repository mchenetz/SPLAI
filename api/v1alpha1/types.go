package v1alpha1

import "time"

type ObjectMeta struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

type SPLAIJob struct {
	APIVersion string        `json:"apiVersion"`
	Kind       string        `json:"kind"`
	Metadata   ObjectMeta    `json:"metadata"`
	Spec       SPLAIJobSpec   `json:"spec"`
	Status     SPLAIJobStatus `json:"status,omitempty"`
}

type SPLAIJobSpec struct {
	Tenant         string           `json:"tenant"`
	Priority       string           `json:"priority"`
	Policy         string           `json:"policy"`
	TimeoutSeconds int              `json:"timeoutSeconds,omitempty"`
	Tasks          []SPLAITask       `json:"tasks,omitempty"`
	Dependencies   []SPLAIDependency `json:"dependencies,omitempty"`
}

type SPLAITask struct {
	TaskID       string            `json:"taskId"`
	Type         string            `json:"type"`
	Inputs       map[string]string `json:"inputs,omitempty"`
	CPU          int               `json:"cpu,omitempty"`
	Memory       string            `json:"memory,omitempty"`
	GPU          bool              `json:"gpu,omitempty"`
	DataLocality string            `json:"dataLocality,omitempty"`
}

type SPLAIDependency struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type SPLAIJobStatus struct {
	Phase             string `json:"phase,omitempty"`
	TraceID           string `json:"traceId,omitempty"`
	TotalTasks        int    `json:"totalTasks,omitempty"`
	CompletedTasks    int    `json:"completedTasks,omitempty"`
	FailedTasks       int    `json:"failedTasks,omitempty"`
	ResultArtifactURI string `json:"resultArtifactURI,omitempty"`
	Message           string `json:"message,omitempty"`
}

type SPLAIWorker struct {
	APIVersion string           `json:"apiVersion"`
	Kind       string           `json:"kind"`
	Metadata   ObjectMeta       `json:"metadata"`
	Spec       SPLAIWorkerSpec   `json:"spec"`
	Status     SPLAIWorkerStatus `json:"status,omitempty"`
}

type SPLAIWorkerSpec struct {
	WorkerID string   `json:"workerId"`
	CPU      int      `json:"cpu"`
	Memory   string   `json:"memory"`
	GPU      bool     `json:"gpu"`
	Models   []string `json:"models,omitempty"`
	Tools    []string `json:"tools,omitempty"`
	Locality string   `json:"locality,omitempty"`
}

type SPLAIWorkerStatus struct {
	Health        string    `json:"health,omitempty"`
	CPUPercent    float64   `json:"cpuPercent,omitempty"`
	MemoryPercent float64   `json:"memoryPercent,omitempty"`
	QueueDepth    int       `json:"queueDepth,omitempty"`
	RunningTasks  int       `json:"runningTasks,omitempty"`
	LastHeartbeat time.Time `json:"lastHeartbeat,omitempty"`
}

type SPLAIPolicy struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   ObjectMeta     `json:"metadata"`
	Spec       SPLAIPolicySpec `json:"spec"`
}

type SPLAIPolicySpec struct {
	Deny               []SPLAIDenyRule `json:"deny,omitempty"`
	AllowModels        []string       `json:"allowModels,omitempty"`
	MaxConcurrentTasks int            `json:"maxConcurrentTasks,omitempty"`
}

type SPLAIDenyRule struct {
	Model string `json:"model"`
	When  string `json:"when"`
}

type SPLAIModelRoute struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Metadata   ObjectMeta         `json:"metadata"`
	Spec       SPLAIModelRouteSpec `json:"spec"`
}

type SPLAIModelRouteSpec struct {
	Rules []SPLAIRouteRule `json:"rules"`
}

type SPLAIRouteRule struct {
	Name     string `json:"name"`
	If       string `json:"if,omitempty"`
	Backend  string `json:"backend"`
	Model    string `json:"model,omitempty"`
	Priority int    `json:"priority,omitempty"`
}
