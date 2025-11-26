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
