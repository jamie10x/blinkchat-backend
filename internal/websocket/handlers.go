package websocket

import (
	"github.com/google/uuid"
	"log"
	"net/http"

	"blinkchat-backend/internal/utils" // Use your actual module path

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// upgrader is used to upgrade HTTP connections to WebSocket connections.
// CheckOrigin allows all origins for development purposes.
// In production, you should restrict this to your frontend's origin.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all connections by default for development.
		// TODO: In production, implement a proper origin check:
		// origin := r.Header.Get("Origin")
		// return origin == "https://yourfrontend.com" || origin == "http://localhost:3000"
		return true
	},
}

// WSHandler handles WebSocket connection requests.
type WSHandler struct {
	hub *Hub
	// userStore store.UserStore // Could be added if more user validation is needed beyond JWT
}

// NewWSHandler creates a new WSHandler.
func NewWSHandler(hub *Hub /*, userStore store.UserStore*/) *WSHandler {
	return &WSHandler{
		hub: hub,
		// userStore: userStore,
	}
}

// HandleWebSocketConnection upgrades HTTP GET requests to WebSocket connections.
// It expects a JWT token as a query parameter for authentication (e.g., /ws?token=YOUR_JWT_HERE).
func (h *WSHandler) HandleWebSocketConnection(c *gin.Context) {
	// 1. Authenticate the connection request using JWT from query parameter
	tokenString := c.Query("token")
	if tokenString == "" {
		log.Println("WS Handler: Missing token in query parameter")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing authentication token"})
		// Note: For WebSockets, we typically don't send a JSON response on upgrade failure.
		// The HTTP connection is upgraded or it's not. We might just return from the handler.
		// Gin's Abort might not work as expected before an upgrade.
		// For simplicity, let's just return. The client will see a failed connection.
		c.AbortWithStatus(http.StatusUnauthorized) // More appropriate for pre-upgrade denial
		return
	}

	claims, err := utils.ValidateJWT(tokenString)
	if err != nil {
		log.Printf("WS Handler: Invalid token: %v", err)
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	userID, err := uuid.Parse(claims.UserID) // claims.UserID is string from JWT
	if err != nil {
		log.Printf("WS Handler: Invalid UserID in token claims: %v", err)
		c.AbortWithStatus(http.StatusUnauthorized) // Or InternalServerError if claims structure is wrong
		return
	}

	// TODO: Optional: Further check if user with userID exists in DB
	// _, userErr := h.userStore.GetUserByID(c.Request.Context(), userID.String())
	// if userErr != nil {
	// 	log.Printf("WS Handler: User %s from token not found in DB: %v", userID, userErr)
	// 	c.AbortWithStatus(http.StatusForbidden) // User validly authenticated but not allowed (e.g. deleted)
	// 	return
	// }

	log.Printf("WS Handler: Authenticated user %s for WebSocket connection", userID)

	// 2. Upgrade the HTTP connection to a WebSocket connection
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WS Handler: Failed to upgrade connection for user %s: %v", userID, err)
		// Gin has already sent a response if Upgrade fails, so just return.
		return
	}
	log.Printf("WS Handler: Connection upgraded for user %s from %s", userID, conn.RemoteAddr())

	// 3. Create a new Client instance
	client := NewClient(h.hub, conn, userID)

	// 4. Register the new client with the hub
	h.hub.register <- client // Send the client to the hub's register channel

	// 5. Start the client's read and write pumps in separate goroutines
	// These goroutines will handle message flow for this client.
	go client.writePump()
	go client.readPump()

	// The HTTP handler function finishes here. The WebSocket connection
	// is now managed by the client's readPump and writePump goroutines.
	log.Printf("WS Handler: Client read/write pumps started for user %s", userID)
}
