# ariman

`ariman` is a Go package designed to serve as an ARI interface between Asterisk and the voicebot.

## Features

- Builds and maintains the necissary call structure for the voicebot, including all bridges and channels.
- Listens for DTMF events to pass to the voicebot.

## Interface Functions

- `func (c *Connector) SetCallbacks(cbi ARIEventHandler)`: Specify a handler object, in this case the voicebot.
- `func (c *Connector) Connect() bool`: Connect to the local Asterisk process via ARI.
- `func (c *Connector) AddURItoCall(callid string, uri string)`: Add a URI to the user portion of the call and generate a bridge to do so if required.
- `func (c *Connector) RemoveURIfromCall(callid string, uri string)`: Remove a URI from the user portion of the call and remove the bridge if no other channels are connected.
- `func (c *Connector) SendCalltoURI(callid string, uri string)`: Transfer the call out of the application (WIP)
- `func (c *Connector) ContinueCall(callid string, context string, extension string, priority int)`: Continue the call into dialplan
- `func (c *Connector) PlayFile(callid string, file string, language string) bool`: Play a file on the user channel.
- `func (c *Connector) HangupCall(callid string)`: Hangup the call.
