package websocket

import (
	"blinkchat-backend/internal/models"
	"github.com/google/uuid"
)

const (
	MessageTypeNewMessage          = "new_message"
	MessageTypeMessageSentAck      = "message_sent_ack"
	MessageTypeMessageStatusUpdate = "message_status_update"
	MessageTypeError               = "error"
	MessageTypeTypingIndicator     = "typing_indicator"
)

// WebSocketMessage wraps all WebSocket traffic.
type WebSocketMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
}

// NewMessagePayload describes a chat message sent by a client.
type NewMessagePayload struct {
	ChatID       *uuid.UUID `json:"chatId,omitempty"`
	ReceiverID   *uuid.UUID `json:"receiverId,omitempty"`
	Content      string     `json:"content"`
	ClientTempID *string    `json:"clientTempId,omitempty"`
}

// MessageSentAckPayload acknowledges message persistence.
type MessageSentAckPayload struct {
	ClientTempID *string              `json:"clientTempId,omitempty"`
	ServerMsgID  uuid.UUID            `json:"serverMsgId"`
	ChatID       uuid.UUID            `json:"chatId"`
	Timestamp    models.JSONTime      `json:"timestamp"`
	Status       models.MessageStatus `json:"status"`
}

// MessageStatusUpdatePayload notifies clients of delivery/read updates.
type MessageStatusUpdatePayload struct {
	MessageID uuid.UUID            `json:"messageId"`
	ChatID    uuid.UUID            `json:"chatId"`
	Status    models.MessageStatus `json:"status"`
	UserID    uuid.UUID            `json:"userId,omitempty"`
	Timestamp models.JSONTime      `json:"timestamp"`
}

// ErrorPayload represents an error message to the client.
type ErrorPayload struct {
	Message string `json:"message"`
	Code    int    `json:"code,omitempty"`
}

// TypingIndicatorPayload signals typing state in a chat.
type TypingIndicatorPayload struct {
	ChatID   uuid.UUID `json:"chatId"`
	UserID   uuid.UUID `json:"userId"`
	IsTyping bool      `json:"isTyping"`
}
