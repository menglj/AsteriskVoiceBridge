# Google STT/TTS/Translate Usage Example

## Environment Setup

```bash
# Set environment variables for phone translation
export USE_GOOGLE_STT_TTS=true
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/your/google-service-account.json
export STT_LANGUAGE=en-US  # Source language for speech recognition
export TTS_LANGUAGE=zh-CN  # Target language for speech synthesis
export TRANSLATE_SOURCE_LANGUAGE=en-US  # Source language for translation
export TRANSLATE_TARGET_LANGUAGE=zh-CN  # Target language for translation

# Optional: Set other required environment variables
export OPMODE=text  # or "hybrid" or "audio"
```

## Docker Usage

Update your Docker run command to include the Google credentials:

```bash
# Mount the Google service account key file
docker run -it \
  -e USE_GOOGLE_STT_TTS=true \
  -e GOOGLE_APPLICATION_CREDENTIALS=/app/google-key.json \
  -e STT_LANGUAGE=en-US \
  -e TTS_LANGUAGE=zh-CN \
  -e TRANSLATE_SOURCE_LANGUAGE=en-US \
  -e TRANSLATE_TARGET_LANGUAGE=zh-CN \
  -v /path/to/your/google-key.json:/app/google-key.json \
  your-image-name
```

## Language Configuration

### English to Chinese Translation
```bash
export STT_LANGUAGE=en-US   # Speech recognition (English)
export TTS_LANGUAGE=cmn-CN  # Speech synthesis (Chinese)
export TRANSLATE_SOURCE_LANGUAGE=en-US  # Translation source
export TRANSLATE_TARGET_LANGUAGE=zh-CN  # Translation target
```

### Chinese to English Translation
```bash
export STT_LANGUAGE=cmn-CN  # Speech recognition (Chinese)
export TTS_LANGUAGE=en-US   # Speech synthesis (English)
export TRANSLATE_SOURCE_LANGUAGE=zh-CN  # Translation source
export TRANSLATE_TARGET_LANGUAGE=en-US  # Translation target
```

### English to Japanese Translation
```bash
export STT_LANGUAGE=en-US  # Speech recognition
export TTS_LANGUAGE=ja-JP  # Speech synthesis
export TRANSLATE_SOURCE_LANGUAGE=en-US  # Translation source
export TRANSLATE_TARGET_LANGUAGE=ja-JP  # Translation target
```

### Other Language Pairs
```bash
# English to Spanish
export STT_LANGUAGE=en-US
export TTS_LANGUAGE=es-ES
export TRANSLATE_SOURCE_LANGUAGE=en-US
export TRANSLATE_TARGET_LANGUAGE=es-ES

# French to German
export STT_LANGUAGE=fr-FR
export TTS_LANGUAGE=de-DE
export TRANSLATE_SOURCE_LANGUAGE=fr-FR
export TRANSLATE_TARGET_LANGUAGE=de-DE
```

## Translation Flow

The system now works as a phone translation service with the following flow:

1. **Speech Recognition (STT)**: Converts speech to text in the source language
2. **Translation**: Translates the text from source to target language
3. **Speech Synthesis (TTS)**: Converts translated text to speech in the target language

### Example: English to Chinese Translation

1. User speaks in English: "Hello, how are you?"
2. STT recognizes: "Hello, how are you?" (English text)
3. Translation converts: "Hello, how are you?" → "你好，你好吗？" (Chinese text)
4. TTS synthesizes: "你好，你好吗？" (Chinese speech)

## Switching Between Providers

### Use Google STT/TTS/Translate
```bash
export USE_GOOGLE_STT_TTS=true
```

### Use Deepgram + OpenAI (original)
```bash
export USE_GOOGLE_STT_TTS=false
# or simply don't set the variable
```

## Configuration Files

### Google Service Account Key
Create a service account in Google Cloud Console and download the JSON key file. The file should look like:

```json
{
  "type": "service_account",
  "project_id": "your-project-id",
  "private_key_id": "...",
  "private_key": "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n",
  "client_email": "your-service-account@your-project.iam.gserviceaccount.com",
  "client_id": "...",
  "auth_uri": "https://accounts.google.com/o/oauth2/auth",
  "token_uri": "https://oauth2.googleapis.com/token",
  "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
  "client_x509_cert_url": "..."
}
```

## Testing the Integration

1. **Start the application** with Google STT/TTS enabled
2. **Make a test call** to your Asterisk system
3. **Check logs** for Google API calls and responses
4. **Verify** that speech is being transcribed and synthesized correctly

## Monitoring and Debugging

### Log Messages to Look For
- `STT:GoogleStart` - Google STT client initialization
- `TTS:NewCall` - Google TTS client initialization
- `STT:MessageResponse` - Speech transcription results
- `TTS:SendText` - Text-to-speech synthesis

### Common Error Messages
- `Google API Token not set` - Missing or invalid credentials
- `Failed to create Speech client` - Authentication or network issues
- `Failed to synthesize speech` - TTS API errors

## Performance Considerations

### STT Performance
- Google's `phone_call` model is optimized for telephony
- Streaming recognition provides low latency
- Interim results allow for real-time feedback

### TTS Performance
- Neural2 voices provide high-quality output
- Each synthesis request is independent
- Audio is cached and streamed efficiently

## Cost Optimization

### STT Costs
- Pay per 15-second increment
- Phone call model may have different pricing
- Consider batch processing for non-real-time use cases

### TTS Costs
- Pay per character synthesized
- Neural2 voices may cost more than standard voices
- Consider caching frequently used phrases
