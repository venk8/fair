package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/satmihir/fair/pkg/config"
	"github.com/satmihir/fair/pkg/request"
	"github.com/satmihir/fair/pkg/tracker"
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

type registerRequest struct {
	ClientID string `json:"client_id"`
}

type registerResponse struct {
	ShouldThrottle bool                 `json:"should_throttle"`
	Stats          *request.ResultStats `json:"stats,omitempty"`
}

type reportRequest struct {
	ClientID string `json:"client_id"`
	Outcome  string `json:"outcome"` // "success" or "failure"
}

func main() {
	cfg := loadConfig()

	// Initialize Fairness Tracker
	trackerConfig := config.DefaultFairnessTrackerConfig()
	trackerConfig.RotationFrequency = cfg.RotationFrequency
	// We could expose more config options here if needed

	trkB := tracker.NewFairnessTrackerBuilder()
	trk, err := trkB.BuildWithConfig(trackerConfig)
	if err != nil {
		log.Fatalf("Failed to build fairness tracker: %v", err)
	}
	defer trk.Close()

	mux := http.NewServeMux()

	// Handler for /register
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req registerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if req.ClientID == "" {
			http.Error(w, "client_id is required", http.StatusBadRequest)
			return
		}

		result := trk.RegisterRequest(r.Context(), []byte(req.ClientID))

		resp := registerResponse{
			ShouldThrottle: result.ShouldThrottle,
			Stats:          result.ResultStats,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Handler for /report
	mux.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req reportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if req.ClientID == "" {
			http.Error(w, "client_id is required", http.StatusBadRequest)
			return
		}

		var outcome request.Outcome
		switch req.Outcome {
		case "success":
			outcome = request.OutcomeSuccess
		case "failure":
			outcome = request.OutcomeFailure
		default:
			http.Error(w, "Invalid outcome. Must be 'success' or 'failure'", http.StatusBadRequest)
			return
		}

		trk.ReportOutcome(r.Context(), []byte(req.ClientID), outcome)
		w.WriteHeader(http.StatusOK)
	})

	// Health checks
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// In a real scenario, we might check if the tracker is ready
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	})

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: mux,
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
