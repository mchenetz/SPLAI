package controllers

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestReconcileJobStatusInitializesProgressAndPhase(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"tasks": []any{
					map[string]any{"taskId": "t1"},
					map[string]any{"taskId": "t2"},
				},
			},
		},
	}
	changed := reconcileJobStatus(obj)
	if !changed {
		t.Fatalf("expected status mutation for fresh object")
	}
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	if phase != "Running" {
		t.Fatalf("expected Running phase, got %q", phase)
	}
	total, _, _ := unstructured.NestedInt64(obj.Object, "status", "totalTasks")
	if total != 2 {
		t.Fatalf("expected totalTasks=2, got %d", total)
	}
	progressTotal, _, _ := unstructured.NestedInt64(obj.Object, "status", "progress", "totalTasks")
	if progressTotal != 2 {
		t.Fatalf("expected progress.totalTasks=2, got %d", progressTotal)
	}
}

func TestReconcileJobStatusMovesToCompleted(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"tasks": []any{
					map[string]any{"taskId": "t1"},
					map[string]any{"taskId": "t2"},
				},
			},
			"status": map[string]any{
				"phase":          "Running",
				"completedTasks": int64(2),
			},
		},
	}
	reconcileJobStatus(obj)
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	if phase != "Completed" {
		t.Fatalf("expected Completed phase, got %q", phase)
	}
	msg, _, _ := unstructured.NestedString(obj.Object, "status", "message")
	if msg == "" {
		t.Fatalf("expected completed status message")
	}
}

func TestReconcileJobStatusMarksFailedWhenFailuresPresent(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"tasks": []any{
					map[string]any{"taskId": "t1"},
				},
			},
			"status": map[string]any{
				"phase":       "Running",
				"failedTasks": int64(1),
			},
		},
	}
	reconcileJobStatus(obj)
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	if phase != "Failed" {
		t.Fatalf("expected Failed phase, got %q", phase)
	}
}
