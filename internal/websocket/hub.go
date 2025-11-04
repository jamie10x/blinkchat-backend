package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"blinkchat-backend/internal/models"
	"blinkchat-backend/internal/store"

	"github.com/google/uuid"
)

// Hub maintains active WebSocket clients and broadcasts messages.
type Hub struct {
	clients    map[uuid.UUID]map[*Client]bool
	clientsMux sync.RWMutex

	processMessage chan HubMessage
	register       chan *Client
	unregister     chan *Client

	userStore    store.UserStore
	chatStore    store.ChatStore
	messageStore store.MessageStore
}

// NewHub returns a Hub wired to the provided stores.
func NewHub(us store.UserStore, cs store.ChatStore, ms store.MessageStore) *Hub {
	return &Hub{
		clients:        make(map[uuid.UUID]map[*Client]bool),
		processMessage: make(chan HubMessage),
		register:       make(chan *Client),
		unregister:     make(chan *Client),
		userStore:      us,
		chatStore:      cs,
		messageStore:   ms,
	}
}

// Run processes hub events until the process exits.
func (h *Hub) Run() {
	log.Println("WebSocket Hub: Starting...")
	for {
		select {
		case client := <-h.register:
			h.clientsMux.Lock()
			if _, ok := h.clients[client.userID]; !ok {
				h.clients[client.userID] = make(map[*Client]bool)
			}
			h.clients[client.userID][client] = true
			log.Printf("WebSocket Hub: Client registered (User: %s, RemoteAddr: %s). Total for user: %d", client.userID, client.conn.RemoteAddr(), len(h.clients[client.userID]))
			h.clientsMux.Unlock()

		case client := <-h.unregister:
			h.clientsMux.Lock()
			if userClients, ok := h.clients[client.userID]; ok {
				if _, clientExists := userClients[client]; clientExists {
					close(client.send)
					delete(userClients, client)
					if len(userClients) == 0 {
						delete(h.clients, client.userID)
					}
					log.Printf("WebSocket Hub: Client unregistered (User: %s, RemoteAddr: %s). Remaining for user: %d", client.userID, client.conn.RemoteAddr(), len(userClients))
				}
			}
			h.clientsMux.Unlock()

		case hubMsg := <-h.processMessage:
			h.handleIncomingMessage(hubMsg.client, hubMsg.rawJSON)
		}
	}
}

func (h *Hub) handleIncomingMessage(senderClient *Client, rawJSON []byte) {
	var wsMsg WebSocketMessage
	if err := json.Unmarshal(rawJSON, &wsMsg); err != nil {
		log.Printf("WebSocket Hub: Error unmarshalling message from User %s: %v. Raw: %s", senderClient.userID, err, string(rawJSON))
		senderClient.SendMessage(MessageTypeError, ErrorPayload{Message: "Invalid message format"})
		return
	}

	log.Printf("WebSocket Hub: Processing message type '%s' from User %s", wsMsg.Type, senderClient.userID)
	ctx := context.Background()

	switch wsMsg.Type {
	case MessageTypeNewMessage:
		var payload NewMessagePayload
		payloadBytes, _ := json.Marshal(wsMsg.Payload)
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			log.Printf("WebSocket Hub: Error unmarshalling NewMessagePayload from User %s: %v", senderClient.userID, err)
			senderClient.SendMessage(MessageTypeError, ErrorPayload{Message: "Invalid new_message payload"})
			return
		}
		if strings.TrimSpace(payload.Content) == "" && payload.AttachmentURL == nil {
			senderClient.SendMessage(MessageTypeError, ErrorPayload{Message: "Message content or attachment required"})
			return
		}
		h.handleNewChatMessageViaWS(ctx, senderClient, payload)

	case MessageTypeMessageStatusUpdate:
		var payload MessageStatusUpdatePayload
		payloadBytes, _ := json.Marshal(wsMsg.Payload)
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			log.Printf("WebSocket Hub: Error unmarshalling MessageStatusUpdatePayload from User %s: %v", senderClient.userID, err)
			senderClient.SendMessage(MessageTypeError, ErrorPayload{Message: "Invalid message_status_update payload"})
			return
		}
		h.handleMessageStatusUpdate(ctx, senderClient, payload)

	case MessageTypeTypingIndicator:
		var payload TypingIndicatorPayload
		payloadBytes, _ := json.Marshal(wsMsg.Payload)
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			log.Printf("WebSocket Hub: Error unmarshalling TypingIndicatorPayload from User %s: %v", senderClient.userID, err)
			senderClient.SendMessage(MessageTypeError, ErrorPayload{Message: "Invalid typing_indicator payload"})
			return
		}
		h.handleTypingIndicator(ctx, senderClient, payload)

	default:
		log.Printf("WebSocket Hub: Unknown message type '%s' from User %s", wsMsg.Type, senderClient.userID)
		senderClient.SendMessage(MessageTypeError, ErrorPayload{Message: "Unknown message type"})
	}
}

func (h *Hub) handleNewChatMessageViaWS(ctx context.Context, senderClient *Client, payload NewMessagePayload) {
	var chatID uuid.UUID
	var createdChat *models.Chat
	var targetUserIDs []uuid.UUID

	if payload.ChatID != nil {
		chatID = *payload.ChatID
		allParticipants, err := h.chatStore.GetAllParticipantsInChat(ctx, chatID)
		if err != nil {
			log.Printf("WS Hub (NewMsgViaWS): Error fetching participants for chat %s: %v", chatID, err)
			senderClient.SendMessage(MessageTypeError, ErrorPayload{Message: "Error processing message details"})
			return
		}
		for _, p := range allParticipants {
			if p.ID != senderClient.userID {
				targetUserIDs = append(targetUserIDs, p.ID)
			}
		}
	} else if payload.ReceiverID != nil {
		receiverID := *payload.ReceiverID
		if senderClient.userID == receiverID {
			senderClient.SendMessage(MessageTypeError, ErrorPayload{Message: "Cannot send message to yourself"})
			return
		}
		participantIDs := []uuid.UUID{senderClient.userID, receiverID}
		existingChat, err := h.chatStore.GetChatByParticipantIDs(ctx, participantIDs)
		if err != nil && !errors.Is(err, store.ErrChatNotFound) {
			log.Printf("WS Hub (NewMsgViaWS): Error finding chat: %v", err)
			senderClient.SendMessage(MessageTypeError, ErrorPayload{Message: "Error processing message"})
			return
		}
		if existingChat != nil {
			chatID = existingChat.ID
		} else {
			newChat, createErr := h.chatStore.CreateChat(ctx, "", false, participantIDs)
			if createErr != nil {
				log.Printf("WS Hub (NewMsgViaWS): Error creating chat: %v", createErr)
				senderClient.SendMessage(MessageTypeError, ErrorPayload{Message: "Error creating chat for message"})
				return
			}
			chatID = newChat.ID
			createdChat = newChat
		}
		targetUserIDs = append(targetUserIDs, receiverID)
	} else {
		senderClient.SendMessage(MessageTypeError, ErrorPayload{Message: "New message requires chatId or receiverId"})
		return
	}

	content := strings.TrimSpace(payload.Content)
	dbMessage := &models.Message{
		ID:        uuid.New(),
		ChatID:    chatID,
		SenderID:  senderClient.userID,
		Content:   content,
		Timestamp: time.Now(),
		Status:    models.StatusSent,
	}
	dbMessage.UpdatedAt = dbMessage.Timestamp
	if payload.AttachmentURL != nil {
		dbMessage.AttachmentURL = payload.AttachmentURL
	}
	if err := h.messageStore.CreateMessage(ctx, dbMessage); err != nil {
		log.Printf("WS Hub (NewMsgViaWS): Error saving message to DB: %v", err)
		senderClient.SendMessage(MessageTypeError, ErrorPayload{Message: "Failed to send message (DB error)"})
		return
	}

	_ = h.chatStore.UpdateParticipantReadThrough(ctx, chatID, senderClient.userID, dbMessage.Timestamp)

	senderUser, err := h.userStore.GetUserByID(ctx, senderClient.userID.String())
	if err == nil && senderUser != nil {
		dbMessage.Sender = senderUser.ToPublicUser()
	} else {
		log.Printf("WS Hub (NewMsgViaWS): Could not fetch sender details for user %s: %v", senderClient.userID, err)
		dbMessage.Sender = &models.PublicUser{ID: senderClient.userID, Username: "Unknown"}
	}

	if createdChat != nil {
		createdChat.LastMessage = dbMessage
	}

	ackPayload := MessageSentAckPayload{
		ClientTempID:  payload.ClientTempID,
		ServerMsgID:   dbMessage.ID,
		ChatID:        chatID,
		Timestamp:     models.JSONTime(dbMessage.Timestamp),
		Status:        dbMessage.Status,
		AttachmentURL: dbMessage.AttachmentURL,
	}
	senderClient.SendMessage(MessageTypeMessageSentAck, ackPayload)

	h.broadcastMessageToTargets(dbMessage, targetUserIDs, createdChat)
}

// BroadcastChatMessage broadcasts a stored message to connected recipients.
func (h *Hub) BroadcastChatMessage(message *models.Message, initialChat *models.Chat) {
	log.Printf("Hub: Received message %s for chat %s to broadcast (Sender: %s)", message.ID, message.ChatID, message.SenderID)
	ctx := context.Background()
	var targetUserIDs []uuid.UUID

	allParticipantsInChat, err := h.chatStore.GetAllParticipantsInChat(ctx, message.ChatID)
	if err != nil {
		log.Printf("Hub (BroadcastChatMessage): Error fetching participants for chat %s: %v", message.ChatID, err)
		return
	}

	for _, p := range allParticipantsInChat {
		if p.ID != message.SenderID {
			targetUserIDs = append(targetUserIDs, p.ID)
		}
	}

	h.broadcastMessageToTargets(message, targetUserIDs, initialChat)
}

func (h *Hub) broadcastMessageToTargets(message *models.Message, targetUserIDs []uuid.UUID, newChatInfo *models.Chat) {
	if len(targetUserIDs) == 0 {
		return
	}

	if newChatInfo != nil {
		chatCopy := *newChatInfo
		chatCopy.LastMessage = cloneMessage(message)
		participants, err := h.chatStore.GetAllParticipantsInChat(context.Background(), message.ChatID)
		if err != nil {
			log.Printf("Hub: Failed to fetch participants for new chat broadcast %s: %v", message.ChatID, err)
		} else {
			h.BroadcastNewChat(&chatCopy, participants, message.SenderID, targetUserIDs...)
		}
	}

	h.clientsMux.RLock()
	defer h.clientsMux.RUnlock()

	for _, targetUserID := range targetUserIDs {
		if userClients, found := h.clients[targetUserID]; found {
			log.Printf("Hub: Broadcasting message %s to user %s (chat %s)", message.ID, targetUserID, message.ChatID)
			for clientInstance := range userClients {
				clientInstance.SendMessage(MessageTypeNewMessage, cloneMessage(message))
			}
		} else {
			log.Printf("Hub: Recipient %s for chat %s is not connected for message %s.", targetUserID, message.ChatID, message.ID)
		}
	}
}

func (h *Hub) handleMessageStatusUpdate(ctx context.Context, senderClient *Client, payload MessageStatusUpdatePayload) {
	err := h.messageStore.UpdateMessageStatus(ctx, payload.MessageID, payload.Status)
	if err != nil {
		log.Printf("WebSocket Hub (StatusUpdate): Error updating message status in DB: %v", err)
		senderClient.SendMessage(MessageTypeError, ErrorPayload{Message: "Failed to update message status (DB error)"})
		return
	}
	log.Printf("WebSocket Hub (StatusUpdate): Message %s status updated to %s for chat %s by user %s",
		payload.MessageID, payload.Status, payload.ChatID, senderClient.userID)

	originalMessage, err := h.messageStore.GetMessageByID(ctx, payload.MessageID)
	if err != nil {
		log.Printf("WebSocket Hub (StatusUpdate): Could not find original message %s: %v", payload.MessageID, err)
		return
	}

	broadcastPayload := MessageStatusUpdatePayload{
		MessageID: payload.MessageID,
		ChatID:    payload.ChatID,
		Status:    payload.Status,
		UserID:    senderClient.userID,
		Timestamp: models.JSONTime(time.Now()),
	}

	h.clientsMux.RLock()
	defer h.clientsMux.RUnlock()

	if originalMessage.SenderID != senderClient.userID {
		if senderUserClients, found := h.clients[originalMessage.SenderID]; found {
			for clientInstance := range senderUserClients {
				clientInstance.SendMessage(MessageTypeMessageStatusUpdate, broadcastPayload)
			}
		}
	}
	if recipientUserClients, found := h.clients[senderClient.userID]; found {
		for clientInstance := range recipientUserClients {
			clientInstance.SendMessage(MessageTypeMessageStatusUpdate, broadcastPayload)
		}
	}
}

func (h *Hub) handleTypingIndicator(ctx context.Context, senderClient *Client, payload TypingIndicatorPayload) {
	log.Printf("WebSocket Hub (Typing): User %s in chat %s isTyping: %v",
		payload.UserID, payload.ChatID, payload.IsTyping)

	if payload.UserID != senderClient.userID {
		log.Printf("WS Hub (Typing): Mismatched UserID in payload (%s) and client session (%s)", payload.UserID, senderClient.userID)
		senderClient.SendMessage(MessageTypeError, ErrorPayload{Message: "Typing indicator user ID mismatch"})
		return
	}

	allParticipants, err := h.chatStore.GetAllParticipantsInChat(ctx, payload.ChatID)
	if err != nil {
		log.Printf("WS Hub (Typing): Error fetching participants for chat %s: %v", payload.ChatID, err)
		senderClient.SendMessage(MessageTypeError, ErrorPayload{Message: "Chat not found for typing indicator"})
		return
	}

	var targetUserIDs []uuid.UUID
	for _, p := range allParticipants {
		if p.ID != senderClient.userID {
			targetUserIDs = append(targetUserIDs, p.ID)
		}
	}

	h.clientsMux.RLock()
	for _, targetUserID := range targetUserIDs {
		if userClients, found := h.clients[targetUserID]; found {
			for clientInstance := range userClients {
				clientInstance.SendMessage(MessageTypeTypingIndicator, payload)
			}
		}
	}
	h.clientsMux.RUnlock()
}

// BroadcastToUser sends a message to all connected clients for a user.
func (h *Hub) BroadcastToUser(userID uuid.UUID, msgType string, payload interface{}) {
	h.clientsMux.RLock()
	defer h.clientsMux.RUnlock()
	if userClients, found := h.clients[userID]; found {
		for client := range userClients {
			client.SendMessage(msgType, payload)
		}
	}
}

// BroadcastNewChat notifies chat participants about a newly created chat or newly added membership.
func (h *Hub) BroadcastNewChat(chat *models.Chat, participants []*models.PublicUser, initiator uuid.UUID, explicitTargets ...uuid.UUID) {
	if chat == nil {
		return
	}

	ctx := context.Background()
	if participants == nil {
		var err error
		participants, err = h.chatStore.GetAllParticipantsInChat(ctx, chat.ID)
		if err != nil {
			log.Printf("BroadcastNewChat: failed to load participants for chat %s: %v", chat.ID, err)
			return
		}
	}

	targetSet := make(map[uuid.UUID]struct{})
	if len(explicitTargets) > 0 {
		for _, id := range explicitTargets {
			if id != initiator {
				targetSet[id] = struct{}{}
			}
		}
	} else {
		for _, participant := range participants {
			if participant.ID != initiator {
				targetSet[participant.ID] = struct{}{}
			}
		}
	}

	if len(targetSet) == 0 {
		return
	}

	h.clientsMux.RLock()
	defer h.clientsMux.RUnlock()

	for targetID := range targetSet {
		chatCopy := *chat
		chatCopy.OtherParticipants = filterParticipantsForViewer(participants, targetID)
		if chatCopy.LastMessage != nil {
			chatCopy.LastMessage = cloneMessage(chatCopy.LastMessage)
			if chatCopy.LastMessage.SenderID != targetID && chatCopy.UnreadCount == 0 {
				chatCopy.UnreadCount = 1
			}
		}
		payload := NewChatPayload{Chat: &chatCopy}
		if userClients, found := h.clients[targetID]; found {
			for client := range userClients {
				client.SendMessage(MessageTypeNewChat, payload)
			}
		}
	}
}

// BroadcastChatUpdated propagates chat metadata changes such as renames to all participants.
func (h *Hub) BroadcastChatUpdated(chatID uuid.UUID, name string, participants []*models.PublicUser) {
	ctx := context.Background()
	chat, err := h.chatStore.GetChatByID(ctx, chatID)
	if err != nil {
		log.Printf("BroadcastChatUpdated: failed to load chat %s: %v", chatID, err)
		return
	}
	chat.Name = name

	if participants == nil {
		participants, err = h.chatStore.GetAllParticipantsInChat(ctx, chatID)
		if err != nil {
			log.Printf("BroadcastChatUpdated: failed to load participants for chat %s: %v", chatID, err)
			return
		}
	}

	h.clientsMux.RLock()
	defer h.clientsMux.RUnlock()

	for _, participant := range participants {
		chatCopy := *chat
		chatCopy.OtherParticipants = filterParticipantsForViewer(participants, participant.ID)
		payload := ChatUpdatedPayload{Chat: &chatCopy}
		if userClients, found := h.clients[participant.ID]; found {
			for client := range userClients {
				client.SendMessage(MessageTypeChatUpdated, payload)
			}
		}
	}
}

// BroadcastMessageUpdate notifies all chat members of message edits.
func (h *Hub) BroadcastMessageUpdate(message *models.Message) {
	if message == nil {
		return
	}
	ctx := context.Background()
	participants, err := h.chatStore.GetAllParticipantsInChat(ctx, message.ChatID)
	if err != nil {
		log.Printf("BroadcastMessageUpdate: failed to load participants for chat %s: %v", message.ChatID, err)
		return
	}

	h.clientsMux.RLock()
	defer h.clientsMux.RUnlock()

	targetSet := make(map[uuid.UUID]struct{})
	targetSet[message.SenderID] = struct{}{}
	for _, participant := range participants {
		targetSet[participant.ID] = struct{}{}
	}

	for targetID := range targetSet {
		if userClients, found := h.clients[targetID]; found {
			for client := range userClients {
				client.SendMessage(MessageTypeMessageUpdated, MessageUpdatedPayload{Message: cloneMessage(message)})
			}
		}
	}
}

// BroadcastMessageDeletion informs participants that a message was removed.
func (h *Hub) BroadcastMessageDeletion(message *models.Message) {
	if message == nil {
		return
	}
	ctx := context.Background()
	participants, err := h.chatStore.GetAllParticipantsInChat(ctx, message.ChatID)
	if err != nil {
		log.Printf("BroadcastMessageDeletion: failed to load participants for chat %s: %v", message.ChatID, err)
		return
	}

	h.clientsMux.RLock()
	defer h.clientsMux.RUnlock()

	targetSet := make(map[uuid.UUID]struct{})
	targetSet[message.SenderID] = struct{}{}
	for _, participant := range participants {
		targetSet[participant.ID] = struct{}{}
	}

	for targetID := range targetSet {
		if userClients, found := h.clients[targetID]; found {
			for client := range userClients {
				client.SendMessage(MessageTypeMessageDeleted, MessageDeletedPayload{Message: cloneMessage(message)})
			}
		}
	}
}

func cloneMessage(message *models.Message) *models.Message {
	if message == nil {
		return nil
	}
	msgCopy := *message
	if message.Sender != nil {
		senderCopy := *message.Sender
		msgCopy.Sender = &senderCopy
	}
	if message.AttachmentURL != nil {
		attachmentCopy := *message.AttachmentURL
		msgCopy.AttachmentURL = &attachmentCopy
	}
	if message.DeletedAt != nil {
		t := *message.DeletedAt
		msgCopy.DeletedAt = &t
	}
	return &msgCopy
}

func filterParticipantsForViewer(participants []*models.PublicUser, viewer uuid.UUID) []*models.PublicUser {
	filtered := make([]*models.PublicUser, 0, len(participants))
	for _, participant := range participants {
		if participant == nil || participant.ID == viewer {
			continue
		}
		copy := *participant
		filtered = append(filtered, &copy)
	}
	return filtered
}
