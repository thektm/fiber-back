package app

import (
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"chat-backend/internal/db"
	"chat-backend/internal/models"
	"chat-backend/internal/services"
	"chat-backend/internal/utils"
	"chat-backend/internal/handlers"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func Run() {
	// Load Env
	if err := utils.LoadEnv(); err != nil {
		log.Println("Warning: .env file not found")
	}

	// Init DB
	connString := utils.GetEnv("DATABASE_URL", "")
	if connString == "" {
		// Fallback to individual vars
		connString = "postgres://" + utils.GetEnv("POSTGRES_USER", "postgres") + ":" +
			utils.GetEnv("POSTGRES_PASSWORD", "postgres") + "@" +
			utils.GetEnv("POSTGRES_HOST", "localhost") + ":" +
			utils.GetEnv("POSTGRES_PORT", "5432") + "/" +
			utils.GetEnv("POSTGRES_DB", "chatdb") + "?sslmode=disable"
	}

	if err := db.InitDB(connString); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.CloseDB()

	// Services
	userService := services.NewUserService()
	chatService := services.NewChatService()

	// Fiber App
	app := fiber.New()

	// Middleware
	app.Use(logger.New())
	app.Use(recover.New())
	app.Use(cors.New())

	// Ensure upload dir exists and serve uploaded files
	uploadDir := utils.GetEnv("UPLOAD_DIR", "uploads")
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		log.Printf("Warning: failed to create upload dir: %v", err)
	}
	app.Static("/uploads", uploadDir)

	// Routes
	api := app.Group("/api")

	// Public Routes
	api.Post("/register", func(c *fiber.Ctx) error {
		var req models.RegisterRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
		}
		user, err := userService.Register(c.Context(), req)
		if err != nil {
			if errors.Is(err, services.ErrUserExists) {
				return c.Status(400).JSON(fiber.Map{"error": "username already exists"})
			}
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(user)
	})

	api.Post("/login", func(c *fiber.Ctx) error {
		var req models.LoginRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
		}
		res, err := userService.Login(c.Context(), req)
		if err != nil {
			return c.Status(401).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(res)
	})

	// Refresh token endpoint
	api.Post("/refresh", func(c *fiber.Ctx) error {
		var body struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
		}
		if body.RefreshToken == "" {
			return c.Status(400).JSON(fiber.Map{"error": "refresh_token required"})
		}

		claims, err := services.ValidateRefreshToken(body.RefreshToken)
		if err != nil {
			return c.Status(401).JSON(fiber.Map{"error": "invalid refresh token"})
		}

		// Extract user info
		userIDf, ok := claims["user_id"].(float64)
		if !ok {
			return c.Status(401).JSON(fiber.Map{"error": "invalid token claims"})
		}
		username, ok := claims["username"].(string)
		if !ok {
			return c.Status(401).JSON(fiber.Map{"error": "invalid token claims"})
		}

		userID := int(userIDf)

		// Generate new tokens
		access, err := services.GenerateJWT(userID, username)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "failed to generate access token"})
		}
		refresh, err := services.GenerateRefreshToken(userID, username)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "failed to generate refresh token"})
		}

		return c.JSON(fiber.Map{
			"access_token":  access,
			"refresh_token": refresh,
		})
	})

	// Protected Routes
	protected := api.Group("/")
	protected.Use(handlers.AuthMiddleware)

	// Chat Routes
	protected.Post("/rooms/direct", func(c *fiber.Ctx) error {
		// Get authenticated user
		userID := c.Locals("user_id").(int)

		var req models.CreateDirectRoomRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
		}

		if req.RecipientID == 0 {
			return c.Status(400).JSON(fiber.Map{"error": "Recipient ID required"})
		}

		res, err := chatService.GetOrCreateDirectRoom(c.Context(), userID, req.RecipientID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		return c.JSON(res)
	})

	// List users (exclude admin). Returns online status per user.
	protected.Get("/users", func(c *fiber.Ctx) error {
		// Authenticated user
		authUserID := c.Locals("user_id").(int)

		users, err := userService.ListUsers(c.Context())
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "failed to fetch users"})
		}

		// Build response with status info
		var resp []map[string]interface{}
		for _, u := range users {
			// Optionally skip the requesting user from the list
			if u.ID == authUserID {
				continue
			}
			status := "offline"
			if handlers.Manager.IsUserOnline(u.ID) {
				status = "online"
			}
			resp = append(resp, map[string]interface{}{
				"id":         u.ID,
				"username":   u.Username,
				"created_at": u.CreatedAt,
				"status":     status,
			})
		}

		return c.JSON(resp)
	})

	// Profile endpoints
	protected.Get("/profile", handlers.GetProfileHandler(userService))
	// Upload a photo (field name: "photo")
	protected.Put("/profile/photo", handlers.UploadPhotoHandler(userService))
	// Delete a photo by id
	protected.Delete("/profile/photo/:photo_id", handlers.DeletePhotoHandler(userService))

	// Health Check
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// WebSocket Route
	// Note: Middleware order matters. AuthMiddleware checks token.
	// WSUpgradeMiddleware checks if it's a WS request.
	app.Use("/ws", handlers.WSUpgradeMiddleware)
	app.Use("/ws", handlers.AuthMiddleware)
	app.Get("/ws", handlers.WebSocketHandler(chatService))

	// Start Server
	port := utils.GetEnv("PORT", "3001")
	go func() {
		if err := app.Listen(":" + port); err != nil {
			log.Panic(err)
		}
	}()

	// Graceful Shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c // Block until signal
	log.Println("Gracefully shutting down...")
	_ = app.Shutdown()
	log.Println("Server shutdown complete")
}
