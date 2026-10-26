package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strings"
)

// HandleSSE handles Server-Sent Events connections for real-time updates.
// GET /events
func (h *Handlers) HandleSSE(w http.ResponseWriter, r *http.Request) {
	// Ensure the response writer supports flushing.
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Subscribe to the hub.
	ch := h.Hub.Subscribe()
	defer h.Hub.Unsubscribe(ch)

	log.Printf("SSE client connected (total: %d)", h.Hub.ClientCount())

	// Use the request context to detect client disconnect.
	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			log.Printf("SSE client disconnected (total: %d)", h.Hub.ClientCount()-1)
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			// Write SSE event. Pages listen for "game-list" via
			// hx-trigger="sse:game-list" and re-fetch their own view.
			// Each line of data must be prefixed with "data: ".
			fmt.Fprintf(w, "event: game-list\n")
			for _, line := range strings.Split(msg, "\n") {
				fmt.Fprintf(w, "data: %s\n", line)
			}
			fmt.Fprintf(w, "\n")
			flusher.Flush()
		}
	}
}
