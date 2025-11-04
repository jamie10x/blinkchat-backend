package auth

import (
	"errors"
	"log"
	"net/http"
	"time"

	"blinkchat-backend/internal/models"
	"blinkchat-backend/internal/store"
	"blinkchat-backend/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AuthHandler handles authentication-related HTTP requests.
type AuthHandler struct {
	userStore store.UserStore
}

func NewAuthHandler(userStore store.UserStore) *AuthHandler {
	return &AuthHandler{
		userStore: userStore,
	}
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req models.CreateUserRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Register: Bad request data: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data", "details": err.Error()})
		return
	}

	hashedPassword, err := utils.HashPassword(req.Password)
	if err != nil {
		log.Printf("Register: Failed to hash password for email %s: %v", req.Email, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process registration"})
		return
	}

	user := &models.User{
		ID:             uuid.New(),
		Username:       req.Username,
		Email:          req.Email,
		HashedPassword: hashedPassword,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	err = h.userStore.CreateUser(c.Request.Context(), user)
	if err != nil {
		if errors.Is(err, store.ErrEmailExists) {
			c.JSON(http.StatusConflict, gin.H{"error": "Email already exists"})
			return
		}
		if errors.Is(err, store.ErrUsernameExists) {
			c.JSON(http.StatusConflict, gin.H{"error": "Username already exists"})
			return
		}
		log.Printf("Register: Failed to create user %s: %v", req.Email, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to register user"})
		return
	}

	token, err := utils.GenerateJWT(user.ID)
	if err != nil {
		log.Printf("Register: Failed to generate JWT for user %s: %v", user.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Registration successful, but failed to generate token"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "User registered successfully",
		"token":   token,
		"user":    user.ToPublicUser(),
	})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginUserRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Login: Bad request data: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data", "details": err.Error()})
		return
	}

	user, err := h.userStore.GetUserByEmail(c.Request.Context(), req.Email)
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
			return
		}
		log.Printf("Login: Failed to get user by email %s: %v", req.Email, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Login failed"})
		return
	}

	if !utils.CheckPasswordHash(req.Password, user.HashedPassword) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	token, err := utils.GenerateJWT(user.ID)
	if err != nil {
		log.Printf("Login: Failed to generate JWT for user %s: %v", user.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Login successful, but failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Login successful",
		"token":   token,
		"user":    user.ToPublicUser(),
	})
}

func (h *AuthHandler) GetMe(c *gin.Context) {
	userIDString, exists := c.Get("userID")
	if !exists {
		log.Println("GetMe: userID not found in context, middleware issue?")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "User ID not found in context"})
		return
	}

	user, err := h.userStore.GetUserByID(c.Request.Context(), userIDString.(string))
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			log.Printf("GetMe: User %s (from token) not found in DB", userIDString.(string))
			c.JSON(http.StatusNotFound, gin.H{"error": "User associated with token not found"})
			return
		}
		log.Printf("GetMe: Failed to get user by ID %s: %v", userIDString.(string), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve user information"})
		return
	}

	c.JSON(http.StatusOK, user.ToPublicUser())
}
