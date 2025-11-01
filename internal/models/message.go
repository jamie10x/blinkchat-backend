package models

import (
	"time"

	"github.com/google/uuid"
)

// MessageStatus indicates the delivery state of a message.
type MessageStatus string

const (
	StatusSent      MessageStatus = "sent"
	StatusDelivered MessageStatus = "delivered"
	StatusRead      MessageStatus = "read"
)

// Message represents a chat message persisted to storage.
type Message struct {
	ID        uuid.UUID     `json:"id" db:"id"`
	ChatID    uuid.UUID     `json:"chatId" db:"chat_id"`
	SenderID  uuid.UUID     `json:"senderId" db:"sender_id"`
	Content   string        `json:"content" db:"content"`
	Timestamp time.Time     `json:"timestamp" db:"created_at"`
	Status    MessageStatus `json:"status" db:"status"`

	Sender *PublicUser `json:"sender,omitempty" db:"-"`
}

type CreateMessageRequest struct {
	ChatID     *uuid.UUID `json:"chatId,omitempty"`
	ReceiverID *uuid.UUID `json:"receiverId,omitempty"`
	Content    string     `json:"content" binding:"required,max=4096"`
}

// MessageAcknowledgementRequest captures status updates for a message.
type MessageAcknowledgementRequest struct {
	MessageID uuid.UUID     `json:"messageId" binding:"required"`
	ChatID    uuid.UUID     `json:"chatId" binding:"required"`
	Status    MessageStatus `json:"status" binding:"required,oneof=delivered read"`
	UserID    uuid.UUID     `json:"userId,omitempty"`
}
