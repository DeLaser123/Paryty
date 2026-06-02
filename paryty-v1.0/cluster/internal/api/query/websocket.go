package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage"
	"go.uber.org/zap"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// WebSocketHandler handles WebSocket connections.
type WebSocketHandler struct {
	store     *storage.Store
	logger    *zap.Logger
	clients   sync.Map // map[*websocket.Conn]bool
	broadcast chan []byte
}

// NewWebSocketHandler creates a new WebSocket handler.
func NewWebSocketHandler(store *storage.Store, logger *zap.Logger) *WebSocketHandler {
	h := &WebSocketHandler{
		store:     store,
		logger:    logger,
		broadcast: make(chan []byte, 256),
	}
	go h.broadcastLoop()
	return h
}

// HandleWebSocket handles WebSocket upgrade and message handling.
func (h *WebSocketHandler) HandleWebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Error("WebSocket upgrade failed", zap.Error(err))
		return
	}
	defer conn.Close()

	// Extract tenant from query parameter on initial connection.
	tenant := c.DefaultQuery("tenant", "default")

	h.clients.Store(conn, true)
	h.logger.Info("Client connected",
		zap.String("remote", conn.RemoteAddr().String()),
		zap.String("tenant", tenant),
	)

	defer func() {
		h.clients.Delete(conn)
		h.logger.Info("Client disconnected", zap.String("remote", conn.RemoteAddr().String()))
	}()

	// Read messages (subscriptions)
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				h.logger.Error("WebSocket read error", zap.Error(err))
			}
			break
		}

		var msg WSMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			h.logger.Error("Invalid message", zap.Error(err))
			continue
		}

		h.handleMessage(conn, tenant, &msg)
	}
}

// WSMessage represents a WebSocket message.
type WSMessage struct {
	Type    string          `json:"type"`
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
}

// WSResponse represents a WebSocket response.
type WSResponse struct {
	Type    string      `json:"type"`
	Topic   string      `json:"topic"`
	Payload interface{} `json:"payload"`
	Time    time.Time   `json:"time"`
}

func (h *WebSocketHandler) handleMessage(conn *websocket.Conn, tenant string, msg *WSMessage) {
	switch msg.Type {
	case "subscribe":
		h.handleSubscribe(conn, tenant, msg.Topic)
	case "unsubscribe":
		h.handleUnsubscribe(conn, msg.Topic)
	case "ping":
		h.sendMessage(conn, &WSResponse{
			Type: "pong",
			Time: time.Now(),
		})
	default:
		h.logger.Warn("Unknown message type", zap.String("type", msg.Type))
	}
}

func (h *WebSocketHandler) handleSubscribe(conn *websocket.Conn, tenant, topic string) {
	h.logger.Info("Client subscribed",
		zap.String("tenant", tenant),
		zap.String("topic", topic),
	)

	// Send initial data based on topic
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch topic {
	case "topology":
		topo, err := h.store.GetTopology(ctx, tenant)
		if err == nil {
			h.sendMessage(conn, &WSResponse{
				Type:    "topology",
				Topic:   topic,
				Payload: topo,
				Time:    time.Now(),
			})
		}
	case "alerts":
		alerts, err := h.store.GetActiveAlerts(ctx, tenant)
		if err == nil {
			h.sendMessage(conn, &WSResponse{
				Type:    "alerts",
				Topic:   topic,
				Payload: alerts,
				Time:    time.Now(),
			})
		}
	}
}

func (h *WebSocketHandler) handleUnsubscribe(conn *websocket.Conn, topic string) {
	h.logger.Info("Client unsubscribed", zap.String("topic", topic))
}

func (h *WebSocketHandler) sendMessage(conn *websocket.Conn, msg *WSResponse) {
	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("Marshal message failed", zap.Error(err))
		return
	}
	conn.WriteMessage(websocket.TextMessage, data) //nolint:errcheck
}

// Broadcast sends a message to all connected clients.
func (h *WebSocketHandler) Broadcast(topic string, payload interface{}) {
	msg := &WSResponse{
		Type:    "update",
		Topic:   topic,
		Payload: payload,
		Time:    time.Now(),
	}
	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("Marshal broadcast failed", zap.Error(err))
		return
	}
	h.broadcast <- data
}

func (h *WebSocketHandler) broadcastLoop() {
	for msg := range h.broadcast {
		h.clients.Range(func(key, value interface{}) bool {
			conn := key.(*websocket.Conn)
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				h.logger.Error("Broadcast failed", zap.Error(err))
				conn.Close()
				h.clients.Delete(conn)
			}
			return true
		})
	}
}
