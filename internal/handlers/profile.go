package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"chat-backend/internal/services"
	"chat-backend/internal/utils"

	"github.com/gofiber/fiber/v2"
)

// GetProfileHandler returns the authenticated user's profile with photos
func GetProfileHandler(userService *services.UserService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(int)
		u, err := userService.GetProfile(c.Context(), userID)
		if err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(u)
	}
}

// UploadPhotoHandler handles uploading/adding a photo for the authenticated user
func UploadPhotoHandler(userService *services.UserService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(int)

		// Expect a multipart form file named "photo"
		fileHeader, err := c.FormFile("photo")
		if err != nil {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "photo file is required"})
		}

		uploadDir := utils.GetEnv("UPLOAD_DIR", "uploads")
		// Ensure upload directory exists
		if err := os.MkdirAll(uploadDir, 0755); err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create upload dir"})
		}

		// Generate unique filename preserving extension
		ext := filepath.Ext(fileHeader.Filename)
		filename := fmt.Sprintf("%d_%d%s", userID, time.Now().UnixNano(), ext)
		destPath := filepath.Join(uploadDir, filename)

		if err := c.SaveFile(fileHeader, destPath); err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "failed to save file"})
		}

		// Build accessible URL (served from /uploads)
		base := utils.GetEnv("BASE_URL", "")
		var url string
		if base == "" {
			url = "/uploads/" + filename
		} else {
			url = fmt.Sprintf("%s/uploads/%s", base, filename)
		}

		photo, err := userService.AddPhoto(c.Context(), userID, filename, url)
		if err != nil {
			// Try to cleanup file if DB insert fails
			_ = os.Remove(destPath)
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}

		return c.Status(http.StatusCreated).JSON(photo)
	}
}

// DeletePhotoHandler deletes a photo by id for the authenticated user
func DeletePhotoHandler(userService *services.UserService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(int)
		idStr := c.Params("photo_id")
		id, err := strconv.Atoi(idStr)
		if err != nil || id <= 0 {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid photo id"})
		}

		if err := userService.DeletePhoto(c.Context(), userID, id); err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}

		return c.SendStatus(http.StatusNoContent)
	}
}

// UpdateProfileHandler updates first_name and last_name for the authenticated user
func UpdateProfileHandler(userService *services.UserService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(int)

		var body struct {
			FirstName *string `json:"first_name"`
			LastName  *string `json:"last_name"`
		}

		if err := c.BodyParser(&body); err != nil {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid request"})
		}

		updated, err := userService.UpdateProfile(c.Context(), userID, body.FirstName, body.LastName)
		if err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}

		return c.JSON(updated)
	}
}
