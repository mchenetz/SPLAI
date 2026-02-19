package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/daef/controllers"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	jobReconciler := controllers.NewJobReconciler()
	workerReconciler := controllers.NewWorkerReconciler(20 * time.Second)

	go jobReconciler.Start(ctx)
	go workerReconciler.Start(ctx)

	<-ctx.Done()
	log.Printf("operator stopping")
}
