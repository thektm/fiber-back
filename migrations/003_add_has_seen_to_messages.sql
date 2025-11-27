-- Add has_seen boolean to messages table
ALTER TABLE messages
ADD COLUMN IF NOT EXISTS has_seen BOOLEAN DEFAULT FALSE;

-- Optional: Index if you plan to query unseen messages
CREATE INDEX IF NOT EXISTS idx_messages_has_seen ON messages(has_seen);
