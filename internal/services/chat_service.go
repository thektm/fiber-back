package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

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
	// By default we store has_seen as FALSE in DB. Clients may interpret has_seen locally
	query := `INSERT INTO messages (room, user_id, username, content, has_seen, reply_to) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at, has_seen, reply_to`

	var replyJSON interface{}
	if msg.ReplyTo != nil {
		b, err := json.Marshal(msg.ReplyTo)
		if err != nil {
			return err
		}
		replyJSON = b
	} else {
		replyJSON = nil
	}

	var replyBytes []byte
	err := db.Pool.QueryRow(ctx, query, msg.Room, msg.UserID, msg.Username, msg.Content, false, replyJSON).Scan(&msg.ID, &msg.CreatedAt, &msg.HasSeen, &replyBytes)
	if err != nil {
		return err
	}
	if len(replyBytes) > 0 {
		var r models.Message
		if err := json.Unmarshal(replyBytes, &r); err == nil {
			msg.ReplyTo = &r
		}
	}
	return nil
}

func (s *ChatService) GetRecentMessages(ctx context.Context, room string, limit int) ([]models.Message, error) {
	query := `SELECT id, room, user_id, username, content, has_seen, reply_to, created_at FROM messages WHERE room = $1 ORDER BY created_at DESC LIMIT $2`
	rows, err := db.Pool.Query(ctx, query, room, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var msg models.Message
		var replyBytes sql.NullString
		if err := rows.Scan(&msg.ID, &msg.Room, &msg.UserID, &msg.Username, &msg.Content, &msg.HasSeen, &replyBytes, &msg.CreatedAt); err != nil {
			return nil, err
		}
		if replyBytes.Valid && len(replyBytes.String) > 0 {
			var r models.Message
			if err := json.Unmarshal([]byte(replyBytes.String), &r); err == nil {
				msg.ReplyTo = &r
			}
		}
		messages = append(messages, msg)
	}

	// Reverse to show oldest first
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// GetRoomParticipants returns all user IDs that are participants of a given room
func (s *ChatService) GetRoomParticipants(ctx context.Context, roomID string) ([]int, error) {
	query := `SELECT user_id FROM room_participants WHERE room_id = $1`
	rows, err := db.Pool.Query(ctx, query, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var userIDs []int
	for rows.Next() {
		var userID int
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, userID)
	}
	return userIDs, nil
}

// MarkMessagesSeen sets has_seen = true for messages in a room that belong to other users
// and were created at or before the provided time. Returns number of rows updated.
func (s *ChatService) MarkMessagesSeen(ctx context.Context, room string, viewerID int, seenBefore time.Time) (int64, error) {
	query := `UPDATE messages SET has_seen = TRUE WHERE room = $1 AND user_id != $2 AND created_at <= $3 AND has_seen = FALSE`
	tag, err := db.Pool.Exec(ctx, query, room, viewerID, seenBefore)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// GetUsersWithSharedRooms returns all user IDs that share at least one room with the given user
func (s *ChatService) GetUsersWithSharedRooms(ctx context.Context, userID int) ([]int, error) {
	query := `
		SELECT DISTINCT p2.user_id
		FROM room_participants p1
		JOIN room_participants p2 ON p1.room_id = p2.room_id AND p2.user_id != $1
		WHERE p1.user_id = $1
	`
	rows, err := db.Pool.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var userIDs []int
	for rows.Next() {
		var uid int
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, uid)
	}
	return userIDs, nil
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

		// If lateral join didn't return a last message (possible race or edge case),
		// fall back to querying the messages table for the latest message for this room.
		if lastMessage.Valid {
			item.LastMessage = lastMessage.String
		}
		if lastCreated.Valid {
			item.LastMessageUnixMs = lastCreated.Time.UnixMilli()
		}
		if item.LastMessage == "" {
			var content string
			var createdAt sql.NullTime
			q := `SELECT content, created_at FROM messages WHERE room = $1 ORDER BY created_at DESC LIMIT 1`
			if err := db.Pool.QueryRow(ctx, q, roomID).Scan(&content, &createdAt); err == nil {
				item.LastMessage = content
				if createdAt.Valid {
					item.LastMessageUnixMs = createdAt.Time.UnixMilli()
				}
			}
		}

		items = append(items, item)
	}

	return items, nil
}
