package user

import (
	"errors"
	"log"
	"net/http"
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

	var foundUser *models.User
	var searchErr error

	if strings.Contains(searchQuery, "@") {
		user, err := h.userStore.GetUserByEmail(c.Request.Context(), searchQuery)
		if err == nil && user != nil {
			foundUser = user
		} else if !errors.Is(err, store.ErrUserNotFound) {
			searchErr = err
		}
	} else {
		log.Println("SearchUsers: Searching by username is not yet implemented.")
	}

	if searchErr != nil {
		log.Printf("SearchUsers: Error during search for '%s': %v", searchQuery, searchErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error during user search"})
		return
	}

	if foundUser != nil {
		c.JSON(http.StatusOK, []*models.PublicUser{foundUser.ToPublicUser()})
		return
	}

	c.JSON(http.StatusOK, make([]*models.PublicUser, 0))
}
