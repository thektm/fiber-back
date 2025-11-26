CREATE TABLE IF NOT EXISTS rooms (
    id VARCHAR(36) PRIMARY KEY, -- UUID string
    type VARCHAR(20) NOT NULL DEFAULT 'direct',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS room_participants (
    room_id VARCHAR(36) REFERENCES rooms(id) ON DELETE CASCADE,
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    joined_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (room_id, user_id)
);

-- Index to quickly find rooms for a user
CREATE INDEX IF NOT EXISTS idx_room_participants_user_id ON room_participants(user_id);
