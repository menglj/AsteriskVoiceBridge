# Deepgram Go Package

This package provides integration with Deepgram's Speech-to-Text (STT) and Text-to-Speech (TTS) APIs.

## Files

### `deepgram-stt.go`

Implements Deepgram Speech-to-Text (STT) functionality.

#### Features

- Sends audio data to Deepgram API for transcription.
- Supports configuration of language, model, and other Deepgram options.
- Handles API authentication and error responses.
- Returns transcribed text and relevant metadata.

## Interface Functions

- `func CreateProvider(codec string) (*DeepgramSTTProvider, bool)`: Create a STT provider instance
- `func (d *DeepgramSTTProvider) SetCallbacks(cbi DeepgramSTTCallBackHandler)`: Set a new callback handler (ie the voicebot)
- `func (d DeepgramSTTProvider) NewCall(callid string, mode string, language string, commandword string, udpproxy *vproxy.IOProxy)`: Create a new deepgram STT call.
- `func (d DeepgramSTTProvider) EndCall(callid string)`: Terminate and delete a given deepgram STT call.
- `func (d DeepgramSTTProvider) ToggleConversation(callid string, toggle bool)`: Toggle conversational mode on/off for a given call.
- `func (d DeepgramSTTProvider) GetConversation(callid string)`: Check if conversational mode is on for a given call.
- `func (d DeepgramSTTProvider) ToggleChat(callid string, toggle bool)`: Toggle chat mode on/off for a given call.
- `func (d DeepgramSTTProvider) GetChat(callid string)`: Check if chat mode is on for a given call.
- `func (d DeepgramSTTProvider) ListenForCommand(callid string)`: Treat the next speech segment as an explicit command.
- `func (d DeepgramSTTProvider) CancelCommand(callid string)`: Cancel treating the next speech segment as an explicit command.
- `func (d DeepgramSTTProvider) ClearBuffers(callid string)`: Clear any current buffered chat, conversational and command text.

### `deepgram-tts.go`

Implements Deepgram Text-to-Speech (TTS) functionality.

#### Features

- Sends text to Deepgram API for speech synthesis.
- Supports voice selection and audio format options.
- Handles API authentication and error responses.
- Returns synthesized audio data.

## Interface Functions

- `func CreateTTSDeepgram() (*TTSDeepgram, bool)`: Create a TTS provider instance
- `func (tts *TTSDeepgram) SetCallbacks(cbi TTSDeepgramCallBackHandler)`: Set a new callback handler (ie the voicebot)
- `func (tts TTSDeepgram) NewCall(callid string, udproxy *vproxy.IOProxy)`: Create a new deepgram TTS call.
- `func (tts TTSDeepgram) EndCall(callid string)`: Terminate and delete a give deepgram TTS call.
- `func (tts TTSDeepgram) Engage(callid string)`: Engage TTS service for a given call.
- `func (tts TTSDeepgram) Disengage(callid string)`: Disengage TTS service for a given call.
- `func (tts TTSDeepgram) AddText(callid string, text string, id string, language string)`: Add text to a call to be read and start recitation if not already started.
- `func (tts TTSDeepgram) CancelText(callid string)`: Cancel any text to be recitated.
- `func (tts TTSDeepgram) PauseText(callid string))`: Pause recitation.
- `func (tts TTSDeepgram) ResumeText(callid string)`: Resume recitation.

## Configuration

Set your Deepgram API key as an environment variable:

```sh
export DG_API_TOKEN=your_api_key
```
