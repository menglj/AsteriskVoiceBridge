# Google STT Pause/Resume Fix for TTS Playback

## Problem
During phone translation, when TTS (Text-to-Speech) is playing translated audio, the STT (Speech-to-Text) stream continues to receive audio data. This causes Google Cloud Speech API to timeout because:

1. TTS audio is being played to the user
2. STT is still trying to process audio (including the TTS output)
3. Google API expects continuous real-time audio
4. During TTS playback, there are gaps in meaningful audio for STT
5. Results in multiple "Audio Timeout Error" messages

## Root Cause
The STT stream was not aware of TTS playback state, causing it to process audio during TTS playback periods.

## Solution Implemented

### 1. STT Pause/Resume Mechanism
- Added `paused` state to `googleSTTClient`
- Added `Pause()` and `Resume()` methods
- Added `IsPaused()` method for state checking

### 2. Audio Data Skipping
- Modified `Write()` method to skip audio data when paused
- Modified `heartbeat()` to skip heartbeat when paused
- Prevents unnecessary API calls during TTS playback

### 3. Integration with VoiceBot
- Modified `HandleTranslationResults()` to pause STT before TTS
- Modified `SendText()` to pause STT before TTS
- Modified `HandlePlaybackComplete()` to resume STT after TTS

### 4. Provider-Level Methods
- Added `PauseCall()` and `ResumeCall()` to `GoogleSTTProvider`
- Thread-safe implementation with proper locking

## Code Changes

### google-stt.go
```go
// Added pause state to struct
type googleSTTClient struct {
    // ... existing fields ...
    paused bool
    pauseMu sync.RWMutex
}

// Added pause/resume methods
func (c *googleSTTClient) Pause() { ... }
func (c *googleSTTClient) Resume() { ... }
func (c *googleSTTClient) IsPaused() bool { ... }

// Modified Write to skip audio when paused
func (c *googleSTTClient) Write(data []byte) (int, error) {
    if c.IsPaused() {
        return len(data), nil // Skip audio data
    }
    // ... send to Google API ...
}

// Modified heartbeat to skip when paused
func (c *googleSTTClient) heartbeat() {
    if c.stream != nil && !c.IsPaused() {
        // Send heartbeat
    }
}
```

### voicebot.go
```go
// Pause STT before TTS
func (v VoiceBot) HandleTranslationResults(...) {
    if useGoogle && v.googleSTTProvider != nil {
        v.googleSTTProvider.PauseCall(callid)
    }
    // Send to TTS
}

// Resume STT after TTS
func (v VoiceBot) HandlePlaybackComplete(...) {
    if useGoogle && v.googleSTTProvider != nil {
        v.googleSTTProvider.ResumeCall(callid)
    }
}
```

## Expected Behavior

### Before Fix
```
User speaks → STT processes → Translation → TTS plays
                                    ↓
                              STT still processing
                                    ↓
                            Audio timeout errors
```

### After Fix
```
User speaks → STT processes → Translation → STT pauses → TTS plays → STT resumes
```

## Testing

### 1. Test Translation Flow
```bash
export USE_GOOGLE_STT_TTS=true
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json
export STT_LANGUAGE=en-US
export TTS_LANGUAGE=cmn-CN
export TRANSLATE_SOURCE_LANGUAGE=en-US
export TRANSLATE_TARGET_LANGUAGE=zh-CN
go run .
```

### 2. Monitor Logs
Look for these log messages:
- `STT:Pause callid=xxx status=Paused` - STT paused
- `STT:Resume callid=xxx status=Resumed` - STT resumed
- `STT:Write callid=xxx status=Skipped - Paused` - Audio skipped
- `STT:heartbeat Event=Skipped - Paused` - Heartbeat skipped

### 3. Expected Log Sequence
```
1. User speaks: "Hello"
2. STT:MessageResponse text="Hello"
3. Translation: "Hello" → "你好"
4. STT:Pause callid=xxx status=Paused
5. TTS:AddText text="你好"
6. TTS playback starts
7. TTS playback completes
8. STT:Resume callid=xxx status=Resumed
9. Ready for next input
```

## Benefits

1. **Eliminates Audio Timeout Errors** - No more timeout during TTS playback
2. **Reduces API Calls** - Skips unnecessary audio processing
3. **Improves Performance** - Less CPU usage during TTS
4. **Better User Experience** - Smooth translation flow
5. **Reduces Costs** - Fewer Google API calls

## Configuration

No additional configuration required. The pause/resume mechanism is automatic and transparent to the user.

## Troubleshooting

### If STT doesn't resume:
1. Check `HandlePlaybackComplete` logs
2. Verify TTS callback is properly set
3. Check for errors in TTS playback

### If audio is still processed during TTS:
1. Check pause state logs
2. Verify `IsPaused()` method
3. Check thread safety of pause state

### If timeouts still occur:
1. Reduce heartbeat interval
2. Check network stability
3. Monitor pause/resume timing
