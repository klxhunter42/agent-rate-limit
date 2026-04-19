package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	wsWriteWait  = 10 * time.Second
	wsPongWait   = 60 * time.Second
	wsPingPeriod = (wsPongWait * 9) / 10
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// WSEvent is the JSON message sent to all connected dashboard clients.
type WSEvent struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data"`
	Timestamp string      `json:"timestamp"`
}

// WSClient represents a single connected WebSocket client.
type WSClient struct {
	hub  *WebSocketHub
	conn *websocket.Conn
	send chan []byte
}

// WebSocketHub maintains the set of active clients and broadcasts events.
type WebSocketHub struct {
	mu      sync.Mutex
	clients map[*WSClient]struct{}
}

// NewWebSocketHub creates a new hub.
func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{clients: make(map[*WSClient]struct{})}
}

// Run starts the hub. It is a no-op currently but provided for future use
// (e.g. buffered broadcast, cleanup goroutines).
func (h *WebSocketHub) Run() {}

// Register adds a client to the hub.
func (h *WebSocketHub) Register(c *WSClient) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

// Unregister removes a client from the hub.
func (h *WebSocketHub) Unregister(c *WSClient) {
	h.mu.Lock()
	delete(h.clients, c)
	close(c.send)
	h.mu.Unlock()
}

// Broadcast sends an event to all connected clients.
func (h *WebSocketHub) Broadcast(eventType string, data interface{}) {
	msg := WSEvent{
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.Marshal(msg)
	if err != nil {
		slog.Error("ws broadcast marshal error", "error", err)
		return
	}

	// Snapshot clients under lock, then release before sending.
	h.mu.Lock()
	clients := make([]*WSClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()

	var toClose []*WSClient
	for _, c := range clients {
		select {
		case c.send <- b:
		default:
			toClose = append(toClose, c)
		}
	}
	// Close slow clients outside the send loop to avoid deadlock.
	for _, c := range toClose {
		h.closeClient(c)
	}
}

func (h *WebSocketHub) closeClient(c *WSClient) {
	h.Unregister(c)
	c.conn.Close()
}

func (c *WSClient) readPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
	}()
	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Debug("ws read error", "error", err)
			}
			break
		}
	}
}

func (c *WSClient) writePump() {
	ticker := time.NewTicker(wsPingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				slog.Debug("ws write error", "error", err)
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// HandleWebSocket upgrades an HTTP connection to WebSocket and registers the client.
func HandleWebSocket(hub *WebSocketHub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Debug("ws upgrade error", "error", err)
		return
	}
	client := &WSClient{
		hub:  hub,
		conn: conn,
		send: make(chan []byte, 256),
	}
	hub.Register(client)
	go client.writePump()
	go client.readPump()
}
