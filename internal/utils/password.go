package utils

import (
	"fmt"
	"log"

	"golang.org/x/crypto/bcrypt"
)

// DefaultCost controls the bcrypt hashing cost.
const DefaultCost = 12

// HashPassword hashes the provided password using bcrypt.
func HashPassword(password string) (string, error) {
	if len(password) == 0 {
		return "", fmt.Errorf("password cannot be empty")
	}

	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(password), DefaultCost)
	if err != nil {
		log.Printf("Error generating bcrypt hash: %v", err)
		return "", fmt.Errorf("failed to hash password: %w", err)
	}
	return string(hashedBytes), nil
}

// CheckPasswordHash reports whether password matches the given bcrypt hash.
func CheckPasswordHash(password, hash string) bool {
	if len(password) == 0 || len(hash) == 0 {
		return false
	}

	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
