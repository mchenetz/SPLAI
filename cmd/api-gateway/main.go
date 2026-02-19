package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/example/splai/internal/api"
	"github.com/example/splai/internal/bootstrap"
	"github.com/example/splai/internal/observability"
	"github.com/example/splai/internal/planner"
)

func main() {
	port := os.Getenv("SPLAI_GATEWAY_PORT")
	if port == "" {
		port = "8080"
	}

	engine, err := bootstrap.NewEngineFromEnv()
	if err != nil {
		log.Fatalf("bootstrap engine: %v", err)
	}
	shutdownTrace, err := observability.InitTracingFromEnv("splai-api-gateway")
	if err != nil {
		log.Fatalf("init tracing: %v", err)
	}
	defer func() { _ = shutdownTrace(context.Background()) }()
	compiler := planner.NewCompiler()
	server := api.NewServer(compiler, engine)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = shutdownTrace(context.Background())
	}()

	log.Printf("splai api-gateway listening on :%s", port)
	if err := http.ListenAndServe(":"+port, server.Handler()); err != nil {
		log.Fatalf("api-gateway failed: %v", err)
	}
}
