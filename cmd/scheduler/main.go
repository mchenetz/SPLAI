package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/example/splai/internal/bootstrap"
	"github.com/example/splai/internal/planner"
	"github.com/example/splai/internal/scheduler"
	"github.com/example/splai/pkg/splaiapi"
)

type enqueueJobRequest struct {
	JobID              string      `json:"job_id"`
	Tenant             string      `json:"tenant"`
	Type               string      `json:"type"`
	Input              string      `json:"input"`
	Policy             string      `json:"policy"`
	Priority           string      `json:"priority"`
	DataClassification string      `json:"data_classification"`
	Model              string      `json:"model"`
	NetworkIsolation   string      `json:"network_isolation"`
	DAG                planner.DAG `json:"dag"`
}

type enqueueJobResponse struct {
	Accepted bool   `json:"accepted"`
	JobID    string `json:"job_id"`
}

func main() {
	port := strings.TrimSpace(os.Getenv("SPLAI_SCHEDULER_PORT"))
	if port == "" {
		port = "8082"
	}

	engine, err := bootstrap.NewEngineFromEnv()
	if err != nil {
		log.Fatalf("bootstrap engine: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/v1/scheduler/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req enqueueJobRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if strings.TrimSpace(req.JobID) == "" {
			req.JobID = "job-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
		}
		if strings.TrimSpace(req.Type) == "" {
			req.Type = "chat"
		}
		if strings.TrimSpace(req.Policy) == "" {
			req.Policy = "enterprise-default"
		}
		if strings.TrimSpace(req.Priority) == "" {
			req.Priority = "interactive"
		}
		if strings.TrimSpace(req.Tenant) == "" {
			req.Tenant = "default"
		}
		if req.DAG.DAGID == "" || len(req.DAG.Tasks) == 0 {
			writeError(w, http.StatusBadRequest, "dag with tasks is required")
			return
		}
		if err := engine.AddJob(
			req.JobID,
			req.Tenant,
			req.Type,
			req.Input,
			req.Policy,
			req.Priority,
			req.DataClassification,
			req.Model,
			req.NetworkIsolation,
			req.DAG,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, enqueueJobResponse{Accepted: true, JobID: req.JobID})
	})
	mux.HandleFunc("/v1/scheduler/jobs/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/v1/scheduler/jobs/")
		if path == "" {
			writeError(w, http.StatusNotFound, "job id is required")
			return
		}
		parts := strings.Split(path, "/")
		jobID := parts[0]
		sub := ""
		if len(parts) > 1 {
			sub = parts[1]
		}
		job, ok, err := engine.GetJob(jobID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !ok {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		if sub == "tasks" {
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			tasks, ok, err := engine.GetJobTasks(jobID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if !ok {
				writeError(w, http.StatusNotFound, "job not found")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"job_id": jobID, "tasks": tasks})
			return
		}
		if sub != "" {
			writeError(w, http.StatusNotFound, "job subresource not found")
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, job)
		case http.MethodDelete:
			accepted, err := engine.CancelJob(jobID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]bool{"accepted": accepted})
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
	mux.HandleFunc("/v1/scheduler/workers/register", func(w http.ResponseWriter, r *http.Request) {
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
		if err := engine.RegisterWorker(req); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, splaiapi.RegisterWorkerResponse{Accepted: true, HeartbeatIntervalSeconds: 5})
	})
	mux.HandleFunc("/v1/scheduler/workers/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/v1/scheduler/workers/")
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
			if err := engine.Heartbeat(workerID, req); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, splaiapi.HeartbeatResponse{Accepted: true})
		case "assignments":
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			maxTasks := 1
			if raw := strings.TrimSpace(r.URL.Query().Get("max_tasks")); raw != "" {
				if v, err := strconv.Atoi(raw); err == nil && v > 0 {
					maxTasks = v
				}
			}
			assignments, err := engine.PollAssignments(workerID, maxTasks)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, splaiapi.PollAssignmentsResponse{Assignments: assignments})
		default:
			writeError(w, http.StatusNotFound, "worker subresource not found")
		}
	})
	mux.HandleFunc("/v1/scheduler/tasks/report", func(w http.ResponseWriter, r *http.Request) {
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
		if err := engine.ReportTaskResult(req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, splaiapi.ReportTaskResultResponse{Accepted: true})
	})

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("splai scheduler listening on :%s", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("scheduler failed: %v", err)
	}
	log.Println("splai scheduler shutting down")
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
