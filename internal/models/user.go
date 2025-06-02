package models

import (
	"time"

	"github.com/google/uuid"
)

// User represents a user in the application.
type User struct {
	ID             uuid.UUID `json:"id" db:"id"`
	Username       string    `json:"username" db:"username"`
	Email          string    `json:"email" db:"email"`
	HashedPassword string    `json:"-" db:"hashed_password"` // Exclude from JSON responses by default
	CreatedAt      time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt      time.Time `json:"updatedAt" db:"updated_at"`
}

// PublicUser is a version of User that is safe to return in API responses
// (e.g., without the hashed password).
type PublicUser struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"` // Consider if email should always be public
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ToPublicUser converts a User to a PublicUser.
func (u *User) ToPublicUser() *PublicUser {
	return &PublicUser{
		ID:        u.ID,
		Username:  u.Username,
		Email:     u.Email, // Decide if you want to expose email here
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}

// --- DTOs (Data Transfer Objects) for User operations ---

// CreateUserRequest defines the expected JSON structure for user registration.
type CreateUserRequest struct {
	Username string `json:"username" binding:"required,min=3,max=50"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6,max=72"` // bcrypt has a 72 char limit
}

// LoginUserRequest defines the expected JSON structure for user login.
type LoginUserRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6,max=72"`
}
