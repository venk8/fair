package tracker

import (
	"context"
	"sync"

	"github.com/satmihir/fair/pkg/config"
	"github.com/satmihir/fair/pkg/data"
	"github.com/satmihir/fair/pkg/logger"
	"github.com/satmihir/fair/pkg/request"
	statepb "github.com/satmihir/fair/pkg/state/api/v1"
	"github.com/satmihir/fair/pkg/state/client"
	"github.com/satmihir/fair/pkg/utils"
)

// FairnessTracker is the main entry point for applications. It keeps track of
// client flows and determines when a request should be throttled to maintain
// fairness.
type FairnessTracker struct {
	trackerConfig *config.FairnessTrackerConfig

	mainStructure      request.Tracker
	secondaryStructure request.Tracker

	ticker utils.ITicker

	stateClient *client.Client
	ctx         context.Context
	cancel      context.CancelFunc

	// Rotation lock to ensure that we don't rotate while updating the structures
	// The act of updating is a "read" in this case since multiple updates can happen
	// concurrently, but none can happen while we are rotating so that's a write.
	rotationLock sync.RWMutex
	stopRotation chan struct{}
}

var newTrackerStructureWithClock = func(
	trackerConfig *config.FairnessTrackerConfig,
	id uint64,
	includeStats bool,
	clock utils.IClock,
) (request.Tracker, error) {
	return data.NewStructureWithClock(trackerConfig, id, includeStats, clock)
}

// NewFairnessTrackerWithClockAndTicker creates a FairnessTracker using the
// provided clock and ticker. It is primarily used for tests and simulations
// where time needs to be controlled.
func NewFairnessTrackerWithClockAndTicker(trackerConfig *config.FairnessTrackerConfig, clock utils.IClock, ticker utils.ITicker) (*FairnessTracker, error) {
	// Guard clause: fail fast and return a clear error when caller passes a nil config.
	if trackerConfig == nil {
		return nil, NewFairnessTrackerError(nil, "trackerConfig must not be nil")
	}

	ctx, cancel := context.WithCancel(context.Background())

	ft := &FairnessTracker{
		trackerConfig: trackerConfig,
		ticker:        ticker,
		stopRotation:  make(chan struct{}),
		rotationLock:  sync.RWMutex{},
		ctx:           ctx,
		cancel:        cancel,
	}

	if trackerConfig.StateServiceAddress != "" {
		ft.stateClient = client.NewClient(trackerConfig.StateServiceAddress, ft.onStateUpdate)
		ft.stateClient.Start(ctx)
	}

	// Calculate initial seeds based on time
	now := clock.Now().UnixMilli()
	windowMs := trackerConfig.RotationFrequency.Milliseconds()
	currentSeed := uint64(now) / uint64(windowMs)
	nextSeed := currentSeed + 1

	st1, err := createStructure(trackerConfig, currentSeed, clock, ft.stateClient)
	if err != nil {
		cancel()
		logger.Printf("error in creating first tracker : %v", err)
		return nil, NewFairnessTrackerError(err, "Failed to create a structure")
	}

	st2, err := createStructure(trackerConfig, nextSeed, clock, ft.stateClient)
	if err != nil {
		cancel()
		logger.Printf("error in creating second tracker : %v", err)
		return nil, NewFairnessTrackerError(err, "Failed to create a structure")
	}

	ft.mainStructure = st1
	ft.secondaryStructure = st2

	if ft.stateClient != nil {
		ft.stateClient.RequestFullState(currentSeed)
		ft.stateClient.RequestFullState(nextSeed)
	}

	// Start a periodic task to rotate underlying structures
	go func() {
		for {
			select {
			case <-ft.stopRotation:
				return
			case <-ticker.C():
				// Calculate seed for the NEXT window
				now := clock.Now().UnixMilli()
				// S2 seed is (now / window) + 1 assuming we just entered window 'now/window'
				// But ticker fires at end of window? Or start?
				// If Ticker period is Window.
				// T0: start.
				// T1: fire. We are now in window 1.
				// We want S2 (window 2) to be the new secondary.
				// So seed = 2. (now/window + 1) -> (1 + 1) = 2.
				seed := (uint64(now) / uint64(windowMs)) + 1

				s, err := createStructure(trackerConfig, seed, clock, ft.stateClient)
				if err != nil {
					logger.Fatalf("failed to create a structure during rotation: %v", err)
					// In production, we might want to retry or degrade gracefully.
					// For now, logging fatal is consistent with previous behavior (implied, though previous just returned).
					continue
				}

				if ft.stateClient != nil {
					ft.stateClient.RequestFullState(seed)
				}

				ft.rotationLock.Lock()
				ft.mainStructure = ft.secondaryStructure
				ft.secondaryStructure = s
				ft.rotationLock.Unlock()
			}
		}
	}()
	return ft, nil
}

// NewFairnessTracker creates a FairnessTracker using the real system clock and
// ticker.
func NewFairnessTracker(trackerConfig *config.FairnessTrackerConfig) (*FairnessTracker, error) {
	if trackerConfig == nil {
		return nil, NewFairnessTrackerError(nil, "Configuration cannot be nil")
	}
	clk := utils.NewRealClock()
	ticker := utils.NewRealTicker(trackerConfig.RotationFrequency)
	return NewFairnessTrackerWithClockAndTicker(trackerConfig, clk, ticker)
}

// RegisterRequest records an incoming request and returns whether it should be
// throttled.
func (ft *FairnessTracker) RegisterRequest(ctx context.Context, clientIdentifier []byte) *request.RegisterRequestResult {
	// We must take the rotation lock to avoid rotation while updating the structures
	ft.rotationLock.RLock()
	defer ft.rotationLock.RUnlock()

	resp := ft.mainStructure.RegisterRequest(ctx, clientIdentifier)

	// To keep the bad workloads data "warm" in the rotated structure, we will update both
	ft.secondaryStructure.RegisterRequest(ctx, clientIdentifier)

	return resp
}

// ReportOutcome updates the trackers with the outcome of the request from the
// given client identifier.
func (ft *FairnessTracker) ReportOutcome(ctx context.Context, clientIdentifier []byte, outcome request.Outcome) *request.ReportOutcomeResult {
	// We must take the rotation lock to avoid rotation while updating the structures
	ft.rotationLock.RLock()
	defer ft.rotationLock.RUnlock()

	resp := ft.mainStructure.ReportOutcome(ctx, clientIdentifier, outcome)

	// To keep the bad workloads data "warm" in the rotated structure, we will update both
	ft.secondaryStructure.ReportOutcome(ctx, clientIdentifier, outcome)

	return resp
}

// Close stops the background rotation goroutine and releases ticker resources.
func (ft *FairnessTracker) Close() {
	close(ft.stopRotation)
	ft.ticker.Stop()
	if ft.cancel != nil {
		ft.cancel()
	}
	if ft.stateClient != nil {
		ft.stateClient.Stop()
	}
}

func createStructure(cfg *config.FairnessTrackerConfig, seed uint64, clock utils.IClock, client *client.Client) (request.Tracker, error) {
	st, err := newTrackerStructureWithClock(cfg, seed, cfg.IncludeStats, clock)
	if err != nil {
		return nil, err
	}

	if client != nil {
		if s, ok := st.(*data.Structure); ok {
			s.SetReportDeltaFunc(func(row, col uint64, delta float64, ts uint64) {
				client.SendDeltaUpdate(seed, []*statepb.BucketDelta{{
					RowId:            row,
					ColId:            col,
					DeltaProb:        delta,
					LastUpdateTimeMs: ts,
				}})
			})
		}
	}
	return st, nil
}

func (ft *FairnessTracker) onStateUpdate(resp *statepb.SyncResponse) {
	ft.rotationLock.RLock()
	defer ft.rotationLock.RUnlock()

	seed := resp.Seed

	// Check if this seed matches main or secondary
	if ft.mainStructure != nil && ft.mainStructure.GetID() == seed {
		applyUpdate(ft.mainStructure, resp.Buckets)
	} else if ft.secondaryStructure != nil && ft.secondaryStructure.GetID() == seed {
		applyUpdate(ft.secondaryStructure, resp.Buckets)
	}
}

func applyUpdate(tracker request.Tracker, buckets []*statepb.Bucket) {
	if s, ok := tracker.(*data.Structure); ok {
		for _, b := range buckets {
			s.UpdateBucket(b.RowId, b.ColId, b.Prob, b.LastUpdateTimeMs)
		}
	}
}
