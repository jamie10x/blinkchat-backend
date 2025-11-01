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

	"blinkchat-backend/internal/auth"
	"blinkchat-backend/internal/chat"
	"blinkchat-backend/internal/config"
	"blinkchat-backend/internal/middleware"
	"blinkchat-backend/internal/store"
	"blinkchat-backend/internal/user"
	"blinkchat-backend/internal/websocket"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	config.LoadConfig(".env")
	if config.Cfg == nil {
		log.Fatal("Error: Configuration not loaded.")
	}

	log.Println("Chat Backend Starting...")
	log.Printf("Server will run on port: %s", config.Cfg.ServerPort)
	log.Printf("JWT Secret (first 5 chars for check): %s...", previewSecret(config.Cfg.JWTSecret))
	log.Printf("Database URL Host (for check): %s", getDBHostForMain(config.Cfg.DatabaseURL))

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

	userStore := store.NewPostgresUserStore(dbpool)
	log.Printf("UserStore initialized: %T", userStore)
	chatStore := store.NewPostgresChatStore(dbpool)
	log.Printf("ChatStore initialized: %T", chatStore)
	messageStore := store.NewPostgresMessageStore(dbpool)
	log.Printf("MessageStore initialized: %T", messageStore)

	wsHub := websocket.NewHub(userStore, chatStore, messageStore)
	go wsHub.Run()
	log.Println("WebSocket Hub initialized and running.")

	authHandler := auth.NewAuthHandler(userStore)
	log.Printf("AuthHandler initialized: %T", authHandler)

	userHandler := user.NewUserHandler(userStore)
	log.Printf("UserHandler initialized: %T", userHandler)

	chatRestHandler := chat.NewRestHandler(chatStore, messageStore, userStore, wsHub)
	log.Printf("ChatRestHandler initialized: %T", chatRestHandler)

	wsHandler := websocket.NewWSHandler(wsHub)
	log.Printf("WSHandler initialized: %T", wsHandler)

	gin.SetMode(gin.ReleaseMode) // Or gin.DebugMode
	r := gin.New()
	r.RedirectTrailingSlash = false
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	corsConfig := cors.DefaultConfig()
	corsConfig.AllowOrigins = []string{"http://localhost:3000", "http://127.0.0.1:3000"}
	corsConfig.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	corsConfig.AllowHeaders = []string{"Origin", "Content-Type", "Accept", "Authorization", "Upgrade", "Connection"}
	corsConfig.AllowCredentials = true
	r.Use(cors.New(corsConfig))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "UP"})
	})

	r.GET("/ws", wsHandler.HandleWebSocketConnection)

	apiV1 := r.Group("/api/v1")
	{
		publicAuthRoutes := apiV1.Group("/auth")
		{
			publicAuthRoutes.POST("/register", authHandler.Register)
			publicAuthRoutes.POST("/login", authHandler.Login)
		}

		protected := apiV1.Group("/")
		protected.Use(middleware.AuthMiddleware())
		{
			protected.GET("/auth/me", authHandler.GetMe)
			protected.GET("/users/:id", userHandler.GetUserByID)
			protected.GET("/users", userHandler.SearchUsers)
			protected.POST("/messages", chatRestHandler.PostMessage)
			protected.GET("/messages", chatRestHandler.GetMessagesByChatID)
			protected.GET("/chats", chatRestHandler.GetChats)
		}
	}

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

func previewSecret(secret string) string {
	if len(secret) >= 5 {
		return secret[:5]
	}
	return secret
}

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
