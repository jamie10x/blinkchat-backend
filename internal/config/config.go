package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// AppConfig contains runtime configuration values.
type AppConfig struct {
	ServerPort  string
	DatabaseURL string
	JWTSecret   string
	TokenMaxAge time.Duration
}

var Cfg *AppConfig

// LoadConfig populates Cfg using environment variables and optional .env file.
func LoadConfig(envPath ...string) {
	envFile := ".env"
	if len(envPath) > 0 {
		envFile = envPath[0]
	}

	err := godotenv.Load(envFile)
	if err != nil {
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

	log.Printf("Configuration loaded: Port=%s, DB_URL_Host=%s, TokenMaxAge=%v", Cfg.ServerPort, getDBHost(Cfg.DatabaseURL), Cfg.TokenMaxAge)
}

func getEnv(key string, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	log.Printf("Warning: Environment variable %s not set, using fallback value: %s", key, fallback)
	return fallback
}

func getDBHost(dbURL string) string {
	parts := strings.Split(dbURL, "@")
	if len(parts) > 1 {
		hostAndDB := strings.Split(parts[1], "/")
		if len(hostAndDB) > 0 {
			return hostAndDB[0]
		}
	}
	return "unknown (could not parse DB_URL for host)"
}
