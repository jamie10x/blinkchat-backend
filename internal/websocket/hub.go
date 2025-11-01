package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"log"
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
			newChat, createErr := h.chatStore.CreateChat(ctx, participantIDs)
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

	dbMessage := &models.Message{
		ID:        uuid.New(),
		ChatID:    chatID,
		SenderID:  senderClient.userID,
		Content:   payload.Content,
		Timestamp: time.Now(),
		Status:    models.StatusSent,
	}
	if err := h.messageStore.CreateMessage(ctx, dbMessage); err != nil {
		log.Printf("WS Hub (NewMsgViaWS): Error saving message to DB: %v", err)
		senderClient.SendMessage(MessageTypeError, ErrorPayload{Message: "Failed to send message (DB error)"})
		return
	}

	senderUser, err := h.userStore.GetUserByID(ctx, senderClient.userID.String())
	if err == nil && senderUser != nil {
		dbMessage.Sender = senderUser.ToPublicUser()
	} else {
		log.Printf("WS Hub (NewMsgViaWS): Could not fetch sender details for user %s: %v", senderClient.userID, err)
		dbMessage.Sender = &models.PublicUser{ID: senderClient.userID, Username: "Unknown"}
	}

	ackPayload := MessageSentAckPayload{
		ClientTempID: payload.ClientTempID,
		ServerMsgID:  dbMessage.ID,
		ChatID:       chatID,
		Timestamp:    models.JSONTime(dbMessage.Timestamp),
		Status:       dbMessage.Status,
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
	h.clientsMux.RLock()
	defer h.clientsMux.RUnlock()

	for _, targetUserID := range targetUserIDs {
		if userClients, found := h.clients[targetUserID]; found {
			log.Printf("Hub: Broadcasting message %s to user %s (chat %s)", message.ID, targetUserID, message.ChatID)
			for clientInstance := range userClients {
				clientInstance.SendMessage(MessageTypeNewMessage, message)
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
