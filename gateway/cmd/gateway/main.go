package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"permit-gateway/internal/gateway"
)

func main() {
	config, err := gateway.LoadConfig()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}
	handler, err := gateway.New(config, os.Stdout)
	if err != nil {
		log.Fatalf("gateway initialization error: %v", err)
	}
	server := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    32 << 10,
	}

	stopped := make(chan os.Signal, 1)
	signal.Notify(stopped, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stopped
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown: %v", err)
		}
	}()
	log.Printf("Permit gateway listening on %s (demo=%t)", config.ListenAddr, config.DemoMode)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
