package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mochigome-git/msp-go/internal/app"
	"github.com/mochigome-git/msp-go/internal/monitor"
	"github.com/mochigome-git/msp-go/pkg/config"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)

	if err := config.Load(".env.local"); err != nil {
		logger.Fatalf("Invalid configuration:\n%v", err)
	}
	cfg := config.Cfg

	if os.Getenv("PERF_MONITOR") == "true" {
		monitor.StartPerformanceMonitor()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.NewApplication(cfg, logger)
	if err != nil {
		logger.Fatalf("Error initializing application: %v", err)
	}

	if err := application.Run(ctx); err != nil {
		logger.Fatalf("Application run failed: %v", err)
	}
}
