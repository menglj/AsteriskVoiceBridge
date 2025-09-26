# Google Cloud STT/TTS/Translate Integration

This package provides integration with Google Cloud Speech-to-Text (STT), Text-to-Speech (TTS), and Translation APIs as an alternative to Deepgram and OpenAI for phone translation services.

## Files

### `google-stt.go`

Implements Google Cloud Speech-to-Text (STT) functionality.

#### Features

- Sends audio data to Google Cloud Speech-to-Text API for transcription
- Supports configuration of language, model, and other Google options
- Handles API authentication and error responses
- Returns transcribed text and relevant metadata
- Supports streaming recognition for real-time transcription

### `google-tts.go`

Implements Google Cloud Text-to-Speech (TTS) functionality.

### `google-translate.go`

Implements Google Cloud Translation API functionality for real-time phone translation.

#### Features

- Real-time text translation between languages
- Supports 100+ languages
- Automatic language detection
- Configurable source and target languages
- Callback-based architecture for integration with voicebot
- Uses high-quality Neural2 voices
- Handles API authentication and error responses
- Returns synthesized audio in G.711 μ-law format

## Setup

### 1. Google Cloud Project Setup

1. Create a Google Cloud project
2. Enable the following APIs:
   - Speech-to-Text API
   - Text-to-Speech API
3. Create a service account and download the JSON key file
4. Set the `GOOGLE_APPLICATION_CREDENTIALS` environment variable to point to the key file

### 2. Environment Variables

Set the following environment variables to use Google STT/TTS:

```bash
export USE_GOOGLE_STT_TTS=true
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/your/service-account-key.json
export STT_LANGUAGE=en-US  # Source language for speech recognition
export TTS_LANGUAGE=zh-CN  # Target language for speech synthesis
export TRANSLATE_SOURCE_LANGUAGE=en-US  # Source language for translation
export TRANSLATE_TARGET_LANGUAGE=zh-CN  # Target language for translation
```

#### Language Configuration

**STT (Speech-to-Text) Language:**
The `STT_LANGUAGE` environment variable controls the speech recognition language. Common values:

- `en-US` - English (US) - Default
- `zh-CN` - Chinese (Simplified, China)
- `zh-TW` - Chinese (Traditional, Taiwan)
- `ja-JP` - Japanese
- `ko-KR` - Korean
- `es-ES` - Spanish (Spain)
- `fr-FR` - French (France)
- `de-DE` - German (Germany)

**TTS (Text-to-Speech) Language:**
The `TTS_LANGUAGE` environment variable controls the speech synthesis language and automatically selects appropriate voice:

- `en-US` - English (US) - Default, uses `en-US-Neural2-J`
- `zh-CN` - Chinese (Simplified) - uses `zh-CN-Wavenet-A`
- `zh-TW` - Chinese (Traditional) - uses `zh-TW-Wavenet-A`
- `ja-JP` - Japanese - uses `ja-JP-Wavenet-A`
- `ko-KR` - Korean - uses `ko-KR-Wavenet-A`
- `es-ES` - Spanish - uses `es-ES-Neural2-A`
- `fr-FR` - French - uses `fr-FR-Neural2-A`
- `de-DE` - German - uses `de-DE-Neural2-A`

**Translation Languages:**
The `TRANSLATE_SOURCE_LANGUAGE` and `TRANSLATE_TARGET_LANGUAGE` environment variables control the translation direction:

- `en-US` - English (US) - Default source
- `zh-CN` - Chinese (Simplified) - Default target
- `zh-TW` - Chinese (Traditional)
- `ja-JP` - Japanese
- `ko-KR` - Korean
- `es-ES` - Spanish
- `fr-FR` - French
- `de-DE` - German
- And 100+ other languages

For a complete list of supported languages, see:
- [Google Cloud Speech-to-Text language support](https://cloud.google.com/speech-to-text/docs/languages)
- [Google Cloud Text-to-Speech voice list](https://cloud.google.com/text-to-speech/docs/voices)
- [Google Cloud Translation language support](https://cloud.google.com/translate/docs/languages)

### 3. Dependencies

The required Google Cloud dependencies are already included in `go.mod`:

- `cloud.google.com/go/speech v1.19.0`
- `cloud.google.com/go/texttospeech v1.7.0`

## Usage

The Google STT/TTS providers implement the same interface as the Deepgram providers, so they can be used as drop-in replacements.

### Switching Between Providers

To use Google STT/TTS instead of Deepgram, set the environment variable:

```bash
export USE_GOOGLE_STT_TTS=true
```

To use Deepgram (default), either unset the variable or set it to false:

```bash
export USE_GOOGLE_STT_TTS=false
# or
unset USE_GOOGLE_STT_TTS
```

### Voice Configuration

The Google TTS implementation uses the following default configuration:

- **Voice**: `en-US-Neural2-J` (High quality neural voice)
- **Language**: `en-US`
- **Audio Encoding**: `MULAW` (G.711 μ-law)
- **Sample Rate**: Configured based on RTP settings

### STT Configuration

The Google STT implementation uses the following default configuration:

- **Model**: `phone_call` (Optimized for phone quality audio)
- **Language**: Configurable via the `defaultlanguage` parameter
- **Audio Encoding**: `MULAW` (G.711 μ-law)
- **Sample Rate**: Configured based on RTP settings
- **Interim Results**: Enabled for real-time transcription

## Interface Functions

### STT Provider

- `func CreateProvider(codec string) (*GoogleSTTProvider, bool)`: Create a STT provider instance
- `func (d *GoogleSTTProvider) SetCallbacks(cbi GoogleSTTCallBackHandler)`: Set callback handler
- `func (d GoogleSTTProvider) NewCall(callid string, mode string, language string, commandword string, udpproxy *vproxy.IOProxy)`: Create a new Google STT call
- `func (d GoogleSTTProvider) EndCall(callid string)`: Terminate and delete a Google STT call
- `func (d GoogleSTTProvider) ToggleConversation(callid string, toggle bool)`: Toggle conversational mode
- `func (d GoogleSTTProvider) GetConversation(callid string)`: Check conversational mode status
- `func (d GoogleSTTProvider) ToggleChat(callid string, toggle bool)`: Toggle chat mode
- `func (d GoogleSTTProvider) GetChat(callid string)`: Check chat mode status
- `func (d GoogleSTTProvider) ListenForCommand(callid string)`: Listen for explicit command
- `func (d GoogleSTTProvider) CancelCommand(callid string)`: Cancel command listening
- `func (d GoogleSTTProvider) ClearBuffers(callid string)`: Clear text buffers

### TTS Provider

- `func CreateTTSGoogle() (*TTSGoogle, bool)`: Create a TTS provider instance
- `func (tts *TTSGoogle) SetCallbacks(cbi TTSGoogleCallBackHandler)`: Set callback handler
- `func (tts TTSGoogle) NewCall(callid string, udproxy *vproxy.IOProxy)`: Create a new Google TTS call
- `func (tts TTSGoogle) EndCall(callid string)`: Terminate and delete a Google TTS call
- `func (tts TTSGoogle) Engage(callid string)`: Engage TTS for a call
- `func (tts TTSGoogle) Disengage(callid string)`: Disengage TTS for a call
- `func (tts TTSGoogle) SendText(callid string, text string, id string, language string)`: Send text for synthesis
- `func (tts TTSGoogle) FlushResponseText(callid string)`: Flush response text
- `func (tts TTSGoogle) AddText(callid string, text string, id string, language string)`: Add text to synthesis queue
- `func (tts TTSGoogle) CancelText(callid string)`: Cancel current text synthesis
- `func (tts TTSGoogle) PauseText(callid string)`: Pause text synthesis
- `func (tts TTSGoogle) ResumeText(callid string)`: Resume text synthesis
- `func (tts TTSGoogle) IsStreaming(callid string)`: Check if currently streaming audio

## Advantages of Google STT/TTS

### Speech-to-Text
- **High Accuracy**: Google's phone_call model is optimized for telephony
- **Language Support**: Supports 120+ languages and dialects
- **Real-time Processing**: Streaming recognition for low latency
- **Custom Models**: Support for domain-specific vocabulary

### Text-to-Speech
- **High Quality**: Neural2 voices provide natural-sounding speech
- **Voice Variety**: 380+ voices across 50+ languages
- **SSML Support**: Advanced speech synthesis markup
- **Custom Voices**: Ability to create custom voice models

## Cost Considerations

Google Cloud STT/TTS pricing is typically more cost-effective than Deepgram for most use cases. Check the current Google Cloud pricing for detailed information.

## Troubleshooting

### Common Issues

1. **Authentication Errors**: Ensure `GOOGLE_APPLICATION_CREDENTIALS` is set correctly
2. **API Not Enabled**: Verify that Speech-to-Text and Text-to-Speech APIs are enabled
3. **Quota Exceeded**: Check your Google Cloud quotas and billing
4. **Network Issues**: Ensure proper network connectivity to Google Cloud APIs

### Logging

The implementation uses structured logging with the `slog` package. Check logs for detailed error information and debugging.
