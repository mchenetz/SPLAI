package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
)

func main() {
	log.Println("splai scheduler starting")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	log.Println("splai scheduler shutting down")
}
