package store

import (
	"context"
	"sync"
	"time"

	statepb "github.com/satmihir/fair/pkg/state/api/v1"
)

// InMemoryStore implements the Store interface using a thread-safe map.
type InMemoryStore struct {
	mu           sync.RWMutex
	buckets      map[uint64]map[uint64]map[uint64]*statepb.Bucket // seed -> row -> col -> bucket
	windowSize   time.Duration
	evictionTTL  time.Duration
	evictionTick time.Duration
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// NewInMemoryStore creates a new InMemoryStore.
func NewInMemoryStore(windowSize, evictionTTL, evictionTick time.Duration) *InMemoryStore {
	return &InMemoryStore{
		buckets:      make(map[uint64]map[uint64]map[uint64]*statepb.Bucket),
		windowSize:   windowSize,
		evictionTTL:  evictionTTL,
		evictionTick: evictionTick,
	}
}

// ApplyDelta applies a delta to a bucket and returns the updated bucket state.
func (s *InMemoryStore) ApplyDelta(seed, rowID, colID uint64, deltaProb float64, timestampMs uint64) (*statepb.Bucket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Initialize maps if they don't exist
	if s.buckets[seed] == nil {
		s.buckets[seed] = make(map[uint64]map[uint64]*statepb.Bucket)
	}
	if s.buckets[seed][rowID] == nil {
		s.buckets[seed][rowID] = make(map[uint64]*statepb.Bucket)
	}

	bucket, exists := s.buckets[seed][rowID][colID]
	if !exists {
		bucket = &statepb.Bucket{
			RowId:  rowID,
			ColId:  colID,
			Prob:   0.0,
			LastUpdateTimeMs: 0,
		}
		s.buckets[seed][rowID][colID] = bucket
	}

	// Apply aggregation logic
	bucket.Prob += deltaProb

	// Clamp probability to [0.0, 1.0]
	if bucket.Prob < 0.0 {
		bucket.Prob = 0.0
	} else if bucket.Prob > 1.0 {
		bucket.Prob = 1.0
	}

	// Max-Timestamp-Wins
	if timestampMs > bucket.LastUpdateTimeMs {
		bucket.LastUpdateTimeMs = timestampMs
	}

	// Return a copy to avoid race conditions if the caller modifies it (though returning pointer to protobuf is standard)
	// Here we return the pointer to the stored bucket, but since we are under lock and the caller presumably just reads it or serializes it, it should be fine.
	// For safety, let's return a copy.
	return &statepb.Bucket{
		RowId:            bucket.RowId,
		ColId:            bucket.ColId,
		Prob:             bucket.Prob,
		LastUpdateTimeMs: bucket.LastUpdateTimeMs,
	}, nil
}

// GetSeed returns all buckets for a given seed.
func (s *InMemoryStore) GetSeed(seed uint64) ([]*statepb.Bucket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seedMap, exists := s.buckets[seed]
	if !exists {
		return []*statepb.Bucket{}, nil
	}

	var result []*statepb.Bucket
	for _, rowMap := range seedMap {
		for _, bucket := range rowMap {
			result = append(result, &statepb.Bucket{
				RowId:            bucket.RowId,
				ColId:            bucket.ColId,
				Prob:             bucket.Prob,
				LastUpdateTimeMs: bucket.LastUpdateTimeMs,
			})
		}
	}
	return result, nil
}

// EvictBefore removes all data for seeds strictly less than the given seed.
func (s *InMemoryStore) EvictBefore(seed uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for sKey := range s.buckets {
		if sKey < seed {
			delete(s.buckets, sKey)
		}
	}
	return nil
}

// Start begins the periodic eviction process.
func (s *InMemoryStore) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.wg.Add(1)

	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.evictionTick)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runEviction()
			}
		}
	}()
}

// Stop halts the periodic eviction process.
func (s *InMemoryStore) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *InMemoryStore) runEviction() {
	// Calculate the cutoff seed
	// seed = timestamp / windowSize
	// We want to evict seeds older than current_time - evictionTTL
	cutoffTime := time.Now().Add(-s.evictionTTL)
	cutoffSeed := uint64(cutoffTime.UnixMilli()) / uint64(s.windowSize.Milliseconds())

	_ = s.EvictBefore(cutoffSeed)
}
