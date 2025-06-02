package auth

import (
	"errors"
	"log"
	"net/http"
	"time"

	"blinkchat-backend/internal/models" // Use your actual module path
	"blinkchat-backend/internal/store"  // Use your actual module path
	"blinkchat-backend/internal/utils"  // Use your actual module path

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AuthHandler handles authentication-related HTTP requests.
type AuthHandler struct {
	userStore store.UserStore
	// We might add other dependencies here later, like a config reference if needed directly
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(userStore store.UserStore) *AuthHandler {
	return &AuthHandler{
		userStore: userStore,
	}
}

// Register handles user registration requests.
// POST /auth/register
func (h *AuthHandler) Register(c *gin.Context) {
	var req models.CreateUserRequest

	// Bind JSON request body to the CreateUserRequest struct
	// and validate it based on struct tags (binding:"...")
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Register: Bad request data: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data", "details": err.Error()})
		return
	}

	// Hash the password
	hashedPassword, err := utils.HashPassword(req.Password)
	if err != nil {
		log.Printf("Register: Failed to hash password for email %s: %v", req.Email, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process registration"})
		return
	}

	// Create the user model
	user := &models.User{
		ID:             uuid.New(), // Generate a new UUID for the user
		Username:       req.Username,
		Email:          req.Email,
		HashedPassword: hashedPassword,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Store the user in the database
	err = h.userStore.CreateUser(c.Request.Context(), user) // Pass context from Gin request
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

	// Generate JWT token
	token, err := utils.GenerateJWT(user.ID)
	if err != nil {
		log.Printf("Register: Failed to generate JWT for user %s: %v", user.ID, err)
		// User is created, but token generation failed. This is tricky.
		// For now, return an error. Could also log and ask user to login.
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Registration successful, but failed to generate token"})
		return
	}

	// Return success response with token and public user info
	c.JSON(http.StatusCreated, gin.H{
		"message": "User registered successfully",
		"token":   token,
		"user":    user.ToPublicUser(),
	})
}

// Login handles user login requests.
// POST /auth/login
func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginUserRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Login: Bad request data: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data", "details": err.Error()})
		return
	}

	// Get user by email
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

	// Check password
	if !utils.CheckPasswordHash(req.Password, user.HashedPassword) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	// Generate JWT token
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

// GetMe handles requests to get the currently authenticated user's details.
// GET /auth/me
func (h *AuthHandler) GetMe(c *gin.Context) {
	// The AuthMiddleware should have set the userID in the context.
	// The key "userID" must match `authorizationPayloadKey` from middleware.
	userIDString, exists := c.Get("userID")
	if !exists {
		// This should ideally not happen if middleware is correctly applied and working.
		log.Println("GetMe: userID not found in context, middleware issue?")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "User ID not found in context"})
		return
	}

	// Convert userIDString (which is string) to uuid.UUID if your store expects it.
	// However, our GetUserByID in UserStore already accepts a string for the ID.
	// So, we can directly use userIDString.

	// Fetch user details from the store
	user, err := h.userStore.GetUserByID(c.Request.Context(), userIDString.(string)) // Assert type
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			// This case implies the user ID from a valid token doesn't exist in the DB anymore
			// (e.g., user deleted after token was issued).
			log.Printf("GetMe: User %s (from token) not found in DB", userIDString.(string))
			c.JSON(http.StatusNotFound, gin.H{"error": "User associated with token not found"})
			return
		}
		log.Printf("GetMe: Failed to get user by ID %s: %v", userIDString.(string), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve user information"})
		return
	}

	// Return public user information
	c.JSON(http.StatusOK, user.ToPublicUser())
}
