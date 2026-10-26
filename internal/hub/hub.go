package hub

import (
	"sync"
)

// Hub is a simple fan-out broadcast hub for SSE.
// Clients subscribe to receive HTML fragment strings.
type Hub struct {
	mu      sync.RWMutex
	clients map[chan string]struct{}
}

func New() *Hub {
	return &Hub{
		clients: make(map[chan string]struct{}),
	}
}

func (h *Hub) Subscribe() chan string {
	ch := make(chan string, 16)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) Unsubscribe(ch chan string) {
	h.mu.Lock()
	delete(h.clients, ch)
	close(ch)
	h.mu.Unlock()
}

// Broadcast sends an HTML fragment to all connected SSE clients.
func (h *Hub) Broadcast(html string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- html:
		default:
			// slow client, drop message
		}
	}
}

// ClientCount returns the number of connected SSE clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
