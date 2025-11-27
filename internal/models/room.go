package models

import "time"

type Room struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateDirectRoomRequest struct {
	RecipientID int `json:"recipient_id"`
}

type RoomResponse struct {
	RoomID string `json:"room_id"`
	IsNew  bool   `json:"is_new"`
}

type RoomListItem struct {
	RoomID            string `json:"room_id"`
	OtherUserID       int    `json:"other_user_id"`
	OtherUsername     string `json:"other_username"`
	OtherUser         *UserInfo `json:"other_user,omitempty"`
	LastMessage       string `json:"last_message,omitempty"`
	LastMessageUnixMs int64  `json:"last_message_unix_ms,omitempty"`
	OtherUserStatus   string `json:"other_user_status"` // "online" or "offline"
}
