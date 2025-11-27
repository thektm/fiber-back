-- Add reply_to JSONB column to messages
ALTER TABLE messages
ADD COLUMN IF NOT EXISTS reply_to JSONB DEFAULT NULL;

-- No index added since this is a JSONB payload; if you only need to reference by id consider storing reply_to_id separately.
