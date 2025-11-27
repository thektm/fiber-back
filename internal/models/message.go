package models

import "time"

type Message struct {
	ID        int       `json:"id"`
	Room      string    `json:"room"`
	UserID    int       `json:"user_id"`
	Username  string    `json:"username"`
	Content   string    `json:"content"`
	HasSeen   bool      `json:"has_seen"`
	ReplyTo   *Message  `json:"reply_to,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// WebSocket Message Structure
type WSMessage struct {
	Event     string            `json:"event"` // "join", "leave", "chat"
	ID        int               `json:"id,omitempty"`
	Room      string            `json:"room,omitempty"`
	Text      string            `json:"text,omitempty"`
	Token     string            `json:"token,omitempty"` // For initial auth if needed
	Timestamp int64             `json:"timestamp,omitempty"`
	Username  string            `json:"username,omitempty"` // Sent to client
	HasSeen   bool              `json:"has_seen,omitempty"`
	ReplyTo   *Message          `json:"reply_to,omitempty"`
	ReplyToID int               `json:"reply_to_id,omitempty"`
	Rooms     []RoomListItem    `json:"rooms,omitempty"`
	History   []ChatHistoryItem `json:"history,omitempty"`
	OtherUser *UserInfo         `json:"other_user,omitempty"`
}

type ChatHistoryItem struct {
	ID            int      `json:"id"`
	Event         string   `json:"event,omitempty"`
	Room          string   `json:"room,omitempty"`
	Text          string   `json:"text"`
	Username      string   `json:"username"`
	Timestamp     int64    `json:"timestamp"`
	IsYourMessage bool     `json:"is_your_message"`
	HasSeen       bool     `json:"has_seen"`
	ReplyTo       *Message `json:"reply_to,omitempty"`
}

// UserInfo holds basic user profile info to send with history/room events
type UserInfo struct {
	ID        int     `json:"id"`
	Username  string  `json:"username"`
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
	Photos    []Photo `json:"photos,omitempty"`
}
