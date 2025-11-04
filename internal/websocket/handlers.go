package websocket

import (
	"log"
	"net/http"

	"blinkchat-backend/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// WSHandler upgrades HTTP connections and attaches them to the hub.
type WSHandler struct {
	hub *Hub
}

// NewWSHandler returns a WebSocket handler bound to the hub.
func NewWSHandler(hub *Hub) *WSHandler {
	return &WSHandler{hub: hub}
}

// HandleWebSocketConnection upgrades the request and registers the resulting client.
func (h *WSHandler) HandleWebSocketConnection(c *gin.Context) {
	tokenString := c.Query("token")
	if tokenString == "" {
		log.Println("WS Handler: Missing token in query parameter")
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	claims, err := utils.ValidateJWT(tokenString)
	if err != nil {
		log.Printf("WS Handler: Invalid token: %v", err)
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		log.Printf("WS Handler: Invalid UserID in token claims: %v", err)
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	log.Printf("WS Handler: Authenticated user %s for WebSocket connection", userID)

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WS Handler: Failed to upgrade connection for user %s: %v", userID, err)
		return
	}
	log.Printf("WS Handler: Connection upgraded for user %s from %s", userID, conn.RemoteAddr())

	client := NewClient(h.hub, conn, userID)
	h.hub.register <- client

	go client.writePump()
	go client.readPump()

	log.Printf("WS Handler: Client read/write pumps started for user %s", userID)
}
