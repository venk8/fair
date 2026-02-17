package store

import (
	"context"
	"testing"
	"time"

	statepb "github.com/satmihir/fair/pkg/state/api/v1"
)

func TestInMemoryStore_ApplyDelta(t *testing.T) {
	s := NewInMemoryStore(time.Minute, 5*time.Minute, time.Second)

	var updated *statepb.Bucket
	var err error

	// Test adding a new bucket
	updated, err = s.ApplyDelta(1, 1, 1, 0.5, 1000)
	if err != nil {
		t.Fatalf("ApplyDelta failed: %v", err)
	}
	if updated.Prob != 0.5 {
		t.Errorf("Expected prob 0.5, got %f", updated.Prob)
	}
	if updated.LastUpdateTimeMs != 1000 {
		t.Errorf("Expected timestamp 1000, got %d", updated.LastUpdateTimeMs)
	}

	// Test updating existing bucket (additive)
	updated, err = s.ApplyDelta(1, 1, 1, 0.2, 2000)
	if err != nil {
		t.Fatalf("ApplyDelta failed: %v", err)
	}
	if updated.Prob != 0.7 {
		t.Errorf("Expected prob 0.7, got %f", updated.Prob)
	}
	if updated.LastUpdateTimeMs != 2000 {
		t.Errorf("Expected timestamp 2000, got %d", updated.LastUpdateTimeMs)
	}

	// Test clamping (max 1.0)
	updated, err = s.ApplyDelta(1, 1, 1, 0.5, 3000) // 0.7 + 0.5 = 1.2 -> 1.0
	if err != nil {
		t.Fatalf("ApplyDelta failed: %v", err)
	}
	if updated.Prob != 1.0 {
		t.Errorf("Expected prob 1.0, got %f", updated.Prob)
	}

	// Test clamping (min 0.0)
	updated, err = s.ApplyDelta(1, 1, 1, -1.5, 4000) // 1.0 - 1.5 = -0.5 -> 0.0
	if err != nil {
		t.Fatalf("ApplyDelta failed: %v", err)
	}
	if updated.Prob != 0.0 {
		t.Errorf("Expected prob 0.0, got %f", updated.Prob)
	}

	// Test timestamp logic (max-wins)
	updated, err = s.ApplyDelta(1, 1, 1, 0.1, 3500) // Older timestamp than 4000
	if err != nil {
		t.Fatalf("ApplyDelta failed: %v", err)
	}
	if updated.LastUpdateTimeMs != 4000 {
		t.Errorf("Expected timestamp to remain 4000, got %d", updated.LastUpdateTimeMs)
	}
}

func TestInMemoryStore_GetSeed(t *testing.T) {
	s := NewInMemoryStore(time.Minute, 5*time.Minute, time.Second)

	s.ApplyDelta(1, 1, 1, 0.5, 1000)
	s.ApplyDelta(1, 1, 2, 0.3, 1000)
	s.ApplyDelta(2, 1, 1, 0.9, 2000)

	buckets, err := s.GetSeed(1)
	if err != nil {
		t.Fatalf("GetSeed failed: %v", err)
	}
	if len(buckets) != 2 {
		t.Errorf("Expected 2 buckets for seed 1, got %d", len(buckets))
	}

	buckets, err = s.GetSeed(2)
	if err != nil {
		t.Fatalf("GetSeed failed: %v", err)
	}
	if len(buckets) != 1 {
		t.Errorf("Expected 1 bucket for seed 2, got %d", len(buckets))
	}

	buckets, err = s.GetSeed(3)
	if len(buckets) != 0 {
		t.Errorf("Expected 0 buckets for seed 3, got %d", len(buckets))
	}
}

func TestInMemoryStore_Eviction(t *testing.T) {
	// Window size 100ms
	// Eviction TTL 200ms (2 windows)
	// Tick 50ms
	s := NewInMemoryStore(100*time.Millisecond, 200*time.Millisecond, 50*time.Millisecond)

	// Start eviction loop
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	defer cancel()

	// Seed 1 (time ~100ms)
	s.ApplyDelta(1, 1, 1, 0.5, 100)
	// Seed 2 (time ~200ms)
	s.ApplyDelta(2, 1, 1, 0.5, 200)
	// Seed 3 (time ~300ms)
	s.ApplyDelta(3, 1, 1, 0.5, 300)

	// Wait a bit, nothing should be evicted yet if we assume "now" is advancing
	// But in test, "now" is real time.

	// Let's test EvictBefore directly first to avoid timing issues
	s.EvictBefore(2) // Evict seeds < 2 (so seed 1 should be gone)

	buckets, _ := s.GetSeed(1)
	if len(buckets) != 0 {
		t.Errorf("Seed 1 should be evicted")
	}
	buckets, _ = s.GetSeed(2)
	if len(buckets) == 0 {
		t.Errorf("Seed 2 should remain")
	}
}
