package broadcast

import (
	statepb "github.com/satmihir/fair/pkg/state/api/v1"
)

type Client struct {
	hub *Hub
	// Buffered channel of outbound messages.
	Send chan *statepb.SyncResponse
}

type Hub struct {
	// Registered clients.
	clients map[*Client]bool

	// Inbound messages from the clients.
	broadcast chan *statepb.SyncResponse

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client
}

func NewHub() *Hub {
	return &Hub{
		broadcast:  make(chan *statepb.SyncResponse),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
			}
		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(h.clients, client)
				}
			}
		}
	}
}

// Broadcast sends a message to all connected clients.
func (h *Hub) Broadcast(msg *statepb.SyncResponse) {
	h.broadcast <- msg
}

// Register creates a new client and registers it with the hub.
func (h *Hub) Register() *Client {
	client := &Client{
		hub:  h,
		Send: make(chan *statepb.SyncResponse, 256),
	}
	h.register <- client
	return client
}

// Unregister removes a client from the hub.
func (h *Hub) Unregister(client *Client) {
	h.unregister <- client
}
