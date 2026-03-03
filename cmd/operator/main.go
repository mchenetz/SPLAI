package main

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/example/splai/controllers"
	ctrl "sigs.k8s.io/controller-runtime"
)

func main() {
	cfg := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{})
	if err != nil {
		log.Fatalf("create manager: %v", err)
	}

	jobReconciler := controllers.NewJobReconciler(mgr.GetClient())
	if err := jobReconciler.SetupWithManager(mgr); err != nil {
		log.Fatalf("setup job reconciler: %v", err)
	}

	staleAfter := workerStaleAfterFromEnv()
	workerReconciler := controllers.NewWorkerReconciler(mgr.GetClient(), staleAfter)
	if err := workerReconciler.SetupWithManager(mgr); err != nil {
		log.Fatalf("setup worker reconciler: %v", err)
	}

	log.Printf("starting operator stale_after=%s", staleAfter)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Fatalf("run manager: %v", err)
	}
}

func workerStaleAfterFromEnv() time.Duration {
	raw := os.Getenv("SPLAI_OPERATOR_WORKER_STALE_AFTER_SECONDS")
	if raw == "" {
		return 20 * time.Second
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 20 * time.Second
	}
	return time.Duration(n) * time.Second
}
