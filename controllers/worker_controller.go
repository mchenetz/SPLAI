package controllers

import (
	"context"
	"log"
	"time"

	"github.com/example/daef/api/v1alpha1"
)

type WorkerReconciler struct {
	staleAfter time.Duration
}

func NewWorkerReconciler(staleAfter time.Duration) *WorkerReconciler {
	return &WorkerReconciler{staleAfter: staleAfter}
}

func (r *WorkerReconciler) Reconcile(_ context.Context, worker *v1alpha1.DAEFWorker) {
	if worker == nil {
		return
	}
	if worker.Status.LastHeartbeat.IsZero() {
		worker.Status.Health = "unhealthy"
		return
	}
	if time.Since(worker.Status.LastHeartbeat) > r.staleAfter {
		worker.Status.Health = "degraded"
	}
	if worker.Status.Health == "" {
		worker.Status.Health = "healthy"
	}
	log.Printf("worker reconciled id=%s health=%s", worker.Spec.WorkerID, worker.Status.Health)
}

func (r *WorkerReconciler) Start(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			log.Printf("worker reconciler heartbeat")
		}
	}
}
