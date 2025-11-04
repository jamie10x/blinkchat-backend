package user

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"blinkchat-backend/internal/models"
	"blinkchat-backend/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// UserHandler exposes user-related HTTP handlers.
type UserHandler struct {
	userStore store.UserStore
}

// NewUserHandler creates a UserHandler.
func NewUserHandler(userStore store.UserStore) *UserHandler {
	return &UserHandler{userStore: userStore}
}

// GetUserByID returns the public profile for a user.
func (h *UserHandler) GetUserByID(c *gin.Context) {
	userIDParam := c.Param("id")

	if _, err := uuid.Parse(userIDParam); err != nil {
		log.Printf("GetUserByID: Invalid user ID format: %s, error: %v", userIDParam, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID format"})
		return
	}

	user, err := h.userStore.GetUserByID(c.Request.Context(), userIDParam)
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			log.Printf("GetUserByID: User %s not found in DB", userIDParam)
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}
		log.Printf("GetUserByID: Failed to get user by ID %s: %v", userIDParam, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve user information"})
		return
	}

	c.JSON(http.StatusOK, user.ToPublicUser())
}

// SearchUsers performs a minimal search over users by email.
func (h *UserHandler) SearchUsers(c *gin.Context) {
	searchQuery := c.Query("search")
	if searchQuery == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Search query parameter is required"})
		return
	}

	log.Printf("SearchUsers: Received search query: %s", searchQuery)

	limit := 20
	if size := c.DefaultQuery("limit", ""); size != "" {
		if parsed, err := strconv.Atoi(size); err == nil && parsed > 0 && parsed <= 50 {
			limit = parsed
		}
	}

	var users []*models.User
	var err error

	if strings.Contains(searchQuery, "@") && !strings.Contains(searchQuery, "%") {
		var user *models.User
		user, err = h.userStore.GetUserByEmail(c.Request.Context(), searchQuery)
		if err == nil && user != nil {
			users = []*models.User{user}
		} else if err != nil && !errors.Is(err, store.ErrUserNotFound) {
			log.Printf("SearchUsers: Error fetching by email '%s': %v", searchQuery, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error during user search"})
			return
		}
	}

	if users == nil {
		users, err = h.userStore.SearchUsers(c.Request.Context(), searchQuery, limit)
		if err != nil {
			log.Printf("SearchUsers: Error searching for '%s': %v", searchQuery, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error during user search"})
			return
		}
	}

	publicUsers := make([]*models.PublicUser, 0, len(users))
	for _, user := range users {
		publicUsers = append(publicUsers, user.ToPublicUser())
	}

	c.JSON(http.StatusOK, publicUsers)
}
