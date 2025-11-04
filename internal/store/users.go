package store

import (
	"context"
	"fmt"
	"strings"

	"blinkchat-backend/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserStore provides persistence operations for user records.
type UserStore interface {
	CreateUser(ctx context.Context, user *models.User) error
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	GetUserByID(ctx context.Context, id string) (*models.User, error)
	SearchUsers(ctx context.Context, query string, limit int) ([]*models.User, error)
}

// PostgresUserStore stores users in PostgreSQL.
type PostgresUserStore struct {
	db *pgxpool.Pool
}

// NewPostgresUserStore returns a Postgres-backed UserStore implementation.
func NewPostgresUserStore(db *pgxpool.Pool) *PostgresUserStore {
	return &PostgresUserStore{db: db}
}

// CreateUser persists a new user record.
func (s *PostgresUserStore) CreateUser(ctx context.Context, user *models.User) error {
	query := `
        INSERT INTO users (id, username, email, hashed_password, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6)
    `

	_, err := s.db.Exec(ctx, query,
		user.ID,
		user.Username,
		user.Email,
		user.HashedPassword,
		user.CreatedAt,
		user.UpdatedAt,
	)

	if err != nil {
		pgErr, ok := err.(*pgconn.PgError)
		if ok && pgErr.Code == "23505" {
			switch pgErr.ConstraintName {
			case "users_email_key":
				return ErrEmailExists
			case "users_username_key":
				return ErrUsernameExists
			}
			return fmt.Errorf("database unique constraint violation: %w, constraint: %s", err, pgErr.ConstraintName)
		}
		return fmt.Errorf("failed to create user: %w", err)
	}
	return nil
}

// GetUserByEmail returns the user with the given email.
func (s *PostgresUserStore) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	query := `
                SELECT id, username, email, hashed_password, created_at, updated_at
                FROM users
                WHERE email = $1
        `
	user := &models.User{}

	err := s.db.QueryRow(ctx, query, email).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.HashedPassword,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}
	return user, nil
}

// GetUserByID returns the user with the given ID.
func (s *PostgresUserStore) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	query := `
                SELECT id, username, email, hashed_password, created_at, updated_at
                FROM users
                WHERE id = $1
        `
	user := &models.User{}

	err := s.db.QueryRow(ctx, query, id).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.HashedPassword,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by ID: %w", err)
	}
	return user, nil
}

// SearchUsers performs a case-insensitive lookup over usernames and emails.
func (s *PostgresUserStore) SearchUsers(ctx context.Context, query string, limit int) ([]*models.User, error) {
	if limit <= 0 {
		limit = 10
	}
	likeQuery := fmt.Sprintf("%%%s%%", strings.ToLower(query))
	sqlQuery := `
                SELECT id, username, email, hashed_password, created_at, updated_at
                FROM users
                WHERE LOWER(username) LIKE $1 OR LOWER(email) LIKE $1
                ORDER BY username ASC
                LIMIT $2
        `

	rows, err := s.db.Query(ctx, sqlQuery, likeQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search users: %w", err)
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		var user models.User
		if err := rows.Scan(
			&user.ID,
			&user.Username,
			&user.Email,
			&user.HashedPassword,
			&user.CreatedAt,
			&user.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan searched user: %w", err)
		}
		users = append(users, &user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating searched users: %w", err)
	}
	return users, nil
}

var (
	ErrUserNotFound   = fmt.Errorf("user not found")
	ErrEmailExists    = fmt.Errorf("email already exists")
	ErrUsernameExists = fmt.Errorf("username already exists")
)
