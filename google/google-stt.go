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

package google

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/asterisk/AsteriskVoiceBridge/vproxy"
	"golang.org/x/exp/slog"

	speech "cloud.google.com/go/speech/apiv1"
	"cloud.google.com/go/speech/apiv1/speechpb"
	"google.golang.org/api/option"
)

const (
	// how many detected words before we trigger VAD
	VAD_TRIGGER = 3
)

func getBitRate() int {
	return vproxy.GetRTP_BITRATE()
}

func getEncoding() speechpb.RecognitionConfig_AudioEncoding {
	encoding := vproxy.GetRTP_MODE()
	if encoding == "ulaw" {
		return speechpb.RecognitionConfig_MULAW
	} else if encoding == "l16" {
		return speechpb.RecognitionConfig_LINEAR16
	}
	return speechpb.RecognitionConfig_MULAW
}

var log = slog.New(slog.NewTextHandler(os.Stderr, nil))

type transcriptCB func( /*callid*/ string /*transcriptText*/, string /*level*/, string) bool

/**************************** googleSTTClient ****************************/

type googleSTTClient struct {
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
	// use StreamTo() function on WSCallback to send audio data to google
	udpproxy   *vproxy.IOProxy
	client     *speech.Client
	stream     speechpb.Speech_StreamingRecognizeClient
	// callback we use to send text segments back to the voicebot
	transcriptCallback transcriptCB
	// how many words have we detected since the last VAD trigger?
	vadwordcount int
	vadtriggered bool
	vadindicated bool
	// mutex for thread safety
	mu sync.Mutex
	// context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

func (c *googleSTTClient) setMode(mode string) {
	c.mu.Lock()
	defer c.mu.Unlock()

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

func (c *googleSTTClient) start() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Create context with cancellation
	c.ctx, c.cancel = context.WithCancel(context.Background())

	// Initialize Google Speech client
	var err error
	c.client, err = speech.NewClient(c.ctx, option.WithCredentialsFile(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")))
	if err != nil {
		log.Error("STT:GoogleStart", "Error", "Failed to create Speech client", "error", err)
		return false
	}

	// Create streaming recognize request
	req := &speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: &speechpb.StreamingRecognitionConfig{
				Config: &speechpb.RecognitionConfig{
					Encoding:        getEncoding(),
					SampleRateHertz: int32(getBitRate()),
					LanguageCode:    c.language,
					Model:          "phone_call", // Use phone call model for better accuracy
					EnableAutomaticPunctuation: true,
					EnableWordTimeOffsets: true,
				},
				InterimResults: true,
			},
		},
	}

	// Create streaming client
	c.stream, err = c.client.StreamingRecognize(c.ctx)
	if err != nil {
		log.Error("STT:GoogleStart", "Error", "Failed to create streaming client", "error", err)
		c.client.Close()
		return false
	}

	// Send initial config
	if err := c.stream.Send(req); err != nil {
		log.Error("STT:GoogleStart", "Error", "Failed to send initial config", "error", err)
		c.stream.CloseSend()
		c.client.Close()
		return false
	}

	// Start goroutine to handle responses
	go c.handleResponses()

	// Start goroutine to stream audio
	go func() {
		log.Info("STT:Call", "Stream", "SessionStart")
		err := c.udpproxy.StreamTo(c)
		log.Info("STT:Call", "Stream", "SessionEnd")
		if err != nil {
			log.Error("STT:Call", "StreamError", err)
		}
	}()

	return true
}

func (c *googleSTTClient) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stream == nil {
		return 0, fmt.Errorf("stream not initialized")
	}

	req := &speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
			AudioContent: data,
		},
	}

	if err := c.stream.Send(req); err != nil {
		log.Error("STT:Write", "Error", "Failed to send audio data", "error", err)
		return 0, err
	}

	return len(data), nil
}

func (c *googleSTTClient) handleResponses() {
	for {
		resp, err := c.stream.Recv()
		if err != nil {
			if err == io.EOF {
				log.Info("STT:handleResponses", "Event", "StreamEnded")
				break
			}
			log.Error("STT:handleResponses", "Error", "Failed to receive response", "error", err)
			break
		}

		for _, result := range resp.Results {
			if len(result.Alternatives) == 0 {
				continue
			}

			transcript := result.Alternatives[0].Transcript
			if transcript == "" {
				continue
			}

			c.mu.Lock()
			isFinal := result.IsFinal
			c.mu.Unlock()

			if isFinal {
				log.Info("STT:MessageResponse", "Text", transcript)
				c.processFinalTranscript(transcript)
			} else {
				log.Debug("STT:InterimResult", "Text", transcript)
				// Handle interim results if needed
			}
		}
	}
}

func (c *googleSTTClient) processFinalTranscript(sentence string) {
	c.mu.Lock()
	defer c.mu.Unlock()

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

				// look for conversational groupings to end with punctuation, but only wait 3 times
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

func (c *googleSTTClient) stop() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	if c.stream != nil {
		c.stream.CloseSend()
	}

	if c.client != nil {
		c.client.Close()
	}

	return true
}

func (c *googleSTTClient) toggleConversation(toggle bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

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

func (c *googleSTTClient) toggleChat(toggle bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

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

func (c *googleSTTClient) commandListen() {
	c.mu.Lock()
	defer c.mu.Unlock()

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

func (c *googleSTTClient) commandClear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.commandMode = false
	c.bufferedCommand = ""
	c.conversational = c.conversationalPauseState
	c.chat = c.chatPauseState
}

func (c *googleSTTClient) clearAllBuffers() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.bufferedEndpoint = ""
	c.bufferedUtterance = ""
	c.bufferedCommand = ""
}

/**************************** GoogleSTTProvider ****************************/

type GoogleSTTCallBackHandler interface {
	HandleTranscriptResults(callid string, text string, level string) bool
}

type GoogleSTTProvider struct {
	encoding           string
	transcriptCallback transcriptCB
	//google instance
	clientMap map[string]*googleSTTClient
	mu        sync.RWMutex
}

/* Create and return a GoogleSTTProvider */
func CreateProvider(codec string) (*GoogleSTTProvider, bool) {
	provider := GoogleSTTProvider{encoding: codec}
	provider.clientMap = make(map[string]*googleSTTClient)

	return &provider, true
}

func (d *GoogleSTTProvider) SetCallbacks(cbi GoogleSTTCallBackHandler) {
	d.transcriptCallback = cbi.HandleTranscriptResults
}

func (d *GoogleSTTProvider) transcriptCB(callid string, text string, level string) bool {
	if d.transcriptCallback != nil {
		d.transcriptCallback(callid, text, level)
	}
	return true
}

func (d *GoogleSTTProvider) createClient(callid string, mode string, language string, keywords []string, udpproxy *vproxy.IOProxy, callback transcriptCB) (*googleSTTClient, bool) {
	client := &googleSTTClient{callid: callid, language: language, keywords: keywords, udpproxy: udpproxy, endpointwaits: 0, transcriptCallback: callback}
	client.setMode(mode)
	return client, true
}

/* New Call indication */
func (d *GoogleSTTProvider) NewCall(callid string, mode string, language string, commandword string, udpproxy *vproxy.IOProxy) bool {
	keywords := []string{commandword}
	client, ok := d.createClient(callid, mode, language, keywords, udpproxy, d.transcriptCB)
	if ok {
		d.mu.Lock()
		d.clientMap[callid] = client
		d.mu.Unlock()
		ok = client.start()
	}
	return ok
}

func (d *GoogleSTTProvider) terminateClient(callid string) bool {
	d.mu.Lock()
	client, ok := d.clientMap[callid]
	d.mu.Unlock()
	if ok {
		ok = client.stop()
	}
	return ok
}

/* Call end indication */
func (d *GoogleSTTProvider) EndCall(callid string) bool {
	ok := d.terminateClient(callid)
	if ok {
		d.mu.Lock()
		delete(d.clientMap, callid)
		d.mu.Unlock()
	}
	return ok
}

/* Toggle Conversation true or false */
func (d *GoogleSTTProvider) ToggleConversation(callid string, toggle bool) bool {
	d.mu.RLock()
	client, ok := d.clientMap[callid]
	d.mu.RUnlock()
	if ok {
		client.toggleConversation(toggle)
	}
	return ok
}

func (d *GoogleSTTProvider) GetConversation(callid string) bool {
	d.mu.RLock()
	client, ok := d.clientMap[callid]
	d.mu.RUnlock()
	if ok {
		return client.conversational
	}
	return false
}

/* Toggle Chat true or false */
func (d *GoogleSTTProvider) ToggleChat(callid string, toggle bool) bool {
	d.mu.RLock()
	client, ok := d.clientMap[callid]
	d.mu.RUnlock()
	if ok {
		client.toggleChat(toggle)
	}
	return ok
}

func (d *GoogleSTTProvider) GetChat(callid string) bool {
	d.mu.RLock()
	client, ok := d.clientMap[callid]
	d.mu.RUnlock()
	if ok {
		return client.chat
	}
	return false
}

/* Start listening for a command */
func (d *GoogleSTTProvider) ListenForCommand(callid string) bool {
	log.Info("STT:ListenForCommand", "CallID", callid)
	d.mu.RLock()
	client, ok := d.clientMap[callid]
	d.mu.RUnlock()
	if ok {
		client.commandListen()
	} else {
		log.Error("STT:ListenForCommand", "Error", "No client found for callid")
	}
	return ok
}

/* Cancel listening for a command */
func (d *GoogleSTTProvider) CancelCommand(callid string) bool {
	log.Info("STT:CancelCommand", "CallID", callid)
	d.mu.RLock()
	client, ok := d.clientMap[callid]
	d.mu.RUnlock()
	if ok {
		client.commandClear()
	}
	return ok
}

/* Clear all current text buffers */
func (d *GoogleSTTProvider) ClearBuffers(callid string) bool {
	d.mu.RLock()
	client, ok := d.clientMap[callid]
	d.mu.RUnlock()
	if ok {
		client.clearAllBuffers()
	}
	return ok
}
