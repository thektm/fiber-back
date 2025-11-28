-- Add voice column to messages table for voice messages
-- Either content (text) or voice must be non-null, but not both null at the same time

ALTER TABLE messages ADD COLUMN voice VARCHAR(500) NULL;

-- Add a check constraint to ensure at least one of content or voice is not null/empty
ALTER TABLE messages ADD CONSTRAINT chk_message_content_or_voice 
    CHECK (
        (content IS NOT NULL AND content != '') OR 
        (voice IS NOT NULL AND voice != '')
    );

-- Make content nullable since voice messages may not have text
ALTER TABLE messages ALTER COLUMN content DROP NOT NULL;
