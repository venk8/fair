package store

import (
	statepb "github.com/satmihir/fair/pkg/state/api/v1"
)

// Store defines the interface for the state service backend.
type Store interface {
	// ApplyDelta applies a delta to a bucket and returns the updated bucket state.
	ApplyDelta(seed, rowID, colID uint64, deltaProb float64, timestampMs uint64) (*statepb.Bucket, error)

	// GetSeed returns all buckets for a given seed.
	GetSeed(seed uint64) ([]*statepb.Bucket, error)

	// EvictBefore removes all data for seeds strictly less than the given seed.
	EvictBefore(seed uint64) error
}
