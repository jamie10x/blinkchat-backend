package models

import (
	"time"

	"github.com/google/uuid"
)

// Chat represents a conversation between one or more users.
type Chat struct {
	ID                uuid.UUID     `json:"id" db:"id"`
	Name              string        `json:"name,omitempty" db:"name"`
	IsGroup           bool          `json:"isGroup" db:"is_group"`
	CreatedAt         time.Time     `json:"createdAt" db:"created_at"`
	UpdatedAt         time.Time     `json:"updatedAt" db:"updated_at"`
	OtherParticipants []*PublicUser `json:"otherParticipants,omitempty"`
	LastMessage       *Message      `json:"lastMessage,omitempty"`
	UnreadCount       int           `json:"unreadCount,omitempty"`
	LastReadAt        *time.Time    `json:"lastReadAt,omitempty"`
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
	Name           string      `json:"name,omitempty" binding:"omitempty,min=1,max=128"`
	ParticipantIDs []uuid.UUID `json:"participantIds" binding:"required,min=1,dive,required"`
}

// UpdateChatRequest captures mutable chat properties.
type UpdateChatRequest struct {
	Name *string `json:"name,omitempty" binding:"omitempty,min=1,max=128"`
}

// ModifyChatParticipantsRequest encapsulates participant add/remove operations.
type ModifyChatParticipantsRequest struct {
	UserIDs []uuid.UUID `json:"userIds" binding:"required,min=1,dive,required"`
}

// MarkChatReadRequest represents a request to update the caller's read position.
type MarkChatReadRequest struct {
	ReadThrough *time.Time `json:"readThrough,omitempty"`
}

// ChatResponse is reserved for future single-chat responses.
