package store

import (
	"context"
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

func (s *PostgresMessageStore) CreateMessage(ctx context.Context, message *models.Message) error {
	query := `
        INSERT INTO messages (id, chat_id, sender_id, content, status, created_at)
        VALUES ($1, $2, $3, $4, $5, $6)
    `

	_, err := s.db.Exec(ctx, query,
		message.ID,
		message.ChatID,
		message.SenderID,
		message.Content,
		message.Status,
		message.Timestamp,
	)

	if err != nil {
		return fmt.Errorf("failed to create message: %w", err)
	}
	return nil
}

func (s *PostgresMessageStore) GetMessagesByChatID(ctx context.Context, chatID uuid.UUID, limit, offset int) ([]*models.Message, error) {
	query := `
        SELECT
            m.id, m.chat_id, m.sender_id, m.content, m.status, m.created_at,
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
		var msg models.Message
		var sender models.PublicUser

		err := rows.Scan(
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
			log.Printf("Error scanning message row: %v", err)
			return nil, fmt.Errorf("failed to scan message row: %w", err)
		}
		sender.ID = msg.SenderID
		msg.Sender = &sender
		messages = append(messages, &msg)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating message rows: %w", err)
	}

	return messages, nil
}

func (s *PostgresMessageStore) GetMessageByID(ctx context.Context, messageID uuid.UUID) (*models.Message, error) {
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

func (s *PostgresMessageStore) GetUnreadMessageCountForUserInChat(ctx context.Context, chatID uuid.UUID, userID uuid.UUID) (int, error) {
	query := `
        SELECT COUNT(*)
        FROM messages
        WHERE chat_id = $1
          AND sender_id != $2
          AND status != $3
    `
	var count int
	err := s.db.QueryRow(ctx, query, chatID, userID, models.StatusRead).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get unread message count: %w", err)
	}
	return count, nil
}

var (
	ErrMessageNotFound = fmt.Errorf("message not found")
)
