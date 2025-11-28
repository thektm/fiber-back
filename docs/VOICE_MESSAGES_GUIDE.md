# Voice Messages - Frontend Integration Guide

This guide explains how to integrate voice messages into your frontend application.

## Overview

Voice messages allow users to send audio recordings instead of or alongside text messages. The backend stores voice files and provides absolute URLs for playback.

## Data Structures

### Message with Voice (from WebSocket or API responses)

```typescript
interface Message {
  id: number;
  room: string;
  user_id: number;
  username: string;
  content?: string;      // Text content (null for voice-only messages)
  voice?: string;        // Voice filename (stored on server)
  voice_url?: string;    // Absolute URL to play the voice file
  has_seen: boolean;
  reply_to?: Message;
  created_at: string;
}
```

### WebSocket Message (chat event)

```typescript
interface WSChatMessage {
  event: "chat";
  id: number;
  room: string;
  text: string;          // Empty string for voice-only messages
  voice?: string;        // Voice filename
  voice_url?: string;    // Absolute URL for voice playback (e.g., "http://example.com/uploads/voices/voice_1_1732789012345.webm")
  username: string;
  timestamp: number;     // Unix milliseconds
  has_seen: boolean;
  reply_to?: Message;
}
```

### History Item (from join event)

```typescript
interface ChatHistoryItem {
  id: number;
  event: "chat";
  room: string;
  text?: string;         // Null for voice-only messages
  voice?: string;        // Voice filename
  voice_url?: string;    // Absolute URL for voice playback
  username: string;
  timestamp: number;
  is_your_message: boolean;
  has_seen: boolean;
  reply_to?: Message;
}
```

### Room List Item (from list event)

```typescript
interface RoomListItem {
  room_id: string;
  other_user_id: number;
  other_user?: UserInfo;
  last_message?: string;      // Text of last message (null if voice-only)
  last_voice?: string;        // Voice filename of last message
  last_voice_url?: string;    // Absolute URL for last voice message
  last_message_unix_ms?: number;
  other_user_status: "online" | "offline";
}
```

## Sending Voice Messages

### Option 1: Standard Upload (Recommended)

Use this endpoint for simple voice uploads. Returns JSON response after completion.

**Endpoint:** `POST /api/messages/voice`

**Headers:**
```
Authorization: Bearer <access_token>
Content-Type: multipart/form-data
```

**Form Data:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `voice` | File | Yes | The audio file (supported: wav, mp3, ogg, webm, m4a, aac) |
| `room` | String | Yes | The room ID to send the message to |
| `reply_to_id` | Number | No | Message ID if replying to another message |

**Example Request (JavaScript):**
```javascript
async function sendVoiceMessage(audioBlob, roomId, replyToId = null) {
  const formData = new FormData();
  formData.append('voice', audioBlob, 'recording.webm');
  formData.append('room', roomId);
  if (replyToId) {
    formData.append('reply_to_id', replyToId.toString());
  }

  const response = await fetch('/api/messages/voice', {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${accessToken}`
    },
    body: formData
  });

  if (!response.ok) {
    throw new Error('Failed to upload voice message');
  }

  return await response.json();
}
```

**Success Response (201 Created):**
```json
{
  "id": 123,
  "room": "uuid-room-id",
  "voice": "voice_1_1732789012345.webm",
  "voice_url": "http://example.com/uploads/voices/voice_1_1732789012345.webm",
  "timestamp": 1732789012345,
  "reply_to": null
}
```

**Error Responses:**
```json
// 400 Bad Request - Missing room
{ "error": "room is required" }

// 400 Bad Request - Missing voice file
{ "error": "voice file is required" }

// 400 Bad Request - Invalid file type
{
  "error": "invalid audio file type",
  "content_type": "video/mp4",
  "allowed": "audio/wav, audio/mpeg, audio/ogg, audio/webm, audio/mp4, audio/aac, audio/m4a"
}
```

### Option 2: Upload with Progress (SSE)

Use this endpoint to show upload progress to the user. Returns Server-Sent Events (SSE).

**Endpoint:** `POST /api/messages/voice/progress`

**Headers:**
```
Authorization: Bearer <access_token>
Content-Type: multipart/form-data
```

**Form Data:** Same as standard upload

**Example Request (JavaScript):**
```javascript
async function sendVoiceMessageWithProgress(audioBlob, roomId, onProgress, onComplete, onError) {
  const formData = new FormData();
  formData.append('voice', audioBlob, 'recording.webm');
  formData.append('room', roomId);

  const response = await fetch('/api/messages/voice/progress', {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${accessToken}`
    },
    body: formData
  });

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split('\n\n');
    buffer = lines.pop(); // Keep incomplete event in buffer

    for (const eventBlock of lines) {
      if (!eventBlock.trim()) continue;
      
      const eventMatch = eventBlock.match(/event: (\w+)/);
      const dataMatch = eventBlock.match(/data: (.+)/);
      
      if (eventMatch && dataMatch) {
        const eventType = eventMatch[1];
        const data = JSON.parse(dataMatch[1]);
        
        switch (eventType) {
          case 'progress':
            onProgress(data.percent, data.uploaded, data.total);
            break;
          case 'complete':
            onComplete(data);
            break;
          case 'error':
            onError(data.error);
            break;
        }
      }
    }
  }
}

// Usage:
sendVoiceMessageWithProgress(
  audioBlob,
  roomId,
  (percent, uploaded, total) => {
    console.log(`Upload progress: ${percent}% (${uploaded}/${total} bytes)`);
    updateProgressBar(percent);
  },
  (result) => {
    console.log('Upload complete:', result);
    // result = { id, room, voice, voice_url, timestamp, reply_to }
  },
  (error) => {
    console.error('Upload failed:', error);
  }
);
```

**SSE Events:**

1. **progress** - Sent during upload
```json
{
  "uploaded": 50000,
  "total": 100000,
  "percent": 50
}
```

2. **complete** - Sent when upload and message creation succeed
```json
{
  "id": 123,
  "room": "uuid-room-id",
  "voice": "voice_1_1732789012345.webm",
  "voice_url": "http://example.com/uploads/voices/voice_1_1732789012345.webm",
  "timestamp": 1732789012345,
  "reply_to": null
}
```

3. **error** - Sent if an error occurs
```json
{
  "error": "failed to save file"
}
```

## Receiving Voice Messages via WebSocket

When a voice message is sent, all users in the room (including the sender) receive a `chat` event:

```json
{
  "event": "chat",
  "id": 123,
  "room": "uuid-room-id",
  "text": "",
  "voice": "voice_1_1732789012345.webm",
  "voice_url": "http://example.com/uploads/voices/voice_1_1732789012345.webm",
  "username": "john_doe",
  "timestamp": 1732789012345,
  "has_seen": false,
  "reply_to": null
}
```

Users not currently in the room receive a notification:

```json
{
  "event": "new_message",
  "room": "uuid-room-id",
  "sender_id": 1,
  "sender_username": "john_doe",
  "type": "voice",
  "timestamp": 1732789012345
}
```

## Message Scenarios

### Scenario 1: Text-Only Message

```json
{
  "id": 100,
  "text": "Hello, how are you?",
  "voice": null,
  "voice_url": "",
  "username": "alice"
}
```

**Display:** Show text bubble with "Hello, how are you?"

### Scenario 2: Voice-Only Message

```json
{
  "id": 101,
  "text": "",
  "voice": "voice_2_1732789012345.webm",
  "voice_url": "http://example.com/uploads/voices/voice_2_1732789012345.webm",
  "username": "bob"
}
```

**Display:** Show audio player component with play/pause button

### Scenario 3: Checking Message Type

```javascript
function getMessageType(message) {
  const hasText = message.text && message.text.trim() !== '';
  const hasVoice = message.voice && message.voice !== '';
  
  if (hasVoice) {
    return 'voice';
  }
  return 'text';
}

function renderMessage(message) {
  const type = getMessageType(message);
  
  if (type === 'voice') {
    return (
      <div className="message voice-message">
        <span className="username">{message.username}</span>
        <AudioPlayer src={message.voice_url} />
        <span className="timestamp">{formatTime(message.timestamp)}</span>
      </div>
    );
  }
  
  return (
    <div className="message text-message">
      <span className="username">{message.username}</span>
      <p>{message.text}</p>
      <span className="timestamp">{formatTime(message.timestamp)}</span>
    </div>
  );
}
```

## Room List Display

When displaying the room list, check if the last message was a voice message:

```javascript
function formatLastMessage(room) {
  if (room.last_voice && room.last_voice !== '') {
    return '🎤 Voice message';
  }
  return room.last_message || 'No messages yet';
}

// Example room list item display
rooms.map(room => (
  <div className="room-item" key={room.room_id}>
    <Avatar user={room.other_user} />
    <div className="room-info">
      <span className="name">{room.other_user?.username}</span>
      <span className="last-message">{formatLastMessage(room)}</span>
    </div>
    <span className="status">{room.other_user_status}</span>
  </div>
));
```

## Recording Audio (Browser)

Here's a complete example of recording audio in the browser:

```javascript
class VoiceRecorder {
  constructor() {
    this.mediaRecorder = null;
    this.chunks = [];
  }

  async start() {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    
    // Use webm for best browser compatibility
    const mimeType = MediaRecorder.isTypeSupported('audio/webm;codecs=opus')
      ? 'audio/webm;codecs=opus'
      : 'audio/webm';
    
    this.mediaRecorder = new MediaRecorder(stream, { mimeType });
    this.chunks = [];
    
    this.mediaRecorder.ondataavailable = (e) => {
      if (e.data.size > 0) {
        this.chunks.push(e.data);
      }
    };
    
    this.mediaRecorder.start(100); // Collect data every 100ms
  }

  stop() {
    return new Promise((resolve) => {
      this.mediaRecorder.onstop = () => {
        const blob = new Blob(this.chunks, { type: this.mediaRecorder.mimeType });
        
        // Stop all tracks to release microphone
        this.mediaRecorder.stream.getTracks().forEach(track => track.stop());
        
        resolve(blob);
      };
      
      this.mediaRecorder.stop();
    });
  }

  cancel() {
    if (this.mediaRecorder && this.mediaRecorder.state !== 'inactive') {
      this.mediaRecorder.stream.getTracks().forEach(track => track.stop());
      this.mediaRecorder.stop();
    }
    this.chunks = [];
  }
}

// Usage:
const recorder = new VoiceRecorder();

// Start recording
recordButton.onclick = () => recorder.start();

// Stop and send
sendButton.onclick = async () => {
  const audioBlob = await recorder.stop();
  await sendVoiceMessage(audioBlob, currentRoomId);
};

// Cancel recording
cancelButton.onclick = () => recorder.cancel();
```

## Playing Voice Messages

```javascript
function AudioPlayer({ src }) {
  const audioRef = useRef(null);
  const [isPlaying, setIsPlaying] = useState(false);
  const [progress, setProgress] = useState(0);
  const [duration, setDuration] = useState(0);

  const togglePlay = () => {
    if (isPlaying) {
      audioRef.current.pause();
    } else {
      audioRef.current.play();
    }
    setIsPlaying(!isPlaying);
  };

  return (
    <div className="audio-player">
      <audio
        ref={audioRef}
        src={src}
        onLoadedMetadata={(e) => setDuration(e.target.duration)}
        onTimeUpdate={(e) => setProgress((e.target.currentTime / e.target.duration) * 100)}
        onEnded={() => setIsPlaying(false)}
      />
      <button onClick={togglePlay}>
        {isPlaying ? '⏸️' : '▶️'}
      </button>
      <div className="progress-bar">
        <div className="progress" style={{ width: `${progress}%` }} />
      </div>
      <span className="duration">{formatDuration(duration)}</span>
    </div>
  );
}

function formatDuration(seconds) {
  const mins = Math.floor(seconds / 60);
  const secs = Math.floor(seconds % 60);
  return `${mins}:${secs.toString().padStart(2, '0')}`;
}
```

## Validation Rules

1. **At least one required:** A message must have either `text` (content) or `voice`, but not both can be null/empty.
2. **Supported audio formats:** wav, mp3, ogg, webm, m4a, aac
3. **Room required:** The `room` field is always required when uploading voice

## Error Handling

Always handle potential errors:

```javascript
try {
  const result = await sendVoiceMessage(audioBlob, roomId);
  console.log('Voice message sent:', result.id);
} catch (error) {
  if (error.message.includes('room is required')) {
    showError('Please select a chat room first');
  } else if (error.message.includes('voice file is required')) {
    showError('Please record a voice message first');
  } else if (error.message.includes('invalid audio file type')) {
    showError('Unsupported audio format. Please use webm, mp3, or wav');
  } else {
    showError('Failed to send voice message. Please try again.');
  }
}
```

## Best Practices

1. **Use WebM format** - Best browser compatibility for recording
2. **Show recording indicator** - Let users know they're recording
3. **Show upload progress** - Use the progress endpoint for better UX
4. **Cache audio** - Consider caching played audio for offline/repeat playback
5. **Limit recording duration** - Prevent very large files (e.g., max 5 minutes)
6. **Handle permissions** - Request microphone permission gracefully
