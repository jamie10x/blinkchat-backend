package chat

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"blinkchat-backend/internal/models"
	"blinkchat-backend/internal/store"
	"blinkchat-backend/internal/websocket"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RestHandler handles REST API requests related to messaging.
type RestHandler struct {
	chatStore    store.ChatStore
	messageStore store.MessageStore
	userStore    store.UserStore
	wsHub        *websocket.Hub
}

func NewRestHandler(cs store.ChatStore, ms store.MessageStore, us store.UserStore, hub *websocket.Hub) *RestHandler {
	return &RestHandler{
		chatStore:    cs,
		messageStore: ms,
		userStore:    us,
		wsHub:        hub,
	}
}

func (h *RestHandler) PostMessage(c *gin.Context) {
	var req models.CreateMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data", "details": err.Error()})
		return
	}

	senderIDString, _ := c.Get("userID")
	senderID, err := uuid.Parse(senderIDString.(string))
	if err != nil {
		log.Printf("PostMessage: Invalid senderID from token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user session"})
		return
	}

	var chatID uuid.UUID
	var createdChat *models.Chat

	if req.ChatID != nil {
		chatID = *req.ChatID
	} else if req.ReceiverID != nil {
		receiverID := *req.ReceiverID
		if senderID == receiverID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot send message to yourself in this context"})
			return
		}

		participantIDs := []uuid.UUID{senderID, receiverID}
		existingChat, err := h.chatStore.GetChatByParticipantIDs(c.Request.Context(), participantIDs)
		if err != nil && !errors.Is(err, store.ErrChatNotFound) {
			log.Printf("PostMessage: Error finding chat for participants %v: %v", participantIDs, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process message"})
			return
		}

		if existingChat != nil {
			chatID = existingChat.ID
		} else {
			newChat, err := h.chatStore.CreateChat(c.Request.Context(), participantIDs)
			if err != nil {
				log.Printf("PostMessage: Error creating chat for participants %v: %v", participantIDs, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create chat for message"})
				return
			}
			chatID = newChat.ID
			createdChat = newChat
		}
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Either chatId or receiverId must be provided"})
		return
	}

	message := &models.Message{
		ID:        uuid.New(),
		ChatID:    chatID,
		SenderID:  senderID,
		Content:   req.Content,
		Timestamp: time.Now(),
		Status:    models.StatusSent,
	}

	err = h.messageStore.CreateMessage(c.Request.Context(), message)
	if err != nil {
		log.Printf("PostMessage: Failed to store message for chat %s: %v", chatID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send message"})
		return
	}

	senderUser, err := h.userStore.GetUserByID(c.Request.Context(), senderID.String())
	if err == nil && senderUser != nil {
		message.Sender = senderUser.ToPublicUser()
	} else {
		log.Printf("PostMessage: Could not fetch sender details for user %s: %v", senderID, err)
		message.Sender = &models.PublicUser{ID: senderID, Username: "Unknown User"}
	}

	if h.wsHub != nil {
		log.Printf("PostMessage: Attempting to broadcast message %s via WebSocket Hub", message.ID)
		h.wsHub.BroadcastChatMessage(message, createdChat)
	} else {
		log.Println("PostMessage: WebSocket Hub is nil, skipping broadcast.")
	}

	c.JSON(http.StatusCreated, message)
}

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

	limitStr := c.DefaultQuery("limit", "20")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 100 {
		limit = 20
	}
	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	messages, err := h.messageStore.GetMessagesByChatID(c.Request.Context(), chatID, limit, offset)
	if err != nil {
		log.Printf("GetMessagesByChatID: Failed to get messages for chat %s: %v", chatID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve messages"})
		return
	}

	if messages == nil {
		messages = make([]*models.Message, 0)
	}
	c.JSON(http.StatusOK, messages)
}

func (h *RestHandler) GetChats(c *gin.Context) {
	userIDString, _ := c.Get("userID")
	userID, err := uuid.Parse(userIDString.(string))
	if err != nil {
		log.Printf("GetChats: Invalid userID from token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user session"})
		return
	}

	limitStr := c.DefaultQuery("limit", "20")
	offsetStr := c.DefaultQuery("offset", "0")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 50 {
		limit = 20
	}
	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	chats, err := h.chatStore.GetUserChats(c.Request.Context(), userID, limit, offset)
	if err != nil {
		log.Printf("GetChats: Failed to get chats for user %s: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve chats"})
		return
	}

	if chats == nil {
		chats = make([]*models.Chat, 0)
	}
	c.JSON(http.StatusOK, chats)
}
