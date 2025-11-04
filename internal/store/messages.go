package store

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"blinkchat-backend/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MessageStore defines persistence operations for messages.
type MessageStore interface {
	CreateMessage(ctx context.Context, message *models.Message) error
	GetMessagesByChatID(ctx context.Context, chatID uuid.UUID, limit, offset int) ([]*models.Message, error)
	GetMessageByID(ctx context.Context, messageID uuid.UUID) (*models.Message, error)
	UpdateMessageStatus(ctx context.Context, messageID uuid.UUID, status models.MessageStatus) error
	GetUnreadMessageCountForUserInChat(ctx context.Context, chatID uuid.UUID, userID uuid.UUID) (int, error)
	UpdateMessageContent(ctx context.Context, messageID uuid.UUID, senderID uuid.UUID, content string, attachmentURL *string) (*models.Message, error)
	SoftDeleteMessage(ctx context.Context, messageID uuid.UUID, senderID uuid.UUID) (*models.Message, error)
}

// PostgresMessageStore implements MessageStore with PostgreSQL.
type PostgresMessageStore struct {
	db *pgxpool.Pool
}

func NewPostgresMessageStore(db *pgxpool.Pool) *PostgresMessageStore {
	return &PostgresMessageStore{
		db: db,
	}
}

func scanMessageWithSender(row pgx.Row) (*models.Message, error) {
	var msg models.Message
	var sender models.PublicUser
	var attachment sql.NullString
	var deletedAt sql.NullTime

	err := row.Scan(
		&msg.ID,
		&msg.ChatID,
		&msg.SenderID,
		&msg.Content,
		&msg.Status,
		&msg.Timestamp,
		&msg.UpdatedAt,
		&deletedAt,
		&attachment,
		&sender.Username,
		&sender.Email,
		&sender.CreatedAt,
		&sender.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	sender.ID = msg.SenderID
	msg.Sender = &sender

	if attachment.Valid {
		url := attachment.String
		msg.AttachmentURL = &url
	}
	if deletedAt.Valid {
		t := deletedAt.Time
		msg.DeletedAt = &t
		msg.IsDeleted = true
		msg.Content = ""
	}
	msg.IsEdited = !msg.UpdatedAt.Equal(msg.Timestamp)
	return &msg, nil
}

func (s *PostgresMessageStore) CreateMessage(ctx context.Context, message *models.Message) error {
	query := `
        INSERT INTO messages (id, chat_id, sender_id, content, status, attachment_url, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
    `
	var attachment interface{}
	if message.AttachmentURL != nil {
		attachment = *message.AttachmentURL
	}
	if message.UpdatedAt.IsZero() {
		message.UpdatedAt = message.Timestamp
	}

	_, err := s.db.Exec(ctx, query,
		message.ID,
		message.ChatID,
		message.SenderID,
		message.Content,
		message.Status,
		attachment,
		message.Timestamp,
		message.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create message: %w", err)
	}
	return nil
}

func (s *PostgresMessageStore) GetMessagesByChatID(ctx context.Context, chatID uuid.UUID, limit, offset int) ([]*models.Message, error) {
	query := `
        SELECT
            m.id, m.chat_id, m.sender_id, m.content, m.status, m.created_at, m.updated_at, m.deleted_at, m.attachment_url,
            u.username AS sender_username, u.email AS sender_email, u.created_at AS sender_created_at, u.updated_at AS sender_updated_at
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
		msg, err := scanMessageWithSender(rows)
		if err != nil {
			log.Printf("Error scanning message row: %v", err)
			return nil, fmt.Errorf("failed to scan message row: %w", err)
		}
		messages = append(messages, msg)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating message rows: %w", err)
	}

	return messages, nil
}

func (s *PostgresMessageStore) GetMessageByID(ctx context.Context, messageID uuid.UUID) (*models.Message, error) {
	query := `
        SELECT
            m.id, m.chat_id, m.sender_id, m.content, m.status, m.created_at, m.updated_at, m.deleted_at, m.attachment_url,
            u.username AS sender_username, u.email AS sender_email, u.created_at AS sender_created_at, u.updated_at AS sender_updated_at
        FROM messages m
        JOIN users u ON m.sender_id = u.id
        WHERE m.id = $1
    `
	msg, err := scanMessageWithSender(s.db.QueryRow(ctx, query, messageID))
	if err != nil {
		if err == pgx.ErrNoRows { // Import pgx if not already
			return nil, ErrMessageNotFound // Custom error
		}
		return nil, fmt.Errorf("failed to get message by ID: %w", err)
	}
	return msg, nil
}

func (s *PostgresMessageStore) UpdateMessageStatus(ctx context.Context, messageID uuid.UUID, status models.MessageStatus) error {
	query := `UPDATE messages SET status = $1, updated_at = NOW() WHERE id = $2`

	result, err := s.db.Exec(ctx, query, status, messageID)
	if err != nil {
		return fmt.Errorf("failed to update message status for message %s: %w", messageID, err)
	}
	if result.RowsAffected() == 0 {
		return ErrMessageNotFound
	}
	return nil
}

func (s *PostgresMessageStore) UpdateMessageContent(ctx context.Context, messageID uuid.UUID, senderID uuid.UUID, content string, attachmentURL *string) (*models.Message, error) {
	if content == "" && attachmentURL == nil {
		return nil, fmt.Errorf("message must contain content or an attachment")
	}
	var attachment interface{}
	if attachmentURL != nil {
		attachment = *attachmentURL
	}
	row := s.db.QueryRow(ctx, `
                UPDATE messages
                SET content = $1,
                    attachment_url = $2,
                    updated_at = NOW()
                WHERE id = $3 AND sender_id = $4 AND deleted_at IS NULL
                RETURNING id
        `, content, attachment, messageID, senderID)

	var updatedID uuid.UUID
	if err := row.Scan(&updatedID); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrMessageNotFound
		}
		return nil, fmt.Errorf("failed to update message content: %w", err)
	}
	return s.GetMessageByID(ctx, updatedID)
}

func (s *PostgresMessageStore) SoftDeleteMessage(ctx context.Context, messageID uuid.UUID, senderID uuid.UUID) (*models.Message, error) {
	row := s.db.QueryRow(ctx, `
                UPDATE messages
                SET deleted_at = NOW(),
                    updated_at = NOW(),
                    content = '',
                    attachment_url = NULL
                WHERE id = $1 AND sender_id = $2 AND deleted_at IS NULL
                RETURNING id
        `, messageID, senderID)
	var deletedID uuid.UUID
	if err := row.Scan(&deletedID); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrMessageNotFound
		}
		return nil, fmt.Errorf("failed to delete message: %w", err)
	}
	return s.GetMessageByID(ctx, deletedID)
}

func (s *PostgresMessageStore) GetUnreadMessageCountForUserInChat(ctx context.Context, chatID uuid.UUID, userID uuid.UUID) (int, error) {
	query := `
        SELECT COUNT(*)
        FROM messages m
        JOIN chat_participants cp ON cp.chat_id = m.chat_id AND cp.user_id = $2
        WHERE m.chat_id = $1
          AND m.sender_id != $2
          AND m.deleted_at IS NULL
          AND m.created_at > COALESCE(cp.last_read_at, 'epoch'::timestamptz)
    `
	var count int
	err := s.db.QueryRow(ctx, query, chatID, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get unread message count: %w", err)
	}
	return count, nil
}

var (
	ErrMessageNotFound = fmt.Errorf("message not found")
)
