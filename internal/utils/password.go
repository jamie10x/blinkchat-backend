package utils

import (
	"fmt"
	"log"

	"golang.org/x/crypto/bcrypt"
)

// DefaultCost is the default cost for bcrypt hashing.
// Higher cost means more secure but slower. Adjust based on your server's capability.
const DefaultCost = 12 // bcrypt.DefaultCost is 10, 12 is a reasonable choice.

// HashPassword generates a bcrypt hash of the password.
func HashPassword(password string) (string, error) {
	// bcrypt has a maximum password length of 72 bytes.
	// If the password is longer, bcrypt will only use the first 72 bytes.
	// It's good practice to validate this on input (e.g., in DTOs)
	// but also be aware of this internal bcrypt limitation.
	if len(password) == 0 {
		return "", fmt.Errorf("password cannot be empty")
	}

	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(password), DefaultCost)
	if err != nil {
		log.Printf("Error generating bcrypt hash: %v", err) // Log the error for server-side visibility
		return "", fmt.Errorf("failed to hash password: %w", err)
	}
	return string(hashedBytes), nil
}

// CheckPasswordHash compares a plain-text password with a bcrypt hash.
// Returns true if they match, false otherwise.
func CheckPasswordHash(password, hash string) bool {
	if len(password) == 0 || len(hash) == 0 {
		// Avoid bcrypt comparison if one is empty, as it might lead to issues
		// or reveal information depending on bcrypt implementation details.
		// bcrypt.CompareHashAndPassword would return an error if hash is malformed or password too long.
		return false
	}

	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	// err is nil if the password matches the hash.
	// err is bcrypt.ErrMismatchedHashAndPassword if they don't match.
	// Other errors can occur if the hash is not a valid bcrypt hash.
	return err == nil
}
