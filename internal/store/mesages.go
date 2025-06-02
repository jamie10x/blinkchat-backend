package store

import (
	"context"
	"fmt" // For error wrapping
	"github.com/jackc/pgx/v5"
	"log"

	"blinkchat-backend/internal/models" // Use your actual module path

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	// "github.com/jackc/pgx/v5" // For pgx.ErrNoRows if needed directly
)

// MessageStore defines the interface for message data operations.
type MessageStore interface {
	// CreateMessage inserts a new message into the database.
	CreateMessage(ctx context.Context, message *models.Message) error

	// GetMessagesByChatID retrieves messages for a specific chat, with pagination.
	// Messages should be ordered by timestamp (e.g., newest or oldest first).
	GetMessagesByChatID(ctx context.Context, chatID uuid.UUID, limit, offset int) ([]*models.Message, error)

	// GetMessageByID retrieves a specific message by its ID.
	GetMessageByID(ctx context.Context, messageID uuid.UUID) (*models.Message, error)

	// UpdateMessageStatus updates the status of a message (e.g., to 'delivered' or 'read').
	UpdateMessageStatus(ctx context.Context, messageID uuid.UUID, status models.MessageStatus) error

	// GetUnreadMessageCountForUserInChat gets the count of unread messages for a specific user in a chat.
	// (This is complex and might involve looking at messages newer than a user's last_read_timestamp for that chat)
	GetUnreadMessageCountForUserInChat(ctx context.Context, chatID uuid.UUID, userID uuid.UUID) (int, error)
}

// PostgresMessageStore implements the MessageStore interface using PostgreSQL.
type PostgresMessageStore struct {
	db *pgxpool.Pool
	// userStore UserStore // Might be needed to fetch sender details if populating message.Sender
}

// NewPostgresMessageStore creates a new PostgresMessageStore.
func NewPostgresMessageStore(db *pgxpool.Pool /*, userStore UserStore*/) *PostgresMessageStore {
	return &PostgresMessageStore{
		db: db,
		// userStore: userStore,
	}
}

// --- Implementations will follow ---

// CreateMessage inserts a new message into the database.
// Assumes message.ID, message.Timestamp, message.Status are set appropriately before calling.
func (s *PostgresMessageStore) CreateMessage(ctx context.Context, message *models.Message) error {
	query := `
        INSERT INTO messages (id, chat_id, sender_id, content, status, created_at)
        VALUES ($1, $2, $3, $4, $5, $6)
    `
	// We assume message.ID is pre-generated (e.g., uuid.New())
	// and message.Timestamp (mapped to created_at) is also set (e.g., time.Now()).
	// message.Status would typically be models.StatusSent initially.

	_, err := s.db.Exec(ctx, query,
		message.ID,
		message.ChatID,
		message.SenderID,
		message.Content,
		message.Status,    // This should be models.MessageStatus type
		message.Timestamp, // This is models.Message.Timestamp which maps to 'created_at'
	)

	if err != nil {
		// Check for foreign key violations (e.g., chat_id or sender_id doesn't exist)
		// pgErr, ok := err.(*pgconn.PgError)
		// if ok && pgErr.Code == "23503" { // foreign_key_violation
		//    return fmt.Errorf("database foreign key violation: %w, constraint: %s", err, pgErr.ConstraintName)
		// }
		return fmt.Errorf("failed to create message: %w", err)
	}
	return nil
}

// GetMessagesByChatID retrieves messages for a specific chat, with pagination.
// Orders by created_at descending (newest first).
func (s *PostgresMessageStore) GetMessagesByChatID(ctx context.Context, chatID uuid.UUID, limit, offset int) ([]*models.Message, error) {
	// Query to fetch messages and also join with users table to get sender's username.
	// This demonstrates populating the message.Sender field.
	// Note: Selecting all user fields might be too much; select only what's needed for PublicUser.
	query := `
        SELECT 
            m.id, m.chat_id, m.sender_id, m.content, m.status, m.created_at,
            u.username AS sender_username, u.email AS sender_email, u.created_at AS sender_created_at, u.updated_at AS sender_updated_at 
            -- Add other fields for PublicUser if needed, like u.id AS sender_user_id
            -- Ensure u.id is selected if PublicUser needs it and it's different from m.sender_id in context
        FROM messages m
        JOIN users u ON m.sender_id = u.id
        WHERE m.chat_id = $1
        ORDER BY m.created_at DESC
        LIMIT $2 OFFSET $3
    `
	rows, err := s.db.Query(ctx, query, chatID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages by chat ID: %w", err)
	}
	defer rows.Close()

	messages := make([]*models.Message, 0)
	for rows.Next() {
		var msg models.Message
		var sender models.PublicUser // Temporary struct to scan sender details into

		// We need to scan u.id as well for sender.ID
		// Let's assume sender_id is enough for PublicUser for now or it's same as m.sender_id
		// If PublicUser.ID is crucial and distinct, ensure it's selected as sender_user_id
		err := rows.Scan(
			&msg.ID,
			&msg.ChatID,
			&msg.SenderID, // This is user.ID for the sender already
			&msg.Content,
			&msg.Status,
			&msg.Timestamp,
			&sender.Username,  // Scanned from u.username
			&sender.Email,     // Scanned from u.email
			&sender.CreatedAt, // Scanned from u.created_at
			&sender.UpdatedAt, // Scanned from u.updated_at
		)
		if err != nil {
			// Log the error and continue if possible, or return
			log.Printf("Error scanning message row: %v", err)
			return nil, fmt.Errorf("failed to scan message row: %w", err)
		}
		sender.ID = msg.SenderID // Populate the ID for the PublicUser sender
		msg.Sender = &sender     // Attach the populated sender
		messages = append(messages, &msg)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating message rows: %w", err)
	}

	return messages, nil
}

func (s *PostgresMessageStore) GetMessageByID(ctx context.Context, messageID uuid.UUID) (*models.Message, error) {
	// Similar to GetMessagesByChatID but for a single message, and might also populate sender.
	query := `
        SELECT 
            m.id, m.chat_id, m.sender_id, m.content, m.status, m.created_at,
            u.username AS sender_username, u.email AS sender_email, u.created_at AS sender_created_at, u.updated_at AS sender_updated_at
        FROM messages m
        JOIN users u ON m.sender_id = u.id
        WHERE m.id = $1
    `
	var msg models.Message
	var sender models.PublicUser

	err := s.db.QueryRow(ctx, query, messageID).Scan(
		&msg.ID,
		&msg.ChatID,
		&msg.SenderID,
		&msg.Content,
		&msg.Status,
		&msg.Timestamp,
		&sender.Username,
		&sender.Email,
		&sender.CreatedAt,
		&sender.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows { // Import pgx if not already
			return nil, ErrMessageNotFound // Custom error
		}
		return nil, fmt.Errorf("failed to get message by ID: %w", err)
	}
	sender.ID = msg.SenderID
	msg.Sender = &sender
	return &msg, nil
}

func (s *PostgresMessageStore) UpdateMessageStatus(ctx context.Context, messageID uuid.UUID, status models.MessageStatus) error {
	query := `UPDATE messages SET status = $1, updated_at = NOW() WHERE id = $2` // Assuming you add updated_at to messages table
	// If messages table does not have updated_at, then:
	// query := `UPDATE messages SET status = $1 WHERE id = $2`

	result, err := s.db.Exec(ctx, query, status, messageID)
	if err != nil {
		return fmt.Errorf("failed to update message status for message %s: %w", messageID, err)
	}
	if result.RowsAffected() == 0 {
		return ErrMessageNotFound // Or a more specific "message not updated" error
	}
	return nil
}

func (s *PostgresMessageStore) GetUnreadMessageCountForUserInChat(ctx context.Context, chatID uuid.UUID, userID uuid.UUID) (int, error) {
	// This is a simplified version. A real implementation might involve:
	// - A 'read_receipts' table (user_id, chat_id, last_read_message_id or last_read_timestamp)
	// - Or, if 'messages.status' is per-recipient (more complex for 1:1, better for group)
	// For now, let's assume we count messages with status 'sent' or 'delivered' for the other user(s) in the chat,
	// and where the sender is NOT the given userID. This is not a perfect unread count for `userID`.

	// A more accurate simple unread count for userID would be:
	// Count messages in chatID WHERE sender_id != userID AND status != 'read' (by this userID)
	// This requires message status to be per-recipient or a separate read tracking mechanism.

	// Let's count messages that are not 'read' and not sent by the current user.
	// This is still an approximation.
	query := `
        SELECT COUNT(*) 
        FROM messages
        WHERE chat_id = $1 
          AND sender_id != $2 
          AND status != $3 -- status != 'read'
    `
	var count int
	err := s.db.QueryRow(ctx, query, chatID, userID, models.StatusRead).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get unread message count: %w", err)
	}
	return count, nil
}

// --- Custom Errors for MessageStore ---
var (
	ErrMessageNotFound = fmt.Errorf("message not found")
	// Add more as needed
)
