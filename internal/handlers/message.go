package handlers

import (
	"context"
	"log"
	"time"

	"chat-backend/internal/models"
	"chat-backend/internal/services"
	"chat-backend/internal/utils"

	"github.com/gofiber/websocket/v2"
)

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
			history = append(history, models.ChatHistoryItem{
				Event:         "chat",
				Room:          *currentRoom,
				Text:          m.Content,
				Username:      m.Username,
				Timestamp:     m.CreatedAt.UnixMilli(),
				IsYourMessage: m.UserID == userID,
				HasSeen:       m.HasSeen,
				ReplyTo:       m.ReplyTo,
			})
		}
		utils.SendJSON(c, models.WSMessage{
			Event:     "history",
			Room:      *currentRoom,
			History:   history,
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

	// Persist
	dbMsg := &models.Message{
		Room:     currentRoom,
		UserID:   userID,
		Username: username,
		Content:  msg.Text,
		ReplyTo:  msg.ReplyTo,
	}

	// Run in background or wait? For reliability, wait.
	if err := chatService.SaveMessage(context.Background(), dbMsg); err != nil {
		utils.LogError(err, "SaveMessage")
		return
	}

	// Broadcast to users currently in the room
	Manager.Broadcast(currentRoom, models.WSMessage{
		Event:     "chat",
		Room:      currentRoom,
		Text:      msg.Text,
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

	// Send the list back
	// set online status for each item
	for i := range rooms {
		if Manager.IsUserOnline(rooms[i].OtherUserID) {
			rooms[i].OtherUserStatus = "online"
		} else {
			rooms[i].OtherUserStatus = "offline"
		}
	}

	utils.SendJSON(c, models.WSMessage{
		Event: "list",
		Rooms: rooms,
	})
}
