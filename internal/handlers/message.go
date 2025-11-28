package handlers

import (
	"context"
	"fmt"
	"log"
	"time"

	"chat-backend/internal/models"
	"chat-backend/internal/services"
	"chat-backend/internal/utils"

	"github.com/gofiber/websocket/v2"
)

// buildVoiceURLFromWS constructs an absolute URL for a voice file from WebSocket connection
func buildVoiceURLFromWS(c *websocket.Conn, filename string) string {
	if filename == "" {
		return ""
	}

	// Try to get base URL from env first
	baseURL := utils.GetEnv("BASE_URL", "")
	if baseURL != "" {
		return fmt.Sprintf("%s/uploads/voices/%s", baseURL, filename)
	}

	// Extract host from WebSocket connection's underlying request
	// The Host header should be available
	host := c.Locals("host")
	if host == nil || host == "" {
		// Fallback to a default if host not available
		return fmt.Sprintf("/uploads/voices/%s", filename)
	}

	// Assume http by default for WebSocket-originated URLs
	// In production, you should configure BASE_URL
	return fmt.Sprintf("http://%s/uploads/voices/%s", host, filename)
}

func HandleMessage(c *websocket.Conn, msgType int, msg []byte, chatService *services.ChatService, userID int, username string, currentRoom *string, connID string) {
	if msgType != websocket.TextMessage {
		return
	}

	var wsMsg models.WSMessage
	if err := utils.SafeJSONParse(msg, &wsMsg); err != nil {
		utils.LogError(err, "JSON Parse")
		return
	}

	// Override username with authenticated user
	wsMsg.Username = username
	wsMsg.Timestamp = time.Now().UnixMilli()

	switch wsMsg.Event {
	case "join":
		handleJoin(c, &wsMsg, userID, username, currentRoom, chatService, connID)
	case "leave":
		handleLeave(c, &wsMsg, currentRoom, connID)
	case "chat":
		handleChat(c, &wsMsg, userID, username, *currentRoom, chatService)
	case "seen":
		handleSeen(c, &wsMsg, userID, username, *currentRoom, chatService)
	case "list":
		handleList(c, &wsMsg, userID, chatService)
	default:
		log.Printf("Unknown event: %s", wsMsg.Event)
	}
}

func handleSeen(c *websocket.Conn, msg *models.WSMessage, userID int, username string, currentRoom string, chatService *services.ChatService) {
	// msg.Timestamp is expected from client. Accept seconds or milliseconds.
	if currentRoom == "" && msg.Room == "" {
		// Unknown room, ignore
		return
	}
	roomID := currentRoom
	if roomID == "" {
		roomID = msg.Room
	}

	// Normalize timestamp
	ts := msg.Timestamp
	if ts == 0 {
		return
	}
	// If timestamp looks like seconds (less than 1e12), convert to milliseconds
	if ts < 1_000_000_000_000 {
		ts = ts * 1000
	}

	seenBefore := time.UnixMilli(ts)

	ctx := context.Background()
	updated, err := chatService.MarkMessagesSeen(ctx, roomID, userID, seenBefore)
	if err != nil {
		utils.LogError(err, "MarkMessagesSeen")
		// Inform client of failure
		utils.SendJSON(c, map[string]interface{}{
			"event":   "seen_failed",
			"room":    roomID,
			"error":   err.Error(),
			"updated": 0,
		})
		return
	}

	// Respond success to sender
	utils.SendJSON(c, models.WSMessage{
		Event:     "seen_successful",
		Room:      roomID,
		Timestamp: msg.Timestamp,
		Username:  username,
	})

	// Broadcast to other participants that messages were seen by this user
	Manager.Broadcast(roomID, map[string]interface{}{
		"event":     "messages_seen",
		"room":      roomID,
		"seen_by":   userID,
		"username":  username,
		"timestamp": msg.Timestamp,
		"count":     updated,
	}, "")
}

func handleJoin(c *websocket.Conn, msg *models.WSMessage, userID int, username string, currentRoom *string, chatService *services.ChatService, connID string) {
	if msg.Room == "" {
		return
	}

	// Leave previous room if any
	if *currentRoom != "" {
		Manager.Leave(*currentRoom, connID)
		// Notify previous room
		Manager.Broadcast(*currentRoom, models.WSMessage{
			Event:     "leave",
			Room:      *currentRoom,
			Username:  username,
			Timestamp: time.Now().UnixMilli(),
		}, "")
	}

	*currentRoom = msg.Room
	Manager.Join(*currentRoom, connID, c, userID, username)

	// Send confirmation to the sender
	utils.SendJSON(c, models.WSMessage{
		Event:     "joined",
		Room:      *currentRoom,
		Username:  username,
		Timestamp: time.Now().UnixMilli(),
	})

	// Notify room
	Manager.Broadcast(*currentRoom, models.WSMessage{
		Event:     "join",
		Room:      *currentRoom,
		Username:  username,
		Timestamp: time.Now().UnixMilli(),
	}, connID)

	// Send recent history as a single packed message
	messages, err := chatService.GetRecentMessages(context.Background(), *currentRoom, 50)
	if err == nil {
		var history []models.ChatHistoryItem
		for _, m := range messages {
			item := models.ChatHistoryItem{
				ID:            m.ID,
				Event:         "chat",
				Room:          *currentRoom,
				Text:          m.Content,
				Voice:         m.Voice,
				Username:      m.Username,
				Timestamp:     m.CreatedAt.UnixMilli(),
				IsYourMessage: m.UserID == userID,
				HasSeen:       m.HasSeen,
				ReplyTo:       m.ReplyTo,
			}
			// Build absolute voice URL if voice exists
			if m.Voice != nil && *m.Voice != "" {
				item.VoiceURL = buildVoiceURLFromWS(c, *m.Voice)
			}
			history = append(history, item)
		}

		// Get other user info for this room
		var otherUserInfo *models.UserInfo
		if otherUserID, err := chatService.GetOtherUserInRoom(context.Background(), *currentRoom, userID); err == nil {
			otherUserInfo, _ = chatService.GetUserInfo(context.Background(), otherUserID)
		}

		utils.SendJSON(c, models.WSMessage{
			Event:     "history",
			Room:      *currentRoom,
			History:   history,
			OtherUser: otherUserInfo,
			Timestamp: time.Now().UnixMilli(),
		})
	}
}

func handleLeave(c *websocket.Conn, msg *models.WSMessage, currentRoom *string, connID string) {
	if *currentRoom != "" {
		Manager.Leave(*currentRoom, connID)

		Manager.Broadcast(*currentRoom, models.WSMessage{
			Event:     "leave",
			Room:      *currentRoom,
			Username:  msg.Username,
			Timestamp: time.Now().UnixMilli(),
		}, connID)

		*currentRoom = ""
	}
}

func handleChat(c *websocket.Conn, msg *models.WSMessage, userID int, username string, currentRoom string, chatService *services.ChatService) {
	if currentRoom == "" {
		return
	}

	// Prepare content - can be nil for voice messages sent via WS
	var content *string
	if msg.Text != "" {
		content = &msg.Text
	}

	// Prepare voice - can be nil for text messages
	var voice *string
	if msg.Voice != "" {
		voice = &msg.Voice
	}

	// Validate: at least one of text or voice must be provided
	if content == nil && voice == nil {
		utils.SendJSON(c, map[string]interface{}{
			"event": "error",
			"error": "message must have either text or voice",
		})
		return
	}

	// Persist
	dbMsg := &models.Message{
		Room:     currentRoom,
		UserID:   userID,
		Username: username,
		Content:  content,
		Voice:    voice,
		ReplyTo:  msg.ReplyTo,
	}

	// If client provided only a reply_to_id, fetch that message and set ReplyTo
	if dbMsg.ReplyTo == nil && msg.ReplyToID != 0 {
		if ref, err := chatService.GetMessageByID(context.Background(), msg.ReplyToID); err == nil {
			dbMsg.ReplyTo = ref
		} else {
			// If lookup fails, log and continue without reply_to
			utils.LogError(err, "GetMessageByID")
		}
	}

	// Run in background or wait? For reliability, wait.
	if err := chatService.SaveMessage(context.Background(), dbMsg); err != nil {
		utils.LogError(err, "SaveMessage")
		return
	}

	// Build voice URL if voice exists
	voiceURL := ""
	if voice != nil && *voice != "" {
		voiceURL = buildVoiceURLFromWS(c, *voice)
	}

	// Broadcast to users currently in the room
	Manager.Broadcast(currentRoom, models.WSMessage{
		ID:        dbMsg.ID,
		Event:     "chat",
		Room:      currentRoom,
		Text:      msg.Text,
		Voice:     msg.Voice,
		VoiceURL:  voiceURL,
		Username:  username,
		Timestamp: dbMsg.CreatedAt.UnixMilli(),
		HasSeen:   dbMsg.HasSeen,
		ReplyTo:   dbMsg.ReplyTo,
	}, "") // Send to everyone including sender so they know it's confirmed

	// Notify room participants who are NOT currently in this room about the new message
	go notifyNewMessage(chatService, currentRoom, userID, username, msg.Text, dbMsg.CreatedAt.UnixMilli())
}

// notifyNewMessage sends a notification to room participants who are not currently viewing the room
func notifyNewMessage(chatService *services.ChatService, roomID string, senderID int, senderUsername string, messageText string, timestamp int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get all participants of this room
	participants, err := chatService.GetRoomParticipants(ctx, roomID)
	if err != nil {
		utils.LogError(err, "GetRoomParticipants")
		return
	}

	// Build the notification message
	notification := map[string]interface{}{
		"event":           "new_message",
		"room":            roomID,
		"sender_id":       senderID,
		"sender_username": senderUsername,
		"text":            messageText,
		"timestamp":       timestamp,
	}

	// Send notification to each participant who is:
	// 1. Not the sender
	// 2. Online but NOT currently in this room
	for _, participantID := range participants {
		if participantID == senderID {
			continue // Don't notify the sender
		}

		// Check if user is online
		if !Manager.IsUserOnline(participantID) {
			continue // User is offline, skip
		}

		// Check if user is currently in this room
		if Manager.IsUserInRoom(participantID, roomID) {
			continue // User is already in the room and will get the chat message
		}

		// Send notification
		Manager.SendToUser(participantID, notification)
	}
}

func handleList(c *websocket.Conn, msg *models.WSMessage, userID int, chatService *services.ChatService) {
	rooms, err := chatService.GetUserRooms(context.Background(), userID)
	if err != nil {
		utils.LogError(err, "GetUserRooms")
		// send empty list with error
		utils.SendJSON(c, models.WSMessage{
			Event: "list",
			Rooms: []models.RoomListItem{},
		})
		return
	}

	// Set online status and voice URL for each item
	for i := range rooms {
		if Manager.IsUserOnline(rooms[i].OtherUserID) {
			rooms[i].OtherUserStatus = "online"
		} else {
			rooms[i].OtherUserStatus = "offline"
		}
		// Build absolute voice URL if last message was a voice
		if rooms[i].LastVoice != nil && *rooms[i].LastVoice != "" {
			rooms[i].LastVoiceURL = buildVoiceURLFromWS(c, *rooms[i].LastVoice)
		}
	}

	utils.SendJSON(c, models.WSMessage{
		Event: "list",
		Rooms: rooms,
	})
}
