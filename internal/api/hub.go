package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Hub fans live events out to every connected admin browser over
// Server-Sent Events. SSE rather than WebSockets: the stream only flows one
// way, it survives every reverse proxy unchanged, and the browser reconnects
// on its own.
type Hub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

// NewHub returns an empty hub.
func NewHub() *Hub { return &Hub{clients: map[chan []byte]struct{}{}} }

// Publish sends an event to every client. Slow clients drop events rather
// than hold up the monitors.
func (h *Hub) Publish(kind string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	msg := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", kind, payload))
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c <- msg:
		default:
		}
	}
}

func (h *Hub) subscribe() chan []byte {
	c := make(chan []byte, 256)
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return c
}

func (h *Hub) unsubscribe(c chan []byte) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

// Clients returns how many browsers are connected.
func (h *Hub) Clients() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // nginx: do not buffer the stream
	c := s.hub.subscribe()
	defer s.hub.unsubscribe(c)

	fmt.Fprintf(w, "retry: 3000\nevent: hello\ndata: {\"time\":%d}\n\n", time.Now().UnixMilli())
	flusher.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-c:
			if _, err := w.Write(msg); err != nil {
				return
			}
			flusher.Flush()
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
