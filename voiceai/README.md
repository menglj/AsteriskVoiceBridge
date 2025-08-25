# voiceai

This package provides an interface to OpenAI realtime APIs for integrating voice AI features within the Asterisk Voice Bridge project.


## Features

- Connects to OpenAI RealTime service and maintains a dialog for the given dialog/call id.

## Interface Functions

- `func CreateOPENAIWebsocketDialogController(defaultCapabilityFile string, mode string)`: Create a new controller instance
- `func (sdc *OPENAIWebsocketDialogController) SetCallbacks(cbi OPENAIDialogControllerCallBackHander)`: Set a new callback handler (ie the voicebot)
- `func (sdc *OPENAIWebsocketDialogController) NewDialog(dialogid string, udproxy *vproxy.IOProxy)`: Create a new OpenAI dialog (a call).
- `func (sdc *OPENAIWebsocketDialogController) DialogComplete(dialogid string)`: Indicate the OpenAI dialog is complete and can be removed.
- `func (sdc *OPENAIWebsocketDialogController) UpdateDialog(dialogid string, tools string, instructions string)`: Update a dialog with new tools and instructions.
- `func (sdc *OPENAIWebsocketDialogController) ExternalVADText(dialogid string, data string, level string)`: Write Text to OpenAI (from your STT engine, ie deepgram)
- `func (sdc *OPENAIWebsocketDialogController) Disengage(dialogid string)`: Disengage the OpenAI dialog (pause the AI from the call)
- `func (sdc *OPENAIWebsocketDialogController) Engage(dialogid string)`: Engage the OpenAI dialog (un-pause the AI from the call)


## Configuration

Set your OpenAI API key as an environment variable:

```sh
export OPENAI_API_TOKEN=your_api_key
```

