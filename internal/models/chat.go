package models

import (
	"time"

	"github.com/google/uuid"
)

// Chat represents a conversation between one or more users.
type Chat struct {
	ID                uuid.UUID     `json:"id" db:"id"`
	CreatedAt         time.Time     `json:"createdAt" db:"created_at"`
	OtherParticipants []*PublicUser `json:"otherParticipants,omitempty"`
	LastMessage       *Message      `json:"lastMessage,omitempty"`
	UnreadCount       int           `json:"unreadCount,omitempty"`
}

// ChatParticipant links a user to a chat.
type ChatParticipant struct {
	ChatID    uuid.UUID `json:"chatId" db:"chat_id"`
	UserID    uuid.UUID `json:"userId" db:"user_id"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
}

// --- DTOs for Chat operations ---

// CreateChatRequest defines the payload for creating a chat.
type CreateChatRequest struct {
	ParticipantIDs []uuid.UUID `json:"participantIds" binding:"required,min=1"`
}

// ChatResponse is reserved for future single-chat responses.
