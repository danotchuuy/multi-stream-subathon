// Package ws implements a minimal broadcast hub: clients connect over a
// WebSocket scoped to one timer ID (the "room") and receive JSON state
// snapshots whenever the hub is told to broadcast one for that timer.
package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Overlay/dashboard pages may be served from a different origin during
	// local development (Vite on :5173 vs the API on :8090).
	CheckOrigin: func(r *http.Request) bool { return true },
}

type client struct {
	timerID string
	conn    *websocket.Conn
	send    chan []byte
}

// Hub tracks connected clients per timer ID and fans out broadcast
// messages to only the clients watching that timer.
type Hub struct {
	mu      sync.Mutex
	clients map[string]map[*client]struct{} // timerID -> clients
}

// NewHub creates an empty Hub.
func NewHub() *Hub {
	return &Hub{clients: make(map[string]map[*client]struct{})}
}

// Broadcast marshals v as JSON and sends it to every client connected to
// the given timer's room.
func (h *Hub) Broadcast(timerID string, v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		log.Printf("ws: marshal broadcast payload: %v", err)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients[timerID] {
		select {
		case c.send <- payload:
		default:
			// Client is too slow to keep up; drop it rather than block
			// the room.
			h.removeLocked(c)
		}
	}
}

// ServeHTTP upgrades the request to a WebSocket and registers the client
// against timerID until it disconnects.
func (h *Hub) ServeHTTP(timerID string, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws: upgrade: %v", err)
		return
	}

	c := &client{timerID: timerID, conn: conn, send: make(chan []byte, 16)}

	h.mu.Lock()
	if h.clients[timerID] == nil {
		h.clients[timerID] = make(map[*client]struct{})
	}
	h.clients[timerID][c] = struct{}{}
	h.mu.Unlock()

	go h.writePump(c)
	h.readPump(c)
}

// readPump discards incoming messages (clients only receive) but must keep
// reading so ping/pong control frames and disconnects are detected.
func (h *Hub) readPump(c *client) {
	defer h.disconnect(c)

	c.conn.SetReadLimit(512)
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (h *Hub) writePump(c *client) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *Hub) disconnect(c *client) {
	h.mu.Lock()
	h.removeLocked(c)
	h.mu.Unlock()
	_ = c.conn.Close()
}

// removeLocked removes c from its room. Caller must hold h.mu.
func (h *Hub) removeLocked(c *client) {
	room := h.clients[c.timerID]
	if _, ok := room[c]; !ok {
		return
	}
	delete(room, c)
	close(c.send)
	if len(room) == 0 {
		delete(h.clients, c.timerID)
	}
}
