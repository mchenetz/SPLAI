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

	"github.com/example/splai/internal/observability"
	"github.com/example/splai/internal/planner"
)

type compileRequest struct {
	JobID string `json:"job_id"`
	Type  string `json:"type"`
	Input string `json:"input"`
	Mode  string `json:"mode"`
}

type compileResponse struct {
	DAG planner.DAG `json:"dag"`
}

func main() {
	port := strings.TrimSpace(os.Getenv("SPLAI_PLANNER_PORT"))
	if port == "" {
		port = "8081"
	}

	shutdownTrace, err := observability.InitTracingFromEnv("splai-planner")
	if err != nil {
		log.Fatalf("init tracing: %v", err)
	}
	defer func() { _ = shutdownTrace(context.Background()) }()

	compiler := planner.NewCompiler()
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/v1/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeJSON(w, http.StatusOK, observability.Default.Snapshot())
	})
	mux.HandleFunc("/v1/metrics/prometheus", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(observability.Default.RenderPrometheus()))
	})
	mux.HandleFunc("/v1/planner/compile", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		started := time.Now()
		defer func() {
			observability.Default.IncCounter("planner_compile_requests_total", nil, 1)
			observability.Default.SetGauge("planner_last_compile_duration_ms", nil, float64(time.Since(started).Milliseconds()))
		}()
		var req compileRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		req.JobID = strings.TrimSpace(req.JobID)
		if req.JobID == "" {
			req.JobID = "job-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
		}
		req.Type = strings.TrimSpace(req.Type)
		if req.Type == "" {
			req.Type = "chat"
		}
		req.Mode = strings.TrimSpace(req.Mode)
		if req.Mode == "" {
			req.Mode = "hybrid"
		}
		dag := compiler.CompileWithMode(req.JobID, req.Type, req.Input, req.Mode)
		writeJSON(w, http.StatusOK, compileResponse{DAG: dag})
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

	log.Printf("splai planner listening on :%s", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("planner failed: %v", err)
	}
	log.Println("splai planner shutting down")
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
