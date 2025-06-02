package websocket

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"log"
	"time"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 2048 // bytes, adjust as needed for your message content (e.g., 4096 for longer text)
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

// Client is a middleman between the WebSocket connection and the hub.
type Client struct {
	hub *Hub // Reference to the hub that manages this client

	// The WebSocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	send chan []byte // This will send raw []byte (JSON marshalled messages)

	userID uuid.UUID // The UserID of the connected user
}

// NewClient creates a new Client instance.
func NewClient(hub *Hub, conn *websocket.Conn, userID uuid.UUID) *Client {
	return &Client{
		hub:    hub,
		conn:   conn,
		send:   make(chan []byte, 256), // Buffered channel
		userID: userID,
	}
}

// readPump pumps messages from the WebSocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) readPump() {
	defer func() {
		// When this goroutine exits, unregister the client and close the connection
		c.hub.unregister <- c
		c.conn.Close()
		log.Printf("Client %s (User: %s) readPump: Unregistered and connection closed.", c.conn.RemoteAddr(), c.userID)
	}()

	c.conn.SetReadLimit(maxMessageSize)
	// Set a deadline for reading the next pong message.
	// If no pong is received within pongWait, the connection is considered dead.
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		// When a pong is received, extend the read deadline.
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		// ReadMessage blocks until a message is received or an error occurs.
		messageType, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Client %s (User: %s) readPump error: %v", c.conn.RemoteAddr(), c.userID, err)
			} else {
				log.Printf("Client %s (User: %s) readPump: Connection closed normally or expected error: %v", c.conn.RemoteAddr(), c.userID, err)
			}
			break // Exit loop on error (which triggers defer)
		}

		// We typically expect text messages containing JSON.
		// Binary messages could be handled if needed.
		if messageType == websocket.TextMessage {
			// Clean up the message (optional, depends on client behavior)
			message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))

			// Here, we need to parse the `message` (which is []byte) into our WebSocketMessage struct
			// and then decide what to do with it based on its type.
			// This logic will likely involve passing it to the hub for processing.
			// For now, let's just log it and prepare to send it to the hub's broadcast or a processing channel.

			log.Printf("Client %s (User: %s) readPump: Received message: %s", c.conn.RemoteAddr(), c.userID, message)

			// Example: Create a structure to send to the hub for processing
			hubMessage := HubMessage{
				client:  c,
				rawJSON: message, // The raw JSON bytes from the client
			}
			// Send to a hub processing channel
			c.hub.processMessage <- hubMessage // We will define processMessage channel in Hub

		} else {
			log.Printf("Client %s (User: %s) readPump: Received non-text message type: %d", c.conn.RemoteAddr(), c.userID, messageType)
		}
	}
}

// writePump pumps messages from the hub to the WebSocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) writePump() {
	// Create a ticker that sends a ping message periodically.
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close() // Ensure connection is closed if writePump exits
		log.Printf("Client %s (User: %s) writePump: Ticker stopped and connection closed.", c.conn.RemoteAddr(), c.userID)
		// Note: Unregistration is handled by readPump's defer or if hub explicitly closes client.
	}()

	for {
		select {
		case message, ok := <-c.send:
			// Set a deadline for writing the message.
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				log.Printf("Client %s (User: %s) writePump: Hub closed send channel.", c.conn.RemoteAddr(), c.userID)
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return // Exit goroutine
			}

			// Get a writer for the next message.
			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				log.Printf("Client %s (User: %s) writePump: Error getting next writer: %v", c.conn.RemoteAddr(), c.userID, err)
				return // Exit goroutine
			}
			// Write the message bytes.
			_, err = w.Write(message)
			if err != nil {
				log.Printf("Client %s (User: %s) writePump: Error writing message: %v", c.conn.RemoteAddr(), c.userID, err)
				// No return here, let the loop continue or be broken by ping ticker or channel close
			}

			// Add queued chat messages to the current WebSocket message.
			// This is an optimization to send multiple queued messages in one WebSocket frame if available.
			// n := len(c.send)
			// for i := 0; i < n; i++ {
			// 	_, _ = w.Write(newline) // Optional: separator if sending multiple JSON objects in one frame
			// 	_, _ = w.Write(<-c.send)
			// }

			// Close the writer to flush the message to the connection.
			if err := w.Close(); err != nil {
				log.Printf("Client %s (User: %s) writePump: Error closing writer: %v", c.conn.RemoteAddr(), c.userID, err)
				return // Exit goroutine
			}

		case <-ticker.C:
			// Periodically send a ping message to keep the connection alive
			// and to check if the client is still responsive.
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("Client %s (User: %s) writePump: Error sending ping: %v", c.conn.RemoteAddr(), c.userID, err)
				return // Exit goroutine on ping error (connection likely dead)
			}
			// log.Printf("Client %s (User: %s) writePump: Sent ping", c.conn.RemoteAddr(), c.userID)
		}
	}
}

// SendMessage is a helper method to send a structured WebSocketMessage to this client.
// It marshals the message to JSON and puts it on the send channel.
func (c *Client) SendMessage(msgType string, payload interface{}) {
	wsMsg := WebSocketMessage{
		Type:    msgType,
		Payload: payload,
	}
	jsonMsg, err := json.Marshal(wsMsg)
	if err != nil {
		log.Printf("Client %s (User: %s) SendMessage: Error marshalling message: %v", c.conn.RemoteAddr(), c.userID, err)
		// Optionally, send an error message back to the client *if* this send channel is for this client
		// But this method is typically called by the hub to send TO this client.
		// So, if marshalling fails, the message just isn't sent.
		return
	}

	// Non-blocking send to avoid deadlocks if the send channel is full
	// and writePump is stuck or slow.
	select {
	case c.send <- jsonMsg:
		// Message queued
	default:
		// Send channel is full, client might be too slow or disconnected.
		log.Printf("Client %s (User: %s) SendMessage: Send channel full. Dropping message of type %s.", c.conn.RemoteAddr(), c.userID, msgType)
		// Consider closing the client connection here if this happens frequently.
		// c.hub.unregister <- c // This could lead to self-unregistering from a non-hub goroutine, be careful.
		// For now, just log and drop. The readPump will eventually detect disconnection.
	}
}

// HubMessage is a wrapper for messages coming from a client to be processed by the Hub
type HubMessage struct {
	client  *Client
	rawJSON []byte // Raw JSON message from the client
}
