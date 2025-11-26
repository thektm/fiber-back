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
}

var Manager = &RoomManager{
	rooms: make(map[string]map[string]*websocket.Conn),
}

func (m *RoomManager) Join(room string, connID string, c *websocket.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.rooms[room]; !ok {
		m.rooms[room] = make(map[string]*websocket.Conn)
	}
	m.rooms[room][connID] = c
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
