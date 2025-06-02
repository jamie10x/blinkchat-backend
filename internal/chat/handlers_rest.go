package chat

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"blinkchat-backend/internal/models" // Use your actual module path
	"blinkchat-backend/internal/store"  // Use your actual module path
	// "blinkchat-backend/internal/websocket" // We'll need this when integrating WebSocket broadcasts

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RestHandler handles REST API requests related to messaging.
type RestHandler struct {
	chatStore    store.ChatStore
	messageStore store.MessageStore
	userStore    store.UserStore // Added to fetch user details if needed, e.g., for participants
	// wsHub        *websocket.Hub // To broadcast new messages; add when WebSocketHub is ready
}

// NewRestHandler creates a new RestHandler.
func NewRestHandler(cs store.ChatStore, ms store.MessageStore, us store.UserStore /*, hub *websocket.Hub */) *RestHandler {
	return &RestHandler{
		chatStore:    cs,
		messageStore: ms,
		userStore:    us,
		// wsHub:        hub,
	}
}

// PostMessage handles requests to send a new message.
// POST /messages
func (h *RestHandler) PostMessage(c *gin.Context) {
	var req models.CreateMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data", "details": err.Error()})
		return
	}

	senderIDString, _ := c.Get("userID") // From AuthMiddleware
	senderID, err := uuid.Parse(senderIDString.(string))
	if err != nil {
		log.Printf("PostMessage: Invalid senderID from token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user session"})
		return
	}

	var chatID uuid.UUID
	var _ *models.Chat

	if req.ChatID != nil { // Client provided a ChatID
		chatID = *req.ChatID
		// Optional: Validate if sender is part of this chatID (complex, might be skipped for MVP)
		// chat, err = h.chatStore.GetChatByID(c.Request.Context(), chatID)
		// if err != nil { ... handle not found or other errors ... }
	} else if req.ReceiverID != nil { // Client provided a ReceiverID (for 1:1 chat)
		receiverID := *req.ReceiverID
		if senderID == receiverID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot send message to yourself in this context"})
			return
		}

		// Find or create 1:1 chat
		participantIDs := []uuid.UUID{senderID, receiverID}
		// Ensure consistent order for lookup if your GetChatByParticipantIDs relies on it
		// For example, sort participantIDs before calling GetChatByParticipantIDs
		// sort.Slice(participantIDs, func(i, j int) bool { return participantIDs[i].String() < participantIDs[j].String() })

		existingChat, err := h.chatStore.GetChatByParticipantIDs(c.Request.Context(), participantIDs)
		if err != nil && !errors.Is(err, store.ErrChatNotFound) {
			log.Printf("PostMessage: Error finding chat for participants %v: %v", participantIDs, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process message"})
			return
		}

		if existingChat != nil {
			chatID = existingChat.ID
			_ = existingChat
		} else {
			// Create new 1:1 chat
			newChat, err := h.chatStore.CreateChat(c.Request.Context(), participantIDs)
			if err != nil {
				log.Printf("PostMessage: Error creating chat for participants %v: %v", participantIDs, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create chat for message"})
				return
			}
			chatID = newChat.ID
			_ = newChat
		}
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Either chatId or receiverId must be provided"})
		return
	}

	// Create the message model
	message := &models.Message{
		ID:        uuid.New(),
		ChatID:    chatID,
		SenderID:  senderID,
		Content:   req.Content,
		Timestamp: time.Now(),
		Status:    models.StatusSent, // Initial status
	}

	err = h.messageStore.CreateMessage(c.Request.Context(), message)
	if err != nil {
		log.Printf("PostMessage: Failed to store message for chat %s: %v", chatID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send message"})
		return
	}

	// Populate sender details for the response
	senderUser, err := h.userStore.GetUserByID(c.Request.Context(), senderID.String())
	if err == nil && senderUser != nil {
		message.Sender = senderUser.ToPublicUser()
	} else {
		log.Printf("PostMessage: Could not fetch sender details for user %s: %v", senderID, err)
		// Continue without sender details if it fails, or handle error differently
	}

	// TODO: Broadcast message via WebSocket to other participant(s) in the chat
	// if h.wsHub != nil && chat != nil {
	//     // Determine recipients from 'chat' or by fetching participants of chatID
	//     // h.wsHub.BroadcastMessageToChat(chatID, message, senderID)
	// }

	c.JSON(http.StatusCreated, message)
}

// GetMessagesByChatID handles requests to retrieve messages for a specific chat.
// GET /messages?chatId=<uuid>&limit=<int>&offset=<int>
func (h *RestHandler) GetMessagesByChatID(c *gin.Context) {
	chatIDStr := c.Query("chatId")
	if chatIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chatId query parameter is required"})
		return
	}
	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid chatId format"})
		return
	}

	// Pagination parameters
	limitStr := c.DefaultQuery("limit", "20")  // Default limit 20
	offsetStr := c.DefaultQuery("offset", "0") // Default offset 0

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 100 { // Max limit 100
		limit = 20
	}
	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	// Optional: Validate if the requesting user is part of this chatID (security)
	// This would involve fetching chat participants for chatID and checking if current user is among them.

	messages, err := h.messageStore.GetMessagesByChatID(c.Request.Context(), chatID, limit, offset)
	if err != nil {
		log.Printf("GetMessagesByChatID: Failed to get messages for chat %s: %v", chatID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve messages"})
		return
	}

	if messages == nil { // Ensure we always return an array, even if empty
		messages = make([]*models.Message, 0)
	}

	c.JSON(http.StatusOK, messages)
}

// GetChats handles requests to list all conversations for the authenticated user.
// GET /chats?limit=<int>&offset=<int>
func (h *RestHandler) GetChats(c *gin.Context) {
	userIDString, _ := c.Get("userID") // From AuthMiddleware
	userID, err := uuid.Parse(userIDString.(string))
	if err != nil {
		log.Printf("GetChats: Invalid userID from token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user session"})
		return
	}

	// Pagination parameters
	limitStr := c.DefaultQuery("limit", "20")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 50 { // Max limit 50 for chat list
		limit = 20
	}
	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	// The store method GetUserChats needs to be fully implemented to populate
	// OtherParticipants, LastMessage, and UnreadCount.
	// This is a complex query.
	chats, err := h.chatStore.GetUserChats(c.Request.Context(), userID, limit, offset)
	if err != nil {
		log.Printf("GetChats: Failed to get chats for user %s: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve chats"})
		return
	}

	if chats == nil { // Ensure we always return an array
		chats = make([]*models.Chat, 0)
	}

	// For each chat, we need to populate OtherParticipants and LastMessage if not done by store
	// The current GetUserChats in store is a placeholder. A full implementation would do this.
	// For now, this will likely return chats with these fields empty.

	c.JSON(http.StatusOK, chats)
}
