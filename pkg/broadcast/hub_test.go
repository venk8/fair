package broadcast

import (
	"testing"
	"time"

	statepb "github.com/satmihir/fair/pkg/state/api/v1"
)

func TestHub_Broadcast(t *testing.T) {
	h := NewHub()
	go h.Run()

	client1 := h.Register()
	client2 := h.Register()

	// Wait for registration
	time.Sleep(10 * time.Millisecond)

	msg := &statepb.SyncResponse{Seed: 123}
	h.Broadcast(msg)

	// Verify both clients received the message
	select {
	case received := <-client1.Send:
		if received.Seed != 123 {
			t.Errorf("Client 1 expected seed 123, got %d", received.Seed)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Client 1 timeout")
	}

	select {
	case received := <-client2.Send:
		if received.Seed != 123 {
			t.Errorf("Client 2 expected seed 123, got %d", received.Seed)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Client 2 timeout")
	}

	h.Unregister(client1)
	time.Sleep(10 * time.Millisecond) // Wait for unregister

	msg2 := &statepb.SyncResponse{Seed: 456}
	h.Broadcast(msg2)

	// Verify client 2 received message 2
	select {
	case received := <-client2.Send:
		if received.Seed != 456 {
			t.Errorf("Client 2 expected seed 456, got %d", received.Seed)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Client 2 timeout")
	}

	// Verify client 1 channel is closed or empty
	select {
	case _, ok := <-client1.Send:
		if ok {
			t.Error("Client 1 should not receive message")
		}
	default:
		// OK
	}
}
