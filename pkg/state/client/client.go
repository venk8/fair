package client

import (
	"context"
	"io"
	"log"
	"sync"
	"time"

	statepb "github.com/satmihir/fair/pkg/state/api/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client is a wrapper around the StateService client.
type Client struct {
	addr     string
	onUpdate func(*statepb.SyncResponse)

	sendCh chan *statepb.SyncRequest

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewClient creates a new Client.
func NewClient(addr string, onUpdate func(*statepb.SyncResponse)) *Client {
	return &Client{
		addr:     addr,
		onUpdate: onUpdate,
		sendCh:   make(chan *statepb.SyncRequest, 1024),
	}
}

// Start begins the client connection loop.
func (c *Client) Start(ctx context.Context) {
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.wg.Add(1)
	go c.run()
}

// Stop terminates the client connection loop.
func (c *Client) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
}

func (c *Client) run() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		if err := c.connectAndStream(); err != nil {
			// Don't log if context canceled (normal shutdown)
			if c.ctx.Err() == nil {
				log.Printf("State Service client error: %v. Reconnecting in 5s...", err)
			}
			select {
			case <-c.ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}
	}
}

func (c *Client) connectAndStream() error {
	// Dial connection
	// We use NewClient (for v1.64+) or Dial (for older).
	// Given we installed v1.79+, NewClient is preferred but Dial is still there.
	// We'll use NewClient.
	conn, err := grpc.NewClient(c.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	client := statepb.NewStateServiceClient(conn)
	stream, err := client.Sync(c.ctx)
	if err != nil {
		return err
	}

	// Create a channel to signal error from sender or receiver
	errCh := make(chan error, 2)

	// Sender goroutine
	go func() {
		for {
			select {
			case <-c.ctx.Done():
				return
			case req := <-c.sendCh:
				if err := stream.Send(req); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
			}
		}
	}()

	// Receiver goroutine
	go func() {
		for {
			resp, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					// Server closed stream gracefully
					select {
					case errCh <- io.EOF:
					default:
					}
					return
				}
				select {
				case errCh <- err:
				default:
				}
				return
			}
			if c.onUpdate != nil {
				c.onUpdate(resp)
			}
		}
	}()

	// Wait for first error or context cancel
	select {
	case <-c.ctx.Done():
		return c.ctx.Err()
	case err := <-errCh:
		return err
	}
}

// SendDeltaUpdate queues a delta update to be sent.
func (c *Client) SendDeltaUpdate(seed uint64, deltas []*statepb.BucketDelta) {
	req := &statepb.SyncRequest{
		Request: &statepb.SyncRequest_DeltaUpdate{
			DeltaUpdate: &statepb.DeltaUpdate{
				Seed:   seed,
				Deltas: deltas,
			},
		},
	}
	select {
	case c.sendCh <- req:
	default:
		log.Printf("State Service client buffer full, dropping delta update")
	}
}

// RequestFullState queues a request for full state.
func (c *Client) RequestFullState(seed uint64) {
	req := &statepb.SyncRequest{
		Request: &statepb.SyncRequest_StateRequest{
			StateRequest: &statepb.StateRequest{
				Seed: seed,
			},
		},
	}
	select {
	case c.sendCh <- req:
	default:
		log.Printf("State Service client buffer full, dropping state request")
	}
}
