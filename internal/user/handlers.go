package user

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"blinkchat-backend/internal/models" // Use your actual module path
	"blinkchat-backend/internal/store"  // Use your actual module path

	"github.com/gin-gonic/gin"
	"github.com/google/uuid" // For validating UUIDs
)

// UserHandler handles user-related HTTP requests.
type UserHandler struct {
	userStore store.UserStore
	// Add other dependencies if needed, e.g., config
}

// NewUserHandler creates a new UserHandler.
func NewUserHandler(userStore store.UserStore) *UserHandler {
	return &UserHandler{
		userStore: userStore,
	}
}

// GetUserByID handles requests to get a user's public profile by their ID.
// GET /users/:id
func (h *UserHandler) GetUserByID(c *gin.Context) {
	userIDParam := c.Param("id") // Get "id" from path parameter, e.g., /users/some-uuid

	// Validate if userIDParam is a valid UUID
	_, err := uuid.Parse(userIDParam)
	if err != nil {
		log.Printf("GetUserByID: Invalid user ID format: %s, error: %v", userIDParam, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID format"})
		return
	}

	// Fetch user details from the store
	// The GetUserByID method in our store already accepts a string for the ID.
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

	// Return public user information
	c.JSON(http.StatusOK, user.ToPublicUser())
}

// SearchUsers handles requests to search for users.
// GET /users?search=<query>
func (h *UserHandler) SearchUsers(c *gin.Context) {
	searchQuery := c.Query("search") // Get "search" from query parameter, e.g., /users?search=john

	if searchQuery == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Search query parameter is required"})
		return
	}

	// Here, you'd typically call a method on your userStore like:
	// users, err := h.userStore.SearchUsers(c.Request.Context(), searchQuery, paginationParams...)
	// For MVP, let's assume a simple search by username or email (if your store supports it).
	// Since our current UserStore doesn't have a SearchUsers method,
	// we'll need to add it or simulate it for now.

	// --- Placeholder for SearchUsers ---
	// Let's simulate by trying to find a user by email if the search query looks like an email,
	// or by username. This is a very basic approach and would need a proper
	// full-text search or LIKE query in a real implementation.

	// For now, we'll return a not implemented error or an empty list,
	// as we need to define the search logic in the store first.
	// For a portfolio piece, it's important to show you know this needs more work.

	log.Printf("SearchUsers: Received search query: %s", searchQuery)

	// Example: If UserStore had SearchUsersByName(ctx, name) (*models.User, error)
	// For simplicity, let's assume we just try to fetch by email if it looks like one.
	// This is NOT a robust search.
	var foundUser *models.User
	var searchErr error

	// This is a very naive search implementation for demonstration purposes.
	// A real implementation would use SQL LIKE, full-text search, or similar.
	// We also haven't implemented a generic SearchUsers in our store yet.
	// Let's assume we're looking for an exact email match for now for simplicity.
	// This requires you to update your UserStore interface and implementation.
	// For now, to make it runnable, let's just fetch by email if it looks like one.

	// Let's just return a placeholder until we implement store.SearchUsers
	// users := make([]*models.PublicUser, 0) // Empty slice
	// c.JSON(http.StatusOK, users)

	// For a quick test, let's pretend we search by email if it contains "@"
	if strings.Contains(searchQuery, "@") {
		user, err := h.userStore.GetUserByEmail(c.Request.Context(), searchQuery)
		if err == nil && user != nil {
			foundUser = user
		} else if !errors.Is(err, store.ErrUserNotFound) {
			searchErr = err
		}
	} else {
		// If not an email, we don't have a GetUserByUsername in our current store interface.
		// You would add this to your store: GetUserByUsername(ctx, username)
		log.Println("SearchUsers: Searching by username is not yet fully implemented in store.")
		// For a demo, you could fetch all users and filter, but that's not scalable.
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

	// If no user found or search logic not fully implemented for the query type
	c.JSON(http.StatusOK, make([]*models.PublicUser, 0)) // Return empty list
}
