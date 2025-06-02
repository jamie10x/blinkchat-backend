package store

import (
	"context"
	"fmt" // For error wrapping

	"blinkchat-backend/internal/models" // Use your actual module path

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn" // For specific pgx error types like unique_violation
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserStore defines the interface for user data operations.
type UserStore interface {
	CreateUser(ctx context.Context, user *models.User) error
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	GetUserByID(ctx context.Context, id string) (*models.User, error) // id will be uuid.UUID as string
}

// PostgresUserStore implements the UserStore interface using PostgreSQL.
type PostgresUserStore struct {
	db *pgxpool.Pool
}

// NewPostgresUserStore creates a new PostgresUserStore.
func NewPostgresUserStore(db *pgxpool.Pool) *PostgresUserStore {
	return &PostgresUserStore{
		db: db,
	}
}

// CreateUser inserts a new user into the database.
// It assumes user.ID is already generated (or will be by DB default)
// and user.HashedPassword is set.
func (s *PostgresUserStore) CreateUser(ctx context.Context, user *models.User) error {
	query := `
        INSERT INTO users (id, username, email, hashed_password, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6)
    `
	// If your users.id has DEFAULT uuid_generate_v4(), you might want to omit it from the INSERT
	// and use RETURNING id, but pgx handles UUIDs fine if you generate them in Go.
	// For this example, we assume user.ID is pre-generated (e.g., uuid.New() in the handler/service).

	_, err := s.db.Exec(ctx, query,
		user.ID,
		user.Username,
		user.Email,
		user.HashedPassword,
		user.CreatedAt,
		user.UpdatedAt,
	)

	if err != nil {
		// Check for unique constraint violation (e.g., email or username already exists)
		pgErr, ok := err.(*pgconn.PgError)
		if ok && pgErr.Code == "23505" { // 23505 is the PostgreSQL error code for unique_violation
			// You can inspect pgErr.ConstraintName to see which constraint was violated
			// e.g., "users_email_key" or "users_username_key"
			if pgErr.ConstraintName == "users_email_key" {
				return ErrEmailExists
			}
			if pgErr.ConstraintName == "users_username_key" {
				return ErrUsernameExists
			}
			return fmt.Errorf("database unique constraint violation: %w, constraint: %s", err, pgErr.ConstraintName)
		}
		return fmt.Errorf("failed to create user: %w", err)
	}
	return nil
}

// GetUserByEmail retrieves a user by their email address.
// Implementation will be added in a subsequent step.
func (s *PostgresUserStore) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	query := `
		SELECT id, username, email, hashed_password, created_at, updated_at
		FROM users
		WHERE email = $1
	`
	user := &models.User{} // Important to initialize the struct to scan into

	// Use QueryRow because we expect at most one row
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
			return nil, ErrUserNotFound // Custom error for not found
		}
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}
	return user, nil
}

// GetUserByID retrieves a user by their ID.
// Implementation will be added in a subsequent step.
func (s *PostgresUserStore) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	// Note: id is string here, but it represents a UUID. pgx handles UUID conversion.
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

// --- Custom Errors ---
// It's good practice to define specific errors for common cases.
var (
	ErrUserNotFound   = fmt.Errorf("user not found")
	ErrEmailExists    = fmt.Errorf("email already exists")
	ErrUsernameExists = fmt.Errorf("username already exists")
	// Add more custom errors as needed
)
