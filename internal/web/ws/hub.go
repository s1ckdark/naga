package ws

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512 * 1024 // 512KB
)

// MessageType identifies the WebSocket message type
type MessageType string

const (
	MsgTaskAssign    MessageType = "task.assign"
	MsgTaskResult    MessageType = "task.result"
	MsgTaskCancel    MessageType = "task.cancel"
	MsgTaskStatus    MessageType = "task.status"
	MsgCapabilityReg MessageType = "capability.register"
	MsgMetrics       MessageType = "metrics"
	MsgPing          MessageType = "ping"
	MsgPong          MessageType = "pong"
)

// Message is the WebSocket message envelope
type Message struct {
	Type      MessageType     `json:"type"`
	DeviceID  string          `json:"deviceId,omitempty"`
	TaskID    string          `json:"taskId,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// Client represents a connected WebSocket client (device)
type Client struct {
	Hub      *Hub
	Conn     *websocket.Conn
	DeviceID string
	Send     chan []byte
	mu       sync.Mutex
}

// Hub manages WebSocket connections
type Hub struct {
	clients      map[string]*Client // deviceID -> client
	register     chan *Client
	unregister   chan *Client
	broadcast    chan []byte
	mu           sync.RWMutex
	onMessage    func(client *Client, msg *Message) // callback for message handling
	onDisconnect func(deviceID string)              // callback when a client disconnects
}

// NewHub creates a new WebSocket hub
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, 256),
	}
}

// SetMessageHandler sets the callback for incoming messages
func (h *Hub) SetMessageHandler(handler func(client *Client, msg *Message)) {
	h.onMessage = handler
}

// SetDisconnectHandler sets the callback invoked when a client disconnects
func (h *Hub) SetDisconnectHandler(handler func(deviceID string)) {
	h.onDisconnect = handler
}

// Register registers a client with the hub
func (h *Hub) Register(client *Client) {
	h.register <- client
}

// Run starts the hub's main loop
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			// Close existing connection for same device
			if old, ok := h.clients[client.DeviceID]; ok {
				close(old.Send)
				old.Conn.Close()
			}
			h.clients[client.DeviceID] = client
			h.mu.Unlock()
			log.Printf("[ws] client connected: %s", client.DeviceID)

		case client := <-h.unregister:
			h.mu.Lock()
			if c, ok := h.clients[client.DeviceID]; ok && c == client {
				delete(h.clients, client.DeviceID)
				close(client.Send)
				if h.onDisconnect != nil {
					go h.onDisconnect(client.DeviceID)
				}
			}
			h.mu.Unlock()
			log.Printf("[ws] client disconnected: %s", client.DeviceID)

		case message := <-h.broadcast:
			h.mu.RLock()
			for _, client := range h.clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(h.clients, client.DeviceID)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// SendToDevice sends a message to a specific device
func (h *Hub) SendToDevice(deviceID string, msg *Message) error {
	h.mu.RLock()
	client, ok := h.clients[deviceID]
	h.mu.RUnlock()

	if !ok {
		return ErrClientNotConnected
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	select {
	case client.Send <- data:
		return nil
	default:
		return ErrSendBufferFull
	}
}

// IsConnected checks if a device is connected
func (h *Hub) IsConnected(deviceID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.clients[deviceID]
	return ok
}

// ConnectedDevices returns list of connected device IDs
func (h *Hub) ConnectedDevices() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]string, 0, len(h.clients))
	for id := range h.clients {
		ids = append(ids, id)
	}
	return ids
}

// ReadPump pumps messages from the WebSocket connection to the hub
func (c *Client) ReadPump() {
	defer func() {
		c.Hub.unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, data, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[ws] read error from %s: %v", c.DeviceID, err)
			}
			break
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("[ws] invalid message from %s: %v", c.DeviceID, err)
			continue
		}
		msg.DeviceID = c.DeviceID

		if c.Hub.onMessage != nil {
			c.Hub.onMessage(c, &msg)
		}
	}
}

// WritePump pumps messages from the hub to the WebSocket connection
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
