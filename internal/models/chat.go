package models

import (
	"time"

	"github.com/google/uuid"
)

// Chat represents a conversation (e.g., a 1:1 chat room).
type Chat struct {
	ID        uuid.UUID `json:"id" db:"id"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
	// UpdatedAt could be useful to sort chats by recent activity.
	// If you add it to the DB, add it here too.
	// UpdatedAt time.Time `json:"updatedAt" db:"updated_at"`

	// --- Fields for display in chat list, not directly in 'chats' table ---
	// These would be populated by specific queries when fetching chat lists.

	// OtherParticipants lists the users in the chat other than the current user.
	// For a 1:1 chat, this will typically be a single user.
	OtherParticipants []*PublicUser `json:"otherParticipants,omitempty"`

	// LastMessage is the most recent message in the chat.
	LastMessage *Message `json:"lastMessage,omitempty"`

	// UnreadCount is the number of unread messages for the current user in this chat.
	UnreadCount int `json:"unreadCount,omitempty"`
}

// ChatParticipant links a user to a chat.
// This struct maps to the 'chat_participants' table.
type ChatParticipant struct {
	ChatID    uuid.UUID `json:"chatId" db:"chat_id"`
	UserID    uuid.UUID `json:"userId" db:"user_id"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
}

// --- DTOs for Chat operations ---

// CreateChatRequest defines the structure for creating a new chat.
// For a 1:1 chat, this would typically involve specifying the other user's ID.
type CreateChatRequest struct {
	// For 1:1 chat, we usually need the ID of the other participant.
	// The sender is the authenticated user.
	ParticipantIDs []uuid.UUID `json:"participantIds" binding:"required,min=1"` // For 1:1, expect one ID other than self
}

// ChatResponse is a more detailed response for a single chat,
// perhaps including more participants' details if needed.
// For now, the base Chat struct is quite comprehensive for list views.
// You can expand this if specific single-chat views need more info.
