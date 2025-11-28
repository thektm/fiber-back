package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"chat-backend/internal/models"
	"chat-backend/internal/services"
	"chat-backend/internal/utils"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

// VoiceUploadRequest represents the multipart form data for voice upload
type VoiceUploadRequest struct {
	Room      string `form:"room"`
	ReplyToID int    `form:"reply_to_id"`
}

// ProgressWriter wraps an io.Writer to track write progress
type ProgressWriter struct {
	Writer      io.Writer
	Total       int64
	Written     int64
	OnProgress  func(written, total int64)
	LastEmitted time.Time
}

func (pw *ProgressWriter) Write(p []byte) (int, error) {
	n, err := pw.Writer.Write(p)
	pw.Written += int64(n)

	// Emit progress at most every 100ms to avoid flooding
	if time.Since(pw.LastEmitted) >= 100*time.Millisecond && pw.OnProgress != nil {
		pw.OnProgress(pw.Written, pw.Total)
		pw.LastEmitted = time.Now()
	}
	return n, err
}

// BuildVoiceURL constructs an absolute URL for a voice file based on request host
func BuildVoiceURL(c *fiber.Ctx, filename string) string {
	if filename == "" {
		return ""
	}

	// Try to get base URL from env first
	baseURL := utils.GetEnv("BASE_URL", "")
	if baseURL != "" {
		return fmt.Sprintf("%s/uploads/voices/%s", baseURL, filename)
	}

	// Extract from request
	protocol := "http"
	if c.Protocol() == "https" || c.Get("X-Forwarded-Proto") == "https" {
		protocol = "https"
	}
	host := c.Hostname()

	return fmt.Sprintf("%s://%s/uploads/voices/%s", protocol, host, filename)
}

// BuildVoiceURLFromRequest constructs an absolute URL for a voice file from fasthttp request
func BuildVoiceURLFromRequest(ctx *fasthttp.RequestCtx, filename string) string {
	if filename == "" {
		return ""
	}

	// Try to get base URL from env first
	baseURL := utils.GetEnv("BASE_URL", "")
	if baseURL != "" {
		return fmt.Sprintf("%s/uploads/voices/%s", baseURL, filename)
	}

	// Extract from request
	protocol := "http"
	if string(ctx.Request.URI().Scheme()) == "https" || string(ctx.Request.Header.Peek("X-Forwarded-Proto")) == "https" {
		protocol = "https"
	}
	host := string(ctx.Host())

	return fmt.Sprintf("%s://%s/uploads/voices/%s", protocol, host, filename)
}

// UploadVoiceHandler handles voice file upload with progress streaming via SSE
// This endpoint receives:
// - voice: the voice file (multipart file)
// - room: the room ID to send the message to
// - reply_to_id: optional, the message ID this is replying to
//
// It streams upload progress as SSE events and finally broadcasts the message
func UploadVoiceHandler(chatService *services.ChatService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(int)
		username := c.Locals("username").(string)

		// Get room from form
		room := c.FormValue("room")
		if room == "" {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "room is required"})
		}

		// Get optional reply_to_id
		replyToIDStr := c.FormValue("reply_to_id")
		var replyToID int
		if replyToIDStr != "" {
			var err error
			replyToID, err = strconv.Atoi(replyToIDStr)
			if err != nil {
				return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid reply_to_id"})
			}
		}

		// Get the voice file
		fileHeader, err := c.FormFile("voice")
		if err != nil {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "voice file is required"})
		}

		// Validate file type (optional but recommended)
		contentType := fileHeader.Header.Get("Content-Type")
		validTypes := map[string]bool{
			"audio/wav":                true,
			"audio/wave":               true,
			"audio/x-wav":              true,
			"audio/mpeg":               true,
			"audio/mp3":                true,
			"audio/ogg":                true,
			"audio/webm":               true,
			"audio/mp4":                true,
			"audio/aac":                true,
			"audio/x-m4a":              true,
			"audio/m4a":                true,
			"application/octet-stream": true, // Allow generic binary for flexibility
		}
		if !validTypes[contentType] {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{
				"error":        "invalid audio file type",
				"content_type": contentType,
				"allowed":      "audio/wav, audio/mpeg, audio/ogg, audio/webm, audio/mp4, audio/aac, audio/m4a",
			})
		}

		// Set up upload directory for voices
		uploadDir := filepath.Join(utils.GetEnv("UPLOAD_DIR", "uploads"), "voices")
		if err := os.MkdirAll(uploadDir, 0755); err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create upload dir"})
		}

		// Generate unique filename
		ext := filepath.Ext(fileHeader.Filename)
		if ext == "" {
			// Try to determine extension from content type
			switch contentType {
			case "audio/wav", "audio/wave", "audio/x-wav":
				ext = ".wav"
			case "audio/mpeg", "audio/mp3":
				ext = ".mp3"
			case "audio/ogg":
				ext = ".ogg"
			case "audio/webm":
				ext = ".webm"
			case "audio/mp4", "audio/aac", "audio/x-m4a", "audio/m4a":
				ext = ".m4a"
			default:
				ext = ".audio"
			}
		}
		filename := fmt.Sprintf("voice_%d_%d%s", userID, time.Now().UnixNano(), ext)
		destPath := filepath.Join(uploadDir, filename)

		// Open source file
		srcFile, err := fileHeader.Open()
		if err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "failed to read uploaded file"})
		}
		defer srcFile.Close()

		// Create destination file
		destFile, err := os.Create(destPath)
		if err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create destination file"})
		}
		defer destFile.Close()

		// Copy file (Fiber already has the full file in memory, so progress is mainly for consistency)
		_, err = io.Copy(destFile, srcFile)
		if err != nil {
			_ = os.Remove(destPath)
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "failed to save file"})
		}

		// Now save the message to DB
		var replyTo *models.Message
		if replyToID != 0 {
			replyTo, err = chatService.GetMessageByID(context.Background(), replyToID)
			if err != nil {
				utils.LogError(err, "GetMessageByID for voice reply")
				// Continue without reply_to
			}
		}

		dbMsg := &models.Message{
			Room:     room,
			UserID:   userID,
			Username: username,
			Content:  nil, // Voice message, no text
			Voice:    &filename,
			ReplyTo:  replyTo,
		}

		if err := chatService.SaveMessage(context.Background(), dbMsg); err != nil {
			_ = os.Remove(destPath)
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "failed to save message"})
		}

		// Build absolute voice URL
		voiceURL := BuildVoiceURL(c, filename)
		dbMsg.VoiceURL = voiceURL

		// Broadcast to room
		Manager.Broadcast(room, models.WSMessage{
			ID:        dbMsg.ID,
			Event:     "chat",
			Room:      room,
			Text:      "",
			Voice:     filename,
			VoiceURL:  voiceURL,
			Username:  username,
			Timestamp: dbMsg.CreatedAt.UnixMilli(),
			HasSeen:   dbMsg.HasSeen,
			ReplyTo:   dbMsg.ReplyTo,
		}, "")

		// Notify room participants who are NOT currently in this room
		go notifyNewVoiceMessage(chatService, room, userID, username, dbMsg.CreatedAt.UnixMilli())

		// Return success response
		return c.Status(http.StatusCreated).JSON(fiber.Map{
			"id":        dbMsg.ID,
			"room":      room,
			"voice":     filename,
			"voice_url": voiceURL,
			"timestamp": dbMsg.CreatedAt.UnixMilli(),
			"reply_to":  dbMsg.ReplyTo,
		})
	}
}

// notifyNewVoiceMessage sends notification to room participants not currently in the room
func notifyNewVoiceMessage(chatService *services.ChatService, roomID string, senderID int, senderUsername string, timestamp int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	participants, err := chatService.GetRoomParticipants(ctx, roomID)
	if err != nil {
		utils.LogError(err, "GetRoomParticipants for voice notification")
		return
	}

	notification := map[string]interface{}{
		"event":           "new_message",
		"room":            roomID,
		"sender_id":       senderID,
		"sender_username": senderUsername,
		"type":            "voice",
		"timestamp":       timestamp,
	}

	for _, participantID := range participants {
		if participantID == senderID {
			continue
		}
		if !Manager.IsUserOnline(participantID) {
			continue
		}
		if Manager.IsUserInRoom(participantID, roomID) {
			continue
		}
		Manager.SendToUser(participantID, notification)
	}
}

// UploadVoiceWithProgressHandler handles voice upload with SSE progress events
// This is an alternative endpoint that streams progress back to the client
func UploadVoiceWithProgressHandler(chatService *services.ChatService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(int)
		username := c.Locals("username").(string)

		// Set SSE headers
		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		c.Set("Connection", "keep-alive")
		c.Set("Transfer-Encoding", "chunked")

		// Helper to send SSE event
		sendEvent := func(eventType string, data interface{}) error {
			jsonData, err := json.Marshal(data)
			if err != nil {
				return err
			}
			_, err = c.Write([]byte(fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, jsonData)))
			return err
		}

		// Get room from form
		room := c.FormValue("room")
		if room == "" {
			_ = sendEvent("error", fiber.Map{"error": "room is required"})
			return nil
		}

		// Get optional reply_to_id
		replyToIDStr := c.FormValue("reply_to_id")
		var replyToID int
		if replyToIDStr != "" {
			var err error
			replyToID, err = strconv.Atoi(replyToIDStr)
			if err != nil {
				_ = sendEvent("error", fiber.Map{"error": "invalid reply_to_id"})
				return nil
			}
		}

		// Get the voice file
		fileHeader, err := c.FormFile("voice")
		if err != nil {
			_ = sendEvent("error", fiber.Map{"error": "voice file is required"})
			return nil
		}

		fileSize := fileHeader.Size

		// Send initial progress
		_ = sendEvent("progress", fiber.Map{
			"uploaded": 0,
			"total":    fileSize,
			"percent":  0,
		})

		// Set up upload directory
		uploadDir := filepath.Join(utils.GetEnv("UPLOAD_DIR", "uploads"), "voices")
		if err := os.MkdirAll(uploadDir, 0755); err != nil {
			_ = sendEvent("error", fiber.Map{"error": "failed to create upload dir"})
			return nil
		}

		// Generate unique filename
		ext := filepath.Ext(fileHeader.Filename)
		if ext == "" {
			ext = ".audio"
		}
		filename := fmt.Sprintf("voice_%d_%d%s", userID, time.Now().UnixNano(), ext)
		destPath := filepath.Join(uploadDir, filename)

		// Open source file
		srcFile, err := fileHeader.Open()
		if err != nil {
			_ = sendEvent("error", fiber.Map{"error": "failed to read uploaded file"})
			return nil
		}
		defer srcFile.Close()

		// Create destination file
		destFile, err := os.Create(destPath)
		if err != nil {
			_ = sendEvent("error", fiber.Map{"error": "failed to create destination file"})
			return nil
		}
		defer destFile.Close()

		// Create progress writer
		pw := &ProgressWriter{
			Writer: destFile,
			Total:  fileSize,
			OnProgress: func(written, total int64) {
				percent := float64(written) / float64(total) * 100
				_ = sendEvent("progress", fiber.Map{
					"uploaded": written,
					"total":    total,
					"percent":  int(percent),
				})
			},
		}

		// Copy with progress
		_, err = io.Copy(pw, srcFile)
		if err != nil {
			_ = os.Remove(destPath)
			_ = sendEvent("error", fiber.Map{"error": "failed to save file"})
			return nil
		}

		// Send 100% progress
		_ = sendEvent("progress", fiber.Map{
			"uploaded": fileSize,
			"total":    fileSize,
			"percent":  100,
		})

		// Save message to DB
		var replyTo *models.Message
		if replyToID != 0 {
			replyTo, _ = chatService.GetMessageByID(context.Background(), replyToID)
		}

		dbMsg := &models.Message{
			Room:     room,
			UserID:   userID,
			Username: username,
			Content:  nil,
			Voice:    &filename,
			ReplyTo:  replyTo,
		}

		if err := chatService.SaveMessage(context.Background(), dbMsg); err != nil {
			_ = os.Remove(destPath)
			_ = sendEvent("error", fiber.Map{"error": "failed to save message"})
			return nil
		}

		// Build absolute voice URL
		voiceURL := BuildVoiceURL(c, filename)

		// Broadcast to room
		Manager.Broadcast(room, models.WSMessage{
			ID:        dbMsg.ID,
			Event:     "chat",
			Room:      room,
			Text:      "",
			Voice:     filename,
			VoiceURL:  voiceURL,
			Username:  username,
			Timestamp: dbMsg.CreatedAt.UnixMilli(),
			HasSeen:   dbMsg.HasSeen,
			ReplyTo:   dbMsg.ReplyTo,
		}, "")

		// Notify others
		go notifyNewVoiceMessage(chatService, room, userID, username, dbMsg.CreatedAt.UnixMilli())

		// Send completion event
		_ = sendEvent("complete", fiber.Map{
			"id":        dbMsg.ID,
			"room":      room,
			"voice":     filename,
			"voice_url": voiceURL,
			"timestamp": dbMsg.CreatedAt.UnixMilli(),
			"reply_to":  dbMsg.ReplyTo,
		})

		return nil
	}
}
