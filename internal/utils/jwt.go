package utils

import (
	"fmt"
	"time"

	"blinkchat-backend/internal/config" // Use your actual module path

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid" // For UserID type if you use uuid.UUID directly
)

// Claims defines the structure of our JWT claims.
type Claims struct {
	UserID string `json:"user_id"` // Can be uuid.UUID if you prefer strong typing here
	jwt.RegisteredClaims
}

// GenerateJWT generates a new JWT for a given user ID.
func GenerateJWT(userID uuid.UUID) (string, error) {
	if config.Cfg == nil || config.Cfg.JWTSecret == "" {
		return "", fmt.Errorf("JWT secret is not configured")
	}
	if config.Cfg.TokenMaxAge <= 0 {
		return "", fmt.Errorf("token max age is not configured or invalid")
	}

	// Create the claims
	claims := &Claims{
		UserID: userID.String(), // Store UserID as a string in claims
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(config.Cfg.TokenMaxAge)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "blinkchat-backend", // Optional: identify the issuer
			// Subject:   userID.String(),  // Optional: can also be user ID
			// Audience:  []string{"blinkchat_users"}, // Optional: who the token is intended for
		},
	}

	// Create token with claims
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Sign the token with our secret
	signedToken, err := token.SignedString([]byte(config.Cfg.JWTSecret))
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return signedToken, nil
}

// ValidateJWT validates a given JWT string.
// If valid, it returns the claims; otherwise, it returns an error.
func ValidateJWT(tokenString string) (*Claims, error) {
	if config.Cfg == nil || config.Cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT secret is not configured for validation")
	}

	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		// Check the signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(config.Cfg.JWTSecret), nil
	})

	if err != nil {
		// More specific error handling can be done here based on the type of error
		// For example, checking for jwt.ErrTokenExpired, jwt.ErrTokenNotValidYet etc.
		// For now, a general error is returned.
		return nil, fmt.Errorf("failed to parse or validate token: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("token is invalid")
	}

	return claims, nil
}
