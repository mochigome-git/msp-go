package main

import (
	"context"
	"log"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"

	"github.com/mochigome-git/msp-go/internal/app"
	"github.com/mochigome-git/msp-go/pkg/config"
)

// Register the profiling handlers with the default HTTP server mux.
// This will serve the profiling endpoints at /debug/pprof.
/**
Memory profile: http://localhost:6060/debug/pprof/heap
Goroutine profile: http://localhost:6060/debug/pprof/goroutine
CPU profile: http://localhost:6060/debug/pprof/profile

Download leap data:
curl http://192.168.0.126:6060/debug/pprof/heap > heap.out
open with pprof tools:
go tool pprof heap.out
command:
top, list, png

**/

func main() {
	config.Load(".env.local")
	cfg := config.Cfg
	cfgPlc := config.Plc

	logger := log.New(os.Stdout, "", log.LstdFlags)

	// go monitor.StartPerformanceMonitor()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.NewApplication(cfg, cfgPlc, logger)
	if err != nil {
		logger.Fatalf("Error initializing application: %v", err)
	}

	if err := application.Run(ctx); err != nil {
		logger.Fatalf("Application run failed: %v", err)
	}
}
