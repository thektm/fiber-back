package utils

import (
	"encoding/json"
	"log"

	"github.com/gofiber/websocket/v2"
)

// SafeJSONParse parses JSON safely
func SafeJSONParse(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// SendJSON sends a JSON payload to a WebSocket connection safely
func SendJSON(c *websocket.Conn, payload interface{}) error {
	// Use a mutex if concurrent writes to the same conn are expected,
	// but typically the room manager handles serialization.
	// Fiber's websocket implementation is not thread-safe for concurrent writes.
	// The caller must ensure thread safety or use a channel-based write pump.
	// For this implementation, we assume the caller (RoomManager) holds a lock or handles it.

	return c.WriteJSON(payload)
}

// LogError logs an error if it's not nil
func LogError(err error, context string) {
	if err != nil {
		log.Printf("Error [%s]: %v", context, err)
	}
}
