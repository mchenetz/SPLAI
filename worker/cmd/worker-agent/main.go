package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/example/splai/internal/observability"
	"github.com/example/splai/worker/internal/config"
	"github.com/example/splai/worker/internal/executor"
	"github.com/example/splai/worker/internal/heartbeat"
	"github.com/example/splai/worker/internal/registration"
	"github.com/example/splai/worker/internal/runtime"
	"github.com/example/splai/worker/internal/telemetry"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTrace, err := observability.InitTracingFromEnv("splai-worker-agent")
	if err != nil {
		log.Fatalf("init tracing: %v", err)
	}
	defer func() { _ = shutdownTrace(context.Background()) }()

	cfg := config.FromEnv()
	t := telemetry.New()

	if err := registration.Register(ctx, cfg); err != nil {
		log.Fatalf("register worker: %v", err)
	}

	hb := heartbeat.New(cfg.ControlPlaneBaseURL, cfg.WorkerID, cfg.APIToken, cfg.HeartbeatInterval)
	exec := executor.New(cfg)
	rt := runtime.New(cfg, exec, hb, t)

	if err := rt.Run(ctx); err != nil {
		log.Fatalf("runtime stopped with error: %v", err)
	}
}
