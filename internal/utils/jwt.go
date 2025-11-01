package utils

import (
	"fmt"
	"time"

	"blinkchat-backend/internal/config"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

// GenerateJWT builds a signed JWT for the supplied user ID.
func GenerateJWT(userID uuid.UUID) (string, error) {
	if config.Cfg == nil || config.Cfg.JWTSecret == "" {
		return "", fmt.Errorf("JWT secret is not configured")
	}
	if config.Cfg.TokenMaxAge <= 0 {
		return "", fmt.Errorf("token max age is not configured or invalid")
	}

	claims := &Claims{
		UserID: userID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(config.Cfg.TokenMaxAge)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "blinkchat-backend",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signedToken, err := token.SignedString([]byte(config.Cfg.JWTSecret))
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return signedToken, nil
}

// ValidateJWT parses and verifies a signed JWT string.
func ValidateJWT(tokenString string) (*Claims, error) {
	if config.Cfg == nil || config.Cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT secret is not configured for validation")
	}

	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(config.Cfg.JWTSecret), nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse or validate token: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("token is invalid")
	}

	return claims, nil
}
