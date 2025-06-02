package models

import (
	"time"

	"github.com/google/uuid"
)

// MessageStatus defines the possible statuses of a message.
// This corresponds to the ENUM type 'message_status' in PostgreSQL.
type MessageStatus string

const (
	StatusSent      MessageStatus = "sent"
	StatusDelivered MessageStatus = "delivered"
	StatusRead      MessageStatus = "read"
)

// Message represents a single chat message.
type Message struct {
	ID        uuid.UUID     `json:"id" db:"id"`
	ChatID    uuid.UUID     `json:"chatId" db:"chat_id"`
	SenderID  uuid.UUID     `json:"senderId" db:"sender_id"`
	Content   string        `json:"content" db:"content"`
	Timestamp time.Time     `json:"timestamp" db:"created_at"` // 'timestamp' in your spec, maps to 'created_at' in DB
	Status    MessageStatus `json:"status" db:"status"`

	// Optional: Sender's public info, can be populated for API responses
	// This helps avoid a separate lookup for sender details on the client.
	Sender *PublicUser `json:"sender,omitempty" db:"-"` // db:"-" means not a direct DB column in this table
}

// --- DTOs for Message operations ---

// CreateMessageRequest defines the structure for sending a new message.
type CreateMessageRequest struct {
	// ChatID can be optional if the system can infer it or create a new chat.
	// For a 1:1 chat, if ChatID is not provided, ReceiverID must be.
	ChatID     *uuid.UUID `json:"chatId,omitempty"`     // Pointer to allow nil if not provided
	ReceiverID *uuid.UUID `json:"receiverId,omitempty"` // Used if ChatID is not known (e.g., starting new 1:1)
	Content    string     `json:"content" binding:"required,max=4096"`
}

// MessageAcknowledgementRequest for updating message status (e.g., delivered, read).
// This would likely be sent over WebSocket, but a REST hook could exist.
type MessageAcknowledgementRequest struct {
	MessageID uuid.UUID     `json:"messageId" binding:"required"`
	ChatID    uuid.UUID     `json:"chatId" binding:"required"` // For context and targeting
	Status    MessageStatus `json:"status" binding:"required,oneof=delivered read"`
	UserID    uuid.UUID     `json:"userId,omitempty"` // User who is acknowledging (sender for delivered, receiver for read)
}
