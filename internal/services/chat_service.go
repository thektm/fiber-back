package services

import (
	"context"
	"database/sql"

	"chat-backend/internal/db"
	"chat-backend/internal/models"

	"github.com/google/uuid"
)

type ChatService struct{}

func NewChatService() *ChatService {
	return &ChatService{}
}

func (s *ChatService) GetOrCreateDirectRoom(ctx context.Context, userID1, userID2 int) (*models.RoomResponse, error) {
	// Check if room exists
	query := `
		SELECT r.id 
		FROM rooms r
		JOIN room_participants p1 ON r.id = p1.room_id
		JOIN room_participants p2 ON r.id = p2.room_id
		WHERE r.type = 'direct' 
		AND p1.user_id = $1 
		AND p2.user_id = $2
		LIMIT 1
	`
	var roomID string
	err := db.Pool.QueryRow(ctx, query, userID1, userID2).Scan(&roomID)
	if err == nil {
		return &models.RoomResponse{RoomID: roomID, IsNew: false}, nil
	}

	// Create new room
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	newRoomID := uuid.New().String()
	_, err = tx.Exec(ctx, "INSERT INTO rooms (id, type) VALUES ($1, 'direct')", newRoomID)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx, "INSERT INTO room_participants (room_id, user_id) VALUES ($1, $2), ($1, $3)", newRoomID, userID1, userID2)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &models.RoomResponse{RoomID: newRoomID, IsNew: true}, nil
}

func (s *ChatService) SaveMessage(ctx context.Context, msg *models.Message) error {
	query := `INSERT INTO messages (room, user_id, username, content) VALUES ($1, $2, $3, $4) RETURNING id, created_at`
	return db.Pool.QueryRow(ctx, query, msg.Room, msg.UserID, msg.Username, msg.Content).Scan(&msg.ID, &msg.CreatedAt)
}

func (s *ChatService) GetRecentMessages(ctx context.Context, room string, limit int) ([]models.Message, error) {
	query := `SELECT id, room, user_id, username, content, created_at FROM messages WHERE room = $1 ORDER BY created_at DESC LIMIT $2`
	rows, err := db.Pool.Query(ctx, query, room, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var msg models.Message
		if err := rows.Scan(&msg.ID, &msg.Room, &msg.UserID, &msg.Username, &msg.Content, &msg.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}

	// Reverse to show oldest first
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// GetUserRooms returns rooms for a user including the other participant and last message
func (s *ChatService) GetUserRooms(ctx context.Context, userID int) ([]models.RoomListItem, error) {
	query := `
	SELECT r.id, u.id as other_user_id, u.username as other_username, m.content as last_message, m.created_at as last_created
	FROM rooms r
	JOIN room_participants p_me ON r.id = p_me.room_id AND p_me.user_id = $1
	JOIN room_participants p_other ON r.id = p_other.room_id AND p_other.user_id != $1
	JOIN users u ON u.id = p_other.user_id
	LEFT JOIN LATERAL (SELECT content, created_at FROM messages WHERE room = r.id ORDER BY created_at DESC LIMIT 1) m ON true
	WHERE r.type = 'direct'
	`

	rows, err := db.Pool.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.RoomListItem
	for rows.Next() {
		var roomID string
		var otherUserID int
		var otherUsername string
		var lastMessage sql.NullString
		var lastCreated sql.NullTime

		if err := rows.Scan(&roomID, &otherUserID, &otherUsername, &lastMessage, &lastCreated); err != nil {
			return nil, err
		}

		item := models.RoomListItem{
			RoomID:        roomID,
			OtherUserID:   otherUserID,
			OtherUsername: otherUsername,
		}
		if lastMessage.Valid {
			item.LastMessage = lastMessage.String
		}
		if lastCreated.Valid {
			item.LastMessageUnixMs = lastCreated.Time.UnixMilli()
		}

		items = append(items, item)
	}

	return items, nil
}
