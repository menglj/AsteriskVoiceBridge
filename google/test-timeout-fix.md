# Google STT Audio Timeout Fix

## Problem
The Google STT API was experiencing "Audio Timeout Error" during phone calls:
```
Audio Timeout Error: Long duration elapsed without audio. Audio should be sent close to real time.
```

## Root Cause
Google Cloud Speech-to-Text API requires continuous audio stream. When there are gaps in audio (silence, network issues, etc.), the API times out.

## Solution Implemented

### 1. Enhanced Error Handling
- Added specific handling for "Audio Timeout Error" and "OutOfRange" errors
- These errors are now treated as non-fatal and the stream continues
- Added context deadline exceeded handling

### 2. Heartbeat Mechanism
- Added periodic heartbeat to send empty audio packets
- Configurable interval via `GOOGLE_STT_HEARTBEAT_INTERVAL` environment variable
- Default interval: 10 seconds
- Keeps the stream alive during silence periods

### 3. Improved Audio Configuration
- Added audio channel configuration for better phone call handling
- Enhanced error recovery in Write function
- Skip empty data packets to avoid unnecessary API calls

### 4. Better Logging
- Added debug logging for heartbeat events
- Improved error categorization (fatal vs non-fatal)
- Added configuration logging

## Configuration

### Environment Variables
```bash
# Required
export USE_GOOGLE_STT_TTS=true
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json

# Optional - STT Configuration
export GOOGLE_STT_HEARTBEAT_INTERVAL=10s  # Default: 10s
```

### Heartbeat Intervals
- `5s` - For unstable connections
- `10s` - Default, good for most calls
- `15s` - For stable connections
- `30s` - For very stable connections

## Testing

### 1. Test with Default Settings
```bash
export USE_GOOGLE_STT_TTS=true
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json
go run .
```

### 2. Test with Aggressive Heartbeat
```bash
export USE_GOOGLE_STT_TTS=true
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json
export GOOGLE_STT_HEARTBEAT_INTERVAL=5s
go run .
```

### 3. Monitor Logs
Look for these log messages:
- `STT:heartbeat Event=HeartbeatStarted` - Heartbeat started
- `STT:heartbeat Event=HeartbeatSent` - Heartbeat sent (debug level)
- `STT:handleResponses Event=AudioTimeout` - Audio timeout (non-fatal)
- `STT:Write Event=NonFatalError` - Non-fatal write error

## Expected Behavior

### Before Fix
- Audio timeout errors would break the STT stream
- Call would fail when silence occurred
- Error: "Failed to receive response"

### After Fix
- Audio timeout errors are logged as warnings
- STT stream continues despite timeouts
- Heartbeat keeps stream alive during silence
- Call continues normally

## Troubleshooting

### If timeouts still occur:
1. Reduce heartbeat interval to 5s
2. Check network stability
3. Monitor for patterns in timeout occurrences
4. Consider using lower audio bitrate

### If heartbeat causes issues:
1. Increase heartbeat interval to 15s or 30s
2. Monitor CPU usage
3. Check for excessive API calls

## Files Modified
- `google/google-stt.go` - Main STT implementation
- `google/README.md` - Documentation updates
- `google/example-usage.md` - Usage examples
