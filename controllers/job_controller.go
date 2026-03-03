package controllers

import (
	"context"
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var splaiJobGVK = schema.GroupVersionKind{
	Group:   "splai.io",
	Version: "v1alpha1",
	Kind:    "SPLAIJob",
}

type JobReconciler struct {
	client client.Client
}

func NewJobReconciler(c client.Client) *JobReconciler {
	return &JobReconciler{client: c}
}

func (r *JobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(splaiJobGVK)
	if err := r.client.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	changed := reconcileJobStatus(obj)
	if !changed {
		return ctrl.Result{}, nil
	}
	if err := r.client.Status().Update(ctx, obj); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *JobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(splaiJobGVK)
	return ctrl.NewControllerManagedBy(mgr).
		For(obj).
		Named("splai-job-controller").
		Complete(r)
}

func reconcileJobStatus(obj *unstructured.Unstructured) bool {
	before, _, _ := unstructured.NestedMap(obj.Object, "status")

	total := 0
	if tasks, found, _ := unstructured.NestedSlice(obj.Object, "spec", "tasks"); found {
		total = len(tasks)
	}
	total64 := int64(total)

	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	completed := readStatusCounter(obj, "completedTasks")
	failed := readStatusCounter(obj, "failedTasks")

	if phase == "" {
		phase = "Queued"
	}
	if total > 0 && (phase == "Queued" || phase == "Planning" || phase == "Scheduled") {
		phase = "Running"
	}
	switch {
	case failed > 0:
		phase = "Failed"
	case total > 0 && completed >= total64:
		phase = "Completed"
		msg, _, _ := unstructured.NestedString(obj.Object, "status", "message")
		if msg == "" {
			_ = unstructured.SetNestedField(obj.Object, "reconciled to terminal phase", "status", "message")
		}
	}

	_ = unstructured.SetNestedField(obj.Object, phase, "status", "phase")
	_ = unstructured.SetNestedField(obj.Object, total64, "status", "totalTasks")
	_ = unstructured.SetNestedField(obj.Object, int64(completed), "status", "completedTasks")
	_ = unstructured.SetNestedField(obj.Object, int64(failed), "status", "failedTasks")
	_ = unstructured.SetNestedField(obj.Object, total64, "status", "progress", "totalTasks")
	_ = unstructured.SetNestedField(obj.Object, int64(completed), "status", "progress", "completedTasks")
	_ = unstructured.SetNestedField(obj.Object, int64(failed), "status", "progress", "failedTasks")

	after, _, _ := unstructured.NestedMap(obj.Object, "status")
	return !reflect.DeepEqual(before, after)
}

func readStatusCounter(obj *unstructured.Unstructured, field string) int64 {
	if v, found, _ := unstructured.NestedInt64(obj.Object, "status", field); found {
		return v
	}
	if v, found, _ := unstructured.NestedInt64(obj.Object, "status", "progress", field); found {
		return v
	}
	return 0
}
