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
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
	"github.com/asterisk/AsteriskVoiceBridge/vproxy"
	texttospeech "cloud.google.com/go/texttospeech/apiv1"
	"cloud.google.com/go/texttospeech/apiv1/texttospeechpb"
	"google.golang.org/api/option"
)

const (
	// How many seconds of audio to buffer
	BUFSIZE_SECONDS = 60
	// Silence timeout
	SILENCE_TIMEOUT = 750 * time.Millisecond
)

func getBufSize() int {
	// enough packets for BUFSIZE_SECONDS
	return BUFSIZE_SECONDS * vproxy.GetRTP_PAYLOAD_SIZE() * (1000 % vproxy.GetRTP_PTIME())
}

func getPSize() int {
	return vproxy.GetRTP_PAYLOAD_SIZE()
}

func getPType() int {
	return vproxy.GetRTP_PAYLOAD_TYPE()
}

const (
	// Default voice and language - can be overridden by environment variables
	DEFAULT_VOICENAME = "en-US-Neural2-J" // High quality neural voice
	DEFAULT_LANGUAGE  = "en-US"
)

// getTTSLanguage returns the TTS language from environment variable or default
func getTTSLanguage() string {
	if lang := os.Getenv("TTS_LANGUAGE"); lang != "" {
		return lang
	}
	return DEFAULT_LANGUAGE
}

// getTTSVoiceName returns the TTS voice name based on language
func getTTSVoiceName() string {
	language := getTTSLanguage()
	// https://cloud.google.com/text-to-speech/docs/list-voices-and-types?hl=zh-cn
	
	// Map languages to appropriate voice names
	switch language {
	case "cmn-CN":
		return "cmn-CN-Wavenet-B" // Chinese (Simplified) female voice
	case "cmn-TW":
		return "cmn-TW-Wavenet-A" // Chinese (Traditional) female voice
	case "ja-JP":
		return "ja-JP-Wavenet-A" // Japanese female voice
	case "ko-KR":
		return "ko-KR-Wavenet-A" // Korean female voice
	case "es-ES":
		return "es-ES-Neural2-A" // Spanish female voice
	case "fr-FR":
		return "fr-FR-Standard-F" // French female voice
	case "de-DE":
		return "de-DE-Neural2-G" // German female voice
	default:
		return DEFAULT_VOICENAME // Default English voice
	}
}

type playbackCompleteCB func( /*callid*/ string /*playbackid*/, string) bool

// ttsCall ties a callid to a udpproxy, current playback, and a stopProcessing channel
type ttsCall struct {
	callid      string
	udpproxy    *vproxy.IOProxy
	client      *texttospeech.Client
	reader      *bytes.Reader
	iowr        *io.PipeWriter
	iore        *io.PipeReader
	processing  bool
	voicebuffer []byte

	currentPlaybackID string
	textStack         []string
	voiceStack        [][]byte

	streaming             bool
	piping                bool
	engaged               bool
	signalStreamDone      chan bool
	deadfile, filerevived chan bool
	wakefile              chan bool
	playbackCompleteCB    playbackCompleteCB
	mu                    sync.Mutex
	ctx                   context.Context
	cancel                context.CancelFunc
}

func (tts *ttsCall) initialize(playbackCompleteCB playbackCompleteCB) {
	tts.reader = nil
	tts.client = nil
	tts.iore, tts.iowr = io.Pipe()
	tts.voicebuffer = make([]byte, 0, getBufSize())
	tts.voiceStack = make([][]byte, 0, getBufSize())
	tts.signalStreamDone = make(chan bool)
	tts.deadfile = make(chan bool)
	tts.filerevived = make(chan bool)
	tts.wakefile = make(chan bool)
	tts.playbackCompleteCB = playbackCompleteCB
	tts.engaged = true
	tts.ctx, tts.cancel = context.WithCancel(context.Background())
}

func (tts *ttsCall) SetClient(client *texttospeech.Client) {
	tts.mu.Lock()
	defer tts.mu.Unlock()
	tts.client = client
}

func (tts *ttsCall) GetClient() *texttospeech.Client {
	tts.mu.Lock()
	defer tts.mu.Unlock()
	return tts.client
}

func (tts *ttsCall) stop() {
	tts.mu.Lock()
	defer tts.mu.Unlock()

	// close the connection
	if tts.cancel != nil {
		tts.cancel()
	}

	if tts.client != nil {
		tts.client.Close()
	}
	log.Info("TTScall:Stop", "Event", "Closing Pipes")
	tts.iowr.Close()
}

// Pipe appends the audio blob to the voiceStack and starts the pipe if it is not already running
// The pipe writes each slice of the voiceStack to the udpproxy one at a time until the stack is empty
// When there are no more voice blobs to write, the pipe stops
func (tts *ttsCall) Pipe(data []byte) bool {
	log.Info("TTS:Pipe", "Event", "Data", "Length", len(data))
	tts.mu.Lock()
	waspiping := tts.piping
	tts.piping = true
	tts.mu.Unlock()

	tts.voiceStack = append(tts.voiceStack, data)
	log.Debug("TTS:Pipe", "Event", "AddedBlobToStack")

	if waspiping {
		log.Debug("TTS:Pipe", "Event", "DataPipeInProgress")
		return true
	}

	go func() {
		var modbytes []byte = nil

		for len(tts.voiceStack) > 0 {
			tts.mu.Lock()
			newslice := tts.voiceStack[0]
			tts.voiceStack = tts.voiceStack[1:]
			tts.mu.Unlock()

			log.Info("TTS:Pipe", "Event", "StackData", "Length", len(newslice))
			if modbytes != nil {
				log.Info("TTS:Pipe", "Event", "PrePendingModBytes", "Length", len(modbytes))
				newslice = append(modbytes, newslice...)
				modbytes = nil
				log.Info("TTS:Pipe", "Event", "NewSliceLength", "Length", len(newslice))
			}

			overlen := len(newslice) % getPSize()
			if overlen != 0 {
				modbytes = newslice[len(newslice)-overlen:]
				newslice = newslice[:len(newslice)-overlen]
				log.Info("TTS:Pipe", "Event", "CachePendingModBytes", "Length", len(modbytes))
			}
			log.Info("TTS:Pipe", "Event", "WritingData", "Length", len(newslice))

			log.Debug("TTS:Pipe", "Event", "WakefileSend")
			tts.wakefile <- true
			log.Debug("TTS:Pipe", "Event", "WakefileSendComplete")

			log.Debug("TTS:Pipe", "Event", "WritingData")
			tts.iowr.Write(newslice)
			log.Debug("TTS:Pipe", "Event", "DataWritten")
		}
		log.Info("TTS:Pipe", "Event", "DataWriteComplete")
		tts.mu.Lock()
		tts.piping = false
		tts.mu.Unlock()
	}()

	return true
}

// Add a dead packet to the voice stack to mark the end of a set of audio blobs
// corresponding to an individual Google TTS request.
func (tts *ttsCall) AddDeadPacket() {
	log.Info("TTS:AddDeadPacket", "Event", "AddingDeadPacket")
	bytes := make([]byte, getPSize())
	bytes = vproxy.GetDeadPacket()
	tts.Pipe(bytes)
}

func (tts *ttsCall) hasPendingText() bool {
	tts.mu.Lock()
	defer tts.mu.Unlock()
	return len(tts.textStack) > 0
}

func (ttsCall *ttsCall) getNextTextSlice() string {
	ttsCall.mu.Lock()
	defer ttsCall.mu.Unlock()
	if len(ttsCall.textStack) == 0 {
		return ""
	}
	text := ttsCall.textStack[0]
	ttsCall.textStack = ttsCall.textStack[1:]
	return text
}

func (tts *ttsCall) addTextSlice(text string) {
	tts.mu.Lock()
	defer tts.mu.Unlock()
	tts.textStack = append(tts.textStack, text)
}

func (tts *ttsCall) clearTextStack() {
	tts.mu.Lock()
	defer tts.mu.Unlock()
	tts.textStack = nil
}

func (tts *ttsCall) IsProcessing() bool {
	tts.mu.Lock()
	defer tts.mu.Unlock()
	return tts.processing
}

func (tts *ttsCall) SetProcessing(processing bool) {
	tts.mu.Lock()
	defer tts.mu.Unlock()
	tts.processing = processing
}

func (tts *ttsCall) Engaged() bool {
	tts.mu.Lock()
	defer tts.mu.Unlock()
	return tts.engaged
}

func (tts *ttsCall) Disengage() {
	tts.mu.Lock()
	defer tts.mu.Unlock()
	tts.engaged = false
}

func (tts *ttsCall) Engage() {
	tts.mu.Lock()
	defer tts.mu.Unlock()
	tts.engaged = true
}

func (tts *ttsCall) SpeakText(text string) error {
	client := tts.GetClient()
	if client == nil {
		return fmt.Errorf("TTS client not initialized")
	}

	// Create the synthesis request
	req := &texttospeechpb.SynthesizeSpeechRequest{
		Input: &texttospeechpb.SynthesisInput{
			InputSource: &texttospeechpb.SynthesisInput_Text{
				Text: text,
			},
		},
		Voice: &texttospeechpb.VoiceSelectionParams{
			LanguageCode: getTTSLanguage(),
			Name:         getTTSVoiceName(),
		},
		AudioConfig: &texttospeechpb.AudioConfig{
			AudioEncoding: texttospeechpb.AudioEncoding_MULAW,
			SampleRateHertz: int32(vproxy.GetRTP_BITRATE()),
		},
	}

	// Perform the synthesis
	resp, err := client.SynthesizeSpeech(tts.ctx, req)
	if err != nil {
		return fmt.Errorf("failed to synthesize speech: %v", err)
	}

	// Pipe the audio data
	tts.Pipe(resp.AudioContent)

	return nil
}

func (tts *ttsCall) FlushText() error {
	// For Google TTS, we don't need to flush as each request is independent
	return nil
}

func (tts *ttsCall) StartProcessing() {
	log.Info("TTS:StartProcessing", "Event", "Start")
	if tts.IsProcessing() {
		log.Info("TTS:StartProcessing", "Event", "AlreadyProcessing")
		return
	}

	tts.SetProcessing(true)
	go tts.readPipe()
	log.Info("TTS:StartProcessing", "Event", "Complete")
}

func (tts *ttsCall) readPipe() {
	log.Info("TTS:Call", "Event", "SessionStart")
	// start listening for playback done
	go tts.listenForPlaybackDone()
	tts.udpproxy.StreamFrom(tts.iore, tts.signalStreamDone, tts.wakefile, tts.filerevived, tts.deadfile)
	log.Info("TTS:Call", "Event", "SessionEnd")
}

// listenForPlaybackDone listens for the readwait and readdone signals to determine when there
// is a sufficient gap in read bytes to indicate that the playback is complete
func (tts *ttsCall) listenForPlaybackDone() {
	for {
		select {
		case <-tts.filerevived:
			log.Info("TTS:listenForPlaybackDone", "Event", "PlaybackStarted")
			tts.mu.Lock()
			tts.streaming = true
			tts.mu.Unlock()
		case <-tts.deadfile:
			log.Info("TTS:listenForPlaybackDone", "Event", "PlaybackStopped")
			// check the text stack
			text := tts.getNextTextSlice()
			if text != "" {
				log.Info("TTS:listenForPlaybackDone", "Event", "NextText", "Text", text)
				err := tts.SpeakText(text)
				if err != nil {
					log.Error("TTS:listenForPlaybackDone", "Error", err)
					tts.playbackCompleteCallBack()
				}
			} else {
				tts.mu.Lock()
				tts.streaming = false
				tts.mu.Unlock()
				log.Info("TTS:listenForPlaybackDone", "Event", "TextStackEmpty")
				tts.playbackCompleteCallBack()
			}
		case <-tts.signalStreamDone:
			log.Info("TTS:listenForPlaybackDone", "Event", "StreamDone")
			return
		}
	}
}

func (tts *ttsCall) playbackCompleteCallBack() {
	if tts.playbackCompleteCB != nil && tts.currentPlaybackID != "" {
		tts.playbackCompleteCB(tts.callid, tts.currentPlaybackID)
		tts.currentPlaybackID = ""
	}
}

func (tts *ttsCall) resetPipe() {
	log.Info("TTS:ResetPipe", "Event", "Start")
	tts.SetProcessing(false)
	tts.mu.Lock()
	tts.voiceStack = nil
	tts.mu.Unlock()
	tts.iore.Close()
	tts.udpproxy.StopAudio()
	tts.iowr.Close()
	tts.iore, tts.iowr = io.Pipe()
	tts.SetProcessing(true)
	go tts.readPipe()
	log.Info("TTS:ResetPipe", "Event", "Complete")
}

type TTSGoogleCallBackHandler interface {
	HandlePlaybackComplete(callid string, playbackid string) bool
}

// TTSGoogle is our main object.  It holds a map of ttsCalls so we can associate a callid with a ttsCall and ultimately a udpproxy
type TTSGoogle struct {
	ttsCalls           map[string]*ttsCall
	playbackCompleteCB playbackCompleteCB
	mu                 sync.RWMutex
}

func (tts *TTSGoogle) initialize() {
	tts.ttsCalls = make(map[string]*ttsCall)
}

func (tts *TTSGoogle) SetCallbacks(cbi TTSGoogleCallBackHandler) {
	tts.playbackCompleteCB = cbi.HandlePlaybackComplete
}

func CreateTTSGoogle() (*TTSGoogle, bool) {
	tts := TTSGoogle{}
	tts.initialize()
	return &tts, true
}

func (tts *TTSGoogle) playBackComplete(callid string, playbackid string) bool {
	if tts.playbackCompleteCB != nil {
		return tts.playbackCompleteCB(callid, playbackid)
	}
	return false
}

func (tts TTSGoogle) NewCall(callid string, udproxy *vproxy.IOProxy) bool {
	log.Info("TTS:NewCall", "CallID", callid)
	
	// Initialize Google Text-to-Speech client
	ctx := context.Background()
	client, err := texttospeech.NewClient(ctx, option.WithCredentialsFile(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")))
	if err != nil {
		log.Error("TTS:Google API Token not set, can not create call!!", "error", err)
		return false
	}

	ttsCall := ttsCall{callid: callid, udpproxy: udproxy, processing: false}
	ttsCall.initialize(tts.playBackComplete)
	ttsCall.SetClient(client)
	
	tts.mu.Lock()
	tts.ttsCalls[callid] = &ttsCall
	tts.mu.Unlock()
	
	ttsCall.StartProcessing()
	return true
}

func (tts TTSGoogle) EndCall(callid string) bool {
	log.Info("TTS:EndCall", "CallID", callid)
	tts.mu.Lock()
	ttsCall, ok := tts.ttsCalls[callid]
	tts.mu.Unlock()
	if !ok {
		return false
	}
	ttsCall.stop()
	tts.mu.Lock()
	delete(tts.ttsCalls, callid)
	tts.mu.Unlock()
	return true
}

func (tts TTSGoogle) Engage(callid string) {
	log.Info("TTS:Engage", "CallID", callid)
	tts.mu.RLock()
	ttsCall, ok := tts.ttsCalls[callid]
	tts.mu.RUnlock()
	if !ok {
		return
	}
	ttsCall.Engage()
}

func (tts TTSGoogle) Disengage(callid string) {
	log.Info("TTS:Disengage", "CallID", callid)
	tts.mu.RLock()
	ttsCall, ok := tts.ttsCalls[callid]
	tts.mu.RUnlock()
	if !ok {
		return
	}
	ttsCall.Disengage()
}

func (tts TTSGoogle) SendText(callid string, text string, id string, language string) {
	log.Info("TTS:SendText", "CallID", callid, "RawText", text, "ID", id, "Language", language)
	// find associated call and associated client
	tts.mu.RLock()
	call, ok := tts.ttsCalls[callid]
	tts.mu.RUnlock()
	if !ok {
		return
	}

	call.currentPlaybackID = id
	err := call.SpeakText(text)
	if err != nil {
		log.Error("TTS:SendText", "ErrorOnSend", err)
		call.currentPlaybackID = ""
		tts.playbackCompleteCB(callid, id)
		return
	}

	log.Info("TTS:SendText", "Event", "Complete")
}

func (tts TTSGoogle) FlushResponseText(callid string) {
	log.Info("TTS:FlushResponseText", "CallID", callid)
	// we don't really care about the text being flushed with this model.  Instead we use the
	// flush marker that google passes through to mark the end of the read segment to mark
	// the end of the audio stream with a deadpacket.
}

// Add Text to the current stack of text to be spoken.  If there is no current speech being processed,
// start the process.
func (tts TTSGoogle) AddText(callid string, text string, id string, language string) {
	log.Info("TTS:AddText", "CallID", callid, "RawText", text, "ID", id, "Language", language)

	// find associated call and associated client
	tts.mu.RLock()
	call, ok := tts.ttsCalls[callid]
	tts.mu.RUnlock()
	if !ok {
		return
	}

	if call.hasPendingText() || call.streaming {
		// add the text to the text stack if we already have text being processed
		log.Info("TTS:AddText", "Event", "TextStacked")
		call.addTextSlice(text)
		return
	}

	call.mu.Lock()
	call.streaming = true
	call.mu.Unlock()
	// otherwise send it to the client
	log.Info("TTS:AddText", "Event", "TextSent")
	tts.SendText(callid, text, id, language)
}

func (tts TTSGoogle) CancelText(callid string) {
	log.Info("TTS:CancelText", "CallID", callid)

	tts.mu.RLock()
	call, ok := tts.ttsCalls[callid]
	tts.mu.RUnlock()
	if !ok {
		return
	}

	// clear the text stack
	call.clearTextStack()
	// reset the pipe
	call.resetPipe()
	// clear streaming flag
	call.mu.Lock()
	call.streaming = false
	call.mu.Unlock()
}

func (tts TTSGoogle) PauseText(callid string) {
	log.Info("TTS:PauseText", "CallID", callid)

	tts.mu.RLock()
	call, ok := tts.ttsCalls[callid]
	tts.mu.RUnlock()
	if !ok {
		return
	}

	call.udpproxy.PauseAudio()
}

func (tts TTSGoogle) ResumeText(callid string) {
	log.Info("TTS:ResumeText", "CallID", callid)

	tts.mu.RLock()
	call, ok := tts.ttsCalls[callid]
	tts.mu.RUnlock()
	if !ok {
		return
	}

	call.udpproxy.ResumeAudio()
}

func (tts TTSGoogle) IsStreaming(callid string) bool {
	tts.mu.RLock()
	call, ok := tts.ttsCalls[callid]
	tts.mu.RUnlock()
	if ok {
		return call.streaming
	}
	return false
}
