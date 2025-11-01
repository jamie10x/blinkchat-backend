package websocket

import (
	"bytes"
	"encoding/json"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 2048
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

// Client bridges a WebSocket connection with the hub.
type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	send   chan []byte
	userID uuid.UUID
}

// NewClient constructs a Client for the given hub connection.
func NewClient(hub *Hub, conn *websocket.Conn, userID uuid.UUID) *Client {
	return &Client{
		hub:    hub,
		conn:   conn,
		send:   make(chan []byte, 256),
		userID: userID,
	}
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
		log.Printf("Client %s (User: %s) readPump: Unregistered and connection closed.", c.conn.RemoteAddr(), c.userID)
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		messageType, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Client %s (User: %s) readPump error: %v", c.conn.RemoteAddr(), c.userID, err)
			} else {
				log.Printf("Client %s (User: %s) readPump: Connection closed: %v", c.conn.RemoteAddr(), c.userID, err)
			}
			break
		}

		if messageType == websocket.TextMessage {
			message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
			log.Printf("Client %s (User: %s) readPump: Received message: %s", c.conn.RemoteAddr(), c.userID, message)

			hubMessage := HubMessage{
				client:  c,
				rawJSON: message,
			}
			c.hub.processMessage <- hubMessage
		} else {
			log.Printf("Client %s (User: %s) readPump: Received non-text message type: %d", c.conn.RemoteAddr(), c.userID, messageType)
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
		log.Printf("Client %s (User: %s) writePump: Ticker stopped and connection closed.", c.conn.RemoteAddr(), c.userID)
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				log.Printf("Client %s (User: %s) writePump: Hub closed send channel.", c.conn.RemoteAddr(), c.userID)
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				log.Printf("Client %s (User: %s) writePump: Error getting next writer: %v", c.conn.RemoteAddr(), c.userID, err)
				return
			}

			if _, err = w.Write(message); err != nil {
				log.Printf("Client %s (User: %s) writePump: Error writing message: %v", c.conn.RemoteAddr(), c.userID, err)
			}

			if err := w.Close(); err != nil {
				log.Printf("Client %s (User: %s) writePump: Error closing writer: %v", c.conn.RemoteAddr(), c.userID, err)
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("Client %s (User: %s) writePump: Error sending ping: %v", c.conn.RemoteAddr(), c.userID, err)
				return
			}
		}
	}
}

// SendMessage places a WebSocketMessage onto the outbound queue for this client.
func (c *Client) SendMessage(msgType string, payload interface{}) {
	wsMsg := WebSocketMessage{
		Type:    msgType,
		Payload: payload,
	}
	jsonMsg, err := json.Marshal(wsMsg)
	if err != nil {
		log.Printf("Client %s (User: %s) SendMessage: Error marshalling message: %v", c.conn.RemoteAddr(), c.userID, err)
		return
	}

	select {
	case c.send <- jsonMsg:
	default:
		log.Printf("Client %s (User: %s) SendMessage: Send channel full. Dropping message of type %s.", c.conn.RemoteAddr(), c.userID, msgType)
	}
}

// HubMessage holds raw JSON from a client awaiting processing.
type HubMessage struct {
	client  *Client
	rawJSON []byte
}
