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
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/asterisk/AsteriskVoiceBridge/vproxy"

	msginterfaces "github.com/deepgram/deepgram-go-sdk/pkg/api/speak/v1/websocket/interfaces"
	interfaces "github.com/deepgram/deepgram-go-sdk/pkg/client/interfaces/v1"
	speak "github.com/deepgram/deepgram-go-sdk/pkg/client/speak"
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
	VOICEMODEL = "aura-luna-en" //"aura-luna-en" //"aura-asteria-en" "aura-arcas-en" "aura-athena-en"(UK) "aura-helios-en" (UK)
)

type playbackCompleteCB func( /*callid*/ string /*playbackid*/, string) bool

// ttsCall ties a callid to a udpproxy, current playback, and a stopProcessing channel
type ttsCall struct {
	callid      string
	udpproxy    *vproxy.IOProxy
	client      *speak.WSCallback
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
}

func (tts *ttsCall) SetClient(client *speak.WSCallback) {
	tts.client = client
}

func (tts *ttsCall) GetClient() *speak.WSCallback {
	return tts.client
}

func (tts *ttsCall) stop() {
	// close the connection
	client := tts.GetClient()
	if client != nil {
		client.Stop()
	}
	log.Info("TTScall:Stop", "Event", "Closing Pipes")
	tts.iowr.Close()
}

// Pipe appends the audio blob to the voiceStack and starts the pipe if it is not already running
// The pipe writes each slice of the voiceStack to the udpproxy one at a time until the stack is empty
// When there are no more voice blobs to write, the pipe stops
func (tts *ttsCall) Pipe(data []byte) bool {
	log.Info("TTS:Pipe", "Event", "Data", "Length", len(data))
	waspiping := tts.piping
	tts.piping = true

	tts.voiceStack = append(tts.voiceStack, data)
	log.Debug("TTS:Pipe", "Event", "AddedBlobToStack")

	if waspiping {
		log.Debug("TTS:Pipe", "Event", "DataPipeInProgress")
		return true
	}

	go func() {
		//timestamp := time.Now().Unix()
		//f, err := os.OpenFile(fmt.Sprintf("/tmp/dg-tts-voiceblob-%d.ulaw", timestamp), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		//if err != nil {
		//log.Error("TTS:Pipe", "FileError", err)
		//}

		var modbytes []byte = nil

		for len(tts.voiceStack) > 0 {

			newslice := tts.voiceStack[0]
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
			tts.voiceStack = tts.voiceStack[1:]

			log.Debug("TTS:Pipe", "Event", "WakefileSend")
			tts.wakefile <- true
			log.Debug("TTS:Pipe", "Event", "WakefileSendComplete")

			log.Debug("TTS:Pipe", "Event", "WritingData")
			tts.iowr.Write(newslice)
			//if _, err := f.Write(newslice); err != nil {
			//log.Error("TTS:Pipe", "FileError", err)
			//}
			log.Debug("TTS:Pipe", "Event", "DataWritten")
		}
		//if err := f.Close(); err != nil {
		//log.Error("TTS:Pipe", "FileError", err)
		//}
		log.Info("TTS:Pipe", "Event", "DataWriteComplete")
		tts.piping = false
	}()

	return true
}

// Add a dead packet to the voice stack to mark the end of a set of audio blobs
// corresponding to an individual DG TTS request.
func (tts *ttsCall) AddDeadPacket() {
	log.Info("TTS:AddDeadPacket", "Event", "AddingDeadPacket")
	//bytes := make([]byte, RTP_PAYLOAD_SIZE*2)
	bytes := make([]byte, getPSize())
	bytes = vproxy.GetDeadPacket()
	//bytes = append(bytes, vproxy.GetDeadPacket()...)
	tts.Pipe(bytes)
}

func (tts *ttsCall) hasPendingText() bool {
	return len(tts.textStack) > 0
}

func (ttsCall *ttsCall) getNextTextSlice() string {
	if len(ttsCall.textStack) == 0 {
		return ""
	}
	text := ttsCall.textStack[0]
	ttsCall.textStack = ttsCall.textStack[1:]
	return text
}

func (tts *ttsCall) addTextSlice(text string) {
	tts.textStack = append(tts.textStack, text)
}

func (tts *ttsCall) clearTextStack() {
	tts.textStack = nil
}

func (tts *ttsCall) IsProcessing() bool {
	return tts.processing
}

func (tts *ttsCall) SetProcessing(processing bool) {
	tts.processing = processing
}

func (tts *ttsCall) Engaged() bool {
	return tts.engaged
}

func (tts *ttsCall) Disengage() {
	tts.engaged = false
}

func (tts *ttsCall) Engage() {
	tts.engaged = true
}

func (tts *ttsCall) SpeakText(text string) error {
	return tts.GetClient().SpeakWithText(text)
}

func (tts *ttsCall) FlushText() error {
	return tts.GetClient().Flush()
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
			tts.streaming = true
		case <-tts.deadfile:
			//if tts.streaming {
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
				err = tts.FlushText()
				if err != nil {
					log.Error("TTS:listenForPlaybackDone", "Error", err)
					tts.playbackCompleteCallBack()
				}
			} else {
				tts.streaming = false
				log.Info("TTS:listenForPlaybackDone", "Event", "TextStackEmpty")
				tts.playbackCompleteCallBack()
			}
			//}
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
	tts.voiceStack = nil
	//tts.piping = false
	tts.iore.Close()
	tts.udpproxy.StopAudio()
	tts.iowr.Close()
	tts.iore, tts.iowr = io.Pipe()
	tts.SetProcessing(true)
	go tts.readPipe()
	log.Info("TTS:ResetPipe", "Event", "Complete")
}

// ttsDeepgramCallback is a transitory object that acts as a Deepgram callback Interface for a tts session
// the callid lets us look up the ttsCall object from the TTSDeepgram object so we reference the actual
// call pointer and not a copy
type ttsDeepgramCallback struct {
	callid   string
	deepgram *TTSDeepgram
}

type TTSDeepgramCallBackHandler interface {
	HandlePlaybackComplete(callid string, playbackid string) bool
}

// TTSDeepgram is our main object.  It holds a map of ttsCalls so we can associate a callid with a ttsCall and ultimately a udpproxy
type TTSDeepgram struct {
	ttsCalls           map[string]*ttsCall
	playbackCompleteCB playbackCompleteCB
}

func (tts *TTSDeepgram) initialize() {
	tts.ttsCalls = make(map[string]*ttsCall)
}

func (tts *TTSDeepgram) SetCallbacks(cbi TTSDeepgramCallBackHandler) {
	tts.playbackCompleteCB = cbi.HandlePlaybackComplete
}

func CreateTTSDeepgram() (*TTSDeepgram, bool) {
	tts := TTSDeepgram{}
	tts.initialize()
	return &tts, true
}

func (tts *TTSDeepgram) playBackComplete(callid string, playbackid string) bool {
	if tts.playbackCompleteCB != nil {
		return tts.playbackCompleteCB(callid, playbackid)
	}
	return false
}

func (tts TTSDeepgram) NewCall(callid string, udproxy *vproxy.IOProxy) bool {
	log.Info("TTS:NewCall", "CallID", callid)
	dgapikey, exists := os.LookupEnv("DG_API_TOKEN")
	if !exists {
		log.Error("TTS:Deepgram API Token not set, can not create call!!")
		return false
	}

	ttsCall := ttsCall{callid: callid, udpproxy: udproxy, processing: false}
	ttsCall.initialize(tts.playBackComplete)
	tts.ttsCalls[callid] = &ttsCall

	// create deepgram callback
	cb := &ttsDeepgramCallback{deepgram: &tts, callid: callid}

	// Go context
	ctx := context.Background()

	// set the TTS options
	clientOpts := &interfaces.ClientOptions{
		APIKey: dgapikey,
	}

	log.Info("TTS:NewCall", "Event", "CreatingClient", "Model", VOICEMODEL, "Encoding", getEncoding(), "SampleRate", getBitRate())
	wssOpts := &interfaces.WSSpeakOptions{
		Model:      VOICEMODEL,
		Encoding:   getEncoding(),
		SampleRate: getBitRate(),
	}

	// create a new stream using the NewStream function
	dgClient, err := speak.NewWSUsingCallback(ctx, dgapikey, clientOpts, wssOpts, cb)
	if err != nil {
		fmt.Println("ERROR creating TTS connection:", err)
		return false
	}
	ttsCall.SetClient(dgClient)
	ttsCall.StartProcessing()

	// connect the websocket to Deepgram
	bConnected := dgClient.Connect()
	if !bConnected {
		fmt.Println("Client.Connect failed")
		return false
	}
	return true
}

func (tts TTSDeepgram) EndCall(callid string) bool {
	log.Info("TTS:EndCall", "CallID", callid)
	ttsCall, ok := tts.ttsCalls[callid]
	if !ok {
		return false
	}
	ttsCall.stop()
	delete(tts.ttsCalls, callid)
	return true
}

func (tts TTSDeepgram) Engage(callid string) {
	log.Info("TTS:Engage", "CallID", callid)
	ttsCall, ok := tts.ttsCalls[callid]
	if !ok {
		return
	}
	ttsCall.Engage()
}

func (tts TTSDeepgram) Disengage(callid string) {
	log.Info("TTS:Disengage", "CallID", callid)
	ttsCall, ok := tts.ttsCalls[callid]
	if !ok {
		return
	}
	ttsCall.Disengage()
}

func (tts TTSDeepgram) SendText(callid string, text string, id string, language string) {
	log.Info("TTS:SendText", "CallID", callid, "RawText", text, "ID", id, "Language", language)
	// find associated call and associated client
	call, ok := tts.ttsCalls[callid]
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

	// Flush the text input
	err = call.FlushText()
	if err != nil {
		log.Error("TTS:SendText", "ErrorOnFlush", err)
		call.currentPlaybackID = ""
		tts.playbackCompleteCB(callid, id)
		return
	}
	log.Info("TTS:SendText", "Event", "Complete")
}

func (tts TTSDeepgram) FlushResponseText(callid string) {
	log.Info("TTS:FlushResponseText", "CallID", callid)
	// we don't really care about the text being flushed with this model.  Instead we use the
	// flush marker that deepgram passes through to mark the end of the read segment to mark
	// the end of the audio stream with a deadpacket.
}

// Add Text to the current stack of text to be spoken.  If there is no current speech being processed,
// start the process.
func (tts TTSDeepgram) AddText(callid string, text string, id string, language string) {
	log.Info("TTS:AddText", "CallID", callid, "RawText", text, "ID", id, "Language", language)

	// find associated call and associated client
	call, ok := tts.ttsCalls[callid]
	if !ok {
		return
	}

	if call.hasPendingText() || call.streaming {
		// add the text to the text stack if we already have text being processed
		log.Info("TTS:AddText", "Event", "TextStacked")
		call.addTextSlice(text)
		return
	}

	call.streaming = true
	// otherwise send it to the client
	log.Info("TTS:AddText", "Event", "TextSent")
	tts.SendText(callid, text, id, language)
}

func (tts TTSDeepgram) CancelText(callid string) {
	log.Info("TTS:CancelText", "CallID", callid)

	call, ok := tts.ttsCalls[callid]
	if !ok {
		return
	}

	// clear the text stack
	call.clearTextStack()
	// reset the pipe
	call.resetPipe()
	// clear streaming flag
	call.streaming = false
}

func (tts TTSDeepgram) PauseText(callid string) {
	log.Info("TTS:PauseText", "CallID", callid)

	call, ok := tts.ttsCalls[callid]
	if !ok {
		return
	}

	call.udpproxy.PauseAudio()
}

func (tts TTSDeepgram) ResumeText(callid string) {
	log.Info("TTS:ResumeText", "CallID", callid)

	call, ok := tts.ttsCalls[callid]
	if !ok {
		return
	}

	call.udpproxy.ResumeAudio()
}

func (tts TTSDeepgram) IsStreaming(callid string) bool {
	call, ok := tts.ttsCalls[callid]
	if ok {
		return call.streaming
	}
	return false
}

func (tts ttsDeepgramCallback) Metadata(md *msginterfaces.MetadataResponse) error {
	log.Info("TTS:Metadata", "Event", "Received", "RequestID", md.RequestID, "Type", md.Type)
	return nil
}

func (tts ttsDeepgramCallback) Binary(byMsg []byte) error {
	log.Debug("TTS:Binary", "Event", "Data")
	log.Info("TTS:Binary", "Data", "Received", "Length", len(byMsg))

	callid := tts.callid
	call, ok := tts.deepgram.ttsCalls[callid]
	if !ok {
		log.Error("TTS:Binary", "Error", "Call not found", "CallID", callid)
		return nil
	}

	if call.Engaged() == false {
		log.Debug("TTS:Binary", "Event", "NotEngaged")
		return nil
	}

	// pipe the binary data to the udpproxy
	ok = call.Pipe(byMsg)

	log.Debug("TTS:Binary", "Event", "Complete")
	return nil
}

func (tts ttsDeepgramCallback) Open(or *msginterfaces.OpenResponse) error {
	log.Info("TTS:Open", "Event", "Received")
	return nil
}

// When we are done with sending a text segment to the TTS engine, we will flush the audio
// stream.  Deepgram will then invoke this callback to let us know that the last of the audio
// blob has been sent.  We use this indication to send a deadpacket to the udp proxy so it knows
// that the audio stream is complete and it shouldn't keep trying to read from the pipe.
func (tts ttsDeepgramCallback) Flush(fl *msginterfaces.FlushedResponse) error {
	log.Debug("TTS:Flush", "Event", "Received")
	callid := tts.callid
	call, ok := tts.deepgram.ttsCalls[callid]
	//_, ok := tts.deepgram.ttsCalls[callid]
	if !ok {
		log.Error("TTS:Flush", "Error", "Call not found", "CallID", callid)
		return nil
	}

	log.Info("TTS:Flush")

	// send a deadpacket to the udpproxy to signal the end of the audio stream
	call.AddDeadPacket()

	return nil
}

func (tts ttsDeepgramCallback) Warning(er *msginterfaces.WarningResponse) error {
	log.Error("TTS:Warning", "Event", "Received", "Warning", er.Description)
	return nil
}

func (tts ttsDeepgramCallback) Error(er *msginterfaces.ErrorResponse) error {
	log.Error("TTS:Error", "Event", "Received", "Error", er.Description)
	return nil
}

func (tts ttsDeepgramCallback) UnhandledEvent(byMsg []byte) error {
	log.Info("TTS:UnhandledEvent", "Event", "Received")
	return nil
}

func (tts ttsDeepgramCallback) Clear(cl *msginterfaces.ClearedResponse) error {
	log.Info("TTS:Clear", "Event", "Received")
	return nil
}

func (tts ttsDeepgramCallback) Close(cr *msginterfaces.CloseResponse) error {
	log.Info("TTS:Close", "Event", "Received")
	return nil
}
