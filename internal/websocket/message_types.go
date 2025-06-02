package websocket

import (
	"blinkchat-backend/internal/models" // Use your actual module path
	"github.com/google/uuid"
)

// Define constants for WebSocket message types
const (
	MessageTypeNewMessage          = "new_message"           // For sending/receiving actual chat messages
	MessageTypeMessageSentAck      = "message_sent_ack"      // Server acknowledges it processed a sent message from client
	MessageTypeMessageStatusUpdate = "message_status_update" // For delivering 'delivered' or 'read' status
	MessageTypeError               = "error"                 // For sending error messages to the client
	MessageTypeTypingIndicator     = "typing_indicator"      // Optional: for typing status
	// Add other message types as needed (e.g., user_online_status, etc.)
)

// WebSocketMessage is a generic wrapper for all messages sent over WebSocket.
// The `Type` field determines how the `Payload` is interpreted.
type WebSocketMessage struct {
	Type    string      `json:"type"`              // e.g., "new_message", "message_status_update"
	Payload interface{} `json:"payload,omitempty"` // Can be any of the payload structs below
}

// --- Specific Payload Structs ---

// NewMessagePayload is the payload for sending a new chat message.
// This is what a client sends when they type and hit send.
type NewMessagePayload struct {
	// Client might not know ChatID when starting a new 1:1 conversation.
	// If ChatID is empty, ReceiverID must be provided.
	// The server will then determine/create the ChatID.
	ChatID     *uuid.UUID `json:"chatId,omitempty"` // Use pointer to allow nil
	ReceiverID *uuid.UUID `json:"receiverId,omitempty"`
	Content    string     `json:"content"`
	// Client might also send a temporary client-generated ID for the message
	// to help with UI updates before server confirms with a permanent ID.
	ClientTempID *string `json:"clientTempId,omitempty"`
}

// MessageSentAckPayload is sent from server to client after processing a NewMessagePayload.
// It confirms the message was saved and provides its server-assigned ID and timestamp.
type MessageSentAckPayload struct {
	ClientTempID *string              `json:"clientTempId,omitempty"` // The temp ID client sent
	ServerMsgID  uuid.UUID            `json:"serverMsgId"`            // The permanent ID from the server
	ChatID       uuid.UUID            `json:"chatId"`                 // The chat ID (might have been created)
	Timestamp    models.JSONTime      `json:"timestamp"`              // Use custom JSONTime for consistent formatting
	Status       models.MessageStatus `json:"status"`                 // Should be 'sent'
}

// InboundMessagePayload is what the server sends to a recipient client
// when a new message arrives for them. It's essentially the full `models.Message`.
// We can reuse models.Message or define a specific payload.
// For simplicity, we can often send the models.Message directly,
// ensuring its Sender field is populated.
// type InboundMessagePayload models.Message // Example: reusing models.Message

// MessageStatusUpdatePayload is used by clients to inform the server
// about 'delivered' or 'read' status, and by the server to broadcast these updates.
type MessageStatusUpdatePayload struct {
	MessageID uuid.UUID            `json:"messageId"`
	ChatID    uuid.UUID            `json:"chatId"`
	Status    models.MessageStatus `json:"status"`           // "delivered" or "read"
	UserID    uuid.UUID            `json:"userId,omitempty"` // User who triggered the status (e.g., reader)
	Timestamp models.JSONTime      `json:"timestamp"`
}

// ErrorPayload is used for sending error details over WebSocket.
type ErrorPayload struct {
	Message string `json:"message"`
	Code    int    `json:"code,omitempty"` // Optional error code
}

// TypingIndicatorPayload for sending typing status.
type TypingIndicatorPayload struct {
	ChatID   uuid.UUID `json:"chatId"`
	UserID   uuid.UUID `json:"userId"`   // User who is typing
	IsTyping bool      `json:"isTyping"` // true if typing, false if stopped
}
