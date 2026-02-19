package controllers

import (
	"context"
	"log"
	"time"

	"github.com/example/splai/api/v1alpha1"
)

type JobReconciler struct{}

func NewJobReconciler() *JobReconciler {
	return &JobReconciler{}
}

func (r *JobReconciler) Reconcile(_ context.Context, job *v1alpha1.SPLAIJob) {
	if job == nil {
		return
	}
	if job.Status.Phase == "" {
		job.Status.Phase = "Queued"
		job.Status.TotalTasks = len(job.Spec.Tasks)
	}
	if job.Status.Phase == "Queued" && len(job.Spec.Tasks) > 0 {
		job.Status.Phase = "Running"
	}
	if job.Status.CompletedTasks == len(job.Spec.Tasks) && len(job.Spec.Tasks) > 0 {
		job.Status.Phase = "Completed"
		if job.Status.Message == "" {
			job.Status.Message = "reconciled to terminal phase"
		}
	}
	log.Printf("job reconciled name=%s phase=%s", job.Metadata.Name, job.Status.Phase)
}

func (r *JobReconciler) Start(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			log.Printf("job reconciler heartbeat")
		}
	}
}
