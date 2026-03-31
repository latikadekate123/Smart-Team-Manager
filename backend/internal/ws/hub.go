package ws

import (
	"context"
	"log"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

type Hub struct {
	mu         sync.RWMutex
	clients    map[string]map[*websocket.Conn]struct{}
	redis      *redis.Client
	hasRedis   bool
	pubStarted bool
}

func NewHub(redisClient *redis.Client) *Hub {
	return &Hub{
		clients:  make(map[string]map[*websocket.Conn]struct{}),
		redis:    redisClient,
		hasRedis: redisClient != nil,
	}
}

func (h *Hub) StartRedisSubscriber(ctx context.Context) {
	if !h.hasRedis || h.pubStarted {
		return
	}
	h.pubStarted = true
	go func() {
		pubsub := h.redis.PSubscribe(ctx, "crdt:*")
		defer pubsub.Close()
		ch := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				boardID := msg.Channel[len("crdt:"):]
				h.broadcast(boardID, []byte(msg.Payload), nil)
			}
		}
	}()
}

func (h *Hub) Register(boardID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[boardID]; !ok {
		h.clients[boardID] = make(map[*websocket.Conn]struct{})
	}
	h.clients[boardID][conn] = struct{}{}
}

func (h *Hub) Unregister(boardID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[boardID]; !ok {
		return
	}
	delete(h.clients[boardID], conn)
	if len(h.clients[boardID]) == 0 {
		delete(h.clients, boardID)
	}
}

func (h *Hub) PublishUpdate(ctx context.Context, boardID string, payload []byte, source *websocket.Conn) {
	if h.hasRedis {
		if err := h.redis.Publish(ctx, "crdt:"+boardID, payload).Err(); err != nil {
			log.Printf("redis publish failed: %v", err)
			h.broadcast(boardID, payload, source)
		}
		return
	}
	h.broadcast(boardID, payload, source)
}

func (h *Hub) broadcast(boardID string, payload []byte, source *websocket.Conn) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for conn := range h.clients[boardID] {
		if source != nil && conn == source {
			continue
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
			log.Printf("ws broadcast error: %v", err)
		}
	}
}
