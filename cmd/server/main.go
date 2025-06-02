package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	// Adjust these import paths to your actual module path
	"blinkchat-backend/internal/auth"
	"blinkchat-backend/internal/chat" // <-- IMPORT CHAT PACKAGE
	"blinkchat-backend/internal/config"
	"blinkchat-backend/internal/middleware"
	"blinkchat-backend/internal/store"
	"blinkchat-backend/internal/user"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	// --- 1. Load Configuration ---
	config.LoadConfig(".env")
	if config.Cfg == nil {
		log.Fatal("Error: Configuration not loaded.")
	}

	log.Println("Chat Backend Starting...")
	log.Printf("Server will run on port: %s", config.Cfg.ServerPort)
	log.Printf("JWT Secret (first 5 chars for check): %s...", config.Cfg.JWTSecret[:5])
	log.Printf("Database URL Host (for check): %s", getDBHostForMain(config.Cfg.DatabaseURL))

	// --- 2. Initialize Database Connection Pool ---
	dbCtx := context.Background()
	dbpool, err := pgxpool.New(dbCtx, config.Cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Unable to create connection pool: %v\n", err)
	}
	defer dbpool.Close()

	err = dbpool.Ping(dbCtx)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}
	log.Println("Successfully connected to the database!")

	// --- 3. Initialize Stores (Repositories) ---
	userStore := store.NewPostgresUserStore(dbpool)
	log.Printf("UserStore initialized: %T", userStore)
	chatStore := store.NewPostgresChatStore(dbpool) // <-- INITIALIZE CHAT STORE
	log.Printf("ChatStore initialized: %T", chatStore)
	messageStore := store.NewPostgresMessageStore(dbpool) // <-- INITIALIZE MESSAGE STORE
	log.Printf("MessageStore initialized: %T", messageStore)

	// --- 4. Initialize Handlers ---
	authHandler := auth.NewAuthHandler(userStore)
	log.Printf("AuthHandler initialized: %T", authHandler)

	userHandler := user.NewUserHandler(userStore)
	log.Printf("UserHandler initialized: %T", userHandler)

	chatRestHandler := chat.NewRestHandler(chatStore, messageStore, userStore) // <-- INITIALIZE CHAT REST HANDLER
	log.Printf("ChatRestHandler initialized: %T", chatRestHandler)

	// --- 5. Initialize Gin Router ---
	gin.SetMode(gin.ReleaseMode) // Or gin.DebugMode
	r := gin.New()
	r.RedirectTrailingSlash = false // Good practice
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	corsConfig := cors.DefaultConfig()
	corsConfig.AllowOrigins = []string{"http://localhost:3000", "http://127.0.0.1:3000"}
	corsConfig.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	corsConfig.AllowHeaders = []string{"Origin", "Content-Type", "Accept", "Authorization"}
	corsConfig.AllowCredentials = true
	r.Use(cors.New(corsConfig))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "UP"})
	})

	// --- Register API Routes ---
	apiV1 := r.Group("/api/v1")
	{
		// Public authentication routes
		publicAuthRoutes := apiV1.Group("/auth")
		{
			publicAuthRoutes.POST("/register", authHandler.Register)
			publicAuthRoutes.POST("/login", authHandler.Login)
		}

		// All protected routes (including auth/me and all user routes)
		protected := apiV1.Group("/")
		protected.Use(middleware.AuthMiddleware())
		{
			// Protected Auth-related routes
			protected.GET("/auth/me", authHandler.GetMe)

			// Protected User routes
			protected.GET("/users/:id", userHandler.GetUserByID)
			protected.GET("/users", userHandler.SearchUsers)

			// Protected Messaging REST routes <-- NEW SECTION
			protected.POST("/messages", chatRestHandler.PostMessage)        // Send a message
			protected.GET("/messages", chatRestHandler.GetMessagesByChatID) // Get messages for a chat
			protected.GET("/chats", chatRestHandler.GetChats)               // List all conversations
		}
	}

	// --- 6. Start HTTP Server with Graceful Shutdown ---
	srv := &http.Server{
		Addr:    ":" + config.Cfg.ServerPort,
		Handler: r,
	}

	go func() {
		log.Printf("Listening and serving HTTP on :%s\n", config.Cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server exiting")
}

// getDBHostForMain helper function (no changes here)
func getDBHostForMain(dbURL string) string {
	if i := strings.Index(dbURL, "@"); i != -1 {
		postAt := dbURL[i+1:]
		if j := strings.Index(postAt, "/"); j != -1 {
			return postAt[:j]
		}
		return postAt
	}
	if strings.HasPrefix(dbURL, "postgres://") {
		urlPart := dbURL[len("postgres://"):]
		if j := strings.Index(urlPart, "/"); j != -1 {
			return urlPart[:j]
		}
		return urlPart
	}
	return "unknown (could not parse DB_URL for host)"
}
