package main

import (
	"log"
	"net/http"
	"os"

	"github.com/example/daef/internal/api"
	"github.com/example/daef/internal/bootstrap"
	"github.com/example/daef/internal/planner"
)

func main() {
	port := os.Getenv("DAEF_GATEWAY_PORT")
	if port == "" {
		port = "8080"
	}

	engine, err := bootstrap.NewEngineFromEnv()
	if err != nil {
		log.Fatalf("bootstrap engine: %v", err)
	}
	compiler := planner.NewCompiler()
	server := api.NewServer(compiler, engine)

	log.Printf("daef api-gateway listening on :%s", port)
	if err := http.ListenAndServe(":"+port, server.Handler()); err != nil {
		log.Fatalf("api-gateway failed: %v", err)
	}
}
