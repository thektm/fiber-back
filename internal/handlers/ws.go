package handlers

import (
	"log"

	"chat-backend/internal/services"
	"chat-backend/internal/utils"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"github.com/google/uuid"
)

// WebSocketHandler handles the websocket connection
func WebSocketHandler(chatService *services.ChatService) fiber.Handler {
	return websocket.New(func(c *websocket.Conn) {
		// Retrieve user info from locals (set by middleware)
		userID := c.Locals("user_id").(int)
		username := c.Locals("username").(string)

		// Generate a unique ID for this connection
		connID := uuid.New().String()

		var currentRoom string

		defer func() {
			if currentRoom != "" {
				Manager.Leave(currentRoom, connID)
				// Notify others
				Manager.Broadcast(currentRoom, map[string]interface{}{
					"event":    "leave",
					"username": username,
					"room":     currentRoom,
				}, connID)
			}
			c.Close()
		}()

		// Send welcome message
		utils.SendJSON(c, map[string]string{
			"event":   "connected",
			"message": "Welcome to the chat server",
		})

		for {
			msgType, msg, err := c.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("error: %v", err)
				}
				break
			}

			HandleMessage(c, msgType, msg, chatService, userID, username, &currentRoom, connID)
		}
	})
}

// WSUpgradeMiddleware upgrades the connection to WebSocket
func WSUpgradeMiddleware(c *fiber.Ctx) error {
	if websocket.IsWebSocketUpgrade(c) {
		c.Locals("allowed", true)
		return c.Next()
	}
	return fiber.ErrUpgradeRequired
}

// AuthMiddleware verifies the JWT token before upgrading
func AuthMiddleware(c *fiber.Ctx) error {
	// Get token from query param `access_token` or Authorization header
	token := c.Query("access_token")
	if token == "" {
		authHeader := c.Get("Authorization")
		if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			token = authHeader[7:]
		}
	}

	if token == "" {
		return fiber.NewError(fiber.StatusUnauthorized, "Missing token")
	}

	claims, err := services.ValidateToken(token)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "Invalid token")
	}

	// Store user info in locals
	// claims["user_id"] comes as float64 from JSON
	if uid, ok := claims["user_id"].(float64); ok {
		c.Locals("user_id", int(uid))
	} else {
		return fiber.NewError(fiber.StatusUnauthorized, "Invalid token claims")
	}

	if u, ok := claims["username"].(string); ok {
		c.Locals("username", u)
	}

	return c.Next()
}
