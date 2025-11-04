package chat

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
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

	if strings.TrimSpace(req.Content) == "" && req.Attachment == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Message content or attachment is required"})
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
			newChat, err := h.chatStore.CreateChat(c.Request.Context(), "", false, participantIDs)
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

	content := strings.TrimSpace(req.Content)
	message := &models.Message{
		ID:        uuid.New(),
		ChatID:    chatID,
		SenderID:  senderID,
		Content:   content,
		Timestamp: time.Now(),
		UpdatedAt: time.Now(),
		Status:    models.StatusSent,
	}
	if req.Attachment != nil {
		message.AttachmentURL = req.Attachment
	}

	err = h.messageStore.CreateMessage(c.Request.Context(), message)
	if err != nil {
		log.Printf("PostMessage: Failed to store message for chat %s: %v", chatID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send message"})
		return
	}

	_ = h.chatStore.UpdateParticipantReadThrough(c.Request.Context(), chatID, senderID, message.Timestamp)

	senderUser, err := h.userStore.GetUserByID(c.Request.Context(), senderID.String())
	if err == nil && senderUser != nil {
		message.Sender = senderUser.ToPublicUser()
	} else {
		log.Printf("PostMessage: Could not fetch sender details for user %s: %v", senderID, err)
		message.Sender = &models.PublicUser{ID: senderID, Username: "Unknown User"}
	}

	if h.wsHub != nil {
		log.Printf("PostMessage: Attempting to broadcast message %s via WebSocket Hub", message.ID)
		if createdChat != nil {
			createdChat.LastMessage = message
		}
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

func (h *RestHandler) CreateChat(c *gin.Context) {
	var req models.CreateChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data", "details": err.Error()})
		return
	}

	callerIDString, _ := c.Get("userID")
	callerID, err := uuid.Parse(callerIDString.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user session"})
		return
	}

	participantsSet := map[uuid.UUID]struct{}{callerID: {}}
	for _, participantID := range req.ParticipantIDs {
		if participantID == callerID {
			continue
		}
		participantsSet[participantID] = struct{}{}
	}

	if len(participantsSet) <= 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "At least one other participant is required"})
		return
	}

	participants := make([]uuid.UUID, 0, len(participantsSet))
	for id := range participantsSet {
		participants = append(participants, id)
	}

	chatName := strings.TrimSpace(req.Name)
	isGroup := chatName != "" || len(participants) > 2

	chat, err := h.chatStore.CreateChat(c.Request.Context(), chatName, isGroup, participants)
	if err != nil {
		log.Printf("CreateChat: failed to create chat: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create chat"})
		return
	}

	participantsDetails, err := h.chatStore.GetAllParticipantsInChat(c.Request.Context(), chat.ID)
	if err != nil {
		log.Printf("CreateChat: failed to fetch participants for chat %s: %v", chat.ID, err)
	}
	chat.OtherParticipants = filterParticipantsForUser(participantsDetails, callerID)
	chat.UnreadCount = 0
	now := time.Now()
	chat.LastReadAt = &now
	chat.LastMessage = nil

	if updateErr := h.chatStore.UpdateParticipantReadThrough(c.Request.Context(), chat.ID, callerID, now); updateErr != nil {
		log.Printf("CreateChat: failed to seed read state for user %s in chat %s: %v", callerID, chat.ID, updateErr)
	}

	if h.wsHub != nil {
		h.wsHub.BroadcastNewChat(chat, participantsDetails, callerID)
	}

	c.JSON(http.StatusCreated, chat)
}

func (h *RestHandler) UpdateChat(c *gin.Context) {
	chatID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid chat ID"})
		return
	}

	var req models.UpdateChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data", "details": err.Error()})
		return
	}
	if req.Name == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No chat updates specified"})
		return
	}

	callerIDString, _ := c.Get("userID")
	callerID, err := uuid.Parse(callerIDString.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user session"})
		return
	}

	chat, err := h.chatStore.UpdateChatName(c.Request.Context(), chatID, strings.TrimSpace(*req.Name))
	if err != nil {
		if errors.Is(err, store.ErrChatNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Chat not found"})
			return
		}
		log.Printf("UpdateChat: failed to update chat %s: %v", chatID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update chat"})
		return
	}

	participantsDetails, err := h.chatStore.GetAllParticipantsInChat(c.Request.Context(), chatID)
	if err == nil {
		chat.OtherParticipants = filterParticipantsForUser(participantsDetails, callerID)
	}

	if unread, err := h.messageStore.GetUnreadMessageCountForUserInChat(c.Request.Context(), chatID, callerID); err == nil {
		chat.UnreadCount = unread
	}

	if h.wsHub != nil {
		h.wsHub.BroadcastChatUpdated(chatID, chat.Name, participantsDetails)
	}

	c.JSON(http.StatusOK, chat)
}

func (h *RestHandler) AddParticipants(c *gin.Context) {
	chatID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid chat ID"})
		return
	}

	var req models.ModifyChatParticipantsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data", "details": err.Error()})
		return
	}

	if len(req.UserIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User IDs are required"})
		return
	}

	for _, userID := range req.UserIDs {
		if err := h.chatStore.AddUserToChat(c.Request.Context(), chatID, userID); err != nil {
			log.Printf("AddParticipants: failed to add user %s to chat %s: %v", userID, chatID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add participant"})
			return
		}
	}

	callerIDString, _ := c.Get("userID")
	callerID, err := uuid.Parse(callerIDString.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user session"})
		return
	}

	chat, err := h.chatStore.GetChatByID(c.Request.Context(), chatID)
	if err != nil {
		log.Printf("AddParticipants: failed to load chat %s: %v", chatID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load chat"})
		return
	}

	participantsDetails, err := h.chatStore.GetAllParticipantsInChat(c.Request.Context(), chatID)
	if err != nil {
		log.Printf("AddParticipants: failed to fetch participants for chat %s: %v", chatID, err)
	}
	chat.OtherParticipants = filterParticipantsForUser(participantsDetails, callerID)

	if unread, err := h.messageStore.GetUnreadMessageCountForUserInChat(c.Request.Context(), chatID, callerID); err == nil {
		chat.UnreadCount = unread
	}

	if h.wsHub != nil {
		h.wsHub.BroadcastNewChat(chat, participantsDetails, callerID, req.UserIDs...)
		h.wsHub.BroadcastChatUpdated(chatID, chat.Name, participantsDetails)
	}

	c.JSON(http.StatusOK, chat)
}

func (h *RestHandler) RemoveParticipant(c *gin.Context) {
	chatID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid chat ID"})
		return
	}
	userID, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	if err := h.chatStore.RemoveUserFromChat(c.Request.Context(), chatID, userID); err != nil {
		log.Printf("RemoveParticipant: failed to remove user %s from chat %s: %v", userID, chatID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove participant"})
		return
	}

	if h.wsHub != nil {
		chat, err := h.chatStore.GetChatByID(c.Request.Context(), chatID)
		if err == nil {
			participants, participantsErr := h.chatStore.GetAllParticipantsInChat(c.Request.Context(), chatID)
			if participantsErr == nil {
				h.wsHub.BroadcastChatUpdated(chatID, chat.Name, participants)
			}
		}
	}

	c.Status(http.StatusNoContent)
}

func (h *RestHandler) MarkChatRead(c *gin.Context) {
	chatID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid chat ID"})
		return
	}

	var req models.MarkChatReadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data", "details": err.Error()})
		return
	}

	callerIDString, _ := c.Get("userID")
	callerID, err := uuid.Parse(callerIDString.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user session"})
		return
	}

	readThrough := time.Now()
	if req.ReadThrough != nil && !req.ReadThrough.IsZero() {
		readThrough = *req.ReadThrough
	}

	if err := h.chatStore.UpdateParticipantReadThrough(c.Request.Context(), chatID, callerID, readThrough); err != nil {
		if errors.Is(err, store.ErrParticipantNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Participant not found in chat"})
			return
		}
		log.Printf("MarkChatRead: failed to update read state for chat %s: %v", chatID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update read state"})
		return
	}

	unread, err := h.messageStore.GetUnreadMessageCountForUserInChat(c.Request.Context(), chatID, callerID)
	if err != nil {
		log.Printf("MarkChatRead: failed to compute unread count for chat %s: %v", chatID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to compute unread count"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"chatId": chatID, "unreadCount": unread, "readThrough": readThrough})
}

func (h *RestHandler) UpdateMessage(c *gin.Context) {
	messageID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid message ID"})
		return
	}

	var req models.UpdateMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data", "details": err.Error()})
		return
	}

	if (req.Content == nil || strings.TrimSpace(*req.Content) == "") && req.Attachment == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Message content or attachment required"})
		return
	}

	callerIDString, _ := c.Get("userID")
	callerID, err := uuid.Parse(callerIDString.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user session"})
		return
	}

	content := ""
	if req.Content != nil {
		content = strings.TrimSpace(*req.Content)
	}

	updated, err := h.messageStore.UpdateMessageContent(c.Request.Context(), messageID, callerID, content, req.Attachment)
	if err != nil {
		if errors.Is(err, store.ErrMessageNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Message not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if h.wsHub != nil {
		h.wsHub.BroadcastMessageUpdate(updated)
	}

	c.JSON(http.StatusOK, updated)
}

func (h *RestHandler) DeleteMessage(c *gin.Context) {
	messageID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid message ID"})
		return
	}

	callerIDString, _ := c.Get("userID")
	callerID, err := uuid.Parse(callerIDString.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user session"})
		return
	}

	deleted, err := h.messageStore.SoftDeleteMessage(c.Request.Context(), messageID, callerID)
	if err != nil {
		if errors.Is(err, store.ErrMessageNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Message not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if h.wsHub != nil {
		h.wsHub.BroadcastMessageDeletion(deleted)
	}

	c.JSON(http.StatusOK, deleted)
}

func filterParticipantsForUser(participants []*models.PublicUser, userID uuid.UUID) []*models.PublicUser {
	filtered := make([]*models.PublicUser, 0, len(participants))
	for _, participant := range participants {
		if participant == nil || participant.ID == userID {
			continue
		}
		filtered = append(filtered, participant)
	}
	return filtered
}
