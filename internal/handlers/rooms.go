package handlers

import (
	"sync"

	"chat-backend/internal/utils"

	"github.com/gofiber/websocket/v2"
)

type RoomManager struct {
	// roomName -> connectionID -> *websocket.Conn
	rooms map[string]map[string]*websocket.Conn
	mu    sync.RWMutex
	// connID -> metadata (includes connection reference)
	connMeta map[string]ConnMeta
}

var Manager = &RoomManager{
	rooms:    make(map[string]map[string]*websocket.Conn),
	connMeta: make(map[string]ConnMeta),
}

type ConnMeta struct {
	UserID   int
	Username string
	Conn     *websocket.Conn
}

func (m *RoomManager) Join(room string, connID string, c *websocket.Conn, userID int, username string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.rooms[room]; !ok {
		m.rooms[room] = make(map[string]*websocket.Conn)
	}
	m.rooms[room][connID] = c
	// store/update metadata with connection
	m.connMeta[connID] = ConnMeta{UserID: userID, Username: username, Conn: c}
}

func (m *RoomManager) Leave(room string, connID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.rooms[room]; ok {
		delete(m.rooms[room], connID)
		if len(m.rooms[room]) == 0 {
			delete(m.rooms, room)
		}
	}
}

func (m *RoomManager) Broadcast(room string, message interface{}, excludeConnID string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if connections, ok := m.rooms[room]; ok {
		for id, conn := range connections {
			if id == excludeConnID {
				continue
			}
			// Note: In a real high-scale app, you might want to use a channel per connection
			// to avoid blocking the broadcaster if one client is slow.
			// For this example, we write directly but handle errors.
			if err := utils.SendJSON(conn, message); err != nil {
				utils.LogError(err, "Broadcast")
				// If write fails, we might want to close and remove the connection,
				// but we'll let the read loop handle the disconnection.
			}
		}
	}
}

func (m *RoomManager) BroadcastToAll(message interface{}) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, connections := range m.rooms {
		for _, conn := range connections {
			if err := utils.SendJSON(conn, message); err != nil {
				utils.LogError(err, "BroadcastToAll")
			}
		}
	}
}

// IsUserOnline checks if any active connection belongs to the given user
func (m *RoomManager) IsUserOnline(userID int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, meta := range m.connMeta {
		if meta.UserID == userID {
			return true
		}
	}
	return false
}

// RegisterConnection stores metadata for a new websocket connection
// Returns true if this is the first connection for this user (user just came online)
func (m *RoomManager) RegisterConnection(connID string, userID int, username string, conn *websocket.Conn) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if user was already online before adding this connection
	wasOnline := false
	for _, meta := range m.connMeta {
		if meta.UserID == userID {
			wasOnline = true
			break
		}
	}

	m.connMeta[connID] = ConnMeta{UserID: userID, Username: username, Conn: conn}

	// Return true if user just came online (wasn't online before)
	return !wasOnline
}

// UnregisterConnection removes metadata and removes the connection from any rooms
// Returns true if this was the last connection for the user (user is now offline)
func (m *RoomManager) UnregisterConnection(connID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get the user ID before removing
	meta, exists := m.connMeta[connID]
	if !exists {
		return false
	}
	userID := meta.UserID

	// Remove conn from all rooms
	for room, conns := range m.rooms {
		if _, ok := conns[connID]; ok {
			delete(conns, connID)
			if len(conns) == 0 {
				delete(m.rooms, room)
			}
		}
	}

	// Remove metadata
	delete(m.connMeta, connID)

	// Check if user has any remaining connections
	for _, m := range m.connMeta {
		if m.UserID == userID {
			return false // User still has other connections, still online
		}
	}

	return true // This was the last connection, user is now offline
}

// GetConnectionsByUserID returns all websocket connections for a given user ID
func (m *RoomManager) GetConnectionsByUserID(userID int) []*websocket.Conn {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var conns []*websocket.Conn
	for _, meta := range m.connMeta {
		if meta.UserID == userID && meta.Conn != nil {
			conns = append(conns, meta.Conn)
		}
	}
	return conns
}

// SendToUser sends a message to all connections of a specific user
func (m *RoomManager) SendToUser(userID int, message interface{}) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, meta := range m.connMeta {
		if meta.UserID == userID && meta.Conn != nil {
			if err := utils.SendJSON(meta.Conn, message); err != nil {
				utils.LogError(err, "SendToUser")
			}
		}
	}
}

// SendToUsers sends a message to all connections of multiple users
func (m *RoomManager) SendToUsers(userIDs []int, message interface{}) {
	for _, userID := range userIDs {
		m.SendToUser(userID, message)
	}
}

// GetUserCurrentRoom returns the room that a user is currently in (if any)
// Returns empty string if user is not in any room
func (m *RoomManager) GetUserCurrentRoom(userID int) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for connID, meta := range m.connMeta {
		if meta.UserID == userID {
			// Find which room this connection is in
			for roomID, roomConns := range m.rooms {
				if _, ok := roomConns[connID]; ok {
					return roomID
				}
			}
		}
	}
	return ""
}

// IsUserInRoom checks if a user is currently in a specific room
func (m *RoomManager) IsUserInRoom(userID int, roomID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	roomConns, ok := m.rooms[roomID]
	if !ok {
		return false
	}

	for connID := range roomConns {
		if meta, ok := m.connMeta[connID]; ok && meta.UserID == userID {
			return true
		}
	}
	return false
}

// GetAllOnlineUserConnections returns a map of userID -> list of connections
// This is used to send messages to users who are online but may not be in any room
func (m *RoomManager) GetAllOnlineUserConnections() map[int][]*websocket.Conn {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[int][]*websocket.Conn)
	for _, meta := range m.connMeta {
		if meta.Conn != nil {
			result[meta.UserID] = append(result[meta.UserID], meta.Conn)
		}
	}
	return result
}

// GetUserIDFromConnMeta returns the user ID for a given connection ID (used before unregistering)
func (m *RoomManager) GetUserIDFromConnMeta(connID string) (int, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if meta, ok := m.connMeta[connID]; ok {
		return meta.UserID, true
	}
	return 0, false
}

// CountUserConnections returns the number of active connections for a user
func (m *RoomManager) CountUserConnections(userID int) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, meta := range m.connMeta {
		if meta.UserID == userID {
			count++
		}
	}
	return count
}
