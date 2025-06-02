package middleware

import (
	"net/http"
	"strings"

	"blinkchat-backend/internal/utils" // Use your actual module path

	"github.com/gin-gonic/gin"
)

const (
	authorizationHeaderKey  = "Authorization"
	authorizationTypeBearer = "bearer" // Case-insensitive comparison will be used
	authorizationPayloadKey = "userID" // Key to store userID in Gin context
)

// AuthMiddleware creates a Gin middleware for JWT authentication.
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader(authorizationHeaderKey)

		// Check if the Authorization header is provided
		if len(authHeader) == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization header is not provided"})
			return
		}

		// The Authorization header should be in the format "Bearer <token>"
		fields := strings.Fields(authHeader) // Splits by whitespace
		if len(fields) < 2 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization header format"})
			return
		}

		// Check if the authorization type is Bearer
		authType := strings.ToLower(fields[0])
		if authType != authorizationTypeBearer {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unsupported authorization type, 'Bearer' required"})
			return
		}

		// Get the token
		accessToken := fields[1]
		claims, err := utils.ValidateJWT(accessToken)
		if err != nil {
			// More specific error checking could be done here for expired tokens etc.
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token", "details": err.Error()})
			return
		}

		// Token is valid, set the userID in the context for downstream handlers
		// The UserID in claims is a string (as we defined it)
		c.Set(authorizationPayloadKey, claims.UserID) // claims.UserID is already a string

		// Continue to the next handler
		c.Next()
	}
}
