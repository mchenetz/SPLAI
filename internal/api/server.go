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
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/example/daef/internal/observability"
	"github.com/example/daef/internal/planner"
	"github.com/example/daef/internal/scheduler"
	"github.com/example/daef/internal/state"
	"github.com/example/daef/pkg/daefapi"
)

type Server struct {
	planner *planner.Compiler
	engine  *scheduler.Engine
	auth    *authorizer
	safety  *adminSafety
	seq     uint64
}

func NewServer(p *planner.Compiler, e *scheduler.Engine) *Server {
	return &Server{planner: p, engine: e, auth: newAuthorizerFromEnv(), safety: newAdminSafetyFromEnv()}
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
	return withLogging(mux)
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

	var req daefapi.SubmitJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Priority == "" {
		req.Priority = "interactive"
	}
	if req.Policy == "" {
		req.Policy = "enterprise-default"
	}
	tenant := tenantFromRequest(r, req.Tenant)
	if _, ok := s.requireTenantAccess(w, r, tenant); !ok {
		return
	}

	jobID := fmt.Sprintf("job-%d", atomic.AddUint64(&s.seq, 1))
	dag := s.planner.Compile(jobID, req.Type, req.Input)
	if err := s.engine.AddJob(jobID, tenant, req.Type, req.Input, req.Policy, req.Priority, dag); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, daefapi.SubmitJobResponse{JobID: jobID})
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
	if _, ok := s.requireTenantAccess(w, r, job.Tenant); !ok {
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

		out := make([]daefapi.JobTaskStatus, 0, len(page))
		for _, t := range page {
			item := daefapi.JobTaskStatus{
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
		resp := daefapi.JobTasksResponse{
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
	if subresource != "" {
		writeError(w, http.StatusNotFound, "job subresource not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, daefapi.JobStatusResponse{
			JobID:             job.ID,
			Status:            job.Status,
			Message:           job.Message,
			ResultArtifactURI: job.ResultArtifactURI,
			CreatedAt:         job.CreatedAt.Format(http.TimeFormat),
			UpdatedAt:         job.UpdatedAt.Format(http.TimeFormat),
		})
	case http.MethodDelete:
		accepted, err := s.engine.CancelJob(jobID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, daefapi.CancelJobResponse{Accepted: accepted})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleRegisterWorker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req daefapi.RegisterWorkerRequest
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
	writeJSON(w, http.StatusOK, daefapi.RegisterWorkerResponse{Accepted: true, HeartbeatIntervalSeconds: 5})
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
		var req daefapi.HeartbeatRequest
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
		writeJSON(w, http.StatusOK, daefapi.HeartbeatResponse{Accepted: true})
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
		writeJSON(w, http.StatusOK, daefapi.PollAssignmentsResponse{Assignments: assignments})
	default:
		writeError(w, http.StatusNotFound, "worker subresource not found")
	}
}

func (s *Server) handleTaskReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req daefapi.ReportTaskResultRequest
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
	if _, ok := s.requireTenantAccess(w, r, job.Tenant); !ok {
		return
	}
	if err := s.engine.ReportTaskResult(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, daefapi.ReportTaskResultResponse{Accepted: true})
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
		tasks := make([]daefapi.DeadLetterTask, 0, len(refs))
		for _, r := range refs {
			tasks = append(tasks, daefapi.DeadLetterTask{JobID: r.JobID, TaskID: r.TaskID})
		}
		writeJSON(w, http.StatusOK, daefapi.ListDeadLettersResponse{Tasks: tasks})
	case http.MethodPost:
		var req daefapi.RequeueDeadLettersRequest
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
			if strings.TrimSpace(r.Header.Get("X-DAEF-Confirm")) != s.safety.confirmToken {
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
			writeJSON(w, http.StatusOK, daefapi.RequeueDeadLettersResponse{DryRun: true, Requested: len(refs), Requeued: count})
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
		writeJSON(w, http.StatusOK, daefapi.RequeueDeadLettersResponse{Requested: len(refs), Requeued: n})
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
	out := make([]daefapi.AuditEvent, 0, len(events))
	for _, e := range events {
		out = append(out, daefapi.AuditEvent{
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
	writeJSON(w, http.StatusOK, daefapi.ListAuditEventsResponse{
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

func hashDeadLetterTasks(tasks []daefapi.DeadLetterTask) string {
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
		t = strings.TrimSpace(r.Header.Get("X-DAEF-Tenant"))
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
