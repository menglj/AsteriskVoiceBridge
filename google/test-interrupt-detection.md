# Google STT Interrupt Detection During TTS Playback

## Problem Solved
The previous implementation completely paused STT during TTS playback, which prevented users from interrupting the TTS with their speech. This made the conversation feel unnatural and unresponsive.

## Solution: Interrupt Detection Mode

### Overview
Instead of completely pausing STT during TTS playback, the system now enters "Interrupt Detection Mode" where:
- STT continues to process audio for interrupt detection
- Only final speech results are processed (no interim results)
- When user speech is detected, TTS is immediately cancelled
- Normal STT processing resumes for the interrupt speech

### Implementation Details

#### 1. Enhanced Pause State
```go
type googleSTTClient struct {
    paused bool           // General pause state
    interruptEnabled bool // Interrupt detection enabled
    pauseMu sync.RWMutex
    interruptMu sync.RWMutex
}
```

#### 2. Smart Audio Processing
```go
func (c *googleSTTClient) Write(data []byte) (int, error) {
    // If paused but interrupt detection enabled, still send audio
    if c.IsPaused() && !c.IsInterruptEnabled() {
        return len(data), nil // Skip audio data
    }
    
    if c.IsPaused() && c.IsInterruptEnabled() {
        // Send audio for interrupt detection
    }
    // ... send to Google API ...
}
```

#### 3. Interrupt-Aware Response Handling
```go
func (c *googleSTTClient) handleResponses() {
    // If paused, only process transcripts for interrupt detection
    if c.IsPaused() && c.IsInterruptEnabled() {
        if isFinal && len(transcript) > 0 {
            // Send interrupt signal to voicebot
            c.transcriptCallback(c.callid, transcript, "interrupt")
        }
        continue
    }
    // ... normal processing ...
}
```

#### 4. VoiceBot Interrupt Handling
```go
func (v VoiceBot) HandleTranscriptResults(callid string, text string, level string) bool {
    if level == "interrupt" {
        // Cancel TTS playback
        v.googleTTSProvider.CancelText(callid)
        // Resume STT for normal processing
        v.googleSTTProvider.ResumeCall(callid)
        // Process the interrupt text as normal translation
        v.translateProvider.TranslateText(callid, text)
    }
}
```

## Behavior Flow

### Normal Translation Flow
```
1. User speaks: "Hello"
2. STT processes: "Hello"
3. Translation: "Hello" → "你好"
4. STT enters interrupt detection mode
5. TTS plays: "你好"
6. TTS completes
7. STT resumes normal mode
8. Ready for next input
```

### Interrupt Flow
```
1. User speaks: "Hello"
2. STT processes: "Hello"
3. Translation: "Hello" → "你好"
4. STT enters interrupt detection mode
5. TTS starts playing: "你好"
6. User interrupts: "Wait, I meant..."
7. STT detects interrupt: "Wait, I meant..."
8. TTS immediately cancelled
9. STT resumes normal mode
10. Translation: "Wait, I meant..." → "等等，我的意思是..."
11. TTS plays: "等等，我的意思是..."
12. Ready for next input
```

## Configuration

No additional configuration required. Interrupt detection is automatically enabled when using Google STT/TTS.

## Testing

### 1. Test Normal Flow
```bash
export USE_GOOGLE_STT_TTS=true
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json
go run .
```

**Test Steps:**
1. Say "Hello"
2. Wait for translation to play
3. Wait for translation to complete
4. Say "How are you?"
5. Verify normal translation flow

### 2. Test Interrupt Flow
```bash
export USE_GOOGLE_STT_TTS=true
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json
go run .
```

**Test Steps:**
1. Say "Hello"
2. Immediately after translation starts playing, say "Wait"
3. Verify TTS stops immediately
4. Verify "Wait" gets translated and played
5. Continue conversation normally

### 3. Monitor Logs
Look for these log messages:

**Normal Flow:**
```
STT:Pause callid=xxx status=PausedWithInterrupt
TTS:AddText text="你好"
STT:heartbeat Event=HeartbeatSent - InterruptMode
TTS playback completes
STT:Resume callid=xxx status=Resumed
```

**Interrupt Flow:**
```
STT:Pause callid=xxx status=PausedWithInterrupt
TTS:AddText text="你好"
STT:InterruptDetection callid=xxx text="Wait"
BOT:HandleTranscriptResults status=InterruptDetected text="Wait"
TTS:CancelText callid=xxx
STT:Resume callid=xxx status=Resumed
Translation: "Wait" → "等等"
TTS:AddText text="等等"
```

## Benefits

1. **Natural Conversation Flow** - Users can interrupt TTS naturally
2. **Responsive System** - Immediate response to user input
3. **No Audio Timeouts** - STT stream stays alive during TTS
4. **Reduced API Calls** - Only processes final results during interrupt mode
5. **Better User Experience** - Feels like talking to a real person

## Troubleshooting

### If interrupts don't work:
1. Check `STT:InterruptDetection` logs
2. Verify `interruptEnabled` state
3. Check TTS cancellation logs
4. Verify STT resume after interrupt

### If audio timeouts still occur:
1. Check heartbeat logs during interrupt mode
2. Verify interrupt detection is enabled
3. Check network stability
4. Monitor interrupt detection timing

### If TTS doesn't cancel on interrupt:
1. Check `TTS:CancelText` logs
2. Verify TTS provider is available
3. Check interrupt callback chain
4. Verify STT resume after interrupt

## Performance Impact

- **Minimal CPU overhead** - Only processes final results during interrupt mode
- **Reduced API calls** - Skips interim results during TTS playback
- **Maintained responsiveness** - Immediate interrupt detection
- **No additional latency** - Interrupt processing is real-time
