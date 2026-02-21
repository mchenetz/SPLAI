package api

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/example/splai/internal/models"
	"github.com/example/splai/internal/observability"
	"github.com/example/splai/internal/planner"
	"github.com/example/splai/internal/scheduler"
	"github.com/example/splai/internal/state"
	"github.com/example/splai/pkg/splaiapi"
	"go.opentelemetry.io/otel/attribute"
)

type Server struct {
	planner *planner.Compiler
	engine  *scheduler.Engine
	auth    *authorizer
	safety  *adminSafety
	limiter *submitLimiter
	router  *models.Router
	compat  bool
	timeout time.Duration
	seq     uint64
}

func NewServer(p *planner.Compiler, e *scheduler.Engine) *Server {
	router, err := models.LoadFromEnv()
	if err != nil {
		log.Printf("model router disabled due to config error: %v", err)
		router = models.NewDefaultRouter()
	}
	timeout := 60 * time.Second
	if raw := strings.TrimSpace(os.Getenv("SPLAI_OPENAI_COMPAT_TIMEOUT_SECONDS")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			timeout = time.Duration(v) * time.Second
		}
	}
	return &Server{
		planner: p,
		engine:  e,
		auth:    newAuthorizerFromEnv(),
		safety:  newAdminSafetyFromEnv(),
		limiter: newSubmitLimiterFromEnv(),
		router:  router,
		compat:  parseEnvBool("SPLAI_OPENAI_COMPAT", false),
		timeout: timeout,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/v1/metrics", s.handleMetrics)
	mux.HandleFunc("/v1/metrics/prometheus", s.handleMetricsPrometheus)
	mux.HandleFunc("/v1/jobs", s.handleJobs)
	mux.HandleFunc("/v1/jobs/", s.handleJobByID)
	mux.HandleFunc("/v1/workers/register", s.handleRegisterWorker)
	mux.HandleFunc("/v1/workers/", s.handleWorkerSubresource)
	mux.HandleFunc("/v1/tasks/report", s.handleTaskReport)
	mux.HandleFunc("/v1/admin/queue/dead-letter", s.handleDeadLetterQueue)
	mux.HandleFunc("/v1/admin/audit", s.handleAuditEvents)
	mux.HandleFunc("/v1/admin/models/prefetch", s.handleModelPrefetch)
	if s.compat {
		mux.HandleFunc("/v1/chat/completions", s.handleOpenAIChatCompletions)
		mux.HandleFunc("/v1/responses", s.handleOpenAIResponses)
	}
	return withTracing(withLogging(mux))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.requireScopes(w, r, "metrics", "operator"); !ok {
		return
	}
	writeJSON(w, http.StatusOK, observability.Default.Snapshot())
}

func (s *Server) handleMetricsPrometheus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.requireScopes(w, r, "metrics", "operator"); !ok {
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(observability.Default.RenderPrometheus()))
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req splaiapi.SubmitJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Priority == "" {
		req.Priority = "interactive"
	}
	if req.LatencyClass == "" {
		req.LatencyClass = req.Priority
	}
	if req.Policy == "" {
		req.Policy = "enterprise-default"
	}
	if req.PlannerMode == "" {
		req.PlannerMode = "template"
	}
	route := s.router.Route(models.RouteInput{
		LatencyClass:       req.LatencyClass,
		ReasoningRequired:  req.ReasoningRequired,
		DataClassification: req.DataClassification,
		RequestedModel:     req.Model,
	})
	if req.Model == "" {
		req.Model = route.Model
	}
	tenant := tenantFromRequest(r, req.Tenant)
	if _, ok := s.requireTenantAction(w, r, tenant, "submit"); !ok {
		return
	}
	if !s.limiter.allow(tenant, time.Now().UTC()) {
		writeError(w, http.StatusTooManyRequests, "submit rate limit exceeded")
		return
	}

	jobID := fmt.Sprintf("job-%d", atomic.AddUint64(&s.seq, 1))
	dag := s.planner.CompileWithMode(jobID, req.Type, req.Input, req.PlannerMode)
	dag = injectModelBackend(dag, route.Backend)
	dag = injectModelInstallTasks(dag, req.Model, req.InstallModelIfMissing)
	if err := s.engine.AddJob(
		jobID,
		tenant,
		req.Type,
		req.Input,
		req.Policy,
		req.Priority,
		req.DataClassification,
		req.Model,
		req.NetworkIsolation,
		dag,
	); err != nil {
		if strings.Contains(err.Error(), "policy denied submit") {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.engine.AppendAuditEvent(state.AuditEventRecord{
		Action:    "model_route_decision",
		Actor:     "system/router",
		Tenant:    tenant,
		Resource:  "models",
		Result:    "ok",
		Details:   fmt.Sprintf("job_id=%s backend=%s model=%s rule=%s", jobID, route.Backend, route.Model, route.Rule),
		CreatedAt: time.Now().UTC(),
	})
	writeJSON(w, http.StatusAccepted, splaiapi.SubmitJobResponse{JobID: jobID})
}

func (s *Server) handleModelPrefetch(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireScopes(w, r, "operator"); !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req splaiapi.PrefetchModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Model = strings.TrimSpace(req.Model)
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}
	req.Source = strings.ToLower(strings.TrimSpace(req.Source))
	if req.Source == "" {
		req.Source = "huggingface"
	}
	if req.Source != "huggingface" {
		writeError(w, http.StatusBadRequest, "source must be huggingface")
		return
	}
	onlyMissing := true
	if req.OnlyMissing != nil {
		onlyMissing = *req.OnlyMissing
	}
	priority := strings.TrimSpace(req.Priority)
	if priority == "" {
		priority = "standard"
	}
	workers, err := s.engine.ListWorkers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	targetSet := map[string]struct{}{}
	if len(req.Workers) > 0 {
		for _, w := range req.Workers {
			if id := strings.TrimSpace(w); id != "" {
				targetSet[id] = struct{}{}
			}
		}
	}
	targeted := make([]state.WorkerRecord, 0, len(workers))
	for _, w := range workers {
		if strings.EqualFold(w.Health, "unhealthy") {
			continue
		}
		if len(targetSet) > 0 {
			if _, ok := targetSet[w.ID]; !ok {
				continue
			}
		}
		targeted = append(targeted, w)
	}
	if len(targeted) == 0 {
		writeError(w, http.StatusBadRequest, "no eligible workers found")
		return
	}

	jobID := fmt.Sprintf("job-%d", atomic.AddUint64(&s.seq, 1))
	tasks := make([]planner.Task, 0, len(targeted))
	workerIDs := make([]string, 0, len(targeted))
	for i, w := range targeted {
		taskID := fmt.Sprintf("prefetch-%03d", i+1)
		tasks = append(tasks, planner.Task{
			TaskID:     taskID,
			Type:       "model_download",
			TimeoutSec: 1800,
			MaxRetries: 1,
			Inputs: map[string]string{
				"model":           req.Model,
				"source":          req.Source,
				"only_if_missing": strconv.FormatBool(onlyMissing),
				"_target_worker":  w.ID,
				"_data_locality":  w.Locality,
			},
		})
		workerIDs = append(workerIDs, w.ID)
	}
	sort.Strings(workerIDs)
	dag := planner.DAG{
		DAGID: "dag-prefetch-" + jobID,
		Tasks: tasks,
	}
	tenant := tenantFromRequest(r, req.Tenant)
	if err := s.engine.AddJob(jobID, tenant, "model_prefetch", req.Model, "enterprise-default", priority, "internal", req.Model, "default", dag); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, splaiapi.PrefetchModelResponse{
		JobID:           jobID,
		Model:           req.Model,
		Source:          req.Source,
		TargetedWorkers: workerIDs,
		ScheduledTasks:  len(tasks),
		OnlyMissing:     onlyMissing,
	})
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatRequest struct {
	Model    string              `json:"model"`
	Messages []openAIChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
	User     string              `json:"user,omitempty"`
}

type openAIResponsesRequest struct {
	Model  string `json:"model"`
	Input  any    `json:"input"`
	Stream bool   `json:"stream"`
	User   string `json:"user,omitempty"`
}

func (s *Server) handleOpenAIChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req openAIChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Stream {
		writeError(w, http.StatusBadRequest, "stream=true is not supported in compatibility mode")
		return
	}
	prompt := buildChatPrompt(req.Messages)
	if prompt == "" {
		writeError(w, http.StatusBadRequest, "messages is required")
		return
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = "llama3-8b-q4"
	}
	content, usedModel, err := s.runOpenAICompatJob(r, model, prompt)
	if err != nil {
		writeError(w, httpStatusForCompatErr(err), err.Error())
		return
	}
	id := fmt.Sprintf("chatcmpl-%d", atomic.AddUint64(&s.seq, 1))
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": time.Now().UTC().Unix(),
		"model":   usedModel,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		},
	})
}

func (s *Server) handleOpenAIResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req openAIResponsesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Stream {
		writeError(w, http.StatusBadRequest, "stream=true is not supported in compatibility mode")
		return
	}
	prompt := buildResponsesPrompt(req.Input)
	if prompt == "" {
		writeError(w, http.StatusBadRequest, "input is required")
		return
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = "llama3-8b-q4"
	}
	content, usedModel, err := s.runOpenAICompatJob(r, model, prompt)
	if err != nil {
		writeError(w, httpStatusForCompatErr(err), err.Error())
		return
	}
	id := fmt.Sprintf("resp_%d", atomic.AddUint64(&s.seq, 1))
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      id,
		"object":  "response",
		"created": time.Now().UTC().Unix(),
		"model":   usedModel,
		"status":  "completed",
		"output": []map[string]any{
			{
				"id":   "msg_" + id,
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": content, "annotations": []any{}},
				},
			},
		},
		"usage": map[string]int{
			"input_tokens":  0,
			"output_tokens": 0,
			"total_tokens":  0,
		},
	})
}

func (s *Server) runOpenAICompatJob(r *http.Request, model, prompt string) (string, string, error) {
	tenant := tenantFromRequest(r, "")
	p, code, msg := s.auth.authorize(r)
	if code != http.StatusOK {
		return "", "", errors.New(msg)
	}
	if s.auth.enabled && !p.canTenantAction(tenant, "submit") {
		return "", "", errors.New("tenant action denied")
	}
	route := s.router.Route(models.RouteInput{
		LatencyClass:       "interactive",
		ReasoningRequired:  true,
		DataClassification: "internal",
		RequestedModel:     model,
	})
	if model == "" {
		model = route.Model
	}
	jobID := fmt.Sprintf("job-%d", atomic.AddUint64(&s.seq, 1))
	dag := s.planner.CompileWithMode(jobID, "chat", prompt, "template")
	dag = injectModelBackend(dag, route.Backend)
	if err := s.engine.AddJob(
		jobID,
		tenant,
		"chat",
		prompt,
		"enterprise-default",
		"interactive",
		"internal",
		model,
		"default",
		dag,
	); err != nil {
		return "", "", err
	}
	deadline := time.Now().UTC().Add(s.timeout)
	for time.Now().UTC().Before(deadline) {
		job, ok, err := s.engine.GetJob(jobID)
		if err != nil {
			return "", "", err
		}
		if !ok {
			return "", "", errors.New("job not found")
		}
		switch job.Status {
		case scheduler.JobCompleted:
			content := strings.TrimSpace(job.ResultArtifactURI)
			if content == "" {
				content = "completed"
			}
			return content, model, nil
		case scheduler.JobFailed, scheduler.JobCanceled:
			if job.Message != "" {
				return "", "", errors.New(job.Message)
			}
			return "", "", errors.New("job failed")
		}
		time.Sleep(200 * time.Millisecond)
	}
	return "", "", errors.New("openai compatibility timeout waiting for completion")
}

func buildChatPrompt(messages []openAIChatMessage) string {
	if len(messages) == 0 {
		return ""
	}
	var b strings.Builder
	for _, m := range messages {
		role := strings.TrimSpace(m.Role)
		if role == "" {
			role = "user"
		}
		text := strings.TrimSpace(m.Content)
		if text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(text)
	}
	return strings.TrimSpace(b.String())
}

func buildResponsesPrompt(in any) string {
	switch v := in.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	default:
		if in == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(in))
	}
}

func httpStatusForCompatErr(err error) int {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timeout"):
		return http.StatusGatewayTimeout
	case strings.Contains(msg, "denied"):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

func (s *Server) handleJobByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/jobs/")
	if path == "" {
		writeError(w, http.StatusNotFound, "job id is required")
		return
	}
	parts := strings.Split(path, "/")
	jobID := parts[0]
	subresource := ""
	if len(parts) > 1 {
		subresource = parts[1]
	}
	job, ok, err := s.engine.GetJob(jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if _, ok := s.requireTenantAction(w, r, job.Tenant, "read"); !ok {
		return
	}

	if subresource == "stream" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.streamJobEvents(w, r, jobID)
		return
	}

	if subresource == "tasks" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		tasks, ok, err := s.engine.GetJobTasks(jobID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !ok {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
		workerFilter := strings.TrimSpace(r.URL.Query().Get("worker_id"))
		limit := 0
		offset := 0
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil || v < 0 {
				writeError(w, http.StatusBadRequest, "limit must be a non-negative integer")
				return
			}
			limit = v
		}
		if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil || v < 0 {
				writeError(w, http.StatusBadRequest, "offset must be a non-negative integer")
				return
			}
			offset = v
		}

		filtered := make([]scheduler.TaskStatus, 0, len(tasks))
		for _, t := range tasks {
			if statusFilter != "" && t.Status != statusFilter {
				continue
			}
			if workerFilter != "" && t.WorkerID != workerFilter {
				continue
			}
			filtered = append(filtered, t)
		}
		total := len(filtered)
		if offset > total {
			offset = total
		}
		page := filtered[offset:]
		if limit > 0 && limit < len(page) {
			page = page[:limit]
		}

		out := make([]splaiapi.JobTaskStatus, 0, len(page))
		for _, t := range page {
			item := splaiapi.JobTaskStatus{
				TaskID:    t.TaskID,
				Type:      t.Type,
				Status:    t.Status,
				Attempt:   t.Attempt,
				WorkerID:  t.WorkerID,
				LeaseID:   t.LeaseID,
				OutputURI: t.OutputURI,
				Error:     t.Error,
				CreatedAt: t.CreatedAt.Format(http.TimeFormat),
				UpdatedAt: t.UpdatedAt.Format(http.TimeFormat),
			}
			if !t.LeaseExpires.IsZero() {
				item.LeaseExpires = t.LeaseExpires.Format(http.TimeFormat)
			}
			out = append(out, item)
		}
		resp := splaiapi.JobTasksResponse{
			JobID:    jobID,
			Total:    total,
			Returned: len(out),
			Offset:   offset,
			Tasks:    out,
		}
		if limit > 0 {
			resp.Limit = limit
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if subresource == "archive" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if _, ok := s.requireTenantAction(w, r, job.Tenant, "cancel"); !ok {
			return
		}
		accepted, err := s.engine.ArchiveJob(jobID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"accepted": accepted})
		return
	}
	if subresource != "" {
		writeError(w, http.StatusNotFound, "job subresource not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, splaiapi.JobStatusResponse{
			JobID:             job.ID,
			Status:            job.Status,
			Message:           job.Message,
			ResultArtifactURI: job.ResultArtifactURI,
			CreatedAt:         job.CreatedAt.Format(http.TimeFormat),
			UpdatedAt:         job.UpdatedAt.Format(http.TimeFormat),
		})
	case http.MethodDelete:
		if _, ok := s.requireTenantAction(w, r, job.Tenant, "cancel"); !ok {
			return
		}
		accepted, err := s.engine.CancelJob(jobID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, splaiapi.CancelJobResponse{Accepted: accepted})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) streamJobEvents(w http.ResponseWriter, r *http.Request, jobID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	job, found, err := s.engine.GetJob(jobID)
	if err != nil {
		_ = writeSSEEvent(w, "error", map[string]any{"message": err.Error()})
		flusher.Flush()
		return
	}
	if !found {
		_ = writeSSEEvent(w, "error", map[string]any{"message": "job not found"})
		flusher.Flush()
		return
	}
	tasks, _, err := s.engine.GetJobTasks(jobID)
	if err != nil {
		_ = writeSSEEvent(w, "error", map[string]any{"message": err.Error()})
		flusher.Flush()
		return
	}
	taskStates := make(map[string]string, len(tasks))
	for _, t := range tasks {
		taskStates[t.TaskID] = taskStateHash(t)
	}
	lastJobState := jobStateHash(job)

	_ = writeSSEEvent(w, "job.snapshot", map[string]any{
		"job":   jobStatusPayload(job),
		"tasks": taskStatusPayloads(tasks),
	})
	flusher.Flush()

	if isTerminalJobStatus(job.Status) {
		_ = writeSSEEvent(w, terminalEventForStatus(job.Status), jobStatusPayload(job))
		flusher.Flush()
		return
	}

	pollTicker := time.NewTicker(500 * time.Millisecond)
	defer pollTicker.Stop()
	keepaliveTicker := time.NewTicker(15 * time.Second)
	defer keepaliveTicker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepaliveTicker.C:
			_, _ = w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		case <-pollTicker.C:
			job, found, err = s.engine.GetJob(jobID)
			if err != nil {
				_ = writeSSEEvent(w, "error", map[string]any{"message": err.Error()})
				flusher.Flush()
				return
			}
			if !found {
				_ = writeSSEEvent(w, "error", map[string]any{"message": "job not found"})
				flusher.Flush()
				return
			}
			tasks, _, err = s.engine.GetJobTasks(jobID)
			if err != nil {
				_ = writeSSEEvent(w, "error", map[string]any{"message": err.Error()})
				flusher.Flush()
				return
			}

			currentJobState := jobStateHash(job)
			if currentJobState != lastJobState {
				_ = writeSSEEvent(w, "job.status", jobStatusPayload(job))
				lastJobState = currentJobState
			}

			for _, t := range tasks {
				currentTaskState := taskStateHash(t)
				prev, exists := taskStates[t.TaskID]
				if !exists || prev != currentTaskState {
					_ = writeSSEEvent(w, "task.update", taskStatusPayload(t))
					taskStates[t.TaskID] = currentTaskState
				}
			}
			flusher.Flush()

			if isTerminalJobStatus(job.Status) {
				_ = writeSSEEvent(w, terminalEventForStatus(job.Status), jobStatusPayload(job))
				flusher.Flush()
				return
			}
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, event string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte("event: " + event + "\n")); err != nil {
		return err
	}
	if _, err := w.Write([]byte("data: " + string(b) + "\n\n")); err != nil {
		return err
	}
	return nil
}

func isTerminalJobStatus(status string) bool {
	switch status {
	case scheduler.JobCompleted, scheduler.JobFailed, scheduler.JobCanceled, scheduler.JobArchived:
		return true
	default:
		return false
	}
}

func terminalEventForStatus(status string) string {
	switch status {
	case scheduler.JobCompleted:
		return "job.completed"
	case scheduler.JobFailed:
		return "job.failed"
	case scheduler.JobCanceled:
		return "job.canceled"
	case scheduler.JobArchived:
		return "job.archived"
	default:
		return "job.terminal"
	}
}

func jobStateHash(job *scheduler.Job) string {
	if job == nil {
		return ""
	}
	return strings.Join([]string{
		job.Status,
		job.Message,
		job.ResultArtifactURI,
		job.UpdatedAt.Format(time.RFC3339Nano),
	}, "|")
}

func taskStateHash(t scheduler.TaskStatus) string {
	return strings.Join([]string{
		t.Status,
		strconv.Itoa(t.Attempt),
		t.WorkerID,
		t.LeaseID,
		t.OutputURI,
		t.Error,
		t.UpdatedAt.Format(time.RFC3339Nano),
	}, "|")
}

func jobStatusPayload(job *scheduler.Job) map[string]any {
	if job == nil {
		return map[string]any{}
	}
	return map[string]any{
		"job_id":              job.ID,
		"status":              job.Status,
		"message":             job.Message,
		"result_artifact_uri": job.ResultArtifactURI,
		"created_at":          job.CreatedAt.Format(http.TimeFormat),
		"updated_at":          job.UpdatedAt.Format(http.TimeFormat),
	}
}

func taskStatusPayloads(tasks []scheduler.TaskStatus) []map[string]any {
	out := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, taskStatusPayload(t))
	}
	return out
}

func taskStatusPayload(t scheduler.TaskStatus) map[string]any {
	out := map[string]any{
		"task_id":             t.TaskID,
		"type":                t.Type,
		"status":              t.Status,
		"attempt":             t.Attempt,
		"worker_id":           t.WorkerID,
		"lease_id":            t.LeaseID,
		"output_artifact_uri": t.OutputURI,
		"error":               t.Error,
		"created_at":          t.CreatedAt.Format(http.TimeFormat),
		"updated_at":          t.UpdatedAt.Format(http.TimeFormat),
	}
	if !t.LeaseExpires.IsZero() {
		out["lease_expires"] = t.LeaseExpires.Format(http.TimeFormat)
	}
	return out
}

func (s *Server) handleRegisterWorker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req splaiapi.RegisterWorkerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.WorkerID == "" {
		writeError(w, http.StatusBadRequest, "worker_id is required")
		return
	}
	if err := s.engine.RegisterWorker(req); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, splaiapi.RegisterWorkerResponse{Accepted: true, HeartbeatIntervalSeconds: 5})
}

func (s *Server) handleWorkerSubresource(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/workers/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		writeError(w, http.StatusNotFound, "worker subresource not found")
		return
	}
	workerID := parts[0]
	sub := parts[1]

	switch sub {
	case "heartbeat":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req splaiapi.HeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Health == "" {
			req.Health = "healthy"
		}
		if err := s.engine.Heartbeat(workerID, req); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, splaiapi.HeartbeatResponse{Accepted: true})
	case "assignments":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		maxTasks := 1
		if raw := r.URL.Query().Get("max_tasks"); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				maxTasks = v
			}
		}
		assignments, err := s.engine.PollAssignments(workerID, maxTasks)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, splaiapi.PollAssignmentsResponse{Assignments: assignments})
	default:
		writeError(w, http.StatusNotFound, "worker subresource not found")
	}
}

func (s *Server) handleTaskReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req splaiapi.ReportTaskResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Status == "" {
		req.Status = scheduler.JobCompleted
	}
	job, ok, err := s.engine.GetJob(req.JobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if _, ok := s.requireTenantAction(w, r, job.Tenant, "report"); !ok {
		return
	}
	if err := s.engine.ReportTaskResult(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, splaiapi.ReportTaskResultResponse{Accepted: true})
}

func (s *Server) handleDeadLetterQueue(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireScopes(w, r, "operator")
	if !ok {
		return
	}
	tenant := tenantFromRequest(r, "")
	switch r.Method {
	case http.MethodGet:
		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				limit = v
			}
		}
		refs, err := s.engine.ListDeadLetters(limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.appendAudit(state.AuditEventRecord{
			Action:     "dead_letter_list",
			Actor:      p.id,
			Tenant:     tenant,
			RemoteAddr: r.RemoteAddr,
			Resource:   "queue/dead-letter",
			Requested:  limit,
			Result:     "ok",
			Details:    fmt.Sprintf("returned=%d", len(refs)),
		})
		tasks := make([]splaiapi.DeadLetterTask, 0, len(refs))
		for _, r := range refs {
			tasks = append(tasks, splaiapi.DeadLetterTask{JobID: r.JobID, TaskID: r.TaskID})
		}
		writeJSON(w, http.StatusOK, splaiapi.ListDeadLettersResponse{Tasks: tasks})
	case http.MethodPost:
		var req splaiapi.RequeueDeadLettersRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if len(req.Tasks) == 0 {
			writeError(w, http.StatusBadRequest, "tasks is required")
			return
		}
		if s.safety.maxBatch > 0 && len(req.Tasks) > s.safety.maxBatch {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("tasks exceeds max batch size %d", s.safety.maxBatch))
			return
		}
		if !req.DryRun && s.safety.confirmThreshold > 0 && len(req.Tasks) >= s.safety.confirmThreshold && s.safety.confirmToken != "" {
			if strings.TrimSpace(r.Header.Get("X-SPLAI-Confirm")) != s.safety.confirmToken {
				writeError(w, http.StatusBadRequest, "confirmation token required for large requeue")
				return
			}
		}
		if !req.DryRun && !s.safety.allowRequeue(time.Now().UTC()) {
			writeError(w, http.StatusTooManyRequests, "requeue rate limit exceeded")
			return
		}
		refs := make([]state.TaskRef, 0, len(req.Tasks))
		for _, t := range req.Tasks {
			if t.JobID == "" || t.TaskID == "" {
				writeError(w, http.StatusBadRequest, "task entries require job_id and task_id")
				return
			}
			refs = append(refs, state.TaskRef{JobID: t.JobID, TaskID: t.TaskID})
		}
		payloadHash := hashDeadLetterTasks(req.Tasks)
		if req.DryRun {
			refsInDead, err := s.engine.ListDeadLetters(len(refs) * 4)
			if err != nil {
				s.appendAudit(state.AuditEventRecord{
					Action:      "dead_letter_requeue",
					Actor:       p.id,
					Tenant:      tenant,
					RemoteAddr:  r.RemoteAddr,
					Resource:    "queue/dead-letter",
					PayloadHash: payloadHash,
					Requested:   len(refs),
					Result:      "error",
					Details:     "dry_run list failed: " + err.Error(),
				})
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			count := countMatchingRefs(refs, refsInDead)
			s.appendAudit(state.AuditEventRecord{
				Action:      "dead_letter_requeue",
				Actor:       p.id,
				Tenant:      tenant,
				RemoteAddr:  r.RemoteAddr,
				Resource:    "queue/dead-letter",
				PayloadHash: payloadHash,
				Requested:   len(refs),
				Result:      "dry_run",
				Details:     fmt.Sprintf("would_requeue=%d", count),
			})
			writeJSON(w, http.StatusOK, splaiapi.RequeueDeadLettersResponse{DryRun: true, Requested: len(refs), Requeued: count})
			return
		}
		n, err := s.engine.RequeueDeadLetters(refs)
		if err != nil {
			s.appendAudit(state.AuditEventRecord{
				Action:      "dead_letter_requeue",
				Actor:       p.id,
				Tenant:      tenant,
				RemoteAddr:  r.RemoteAddr,
				Resource:    "queue/dead-letter",
				PayloadHash: payloadHash,
				Requested:   len(refs),
				Result:      "error",
				Details:     err.Error(),
			})
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.appendAudit(state.AuditEventRecord{
			Action:      "dead_letter_requeue",
			Actor:       p.id,
			Tenant:      tenant,
			RemoteAddr:  r.RemoteAddr,
			Resource:    "queue/dead-letter",
			PayloadHash: payloadHash,
			Requested:   len(refs),
			Result:      "ok",
			Details:     fmt.Sprintf("requeued=%d", n),
		})
		log.Printf("audit action=dead_letter_requeue principal=%s remote=%s requested=%d requeued=%d", p.id, r.RemoteAddr, len(refs), n)
		writeJSON(w, http.StatusOK, splaiapi.RequeueDeadLettersResponse{Requested: len(refs), Requeued: n})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleAuditEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireScopes(w, r, "operator"); !ok {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := 50
	offset := 0
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	actor := strings.TrimSpace(r.URL.Query().Get("actor"))
	tenant := strings.TrimSpace(r.URL.Query().Get("tenant"))
	result := strings.TrimSpace(r.URL.Query().Get("result"))
	format := strings.TrimSpace(r.URL.Query().Get("format"))
	from, to, err := parseTimeRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = v
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			writeError(w, http.StatusBadRequest, "offset must be a non-negative integer")
			return
		}
		offset = v
	}
	events, err := s.engine.ListAuditEvents(state.AuditQuery{
		Limit:  limit,
		Offset: offset,
		Action: action,
		Actor:  actor,
		Tenant: tenant,
		Result: result,
		From:   from,
		To:     to,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if strings.EqualFold(format, "csv") {
		writeAuditCSV(w, events)
		return
	}
	out := make([]splaiapi.AuditEvent, 0, len(events))
	for _, e := range events {
		out = append(out, splaiapi.AuditEvent{
			ID:          e.ID,
			Action:      e.Action,
			Actor:       e.Actor,
			Tenant:      e.Tenant,
			RemoteAddr:  e.RemoteAddr,
			Resource:    e.Resource,
			PayloadHash: e.PayloadHash,
			PrevHash:    e.PrevHash,
			EventHash:   e.EventHash,
			Requested:   e.Requested,
			Result:      e.Result,
			Details:     e.Details,
			CreatedAt:   e.CreatedAt.Format(http.TimeFormat),
		})
	}
	writeJSON(w, http.StatusOK, splaiapi.ListAuditEventsResponse{
		Returned: len(out),
		Limit:    limit,
		Offset:   offset,
		Events:   out,
	})
}

func (s *Server) requireScopes(w http.ResponseWriter, r *http.Request, scopes ...string) (principal, bool) {
	p, code, msg := s.auth.authorize(r, scopes...)
	if code != http.StatusOK {
		writeError(w, code, msg)
		return principal{}, false
	}
	return p, true
}

func (s *Server) requireTenantAccess(w http.ResponseWriter, r *http.Request, tenant string) (principal, bool) {
	p, code, msg := s.auth.authorize(r)
	if code != http.StatusOK {
		writeError(w, code, msg)
		return principal{}, false
	}
	if !s.auth.enabled {
		return p, true
	}
	if !p.canAccessTenant(tenant) {
		writeError(w, http.StatusForbidden, "tenant access denied")
		return principal{}, false
	}
	return p, true
}

func (s *Server) requireTenantAction(w http.ResponseWriter, r *http.Request, tenant, action string) (principal, bool) {
	p, code, msg := s.auth.authorize(r)
	if code != http.StatusOK {
		writeError(w, code, msg)
		return principal{}, false
	}
	if !s.auth.enabled {
		return p, true
	}
	if !p.canTenantAction(tenant, action) {
		writeError(w, http.StatusForbidden, "tenant action denied")
		return principal{}, false
	}
	return p, true
}

func hashDeadLetterTasks(tasks []splaiapi.DeadLetterTask) string {
	if len(tasks) == 0 {
		return ""
	}
	b, err := json.Marshal(tasks)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func tenantFromRequest(r *http.Request, reqTenant string) string {
	t := strings.TrimSpace(reqTenant)
	if t == "" {
		t = strings.TrimSpace(r.Header.Get("X-SPLAI-Tenant"))
	}
	if t == "" {
		t = "default"
	}
	return t
}

func countMatchingRefs(requested []state.TaskRef, inDead []state.TaskRef) int {
	available := make(map[string]int, len(inDead))
	for _, r := range inDead {
		available[r.JobID+"|"+r.TaskID]++
	}
	count := 0
	for _, r := range requested {
		k := r.JobID + "|" + r.TaskID
		if available[k] > 0 {
			available[k]--
			count++
		}
	}
	return count
}

func injectModelInstallTasks(dag planner.DAG, model string, installIfMissing bool) planner.DAG {
	model = strings.TrimSpace(model)
	if !installIfMissing || model == "" || len(dag.Tasks) == 0 {
		return dag
	}
	needsInstall := false
	for _, t := range dag.Tasks {
		if strings.EqualFold(strings.TrimSpace(t.Type), "llm_inference") {
			needsInstall = true
			break
		}
	}
	if !needsInstall {
		return dag
	}
	installTaskID := "t0-model-install"
	existing := map[string]struct{}{}
	for _, t := range dag.Tasks {
		existing[t.TaskID] = struct{}{}
	}
	if _, ok := existing[installTaskID]; ok {
		installTaskID = "t0-model-install-1"
	}
	dag.Tasks = append([]planner.Task{
		{
			TaskID:     installTaskID,
			Type:       "model_download",
			TimeoutSec: 1800,
			MaxRetries: 1,
			Inputs: map[string]string{
				"model":           model,
				"source":          "huggingface",
				"only_if_missing": "true",
			},
		},
	}, dag.Tasks...)
	for i := range dag.Tasks {
		if dag.Tasks[i].TaskID == installTaskID {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(dag.Tasks[i].Type), "llm_inference") {
			continue
		}
		if dag.Tasks[i].Inputs == nil {
			dag.Tasks[i].Inputs = map[string]string{}
		}
		dag.Tasks[i].Inputs["_install_model_if_missing"] = "true"
		dag.Tasks[i].Inputs["model_source"] = "huggingface"
		if !containsString(dag.Tasks[i].Dependencies, installTaskID) {
			dag.Tasks[i].Dependencies = append(dag.Tasks[i].Dependencies, installTaskID)
		}
	}
	return dag
}

func injectModelBackend(dag planner.DAG, backend string) planner.DAG {
	backend = strings.TrimSpace(backend)
	if backend == "" {
		return dag
	}
	for i := range dag.Tasks {
		if !strings.EqualFold(strings.TrimSpace(dag.Tasks[i].Type), "llm_inference") {
			continue
		}
		if dag.Tasks[i].Inputs == nil {
			dag.Tasks[i].Inputs = map[string]string{}
		}
		if strings.TrimSpace(dag.Tasks[i].Inputs["backend"]) == "" {
			dag.Tasks[i].Inputs["backend"] = backend
		}
	}
	return dag
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (s *Server) appendAudit(event state.AuditEventRecord) {
	if err := s.engine.AppendAuditEvent(event); err != nil {
		log.Printf("audit persist failed action=%s actor=%s err=%v", event.Action, event.Actor, err)
	}
	observability.Default.IncCounter("admin_actions_total", map[string]string{
		"actor":  event.Actor,
		"action": event.Action,
		"result": event.Result,
		"tenant": event.Tenant,
	}, 1)
}

func parseTimeRange(fromRaw, toRaw string) (time.Time, time.Time, error) {
	parse := func(raw string) (time.Time, error) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return time.Time{}, nil
		}
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, errors.New("time filters must be RFC3339")
		}
		return t.UTC(), nil
	}
	from, err := parse(fromRaw)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	to, err := parse(toRaw)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return from, to, nil
}

func writeAuditCSV(w http.ResponseWriter, events []state.AuditEventRecord) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "created_at", "action", "actor", "tenant", "remote_addr", "resource", "payload_hash", "prev_hash", "event_hash", "requested", "result", "details"})
	for _, e := range events {
		_ = cw.Write([]string{
			strconv.FormatInt(e.ID, 10),
			e.CreatedAt.Format(time.RFC3339),
			e.Action,
			e.Actor,
			e.Tenant,
			e.RemoteAddr,
			e.Resource,
			e.PayloadHash,
			e.PrevHash,
			e.EventHash,
			strconv.Itoa(e.Requested),
			e.Result,
			e.Details,
		})
	}
	cw.Flush()
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func withTracing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := observability.StartSpan(r.Context(), "http.request",
			attribute.String("http.method", r.Method),
			attribute.String("http.path", r.URL.Path),
		)
		defer span.End()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		traceID := span.SpanContext().TraceID().String()
		if traceID != "" {
			sw.Header().Set("X-Trace-ID", traceID)
		}
		next.ServeHTTP(sw, r.WithContext(ctx))
		span.SetAttributes(attribute.Int("http.status_code", sw.status))
	})
}

func parseEnvBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes":
		return true
	case "0", "false", "no":
		return false
	default:
		return fallback
	}
}
