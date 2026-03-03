package controllers

import (
	"context"
	"reflect"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var splaiWorkerGVK = schema.GroupVersionKind{
	Group:   "splai.io",
	Version: "v1alpha1",
	Kind:    "SPLAIWorker",
}

type WorkerReconciler struct {
	client     client.Client
	staleAfter time.Duration
}

func NewWorkerReconciler(c client.Client, staleAfter time.Duration) *WorkerReconciler {
	return &WorkerReconciler{client: c, staleAfter: staleAfter}
}

func (r *WorkerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(splaiWorkerGVK)
	if err := r.client.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	changed := reconcileWorkerStatus(obj, r.staleAfter, time.Now().UTC())
	if changed {
		if err := r.client.Status().Update(ctx, obj); err != nil {
			return ctrl.Result{}, err
		}
	}
	if r.staleAfter > 0 {
		return ctrl.Result{RequeueAfter: r.staleAfter / 2}, nil
	}
	return ctrl.Result{}, nil
}

func (r *WorkerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(splaiWorkerGVK)
	return ctrl.NewControllerManagedBy(mgr).
		For(obj).
		Named("splai-worker-controller").
		Complete(r)
}

func reconcileWorkerStatus(obj *unstructured.Unstructured, staleAfter time.Duration, now time.Time) bool {
	before, _, _ := unstructured.NestedMap(obj.Object, "status")

	health := "unhealthy"
	lastHeartbeat, _, _ := unstructured.NestedString(obj.Object, "status", "lastHeartbeat")
	lastHeartbeat = strings.TrimSpace(lastHeartbeat)
	if ts, ok := parseHeartbeatTime(lastHeartbeat); ok {
		age := now.Sub(ts)
		if staleAfter > 0 && age > staleAfter {
			health = "degraded"
		} else {
			health = "healthy"
		}
	}
	_ = unstructured.SetNestedField(obj.Object, health, "status", "health")

	after, _, _ := unstructured.NestedMap(obj.Object, "status")
	return !reflect.DeepEqual(before, after)
}

func parseHeartbeatTime(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return ts.UTC(), true
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts.UTC(), true
	}
	return time.Time{}, false
}
