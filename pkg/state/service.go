package state

import (
	"io"
	"log"

	"github.com/satmihir/fair/pkg/broadcast"
	statepb "github.com/satmihir/fair/pkg/state/api/v1"
	"github.com/satmihir/fair/pkg/state/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Service implements the StateServiceServer interface.
type Service struct {
	statepb.UnimplementedStateServiceServer
	store *store.InMemoryStore
	hub   *broadcast.Hub
}

// NewService creates a new Service.
func NewService(store *store.InMemoryStore, hub *broadcast.Hub) *Service {
	return &Service{
		store: store,
		hub:   hub,
	}
}

// Sync handles the bidirectional stream for delta submission and state reception.
func (s *Service) Sync(stream statepb.StateService_SyncServer) error {
	// Register the client with the hub.
	client := s.hub.Register()
	defer s.hub.Unregister(client)

	// Create a channel to signal when the stream is closed by the client
	done := make(chan struct{})

	// Start a goroutine to send updates from the hub to the client
	go func() {
		defer close(done)
		for msg := range client.Send {
			if err := stream.Send(msg); err != nil {
				log.Printf("Failed to send update to client: %v", err)
				return
			}
		}
	}()

	// Read messages from the client
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Unknown, "failed to read request: %v", err)
		}

		switch payload := req.Request.(type) {
		case *statepb.SyncRequest_DeltaUpdate:
			// Process delta update
			deltaUpdate := payload.DeltaUpdate
			seed := deltaUpdate.Seed
			for _, delta := range deltaUpdate.Deltas {
				bucket, err := s.store.ApplyDelta(seed, delta.RowId, delta.ColId, delta.DeltaProb, delta.LastUpdateTimeMs)
				if err != nil {
					log.Printf("Failed to apply delta: %v", err)
					continue
				}

				// Broadcast the updated bucket
				resp := &statepb.SyncResponse{
					Seed:    seed,
					Buckets: []*statepb.Bucket{bucket},
				}
				s.hub.Broadcast(resp)
			}

		case *statepb.SyncRequest_StateRequest:
			// Client requested full state for a seed
			seed := payload.StateRequest.Seed
			buckets, err := s.store.GetSeed(seed)
			if err != nil {
				log.Printf("Failed to get seed state: %v", err)
				continue
			}

			// Send the full state back to THIS client only.
			// Use the channel to serialize with broadcasts.
			resp := &statepb.SyncResponse{
				Seed:    seed,
				Buckets: buckets,
			}
			select {
			case client.Send <- resp:
			case <-stream.Context().Done():
				return stream.Context().Err()
			}

		default:
			log.Printf("Received unknown request type")
		}
	}
}
