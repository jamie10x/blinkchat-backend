package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"blinkchat-backend/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ChatStore defines persistence operations for chats and participants.
type ChatStore interface {
	CreateChat(ctx context.Context, name string, isGroup bool, participantIDs []uuid.UUID) (*models.Chat, error)
	GetChatByID(ctx context.Context, chatID uuid.UUID) (*models.Chat, error)
	GetChatByParticipantIDs(ctx context.Context, participantIDs []uuid.UUID) (*models.Chat, error)
	GetUserChats(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*models.Chat, error)
	AddUserToChat(ctx context.Context, chatID uuid.UUID, userID uuid.UUID) error
	RemoveUserFromChat(ctx context.Context, chatID uuid.UUID, userID uuid.UUID) error
	GetAllParticipantsInChat(ctx context.Context, chatID uuid.UUID) ([]*models.PublicUser, error)
	UpdateChatName(ctx context.Context, chatID uuid.UUID, name string) (*models.Chat, error)
	UpdateParticipantReadThrough(ctx context.Context, chatID uuid.UUID, userID uuid.UUID, readThrough time.Time) error
}

// PostgresChatStore implements ChatStore with PostgreSQL.
type PostgresChatStore struct {
	db *pgxpool.Pool
}

func NewPostgresChatStore(db *pgxpool.Pool) *PostgresChatStore {
	return &PostgresChatStore{
		db: db,
	}
}

func (s *PostgresChatStore) CreateChat(ctx context.Context, name string, isGroup bool, participantIDs []uuid.UUID) (*models.Chat, error) {
	if len(participantIDs) == 0 {
		return nil, fmt.Errorf("at least one participant is required to create a chat")
	}

	unique := make(map[uuid.UUID]struct{}, len(participantIDs))
	ordered := make([]uuid.UUID, 0, len(participantIDs))
	for _, id := range participantIDs {
		if _, seen := unique[id]; seen {
			continue
		}
		unique[id] = struct{}{}
		ordered = append(ordered, id)
	}
	if len(ordered) == 0 {
		return nil, fmt.Errorf("no valid participants provided")
	}

	if len(ordered) > 2 {
		isGroup = true
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	var chatID uuid.UUID
	var createdAt time.Time
	var updatedAt time.Time
	chatQuery := `INSERT INTO chats (name, is_group, created_at, updated_at) VALUES ($1, $2, NOW(), NOW()) RETURNING id, name, is_group, created_at, updated_at`
	err = tx.QueryRow(ctx, chatQuery, name, isGroup).Scan(&chatID, &name, &isGroup, &createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create chat entry: %w", err)
	}
	participantQuery := `INSERT INTO chat_participants (chat_id, user_id, created_at, last_read_at) VALUES ($1, $2, NOW(), NOW()) ON CONFLICT (chat_id, user_id) DO NOTHING`
	for _, userID := range ordered {
		_, err = tx.Exec(ctx, participantQuery, chatID, userID)
		if err != nil {
			return nil, fmt.Errorf("failed to add participant %s to chat %s: %w", userID, chatID, err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}
	createdChat := &models.Chat{ID: chatID, Name: name, IsGroup: isGroup, CreatedAt: createdAt, UpdatedAt: updatedAt}
	participants, err := s.getChatParticipantsInternal(ctx, chatID)
	if err == nil {
		createdChat.OtherParticipants = participants
	}
	return createdChat, nil
}

func (s *PostgresChatStore) getChatParticipantsInternal(ctx context.Context, chatID uuid.UUID) ([]*models.PublicUser, error) {
	query := `
        SELECT u.id, u.username, u.email, u.created_at, u.updated_at
        FROM users u
        JOIN chat_participants cp ON u.id = cp.user_id
        WHERE cp.chat_id = $1
    `
	rows, err := s.db.Query(ctx, query, chatID)
	if err != nil {
		return nil, fmt.Errorf("failed to query chat participants internal for chatID %s: %w", chatID, err)
	}
	defer rows.Close()
	var participants []*models.PublicUser
	for rows.Next() {
		var p models.PublicUser
		err := rows.Scan(&p.ID, &p.Username, &p.Email, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan chat participant internal for chatID %s: %w", chatID, err)
		}
		participants = append(participants, &p)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating chat participant internal rows for chatID %s: %w", chatID, err)
	}
	return participants, nil
}

func (s *PostgresChatStore) GetAllParticipantsInChat(ctx context.Context, chatID uuid.UUID) ([]*models.PublicUser, error) {
	return s.getChatParticipantsInternal(ctx, chatID)
}

func (s *PostgresChatStore) GetChatByID(ctx context.Context, chatID uuid.UUID) (*models.Chat, error) {
	query := `SELECT id, name, is_group, created_at, updated_at FROM chats WHERE id = $1`
	chat := &models.Chat{}
	err := s.db.QueryRow(ctx, query, chatID).Scan(&chat.ID, &chat.Name, &chat.IsGroup, &chat.CreatedAt, &chat.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrChatNotFound
		}
		return nil, fmt.Errorf("failed to get chat by ID %s: %w", chatID, err)
	}
	allParticipants, err := s.GetAllParticipantsInChat(ctx, chatID)
	if err != nil {
		log.Printf("GetChatByID: Error fetching participants for chat %s: %v", chatID, err)
	} else {
		chat.OtherParticipants = allParticipants
	}
	return chat, nil
}

func (s *PostgresChatStore) GetChatByParticipantIDs(ctx context.Context, participantIDs []uuid.UUID) (*models.Chat, error) {
	if len(participantIDs) != 2 {
		return nil, fmt.Errorf("GetChatByParticipantIDs expects exactly two participant IDs for 1:1 chat lookup")
	}
	query := `
                SELECT c.id, c.name, c.is_group, c.created_at, c.updated_at
                FROM chats c
                WHERE EXISTS (
                        SELECT 1 FROM chat_participants cp1 WHERE cp1.chat_id = c.id AND cp1.user_id = $1
                ) AND EXISTS (
                        SELECT 1 FROM chat_participants cp2 WHERE cp2.chat_id = c.id AND cp2.user_id = $2
                ) AND (
                        SELECT COUNT(*) FROM chat_participants cp_count WHERE cp_count.chat_id = c.id
                ) = 2
                AND c.is_group = FALSE
                LIMIT 1;
        `
	userA := participantIDs[0]
	userB := participantIDs[1]
	chat := &models.Chat{}
	err := s.db.QueryRow(ctx, query, userA, userB).Scan(&chat.ID, &chat.Name, &chat.IsGroup, &chat.CreatedAt, &chat.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrChatNotFound
		}
		return nil, fmt.Errorf("failed to get chat by participant IDs: %w", err)
	}
	return chat, nil
}

func (s *PostgresChatStore) GetUserChats(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*models.Chat, error) {
	query := `
WITH user_chat_ids AS (
    SELECT cp.chat_id, cp.last_read_at
    FROM chat_participants cp
    WHERE cp.user_id = $1
),
chat_participant_details AS (
    SELECT
        cp.chat_id,
        jsonb_agg(jsonb_build_object(
            'id', u.id,
            'username', u.username,
            'email', u.email,
            'createdAt', u.created_at,
            'updatedAt', u.updated_at
        )) FILTER (WHERE u.id != $1) AS other_participants_json
    FROM chat_participants cp
    JOIN users u ON cp.user_id = u.id
    WHERE cp.chat_id IN (SELECT uci.chat_id FROM user_chat_ids uci)
    GROUP BY cp.chat_id
),
ranked_messages AS (
    SELECT
        m.id AS message_id,
        m.chat_id,
        m.sender_id,
        m.content,
        m.status,
        m.created_at AS message_timestamp,
        m.updated_at AS message_updated_at,
        m.deleted_at,
        m.attachment_url,
        u_sender.id AS sender_user_id,
        u_sender.username AS sender_username,
        u_sender.email AS sender_email,
        u_sender.created_at AS sender_user_created_at,
        u_sender.updated_at AS sender_user_updated_at,
        ROW_NUMBER() OVER (PARTITION BY m.chat_id ORDER BY m.created_at DESC) as rn
    FROM messages m
    JOIN users u_sender ON m.sender_id = u_sender.id
    WHERE m.chat_id IN (SELECT uci.chat_id FROM user_chat_ids uci)
),
last_messages AS (
    SELECT *
    FROM ranked_messages
    WHERE rn = 1
),
unread_counts AS (
    SELECT
        m.chat_id,
        COUNT(*) FILTER (
            WHERE m.sender_id != $1
              AND m.deleted_at IS NULL
              AND m.created_at > COALESCE(cp.last_read_at, 'epoch')
        ) AS unread_count
    FROM messages m
    JOIN chat_participants cp ON cp.chat_id = m.chat_id AND cp.user_id = $1
    WHERE m.chat_id IN (SELECT chat_id FROM user_chat_ids)
    GROUP BY m.chat_id
)
SELECT
    c.id AS chat_id,
    c.name,
    c.is_group,
    c.created_at AS chat_created_at,
    c.updated_at AS chat_updated_at,
    uci.last_read_at,
    COALESCE(uc.unread_count, 0) AS unread_count,
    cpd.other_participants_json,
    lm.message_id,
    lm.content AS last_message_content,
    lm.message_timestamp AS last_message_timestamp,
    lm.message_updated_at,
    lm.deleted_at AS last_message_deleted_at,
    lm.attachment_url,
    lm.status AS last_message_status,
    lm.sender_user_id AS last_message_sender_id,
    lm.sender_username AS last_message_sender_username,
    lm.sender_email AS last_message_sender_email,
    lm.sender_user_created_at AS last_message_sender_created_at,
    lm.sender_user_updated_at AS last_message_sender_updated_at
FROM chats c
JOIN user_chat_ids uci ON c.id = uci.chat_id
LEFT JOIN chat_participant_details cpd ON c.id = cpd.chat_id
LEFT JOIN last_messages lm ON c.id = lm.chat_id
LEFT JOIN unread_counts uc ON c.id = uc.chat_id
ORDER BY lm.message_timestamp DESC NULLS LAST, c.updated_at DESC
LIMIT $2 OFFSET $3;
    `

	rows, err := s.db.Query(ctx, query, userID, limit, offset)
	if err != nil {
		log.Printf("Error querying user chats for userID %s: %v", userID, err)
		return nil, fmt.Errorf("failed to query user chats: %w", err)
	}
	defer rows.Close()

	var chatsSlice []*models.Chat

	for rows.Next() {
		var chatID uuid.UUID
		var chatName sql.NullString
		var chatIsGroup bool
		var chatCreatedAt time.Time
		var chatUpdatedAt time.Time
		var lastReadAt sql.NullTime
		var unreadCount int
		var otherParticipantsJSONBytes []byte
		var lastMessageID sql.NullString
		var lastMessageContent sql.NullString
		var lastMessageTimestamp sql.NullTime
		var lastMessageUpdatedAt sql.NullTime
		var lastMessageDeletedAt sql.NullTime
		var attachmentURL sql.NullString
		var lastMessageStatus sql.NullString
		var lastMessageSenderID sql.NullString
		var lastMessageSenderUsername sql.NullString
		var lastMessageSenderEmail sql.NullString
		var lastMessageSenderCreatedAt sql.NullTime
		var lastMessageSenderUpdatedAt sql.NullTime

		err := rows.Scan(
			&chatID,
			&chatName,
			&chatIsGroup,
			&chatCreatedAt,
			&chatUpdatedAt,
			&lastReadAt,
			&unreadCount,
			&otherParticipantsJSONBytes, // Scan as []byte
			&lastMessageID,
			&lastMessageContent,
			&lastMessageTimestamp,
			&lastMessageUpdatedAt,
			&lastMessageDeletedAt,
			&attachmentURL,
			&lastMessageStatus,
			&lastMessageSenderID,
			&lastMessageSenderUsername,
			&lastMessageSenderEmail,
			&lastMessageSenderCreatedAt,
			&lastMessageSenderUpdatedAt,
		)
		if err != nil {
			log.Printf("Error scanning user chat row: %v", err)
			return nil, fmt.Errorf("failed to scan user chat row: %w", err)
		}

		chat := &models.Chat{
			ID:          chatID,
			Name:        chatName.String,
			IsGroup:     chatIsGroup,
			CreatedAt:   chatCreatedAt,
			UpdatedAt:   chatUpdatedAt,
			UnreadCount: unreadCount,
		}

		if lastReadAt.Valid {
			t := lastReadAt.Time
			chat.LastReadAt = &t
		}

		if otherParticipantsJSONBytes != nil {
			var participants []*models.PublicUser
			if err := json.Unmarshal(otherParticipantsJSONBytes, &participants); err != nil {
				log.Printf("Error unmarshalling other_participants_json for chat %s: %v", chatID, err)
				chat.OtherParticipants = []*models.PublicUser{}
			} else {
				chat.OtherParticipants = participants
			}
		} else {
			chat.OtherParticipants = []*models.PublicUser{}
		}

		if lastMessageID.Valid {
			senderUUID, parseErr1 := uuid.Parse(lastMessageSenderID.String)
			lmID, parseErr2 := uuid.Parse(lastMessageID.String)

			if parseErr1 != nil {
				log.Printf("Error parsing last message sender ID '%s': %v", lastMessageSenderID.String, parseErr1)
			} else if parseErr2 != nil {
				log.Printf("Error parsing last message ID '%s': %v", lastMessageID.String, parseErr2)
			} else {
				lastUpdated := lastMessageTimestamp.Time
				if lastMessageUpdatedAt.Valid {
					lastUpdated = lastMessageUpdatedAt.Time
				}
				chat.LastMessage = &models.Message{
					ID:        lmID,
					ChatID:    chatID,
					SenderID:  senderUUID,
					Content:   lastMessageContent.String,
					Timestamp: lastMessageTimestamp.Time,
					UpdatedAt: lastUpdated,
					Status:    models.MessageStatus(lastMessageStatus.String),
					Sender: &models.PublicUser{
						ID:        senderUUID,
						Username:  lastMessageSenderUsername.String,
						Email:     lastMessageSenderEmail.String,
						CreatedAt: lastMessageSenderCreatedAt.Time,
						UpdatedAt: lastMessageSenderUpdatedAt.Time,
					},
				}
				if attachmentURL.Valid {
					url := attachmentURL.String
					chat.LastMessage.AttachmentURL = &url
				}
				if lastMessageDeletedAt.Valid {
					t := lastMessageDeletedAt.Time
					chat.LastMessage.DeletedAt = &t
					chat.LastMessage.IsDeleted = true
					chat.LastMessage.Content = ""
				}
				chat.LastMessage.IsEdited = !chat.LastMessage.UpdatedAt.Equal(chat.LastMessage.Timestamp)
			}
		}
		chatsSlice = append(chatsSlice, chat)
	}
	if err = rows.Err(); err != nil {
		log.Printf("Error after iterating user chat rows: %v", err)
		return nil, fmt.Errorf("error iterating user chat rows: %w", err)
	}

	return chatsSlice, nil
}

func (s *PostgresChatStore) getOtherChatParticipants(ctx context.Context, chatID uuid.UUID, currentUserID uuid.UUID) ([]*models.PublicUser, error) {
	log.Printf("getOtherChatParticipants called for chat %s, user %s - this might be redundant now", chatID, currentUserID)
	return s.getChatParticipantsInternal(ctx, chatID)
}

func (s *PostgresChatStore) AddUserToChat(ctx context.Context, chatID uuid.UUID, userID uuid.UUID) error {
	query := `
                INSERT INTO chat_participants (chat_id, user_id, created_at, last_read_at)
                VALUES ($1, $2, NOW(), NOW())
                ON CONFLICT (chat_id, user_id) DO NOTHING
        `
	_, err := s.db.Exec(ctx, query, chatID, userID)
	if err != nil {
		return fmt.Errorf("failed to add user %s to chat %s: %w", userID, err)
	}
	_, err = s.db.Exec(ctx, `UPDATE chats SET is_group = TRUE, updated_at = NOW() WHERE id = $1`, chatID)
	if err != nil {
		return fmt.Errorf("failed to flag chat %s as group when adding user %s: %w", chatID, userID, err)
	}
	return nil
}

func (s *PostgresChatStore) RemoveUserFromChat(ctx context.Context, chatID uuid.UUID, userID uuid.UUID) error {
	query := `DELETE FROM chat_participants WHERE chat_id = $1 AND user_id = $2`
	_, err := s.db.Exec(ctx, query, chatID, userID)
	if err != nil {
		return fmt.Errorf("failed to remove user %s from chat %s: %w", userID, err)
	}
	_, err = s.db.Exec(ctx, `UPDATE chats SET updated_at = NOW() WHERE id = $1`, chatID)
	if err != nil {
		return fmt.Errorf("failed to update chat %s timestamp while removing user %s: %w", chatID, userID, err)
	}
	return nil
}

var (
	ErrChatNotFound        = fmt.Errorf("chat not found")
	ErrParticipantNotFound = fmt.Errorf("chat participant not found")
)

func (s *PostgresChatStore) UpdateChatName(ctx context.Context, chatID uuid.UUID, name string) (*models.Chat, error) {
	query := `
                UPDATE chats
                SET name = $1,
                    is_group = CASE WHEN $1 <> '' THEN TRUE ELSE is_group END,
                    updated_at = NOW()
                WHERE id = $2
                RETURNING id, name, is_group, created_at, updated_at
        `
	chat := &models.Chat{}
	err := s.db.QueryRow(ctx, query, name, chatID).Scan(&chat.ID, &chat.Name, &chat.IsGroup, &chat.CreatedAt, &chat.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrChatNotFound
		}
		return nil, fmt.Errorf("failed to update chat name: %w", err)
	}
	participants, err := s.getChatParticipantsInternal(ctx, chatID)
	if err == nil {
		chat.OtherParticipants = participants
	}
	return chat, nil
}

func (s *PostgresChatStore) UpdateParticipantReadThrough(ctx context.Context, chatID uuid.UUID, userID uuid.UUID, readThrough time.Time) error {
	query := `
                UPDATE chat_participants
                SET last_read_at = GREATEST(COALESCE(last_read_at, 'epoch'::timestamptz), $3)
                WHERE chat_id = $1 AND user_id = $2
        `
	result, err := s.db.Exec(ctx, query, chatID, userID, readThrough)
	if err != nil {
		return fmt.Errorf("failed to update read state for chat %s and user %s: %w", chatID, userID, err)
	}
	if result.RowsAffected() == 0 {
		return ErrParticipantNotFound
	}
	_, err = s.db.Exec(ctx, `UPDATE chats SET updated_at = GREATEST(updated_at, NOW()) WHERE id = $1`, chatID)
	if err != nil {
		return fmt.Errorf("failed to touch chat %s while updating read state: %w", chatID, err)
	}
	return nil
}
