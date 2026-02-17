package integration

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/satmihir/fair/pkg/broadcast"
	"github.com/satmihir/fair/pkg/config"
	"github.com/satmihir/fair/pkg/logger"
	"github.com/satmihir/fair/pkg/request"
	statepb "github.com/satmihir/fair/pkg/state/api/v1"
	"github.com/satmihir/fair/pkg/state"
	"github.com/satmihir/fair/pkg/state/store"
	"github.com/satmihir/fair/pkg/tracker"
	"google.golang.org/grpc"
)

type testLogger struct {
	t *testing.T
}

func (l *testLogger) Printf(format string, args ...any) {
	l.t.Logf(format, args...)
}
func (l *testLogger) Print(args ...any) {
	l.t.Log(args...)
}
func (l *testLogger) Println(args ...any) {
	l.t.Log(args...)
}
func (l *testLogger) Fatalf(format string, args ...any) {
	l.t.Fatalf(format, args...)
}


func TestStateServiceIntegration(t *testing.T) {
	// Set logger
	logger.SetLogger(&testLogger{t})

	// 1. Start State Service
	lis, err := net.Listen("tcp", ":0") // Random port
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	addr := lis.Addr().String()

	sStore := store.NewInMemoryStore(5*time.Minute, 15*time.Minute, 1*time.Minute)
	hub := broadcast.NewHub()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sStore.Start(ctx)
	go hub.Run()

	grpcServer := grpc.NewServer()
	stateService := state.NewService(sStore, hub)
	statepb.RegisterStateServiceServer(grpcServer, stateService)

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			// t.Errorf("failed to serve: %v", err)
		}
	}()
	defer grpcServer.Stop()

	// 2. Configure Trackers
	cfg := &config.FairnessTrackerConfig{
		M: 10,
		L: 2,
		Pi: 0.1,
		Pd: 0.05,
		Lambda: 0.0, // No decay for deterministic test
		RotationFrequency: 5 * time.Minute,
		IncludeStats: true,
		FinalProbabilityFunction: func(probs []float64) float64 {
			// Return max probability for testing
			max := 0.0
			for _, p := range probs {
				if p > max {
					max = p
				}
			}
			return max
		},
		StateServiceAddress: addr,
	}

	// Tracker 1
	ft1, err := tracker.NewFairnessTracker(cfg)
	if err != nil {
		t.Fatalf("Failed to create ft1: %v", err)
	}
	defer ft1.Close()

	// Tracker 2
	ft2, err := tracker.NewFairnessTracker(cfg)
	if err != nil {
		t.Fatalf("Failed to create ft2: %v", err)
	}
	defer ft2.Close()

	// 3. Simulate requests on FT1
	// RegisterRequest doesn't update buckets unless decay happens. Lambda=0.
	// So we rely on ReportOutcome.

	clientID := []byte("client-1")
	ft1.RegisterRequest(ctx, clientID)
	// Report FAILURE -> increase probability by Pi (0.1)
	ft1.ReportOutcome(ctx, clientID, request.OutcomeFailure)

	// Wait for sync
	time.Sleep(500 * time.Millisecond)

	// 4. Verify FT2 has updated probability
	// RegisterRequest on FT2 should return updated stats
	res := ft2.RegisterRequest(ctx, clientID)

	// Check stats
	if res.ResultStats == nil {
		t.Fatalf("FT2 result stats is nil")
	}

	// We expect probability to be ~0.1
	// buckets[0] + buckets[1] ...
	// With L=2, both levels should be updated.
	// Since hashes are deterministic, clientID should hit same buckets on both trackers.

	// Check FinalProbability
	if res.ResultStats.FinalProbability < 0.09 {
		t.Errorf("FT2 expected prob ~0.1, got %f", res.ResultStats.FinalProbability)
	}
}
