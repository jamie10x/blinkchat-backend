package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// AppConfig holds the application configuration
type AppConfig struct {
	ServerPort  string
	DatabaseURL string
	JWTSecret   string
	TokenMaxAge time.Duration // Changed to time.Duration for easier use
}

// Global variable to hold the loaded configuration
var Cfg *AppConfig

// LoadConfig loads configuration from environment variables
// It first tries to load from a .env file if present.
func LoadConfig(envPath ...string) {
	// Determine the path to the .env file.
	// If no path is provided, it assumes .env is in the current working directory.
	// This is useful because the working directory can change depending on how you run the app.
	// For `go run cmd/server/main.go`, it might be `cmd/server/`.
	// For a compiled binary run from the root, it would be the root.
	// We'll handle this better in main.go by providing the path.
	envFile := ".env"
	if len(envPath) > 0 {
		envFile = envPath[0]
	}

	err := godotenv.Load(envFile)
	if err != nil {
		// Log a warning if .env is not found, but don't make it fatal.
		// This allows the app to run in environments where .env is not used (e.g., production with actual env vars).
		log.Printf("Warning: Could not load %s file: %v. Relying on environment variables.", envFile, err)
	}

	port := getEnv("PORT", "8080")
	dbURL := getEnv("DATABASE_URL", "postgres://user:password@localhost:5432/chat_db?sslmode=disable")
	jwtSecret := getEnv("JWT_SECRET", "a_very_long_and_secure_default_secret_key_please_change_this")

	tokenHoursStr := getEnv("TOKEN_HOURS", "72")
	tokenHours, err := strconv.Atoi(tokenHoursStr)
	if err != nil {
		log.Printf("Warning: Invalid TOKEN_HOURS value '%s', using default 72h. Error: %v", tokenHoursStr, err)
		tokenHours = 72
	}

	Cfg = &AppConfig{
		ServerPort:  port,
		DatabaseURL: dbURL,
		JWTSecret:   jwtSecret,
		TokenMaxAge: time.Hour * time.Duration(tokenHours),
	}

	// Log the loaded configuration for verification (excluding sensitive data like JWT secret in real production logs)
	log.Printf("Configuration loaded: Port=%s, DB_URL_Host=%s, TokenMaxAge=%v", Cfg.ServerPort, getDBHost(Cfg.DatabaseURL), Cfg.TokenMaxAge)
}

// getEnv reads an environment variable or returns a default value
func getEnv(key string, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	log.Printf("Warning: Environment variable %s not set, using fallback value: %s", key, fallback)
	return fallback
}

// Helper function to extract host from DB URL for logging, to avoid logging full credentials
func getDBHost(dbURL string) string {
	// Basic parsing, can be made more robust if needed
	// Example: postgres://user:password@localhost:5432/dbname?sslmode=disable
	// We want to extract "localhost:5432"
	parts := strings.Split(dbURL, "@")
	if len(parts) > 1 {
		hostAndDB := strings.Split(parts[1], "/")
		if len(hostAndDB) > 0 {
			return hostAndDB[0]
		}
	}
	return "unknown (could not parse DB_URL for host)"
}
