package models

import "time"

// Photo represents a user-uploaded photo
type Photo struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	Filename  string    `json:"filename"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}
