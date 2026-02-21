package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/example/splai/internal/observability"
	"github.com/example/splai/pkg/splaiapi"
	"github.com/example/splai/worker/internal/config"
	"github.com/example/splai/worker/internal/executor"
	"github.com/example/splai/worker/internal/heartbeat"
	"github.com/example/splai/worker/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

type Runtime struct {
	cfg        config.Config
	exec       *executor.Executor
	hb         *heartbeat.Client
	tel        telemetry.Client
	httpClient *http.Client
}

func New(cfg config.Config, exec *executor.Executor, hb *heartbeat.Client, tel telemetry.Client) *Runtime {
	return &Runtime{
		cfg:        cfg,
		exec:       exec,
		hb:         hb,
		tel:        tel,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (r *Runtime) Run(ctx context.Context) error {
	go r.hb.Start(ctx)
	t := time.NewTicker(r.cfg.PollInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := r.pollAndRun(ctx); err != nil {
				log.Printf("poll failed: %v", err)
			}
		}
	}
}

func (r *Runtime) pollAndRun(ctx context.Context) error {
	ctx, span := observability.StartSpan(ctx, "worker.poll_assignments",
		attribute.String("worker.id", r.cfg.WorkerID),
		attribute.Int("max_tasks", r.cfg.MaxParallelTasks),
	)
	defer span.End()
	url := strings.TrimRight(r.cfg.ControlPlaneBaseURL, "/") + "/v1/workers/" + r.cfg.WorkerID + "/assignments?max_tasks=" + intToString(r.cfg.MaxParallelTasks)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if tok := strings.TrimSpace(r.cfg.APIToken); tok != "" {
		req.Header.Set("X-SPLAI-Token", tok)
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return statusError(resp.Status)
	}

	var result splaiapi.PollAssignmentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	r.hb.SetStats(0, len(result.Assignments))
	for _, a := range result.Assignments {
		taskCtx, taskSpan := observability.StartSpan(ctx, "worker.execute_task",
			attribute.String("worker.id", r.cfg.WorkerID),
			attribute.String("job.id", a.JobID),
			attribute.String("task.id", a.TaskID),
			attribute.String("task.type", a.Type),
		)
		r.hb.SetStats(1, 0)
		_ = r.report(taskCtx, splaiapi.ReportTaskResultRequest{
			WorkerID:       r.cfg.WorkerID,
			JobID:          a.JobID,
			TaskID:         a.TaskID,
			LeaseID:        a.LeaseID,
			IdempotencyKey: buildIdempotencyKey(r.cfg.WorkerID, a.JobID, a.TaskID, a.Attempt) + ":running",
			Status:         "Running",
		})
		started := time.Now()
		artifactURI, runErr := r.exec.Run(taskCtx, executor.Task{JobID: a.JobID, TaskID: a.TaskID, Type: a.Type, Input: a.Inputs})
		duration := time.Since(started)

		report := splaiapi.ReportTaskResultRequest{
			WorkerID:       r.cfg.WorkerID,
			JobID:          a.JobID,
			TaskID:         a.TaskID,
			LeaseID:        a.LeaseID,
			IdempotencyKey: buildIdempotencyKey(r.cfg.WorkerID, a.JobID, a.TaskID, a.Attempt),
			Status:         "Completed",
			DurationMillis: duration.Milliseconds(),
		}
		if runErr != nil {
			report.Status = "Failed"
			report.Error = runErr.Error()
		} else {
			report.OutputArtifactURI = artifactURI
		}
		if err := r.report(ctx, report); err != nil {
			log.Printf("report failed job=%s task=%s: %v", a.JobID, a.TaskID, err)
		}
		r.tel.Incr("worker.task.executed")
		observability.Default.SetGauge("worker_task_duration_ms", map[string]string{
			"worker_id": r.cfg.WorkerID,
			"task_type": a.Type,
		}, float64(duration.Milliseconds()))
		if runErr != nil {
			observability.Default.IncCounter("worker_task_failures_total", map[string]string{
				"worker_id": r.cfg.WorkerID,
				"task_type": a.Type,
			}, 1)
		}
		r.hb.SetStats(0, 0)
		taskSpan.End()
	}
	return nil
}

func (r *Runtime) report(ctx context.Context, payload splaiapi.ReportTaskResultRequest) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := strings.TrimRight(r.cfg.ControlPlaneBaseURL, "/") + "/v1/tasks/report"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := strings.TrimSpace(r.cfg.APIToken); tok != "" {
		req.Header.Set("X-SPLAI-Token", tok)
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return statusError(resp.Status)
	}
	return nil
}

func intToString(v int) string {
	return strconv.Itoa(v)
}

func statusError(status string) error {
	return &runtimeError{status: status}
}

func buildIdempotencyKey(workerID, jobID, taskID string, attempt int) string {
	return workerID + ":" + jobID + ":" + taskID + ":" + strconv.Itoa(attempt)
}

type runtimeError struct {
	status string
}

func (e *runtimeError) Error() string {
	return "control-plane request failed: " + e.status
}
