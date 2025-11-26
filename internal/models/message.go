package models

import "time"

type Message struct {
	ID        int       `json:"id"`
	Room      string    `json:"room"`
	UserID    int       `json:"user_id"`
	Username  string    `json:"username"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// WebSocket Message Structure
type WSMessage struct {
	Event     string `json:"event"` // "join", "leave", "chat"
	Room      string `json:"room,omitempty"`
	Text      string `json:"text,omitempty"`
	Token     string `json:"token,omitempty"` // For initial auth if needed
	Timestamp int64  `json:"timestamp,omitempty"`
	Username  string `json:"username,omitempty"` // Sent to client
}
