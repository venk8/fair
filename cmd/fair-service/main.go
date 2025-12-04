package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/satmihir/fair/pkg/config"
	"github.com/satmihir/fair/pkg/service"
	"github.com/satmihir/fair/pkg/tracker"
	fairhttp "github.com/satmihir/fair/pkg/transport/http"
)

// Config holds the service configuration
type Config struct {
	Port              string
	RotationFrequency time.Duration
}

func loadConfig() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	rotationFreqStr := os.Getenv("FAIR_ROTATION_FREQUENCY")
	rotationFreq := time.Minute // default
	if rotationFreqStr != "" {
		if d, err := time.ParseDuration(rotationFreqStr); err == nil {
			rotationFreq = d
		} else {
			log.Printf("Invalid FAIR_ROTATION_FREQUENCY, using default: %v", err)
		}
	}

	return &Config{
		Port:              port,
		RotationFrequency: rotationFreq,
	}
}

func main() {
	cfg := loadConfig()

	// Initialize Fairness Tracker
	trackerConfig := config.DefaultFairnessTrackerConfig()
	trackerConfig.RotationFrequency = cfg.RotationFrequency

	trkB := tracker.NewFairnessTrackerBuilder()
	trk, err := trkB.BuildWithConfig(trackerConfig)
	if err != nil {
		log.Fatalf("Failed to build fairness tracker: %v", err)
	}

	// Initialize Service
	svc := service.NewService(trk)
	defer svc.Close()

	// Initialize HTTP Handler
	handler := fairhttp.NewHandler(svc)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: handler,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()
	log.Printf("Server started on port %s", cfg.Port)

	<-done
	log.Print("Server stopped")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server Shutdown Failed:%+v", err)
	}
	log.Print("Server Exited Properly")
}
