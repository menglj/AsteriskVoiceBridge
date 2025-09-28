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
	"time"

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

// getSilentAudioPacket returns a silent audio packet for the current encoding
func getSilentAudioPacket() []byte {
	encoding := vproxy.GetRTP_MODE()
	if encoding == "l16" {
		// Linear 16-bit silence (all zeros)
		size := vproxy.GetRTP_PAYLOAD_SIZE()
		return make([]byte, size)
	} else {
		// μ-law silence (0x7F for μ-law silence)
		size := vproxy.GetRTP_PAYLOAD_SIZE()
		silentPacket := make([]byte, size)
		for i := range silentPacket {
			silentPacket[i] = 0x7F // μ-law silence value
		}
		return silentPacket
	}
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
	// pause state for TTS playback
	paused bool
	pauseMu sync.RWMutex
	// interrupt detection during TTS playback
	interruptEnabled bool
	interruptMu sync.RWMutex
	// No longer using silent mode - let Asterisk sound:longsilence handle continuous audio
}

// Pause pauses the STT stream to prevent audio timeout during TTS playback
// but keeps interrupt detection enabled
func (c *googleSTTClient) Pause() {
	c.pauseMu.Lock()
	defer c.pauseMu.Unlock()
	c.paused = true
	
	c.interruptMu.Lock()
	c.interruptEnabled = true
	c.interruptMu.Unlock()
	
	log.Info("STT:Pause", "callid", c.callid, "status", "PausedWithInterrupt")
}

// Resume resumes the STT stream after TTS playback
func (c *googleSTTClient) Resume() {
	c.pauseMu.Lock()
	defer c.pauseMu.Unlock()
	c.paused = false
	
	c.interruptMu.Lock()
	c.interruptEnabled = false
	c.interruptMu.Unlock()
	
	log.Info("STT:Resume", "callid", c.callid, "status", "Resumed")
}

// IsPaused returns whether the STT stream is currently paused
func (c *googleSTTClient) IsPaused() bool {
	c.pauseMu.RLock()
	defer c.pauseMu.RUnlock()
	return c.paused
}

// IsInterruptEnabled returns whether interrupt detection is enabled
func (c *googleSTTClient) IsInterruptEnabled() bool {
	c.interruptMu.RLock()
	defer c.interruptMu.RUnlock()
	return c.interruptEnabled
}

// No longer using silent mode - let Asterisk sound:longsilence handle continuous audio

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
					// Add audio channel configuration for better phone call handling
					AudioChannelCount: 1,
					EnableSeparateRecognitionPerChannel: false,
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
	
	// Start heartbeat to keep stream alive
	go c.heartbeat()

	return true
}

// heartbeat sends periodic empty audio packets to keep the stream alive
func (c *googleSTTClient) heartbeat() {
	// Get heartbeat interval from environment variable, default to 10 seconds
	heartbeatInterval := 10 * time.Second
	if interval := os.Getenv("GOOGLE_STT_HEARTBEAT_INTERVAL"); interval != "" {
		if duration, err := time.ParseDuration(interval); err == nil {
			heartbeatInterval = duration
		}
	}
	
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	
	log.Info("STT:heartbeat", "Event", "HeartbeatStarted", "interval", heartbeatInterval)
	
	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			if c.stream != nil && !c.IsPaused() {
				// Send heartbeat in normal mode to keep Google STT connection alive
				// This prevents audio timeout during periods of silence
				silentPacket := getSilentAudioPacket()
				req := &speechpb.StreamingRecognizeRequest{
					StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
						AudioContent: silentPacket,
					},
				}
				
				if err := c.stream.Send(req); err != nil {
					log.Error("STT:heartbeat", "Error", "Failed to send heartbeat", "error", err)
				} else {
					log.Info("STT:heartbeat", "Event", "SilentPacketSent - NormalMode")
				}
			} else if c.IsPaused() && c.IsInterruptEnabled() {
				// Send heartbeat in interrupt mode to detect user speech during TTS
				silentPacket := getSilentAudioPacket()
				req := &speechpb.StreamingRecognizeRequest{
					StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
						AudioContent: silentPacket,
					},
				}
				
				if err := c.stream.Send(req); err != nil {
					log.Error("STT:heartbeat", "Error", "Failed to send heartbeat", "error", err)
				} else {
					log.Info("STT:heartbeat", "Event", "SilentPacketSent - InterruptMode")
				}
			} else {
				log.Debug("STT:heartbeat", "Event", "Skipped - Paused")
			}
			c.mu.Unlock()
			
		case <-c.ctx.Done():
			log.Debug("STT:heartbeat", "Event", "HeartbeatStopped")
			return
		}
	}
}

func (c *googleSTTClient) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stream == nil {
		return 0, fmt.Errorf("stream not initialized")
	}

	// Skip empty data packets
	if len(data) == 0 {
		return 0, nil
	}

	// Always send real audio data to Google STT (like Deepgram approach)
	// The sound:longsilence from Asterisk will provide continuous audio stream

	// If paused but interrupt detection is enabled, still send audio for interrupt detection
	if c.IsPaused() && !c.IsInterruptEnabled() {
		log.Debug("STT:Write", "callid", c.callid, "status", "Skipped - Paused")
		return len(data), nil
	}
	
	if c.IsPaused() && c.IsInterruptEnabled() {
		log.Debug("STT:Write", "callid", c.callid, "status", "InterruptDetection")
	}

	req := &speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
			AudioContent: data,
		},
	}

	if err := c.stream.Send(req); err != nil {
		// Check for specific errors that are not fatal
		if strings.Contains(err.Error(), "Audio Timeout Error") ||
		   strings.Contains(err.Error(), "OutOfRange") ||
		   strings.Contains(err.Error(), "context deadline exceeded") {
			log.Debug("STT:Write", "Event", "NonFatalError", "error", err)
			return len(data), nil // Return success for non-fatal errors
		}
		
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
			
			// Check for specific Google Cloud errors
			if strings.Contains(err.Error(), "Audio Timeout Error") {
				log.Warn("STT:handleResponses", "Event", "AudioTimeout", "error", err)
				// Audio timeout is not fatal, continue listening
				continue
			}
			
			if strings.Contains(err.Error(), "OutOfRange") {
				log.Warn("STT:handleResponses", "Event", "OutOfRange", "error", err)
				// Out of range error, continue listening
				continue
			}
			
			if strings.Contains(err.Error(), "context deadline exceeded") {
				log.Warn("STT:handleResponses", "Event", "ContextDeadlineExceeded", "error", err)
				// Context deadline exceeded, continue listening
				continue
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

			// If paused, only process transcripts for interrupt detection
			if c.IsPaused() && c.IsInterruptEnabled() {
				// Only process final results for interrupt detection
				if isFinal && len(transcript) > 0 {
					log.Info("STT:InterruptDetection", "callid", c.callid, "text", transcript)
					// Send interrupt signal to voicebot
					if c.transcriptCallback != nil {
						c.transcriptCallback(c.callid, transcript, "interrupt")
					}
				}
				continue
			}

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

/* Pause STT for a call (during TTS playback) */
func (d *GoogleSTTProvider) PauseCall(callid string) bool {
	d.mu.RLock()
	client, exists := d.clientMap[callid]
	d.mu.RUnlock()
	
	if exists && client != nil {
		client.Pause()
		return true
	}
	return false
}

/* Resume STT for a call (after TTS playback) */
func (d *GoogleSTTProvider) ResumeCall(callid string) bool {
	d.mu.RLock()
	client, exists := d.clientMap[callid]
	d.mu.RUnlock()
	
	if exists && client != nil {
		client.Resume()
		return true
	}
	return false
}

// No longer using silent mode - let Asterisk sound:longsilence handle continuous audio

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
