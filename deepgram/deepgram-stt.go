/*
 * asteriskvoicebridge
 *
 * Copyright (C) 2025, Sangoma Technologies Corporation
 *
 * Michael Bradeen <mbradeen@sangoma.com>
 *
 * This program is free software, distributed under the terms of
 * the GNU AFFERO General Public License Version 3. See the LICENSE file
 * at the top of the source tree.
 */

package deepgram

import (
	"context"
	"os"
	"strings"

	"github.com/asterisk/AsteriskVoiceBridge/vproxy"
	"golang.org/x/exp/slog"

	msginterfaces "github.com/deepgram/deepgram-go-sdk/pkg/api/listen/v1/websocket/interfaces"
	interfaces "github.com/deepgram/deepgram-go-sdk/pkg/client/interfaces"
	client "github.com/deepgram/deepgram-go-sdk/pkg/client/listen"
)

const (
	// how many detected words before we trigger VAD
	VAD_TRIGGER = 3
)

func getBitRate() int {
	return vproxy.GetRTP_BITRATE()
}

func getEncoding() string {
	encoding := vproxy.GetRTP_MODE()
	if encoding == "ulaw" {
		return "mulaw"
	} else if encoding == "l16" {
		return "linear16"
	}

	return "mulaw"
}

var log = slog.New(slog.NewTextHandler(os.Stderr, nil))

type transcriptCB func( /*callid*/ string /*transcriptText*/, string /*level*/, string) bool

/**************************** deepgramSTTClient ****************************/

type deepgramSTTClient struct {
	// callid is the unique identifier for the call
	// language is the language we are listening for
	callid, language string
	// keywords are any special words we want to boost the recognition of
	keywords []string
	// conversational uses endpointing along with punctuation detection to build text segments
	// chat uses utterence ends to build text segments
	// commandMode also uses utterance ends to build segments, but only listens for a single command
	// before returning to the previous mode(s)
	conversational, chat, commandMode bool
	// ai mode is a modified conversational model
	aimode bool
	// how we remember the previous state of conversational and chat modes when we enter command mode
	conversationalPauseState, chatPauseState bool
	// buffers for the different modes
	bufferedEndpoint, bufferedUtterance, bufferedCommand string
	// how many times have we waited for conversational mode to build a segment?
	endpointwaits int
	// use StreamTo() function on WSCallback to send audio data to deepgram
	udpproxy   *vproxy.IOProxy
	dgCallBack *client.WSCallback
	// callback we use to send text segments back to the voicebot
	transcriptCallback transcriptCB
	// how many words have we detected since the last VAD trigger?
	vadwordcount int
	vadtriggered bool
	vadindicated bool
}

func (c *deepgramSTTClient) setMode(mode string) {

	if strings.Contains(mode, "chat") {
		log.Info("STT:setMode", "callid", c.callid, "mode", "chat")
		c.chat = true
		c.chatPauseState = true
	}

	if strings.Contains(mode, "conversationalai") {
		log.Info("STT:setMode", "callid", c.callid, "mode", "conversationalai")
		c.aimode = true
		c.conversational = true
		c.conversationalPauseState = true
		c.chat = false
		c.chatPauseState = false
		c.commandMode = false
	} else if strings.Contains(mode, "conversational") {
		log.Info("STT:setMode", "callid", c.callid, "mode", "conversational")
		c.conversational = true
		c.conversationalPauseState = true
	}
}

func (c *deepgramSTTClient) start() bool {

	dgapikey, exists := os.LookupEnv("DG_API_TOKEN")
	if !exists {
		log.Error("STT:DeepgramStart", "Error", "API Token not set, can not create call!!")
		return false
	}

	endpointing := "true"
	vadevents := false
	model := "nova-2-phonecall"
	utterenceendms := "1000"
	if c.aimode {
		model = "nova-2-conversationalai"
		endpointing = "500"
		vadevents = true
		utterenceendms = "1200"
	}

	ctx := context.Background()
	// set the Transcription options
	log.Info("STT:NewCall", "Event", "CreatingClient", "Model", model, "Encoding", getEncoding(), "SampleRate", getBitRate())
	transcriptOptions := &interfaces.LiveTranscriptionOptions{
		Language:       c.language,
		Model:          model,
		SmartFormat:    true,
		Encoding:       getEncoding(),
		Channels:       1,
		SampleRate:     getBitRate(),
		InterimResults: true,
		UtteranceEndMs: utterenceendms,
		Endpointing:    endpointing,
		VadEvents:      vadevents,
		//Keywords:       c.keywords,
	}
	// set the client options
	clientOptions := &interfaces.ClientOptions{
		EnableKeepAlive: true, // Enable KeepAlive option
	}

	// Create a new Deepgram LiveTranscription client with config options
	dgClient, err := client.NewWSUsingCallback(ctx, dgapikey, clientOptions, transcriptOptions, c)
	if err != nil {
		log.Error("STT:DeepgramStart", "LiveTranscriptionClient", err)
		dgClient.Stop()
		return false
	}

	c.dgCallBack = dgClient
	log.Info("STT:DeepgramStart", "LiveTranscriptionClient", "Created")

	// Start the Deepgram LiveTranscription client
	bConencted := dgClient.Connect()
	if !bConencted {
		log.Error("STT:DeepgramStart", "LiveTranscriptionClient", "ConnectionFailed")
		dgClient.Stop()
		return false
	}

	go func() {
		log.Info("STT:Call", "Stream", "SessionStart")
		err := c.udpproxy.StreamTo(dgClient)
		log.Info("STT:Call", "Stream", "SessionEnd")
		// comment previous and uncomment next to test rtp echo
		//err := c.udpproxy.Echo()
		if err != nil {
			// handle error
		}
	}()
	return true
}

func (c *deepgramSTTClient) stop() bool {
	if c.dgCallBack == nil {
		log.Error("STT:DeepgramStop", "Callback", "NotFound")
		return false
	}
	c.dgCallBack.Stop()
	return true
}

func (c *deepgramSTTClient) toggleConversation(toggle bool) {
	log.Info("STT:toggleConversation", "toggle", toggle)
	if toggle {
		c.conversational = true
		c.conversationalPauseState = true
		c.commandMode = false
	} else {
		c.conversational = false
		c.conversationalPauseState = false
		c.bufferedEndpoint = ""
	}
}

func (c *deepgramSTTClient) toggleChat(toggle bool) {
	log.Info("STT:toggleChat", "toggle", toggle)
	if toggle {
		c.chat = true
		c.chatPauseState = true
		c.commandMode = false
	} else {
		c.chat = false
		c.chatPauseState = false
		c.bufferedUtterance = ""
	}
}

func (c *deepgramSTTClient) commandListen() {
	// clear the command buffer and wait
	c.bufferedCommand = ""
	c.commandMode = true
	c.conversationalPauseState = c.conversational
	c.conversational = false
	c.chatPauseState = c.chat
	c.chat = false
	// clear the other buffers even if we lose some text
	c.bufferedEndpoint = ""
	c.bufferedUtterance = ""
}

func (c *deepgramSTTClient) commandClear() {
	c.commandMode = false
	c.bufferedCommand = ""
	c.conversational = c.conversationalPauseState
	c.chat = c.chatPauseState
}

func (c *deepgramSTTClient) clearAllBuffers() {
	c.bufferedEndpoint = ""
	c.bufferedUtterance = ""
	c.bufferedCommand = ""
}

// functions needed for Deepgram LiveMessageCallback interface
func (c *deepgramSTTClient) Open(or *msginterfaces.OpenResponse) error {
	log.Info("STT:OpenResponse", "Type", or.Type)
	return nil
}

func (c *deepgramSTTClient) Message(mr *msginterfaces.MessageResponse) error {
	sentence := mr.Channel.Alternatives[0].Transcript

	if len(mr.Channel.Alternatives) == 0 || sentence == "" {
		return nil
	}

	if mr.IsFinal {
		log.Info("STT:MessageResponse", "Text", sentence)

		if !c.commandMode {
			if c.conversational {

				if c.aimode {
					if c.vadtriggered && !c.vadindicated {
						c.vadwordcount += strings.Count(sentence, " ") + 1
						if c.vadwordcount >= VAD_TRIGGER {
							log.Info("STT:MessageResponse", "AIType", "VADTriggered")
							c.transcriptCallback(c.callid, "", "conversationalai-vad-start")
							c.vadindicated = true
						} else {
							log.Info("STT:MessageResponse", "AIType", "VADNotTriggered", "WordCount", c.vadwordcount)
						}
					} else {
						log.Info("STT:MessageResponse", "AIType", "VADPreviouslyTriggered")
					}

					// send text to the callback in conversational mode as long as we have something
					c.transcriptCallback(c.callid, sentence, "conversationalai")
					c.bufferedEndpoint = ""
					c.endpointwaits = 0
				} else {
					if c.bufferedEndpoint == "" {
						c.bufferedEndpoint = sentence
					} else {
						c.bufferedEndpoint = c.bufferedEndpoint + " " + sentence
					}
				}

				if mr.SpeechFinal {
					// look for conversational groupings to end with punctiation, but only wait 3 times
					if c.endpointwaits >= 3 || strings.HasSuffix(sentence, ".") || strings.HasSuffix(sentence, "!") || strings.HasSuffix(sentence, "?") {
						if c.aimode {
							c.transcriptCallback(c.callid, "", "conversationalai-vad-timeout")
							c.vadwordcount = 0
							c.vadtriggered = false
							c.endpointwaits = 0
						} else {
							if c.bufferedEndpoint != "" {
								// send text to the callback in conversational mode as long as we have something
								c.transcriptCallback(c.callid, c.bufferedEndpoint, "conversational")
								c.bufferedEndpoint = ""
								c.endpointwaits = 0
							}
						}
					} else {
						c.endpointwaits++
					}
				}

			}
			// conversational and chat can co-exist
			if c.chat {
				if c.bufferedUtterance == "" {
					c.bufferedUtterance = sentence
				} else {
					c.bufferedUtterance = c.bufferedUtterance + " " + sentence
				}
			}
			// send text to the callback in passive mode for local commands
			c.transcriptCallback(c.callid, sentence, "passive")
		} else {
			c.bufferedCommand = c.bufferedCommand + sentence
		}
	}

	return nil
}

func (c *deepgramSTTClient) Metadata(md *msginterfaces.MetadataResponse) error {
	log.Info("STT:MetadataResponse", "response", md)
	return nil
}

func (c *deepgramSTTClient) SpeechStarted(ssr *msginterfaces.SpeechStartedResponse) error {
	log.Info("STT:SpeechStartedResponse")
	//c.transcriptCallback(c.callid, "", "conversationalai-vad-start")
	c.vadwordcount = 0
	c.vadtriggered = true
	return nil
}

// text detection timeout
func (c *deepgramSTTClient) UtteranceEnd(ur *msginterfaces.UtteranceEndResponse) error {
	log.Info("STT:UtteranceEndResponse", "Type", ur.Type)

	if c.commandMode {
		if c.bufferedCommand != "" {
			c.transcriptCallback(c.callid, c.bufferedCommand, "command")
			c.commandClear()
		}
		return nil
	}

	if c.aimode {
		log.Info("STT:UtteranceEndResponse", "AIType", "VADLongTimeout")
		c.transcriptCallback(c.callid, "", "conversationalai-vad-longtimeout")
		c.vadwordcount = 0
		c.vadtriggered = false
		return nil
	}

	// chat and conversation can co-exist
	if c.chat && c.bufferedUtterance != "" {
		c.transcriptCallback(c.callid, c.bufferedUtterance, "chat")
		c.bufferedUtterance = ""
	}

	if c.conversational && c.bufferedEndpoint != "" {
		c.transcriptCallback(c.callid, c.bufferedEndpoint, "conversational")
		c.bufferedEndpoint = ""
	}

	return nil
}

func (c *deepgramSTTClient) Close(cr *msginterfaces.CloseResponse) error {
	log.Info("STT:CloseResponse", "Type", cr.Type)
	return nil
}
func (c *deepgramSTTClient) Error(er *msginterfaces.ErrorResponse) error {
	log.Info("STT:ErrorResponse", "response", er)
	return nil
}

func (c *deepgramSTTClient) UnhandledEvent(byData []byte) error {
	log.Info("STT:UnhandledEvent", "response", byData)
	return nil
}

/**************************** DeepgramSTTProvider ****************************/

type DeepgramSTTCallBackHandler interface {
	HandleTranscriptResults(callid string, text string, level string) bool
}

type DeepgramSTTProvider struct {
	encoding           string
	transcriptCallback transcriptCB
	//deepgram instance
	clientMap map[string]*deepgramSTTClient
}

/* Create and return a DeepgramSTTProvider */
func CreateProvider(codec string) (*DeepgramSTTProvider, bool) {

	provider := DeepgramSTTProvider{encoding: codec}
	provider.clientMap = make(map[string]*deepgramSTTClient)

	client.Init(client.InitLib{
		LogLevel: client.LogLevelDefault, // LogLevelDefault, LogLevelFull, LogLevelDebug, LogLevelTrace, LogLevelVerbose
	})

	return &provider, true
}

func (d *DeepgramSTTProvider) SetCallbacks(cbi DeepgramSTTCallBackHandler) {
	d.transcriptCallback = cbi.HandleTranscriptResults
}

func (d *DeepgramSTTProvider) transcriptCB(callid string, text string, level string) bool {
	d.transcriptCallback(callid, text, level)
	return true
}

func (d DeepgramSTTProvider) createClient(callid string, mode string, language string, keywords []string, udpproxy *vproxy.IOProxy, callback transcriptCB) (*deepgramSTTClient, bool) {
	client := &deepgramSTTClient{callid: callid, language: language, keywords: keywords, udpproxy: udpproxy, endpointwaits: 0, transcriptCallback: callback}
	client.setMode(mode)
	return client, true
}

/* New Call indication */
func (d DeepgramSTTProvider) NewCall(callid string, mode string, language string, commandword string, udpproxy *vproxy.IOProxy) bool {
	keywords := []string{commandword}
	client, ok := d.createClient(callid, mode, language, keywords, udpproxy, d.transcriptCB)
	if ok {
		d.clientMap[callid] = client
		ok = client.start()
	}
	return ok
}

func (d DeepgramSTTProvider) terminateClient(callid string) bool {
	client, ok := d.clientMap[callid]
	if ok {
		ok = client.stop()
	}
	return ok
}

/* Call end indication */
func (d DeepgramSTTProvider) EndCall(callid string) bool {
	ok := d.terminateClient(callid)
	if ok {
		delete(d.clientMap, callid)
	}
	return ok
}

/* Toggle Conversation true or false */
func (d DeepgramSTTProvider) ToggleConversation(callid string, toggle bool) bool {
	client, ok := d.clientMap[callid]
	if ok {
		client.toggleConversation(toggle)
	}
	return ok
}

func (d DeepgramSTTProvider) GetConversation(callid string) bool {
	client, ok := d.clientMap[callid]
	if ok {
		return client.conversational
	}
	return false
}

/* Toggle Chat true or false */
func (d DeepgramSTTProvider) ToggleChat(callid string, toggle bool) bool {
	client, ok := d.clientMap[callid]
	if ok {
		client.toggleChat(toggle)
	}
	return ok
}

func (d DeepgramSTTProvider) GetChat(callid string) bool {
	client, ok := d.clientMap[callid]
	if ok {
		return client.chat
	}
	return false
}

/* Start listening for a command */
func (d DeepgramSTTProvider) ListenForCommand(callid string) bool {
	log.Info("STT:ListenForCommand", "CallID", callid)
	client, ok := d.clientMap[callid]
	if ok {
		client.commandListen()
	} else {
		log.Error("STT:ListenForCommand", "Error", "No client found for callid")
	}
	return ok
}

/* Cancel listening for a command */
func (d DeepgramSTTProvider) CancelCommand(callid string) bool {
	log.Info("STT:CancelCommand", "CallID", callid)
	client, ok := d.clientMap[callid]
	if ok {
		client.commandClear()
	}
	return ok
}

/* Clear all current text buffers */
func (d DeepgramSTTProvider) ClearBuffers(callid string) bool {
	client, ok := d.clientMap[callid]
	if ok {
		client.clearAllBuffers()
	}
	return ok
}
