package controllers

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestReconcileWorkerStatusWithoutHeartbeatIsUnhealthy(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{}}}
	changed := reconcileWorkerStatus(obj, 20*time.Second, time.Now().UTC())
	if !changed {
		t.Fatalf("expected health status update")
	}
	health, _, _ := unstructured.NestedString(obj.Object, "status", "health")
	if health != "unhealthy" {
		t.Fatalf("expected unhealthy, got %q", health)
	}
}

func TestReconcileWorkerStatusStaleHeartbeatIsDegraded(t *testing.T) {
	now := time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"status": map[string]any{
				"lastHeartbeat": now.Add(-2 * time.Minute).Format(time.RFC3339),
			},
		},
	}
	reconcileWorkerStatus(obj, 30*time.Second, now)
	health, _, _ := unstructured.NestedString(obj.Object, "status", "health")
	if health != "degraded" {
		t.Fatalf("expected degraded, got %q", health)
	}
}

func TestReconcileWorkerStatusRecentHeartbeatIsHealthy(t *testing.T) {
	now := time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"status": map[string]any{
				"lastHeartbeat": now.Add(-5 * time.Second).Format(time.RFC3339Nano),
			},
		},
	}
	reconcileWorkerStatus(obj, 30*time.Second, now)
	health, _, _ := unstructured.NestedString(obj.Object, "status", "health")
	if health != "healthy" {
		t.Fatalf("expected healthy, got %q", health)
	}
}
