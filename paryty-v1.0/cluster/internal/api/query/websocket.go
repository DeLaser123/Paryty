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
	"github.com/twmb/franz-go/pkg/kgo"
	"go.uber.org/zap"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// clientState holds per-connection metadata including tenant and subscriptions.
type clientState struct {
	tenant        string
	subscriptions map[string]bool // channel → subscribed?
}

// WebSocketHandler handles WebSocket connections.
type WebSocketHandler struct {
	store       *storage.Store
	logger      *zap.Logger
	clients     sync.Map // map[*websocket.Conn]*clientState
	broadcast   chan []byte
	eventFanout chan []byte // fan-in from Redpanda events consumer
	mu          sync.RWMutex
}

// NewWebSocketHandler creates a new WebSocket handler.
func NewWebSocketHandler(store *storage.Store, logger *zap.Logger) *WebSocketHandler {
	h := &WebSocketHandler{
		store:       store,
		logger:      logger,
		broadcast:   make(chan []byte, 256),
		eventFanout: make(chan []byte, 256),
	}
	go h.broadcastLoop()
	go h.eventFanoutLoop()
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

	state := &clientState{
		tenant:        tenant,
		subscriptions: make(map[string]bool),
	}

	h.clients.Store(conn, state)
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

		h.handleMessage(conn, state, &msg)
	}
}

// WSMessage represents a WebSocket message.
// Accepts both "channel" (frontend) and "topic" (legacy) fields.
type WSMessage struct {
	Type    string          `json:"type"`
	Channel string          `json:"channel"`
	Topic   string          `json:"topic"`
	ID      string          `json:"id"`
	Payload json.RawMessage `json:"payload"`
}

// topic returns the effective topic, preferring channel over topic.
func (m *WSMessage) effectiveTopic() string {
	if m.Channel != "" {
		return m.Channel
	}
	return m.Topic
}

// WSResponse represents a WebSocket response matching the frontend's WsIncomingMessage format.
type WSResponse struct {
	Type    string      `json:"type"`
	Channel string      `json:"channel"`
	Data    interface{} `json:"data"`
	ID      string      `json:"id,omitempty"`
	Time    time.Time   `json:"timestamp"`
}

func (h *WebSocketHandler) handleMessage(conn *websocket.Conn, state *clientState, msg *WSMessage) {
	topic := msg.effectiveTopic()
	switch msg.Type {
	case "subscribe":
		h.handleSubscribe(conn, state, state.tenant, topic)
	case "unsubscribe":
		h.handleUnsubscribe(state, topic)
	case "ping":
		h.sendMessage(conn, &WSResponse{
			Type: "pong",
			Time: time.Now(),
		})
	default:
		h.logger.Warn("Unknown message type", zap.String("type", msg.Type))
	}
}

func (h *WebSocketHandler) handleSubscribe(conn *websocket.Conn, state *clientState, tenant, topic string) {
	h.logger.Info("Client subscribed",
		zap.String("tenant", tenant),
		zap.String("topic", topic),
	)

	// Track subscription in per-client state
	state.subscriptions[topic] = true

	// Send initial data based on topic
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch topic {
	case "topology":
		topo, err := h.store.GetTopology(ctx, tenant)
		if err == nil && topo != nil {
			h.sendMessage(conn, &WSResponse{
				Type:    "topology",
				Channel: topic,
				Data:    toFrontendTopology(topo),
				Time:    time.Now(),
			})
		}
	case "alerts":
		alerts, err := h.store.GetActiveAlerts(ctx, tenant)
		if err == nil {
			h.sendMessage(conn, &WSResponse{
				Type:    "alerts",
				Channel: topic,
				Data:    alerts,
				Time:    time.Now(),
			})
		}
	case "events":
		// Events are streaming-only — no initial data.
		// REST handles catch-up via EventQuery.
	}
}

func (h *WebSocketHandler) handleUnsubscribe(state *clientState, topic string) {
	h.logger.Info("Client unsubscribed", zap.String("topic", topic))
	delete(state.subscriptions, topic)
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
		Channel: topic,
		Data:    payload,
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

// eventFanoutLoop fans out Redpanda events only to clients subscribed to "events".
func (h *WebSocketHandler) eventFanoutLoop() {
	for msg := range h.eventFanout {
		h.clients.Range(func(key, value interface{}) bool {
			conn := key.(*websocket.Conn)
			state := value.(*clientState)
			if state.subscriptions["events"] {
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					h.logger.Error("Event fanout failed", zap.Error(err))
					conn.Close()
					h.clients.Delete(conn)
				}
			}
			return true
		})
	}
}

// StartEventConsumer creates a franz-go consumer reading from the events topic
// and fans out event messages via eventFanout to subscribed WebSocket clients.
func (h *WebSocketHandler) StartEventConsumer(ctx context.Context, brokers []string, tenant string) error {
	topic := "paryty." + tenant + ".events"

	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup("ws-event-fanout-"+tenant),
		kgo.ConsumeTopics(topic),
	)
	if err != nil {
		return err
	}

	h.logger.Info("Event consumer started",
		zap.String("topic", topic),
		zap.Strings("brokers", brokers),
	)

	go func() {
		defer client.Close()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			fetches := client.PollFetches(ctx)
			if fetches.IsClientClosed() {
				return
			}
			if errs := fetches.Errors(); len(errs) > 0 {
				h.logger.Error("Event consumer fetch errors", zap.Int("count", len(errs)))
				continue
			}

			fetches.EachRecord(func(record *kgo.Record) {
				msg := &WSResponse{
					Type:    "update",
					Channel: "events",
					Data:    json.RawMessage(record.Value),
					Time:    time.Now(),
				}
				data, err := json.Marshal(msg)
				if err != nil {
					h.logger.Error("Marshal event failed", zap.Error(err))
					return
				}
				select {
				case h.eventFanout <- data:
				default:
					// Drop if fanout channel is full (backpressure)
				}
			})
		}
	}()

	return nil
}
