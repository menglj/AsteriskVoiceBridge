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

package voiceai

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	vproxy "github.com/asterisk/AsteriskVoiceBridge/vproxy"

	"github.com/coder/websocket"
)

var log = slog.New(slog.NewTextHandler(os.Stderr, nil))

const (
	OPENAI_WS_API_URI = "wss://api.openai.com/v1/realtime?model=gpt-4o-realtime-preview-2024-10-01"

	// 160 bytes per packet = 20 ms of audio
	RTP_PAYLOAD_SIZE = 160
	RPT_PTIME_MS     = 20
	// 50 packets per second
	PACKETS_PER_SEC = 50
	// How many seconds of audio to buffer on the read side
	READ_BUFSIZE_SECONDS = 60
	// enough packets for READ_BUFSIZE_SECONDS
	READ_BUFSIZE = RTP_PAYLOAD_SIZE * PACKETS_PER_SEC * READ_BUFSIZE_SECONDS
	// How many seconds of audio to buffer on the write side
	WRITE_BUFSIZE_SECONDS = 1
	// enough packets for WRITE_BUFSIZE_SECONDS
	WRITE_BUFSIZE = RTP_PAYLOAD_SIZE * PACKETS_PER_SEC * WRITE_BUFSIZE_SECONDS
	// ulaw
	RTP_PAYLOAD_TYPE = 0
	// Silence timeout
	//SILENCE_TIMEOUT = 750 * time.Millisecond
	SILENCE_TIMEOUT = 80 * time.Millisecond

	// OpenAI Audio chunk size
	OPENAI_AUDIO_CHUNK_DUR_MS = 200
	OPENAI_AUDIO_CHUNK_SIZE   = RTP_PAYLOAD_SIZE * OPENAI_AUDIO_CHUNK_DUR_MS / RPT_PTIME_MS

	MODE_TEXT   = "text"
	MODE_AUDIO  = "audio"
	MODE_HYBRID = "hybrid"

	MAX_SENTENCE_LEN = 256
)

type playbackCompleteCB func( /*callid*/ string /*playbackid*/, string) bool

type openaiDialog struct {
	udpproxy         *vproxy.IOProxy
	reader           *bytes.Reader
	iowr             *io.PipeWriter
	iore             *io.PipeReader
	processing       bool
	streaming        bool
	voicebuffer      bytes.Buffer
	localvoicebuffer bytes.Buffer

	responseText string

	mode        string
	engaged     bool
	sendingtext bool

	tools        string
	instructions string

	signalStreamDone      chan bool
	deadfile, filerevived chan bool
	wakefile              chan bool
	playbackCompleteCB    playbackCompleteCB
}

func (sd *openaiDialog) initialize(playbackCompleteCB playbackCompleteCB, mode string) {
	sd.reader = nil
	sd.iore, sd.iowr = io.Pipe()
	sd.signalStreamDone = make(chan bool)
	sd.wakefile = make(chan bool)
	sd.filerevived = make(chan bool)
	sd.deadfile = make(chan bool)
	sd.playbackCompleteCB = playbackCompleteCB
	sd.mode = mode
	sd.engaged = false
}

func (sd openaiDialog) getMode() string {
	return sd.mode
}

func (sd openaiDialog) receiveAudioFromAI() bool {
	return sd.mode == MODE_AUDIO
}

func (sd openaiDialog) sendAudioToAI() bool {
	return sd.mode == MODE_AUDIO || sd.mode == MODE_HYBRID
}

func (sd *openaiDialog) setMode(mode string) {
	sd.mode = mode
}

func (sd *openaiDialog) addDialogAudio(byMsg []byte) {
	sd.Pipe(byMsg)
}

func (sd *openaiDialog) addDialogText(text string) {
	sd.responseText += text
}

func (sd openaiDialog) getDialogText() string {
	return sd.responseText
}

func (sd openaiDialog) isTerminated() bool {

	if len(sd.responseText) > MAX_SENTENCE_LEN {
		return true
	}

	if strings.HasSuffix(sd.responseText, "Mr.") ||
		strings.HasSuffix(sd.responseText, "Mrs.") ||
		strings.HasSuffix(sd.responseText, "Ms.") ||
		strings.HasSuffix(sd.responseText, "Dr.") ||
		strings.HasSuffix(sd.responseText, "Jr.") ||
		strings.HasSuffix(sd.responseText, "Sr.") ||
		strings.HasSuffix(sd.responseText, "Mx.") ||
		strings.HasSuffix(sd.responseText, "Mz.") {
		return false
	}

	if strings.HasSuffix(sd.responseText, ".") ||
		//strings.HasSuffix(sd.responseText, ",") ||
		strings.HasSuffix(sd.responseText, "!") ||
		strings.HasSuffix(sd.responseText, "?") ||
		strings.HasSuffix(sd.responseText, ";") ||
		strings.HasSuffix(sd.responseText, ":") {

		return true
	}

	return false
}

func (sd *openaiDialog) flushDialogText() string {
	responseText := sd.responseText
	sd.responseText = ""
	return responseText
}

func (sd *openaiDialog) addDeadPacket() {
	// send a dead packet to the proxy
	sd.Pipe(vproxy.GetDeadPacket())
}

func (sd *openaiDialog) addRemoteAudio(byMsg []byte) {
	sd.voicebuffer.Write(byMsg)
}

func (sd *openaiDialog) addLocalAudio(byMsg []byte) {
	sd.localvoicebuffer.Write(byMsg)
}

func (sd openaiDialog) isLocalBufferFull() bool {
	return sd.localvoicebuffer.Len() >= OPENAI_AUDIO_CHUNK_SIZE
}

func (sd *openaiDialog) pullLocalAudio() []byte {
	retbytes := sd.localvoicebuffer.Bytes()
	sd.localvoicebuffer.Truncate(0)
	return retbytes
}

func (sd *openaiDialog) clearDialogAudio() {
	sd.voicebuffer.Truncate(0)
	sd.localvoicebuffer.Truncate(0)
}

func (sd *openaiDialog) resetPipe(hard bool) {
	if sd.receiveAudioFromAI() {
		log.Info("OPENAIWS:ResetPipe", "Event", "Start")
		sd.iore.Close()
		if hard {
			sd.udpproxy.StopAudio()
		}
		sd.iowr.Close()
		sd.iore, sd.iowr = io.Pipe()
		go sd.readPipe()
		log.Info("OPENAIWS:ResetPipe", "Event", "Complete")
	} else {
		log.Info("OPENAIWS:ResetPipe", "Event", "NotRequired")
	}
}

func (sd *openaiDialog) Pipe(data []byte) {
	if sd.receiveAudioFromAI() {
		//go sd.iowr.Write(data)
		sd.iowr.Write(data)
	}
}

func (sd *openaiDialog) Start(writer io.Writer) {
	log.Info("OPENAIWS:Start")
	sd.processing = true
	if sd.sendAudioToAI() {
		go sd.udpproxy.StreamTo(writer)
	}
	if sd.receiveAudioFromAI() {
		go sd.readPipe()
	}
}

func (sd *openaiDialog) Engage() {
	log.Info("OPENAIWS:Engage")
	sd.engaged = true
	if sd.receiveAudioFromAI() {
		sd.udpproxy.Engage()
	}
}

func (sd *openaiDialog) Disengage() {
	log.Info("OPENAIWS:Disengage")
	sd.engaged = false
	if sd.receiveAudioFromAI() {
		sd.udpproxy.Disengage()
	}
}

func (sd openaiDialog) IsEngaged() bool {
	return sd.engaged
}

func (sd *openaiDialog) readPipe() {
	if sd.receiveAudioFromAI() {
		log.Info("OPENAIWS:readPipe", "Event", "SessionStart")
		// start pipe monitoring thread
		go sd.waitForPlaybackComplete()
		// start reading from the pipe
		sd.udpproxy.StreamFrom(sd.iore, sd.signalStreamDone, sd.wakefile, sd.filerevived, sd.deadfile)
		log.Info("OPENAIWS:readPipe", "Event", "SessionStop")
	} else {
		log.Info("OPENAIWS:readPipe", "Event", "NotRequired")
	}
}

func (sd *openaiDialog) IsStreaming() bool {
	return sd.streaming
}

func (sd *openaiDialog) waitForPlaybackComplete() {
	sd.streaming = false
	for {
		select {
		case <-sd.filerevived:
			log.Info("OPENAIWS:waitForPlaybackComplete", "Event", "PlaybackStarted")
			sd.streaming = true
		case <-sd.deadfile:
			if sd.streaming {
				log.Info("OPENAIWS:waitForPlaybackComplete", "Event", "PlaybackComplete")
				sd.streaming = false
			}
		case <-sd.signalStreamDone:
			log.Info("OPENAIWS:waitForPlaybackComplete", "Event", "StreamDone")
			sd.streaming = false
			return
		}
	}
}

func (sd *openaiDialog) Stop() {
	log.Info("OPENAIWS:Stop")
	sd.processing = false
	if sd.receiveAudioFromAI() {
		sd.iowr.Close()
	}
}

func (sd *openaiDialog) Flush() {
	log.Debug("OPENAIWS:Flush", "Error", "Deprecated")
}

type openaiDialogCompleteCallBack func(string, string, string, string, string) bool
type openaiDialogControllerCallBack func(string, string) bool

type OPENAIDialogControllerCallBackHander interface {
	HandleDialogComplete(dialogid string, funcname string, parameters string, dialog string, prompt string) bool
	HandleDialogContinue(dialogid string, prompt string) bool
	HandleDialogContinueComplete(dialogid string, prompt string) bool
	HandleDialogSpeechDetected(dialogid string, prompt string) bool
	HandleDialogError(dialogid string, err string) bool
}

type websocketDialog struct {
	Dialog                       *openaiDialog
	c                            *websocket.Conn
	wsseqnum                     int
	ctxcancel                    context.CancelFunc
	voicecounter                 int
	ready                        bool
	sessionCreated               chan bool
	dialogTextCallBack           func(string, string) bool
	dialogTextFlushCallBack      func(string, string) bool
	dialogSpeechDetectedCallBack func(string, string) bool
}

func (wd *websocketDialog) Write(data []byte) (int, error) {
	//log.Debug("OPENAIWS:Write", "AddingNBytes", len(data))
	//log.Info("OPENAIWS:Write", "AddingNBytes", len(data))

	if !wd.ready {
		// Wait till we get the session.updated event before starting to stream to audio to the bot
		log.Debug("OPENAIWS:Write", "Status", "NotReady")
		return 0, nil
	}

	wd.Dialog.addLocalAudio(data)
	if wd.Dialog.isLocalBufferFull() {
		rdBuff := wd.Dialog.pullLocalAudio()
		sessionUpdate := "{\"type\": \"input_audio_buffer.append\",\"audio\": \"" + base64.StdEncoding.EncodeToString(rdBuff) + "\"}"
		updateData := []byte(sessionUpdate)
		ctx := context.Background()
		err := wd.c.Write(ctx, websocket.MessageText, updateData)
		if err != nil {
			log.Error("OPENAIWS:Write", "ErrorSending", err)
		} else {
			log.Debug("OPENAIWS:Write", "Status", "BlockSent", "Size", len(rdBuff))
		}
	} else {
		log.Debug("OPENAIWS:Write", "Status", "Buffering", "Size", wd.Dialog.localvoicebuffer.Len())
	}

	return len(data), nil
}

func (wd *websocketDialog) WriteText(data string, level string) (int, error) {
	log.Info("OPENAIWS:WriteText", "Data", data, "Level", level)

	if !wd.ready {
		// Wait till we get the session.updated event before starting to stream to audio to the bot
		log.Info("OPENAIWS:Write", "Status", "NotReady")
		return 0, nil
	}

	sessionUpdate := ""

	if level == "conversationalai-vad-timeout" || level == "conversationalai-vad-longtimeout" {
		if !wd.Dialog.sendingtext {
			return 0, nil
		}
		// generate a response create message
		sessionUpdate = "{\"type\": \"response.create\"}"
		wd.Dialog.sendingtext = false
	} else if level == "conversationalai-vad-start" {
		return 0, nil
	} else if level == "conversationalai" {
		wd.Dialog.sendingtext = true
		sessionUpdate = "{\"type\": \"conversation.item.create\",\"item\": {\"type\": \"message\",\"role\": \"user\",\"content\": [{\"type\": \"input_text\",\"text\": \"" + data + "\"}]}}"
	} else {
		log.Error("OPENAIWS:WriteText", "Error", "InvalidTextLevel")
		return 0, nil
	}

	log.Info("OPENAIWS:WriteText", "SendingUpdate", sessionUpdate)

	updateData := []byte(sessionUpdate)
	ctx := context.Background()
	err := wd.c.Write(ctx, websocket.MessageText, updateData)
	if err != nil {
		log.Error("OPENAIWS:WriteText", "ErrorSending", err)
	}
	return len(data), nil

}

func (wd *websocketDialog) setSeqNum(seq int) {
	wd.wsseqnum = seq
}

func (wd *websocketDialog) getCurSeqNum() int {
	return wd.wsseqnum
}

func (wd *websocketDialog) getNextSeqNum() int {
	wd.wsseqnum++
	return wd.wsseqnum
}

func (wd *websocketDialog) initialize(sdc *OPENAIWebsocketDialogController, dialog *openaiDialog, dialogid string) {
	wd.sessionCreated = make(chan bool)
	wd.Dialog = dialog
	ctx, ctxcancel := context.WithCancel(context.Background())
	wd.ctxcancel = ctxcancel

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpclient := &http.Client{Transport: tr}

	authToken, ok := os.LookupEnv("OPENAI_API_TOKEN")
	if !ok {
		log.Error("OPENAIWS:Initialize", "ErrorReadingToken", "OPENAI_API_TOKEN not set")
		return
	}
	authString := "Bearer " + authToken
	headeropts := http.Header{}
	headeropts.Add("Authorization", authString)
	headeropts.Add("x-ms-client-request-id", "12345678")
	headeropts.Add("api-key", authToken)
	headeropts.Add("OpenAI-Beta", "realtime=v1")
	headeropts.Add("Content-Type", "application/json")
	headeropts.Add("accept", "application/json")

	wsoptions := &websocket.DialOptions{
		HTTPClient: httpclient,
		HTTPHeader: headeropts,
	}

	c, r, err := websocket.Dial(ctx, OPENAI_WS_API_URI, wsoptions)
	if err != nil {
		log.Error("OPENAIWS:Initialize", "ErrorDialing", err)
		wd.ctxcancel = nil
		return
	}

	log.Info("OPENAIWS:Initialize", "Connected", r.StatusCode)
	wd.ctxcancel = nil
	wd.c = c
	wd.dialogTextCallBack = sdc.dialogContinue
	wd.dialogTextFlushCallBack = sdc.dialogContinueComplete
	wd.dialogSpeechDetectedCallBack = sdc.dialogSpeechDetected
	go sdc.dialogs[dialogid].listen(sdc, dialogid)
}

func (wd *websocketDialog) created() {
	wd.sessionCreated <- true
}

func (wd *websocketDialog) terminate() {
	log.Info("OPENAIWS:terminate")
	if wd.ctxcancel != nil {
		wd.ctxcancel()
	}
	log.Info("OPENAIWS:terminate", "Status", "ClosingWebSocket")
	wd.c.Close(websocket.StatusNormalClosure, "")
	log.Info("OPENAIWS:terminate", "Status", "Terminated")

}

type OpenAIToolSet struct {
	Tools []OpenAITool `json:"tools"`
}

type OpenAITool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  OpenAIToolParam `json:"parameters"`
}

type OpenAIToolParam struct {
	Type       string                               `json:"type"`
	Properties map[string]OpenAIToolParamProperties `json:"properties"`
	Required   []string                             `json:"required"`
}

// custom marshalling for OpenAIToolParam to not include Required if it's empty
func (toolparam OpenAIToolParam) MarshalJSON() ([]byte, error) {
	type Alias OpenAIToolParam
	if len(toolparam.Required) == 0 {
		return json.Marshal(&struct {
			Required interface{} `json:"required,omitempty"`
			*Alias
		}{
			Alias: (*Alias)(&toolparam),
		})
	}
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(&toolparam),
	})
}

type OpenAIToolParamProperties struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum"`
}

func (toolset OpenAIToolSet) BuildSessionUpdate(instructions string) (string, error) {
	toolstring := ""
	for tool := range toolset.Tools {
		toolbytes, err := json.Marshal(tool)
		if err != nil {
			return "", err
		}
		toolstring += string(toolbytes)
	}

	//modalities := " \"modalities\": [\"text\", \"audio\"], "
	modalities := " \"modalities\": [\"text\"], "
	//audioSettings := "\"input_audio_format\": \"g711_ulaw\", \"output_audio_format\": \"g711_ulaw\", \"input_audio_transcription\": {\"model\": \"whisper-1\"}, \"turn_detection\": {\"threshold\": 0.4, \"silence_duration_ms\": 600, \"type\": \"server_vad\"}, "
	audioSettings := "\"input_audio_format\": \"g711_ulaw\", \"input_audio_transcription\": {\"model\": \"whisper-1\"}, \"turn_detection\": {\"threshold\": 0.4, \"silence_duration_ms\": 600, \"type\": \"server_vad\"}, "
	//sessionUpdate := "{\"type\": \"session.update\",\"session\": {\"voice\": \"alloy\", \"instructions\": \"" + instructions + "\", \"input_audio_format\": \"g711_ulaw\", \"output_audio_format\": \"g711_ulaw\", \"input_audio_transcription\": {\"model\": \"whisper-1\"}, \"turn_detection\": {\"threshold\": 0.4, \"silence_duration_ms\": 600, \"type\": \"server_vad\"}, \"tools\":[" + toolstring + "]}}"
	sessionUpdate := "{\"type\": \"session.update\",\"session\": {\"voice\": \"alloy\"," + modalities + "\"instructions\": \"" + instructions + "\", " + audioSettings + "\"tools\":[" + toolstring + "]}}"

	return sessionUpdate, nil
}

type responseDelta struct {
	Type         string `json:"type"`
	EventId      string `json:"event_id"`
	ResponseId   string `json:"response_id"`
	ItemId       string `json:"item_id"`
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	Delta        string `json:"delta"`
}

type responseTranscript struct {
	Type         string `json:"type"`
	EventId      string `json:"event_id"`
	ResponseId   string `json:"response_id"`
	ItemId       string `json:"item_id"`
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	Transcript   string `json:"transcript"`
}

type responseFunctionCall struct {
	Type         string `json:"type"`
	EventId      string `json:"event_id"`
	ResponseId   string `json:"response_id"`
	ItemId       string `json:"item_id"`
	OutputIndex  int    `json:"output_index"`
	Callid       string `json:"call_id"`
	FunctionName string `json:"name"`
	Arguments    string `json:"arguments"`
}

func (wd *websocketDialog) listen(sdc *OPENAIWebsocketDialogController, dialogid string) {
	log.Info("OPENAIWS:Listening", "DialogID", dialogid)

	ctx, ctxcancel := context.WithCancel(context.Background())
	wd.ctxcancel = ctxcancel
	initialResponseSent := false

	for {
		select {
		case <-ctx.Done():
			log.Info("OPENAIWS:ListenContextDone", "DialogID", dialogid)
			return
		case <-time.After(5 * time.Millisecond):
			mtype, msg, err := wd.c.Read(ctx)
			if err != nil {
				break
			}
			log.Debug("OPENAIWS:NewMessage", "MessageType", mtype)
			log.Debug("OPENAIWS:NewMessage", "MessageLen", len(msg))

			if mtype == websocket.MessageText && len(msg) > 0 {
				log.Debug("OPENAIWS:listen", "MessageReceived", "Text")
				log.Debug("OPENAIWS:NewMessage", "Message", string(msg))
				//unmarshal the message
				var message RealtimeRealtimeRequestCommand
				err := json.Unmarshal(msg, &message)
				if err != nil {
					log.Error("OPENAIWS:listen", "ErrorUnmarshalling", err)
				} else {
					commandType, err := message.Type.AsRealtimeRealtimeRequestCommandType1()
					if err == nil {
						if commandType == RealtimeRealtimeRequestCommandType1(SessionCreated) {
							// Session created, launch the thread to indicate the session is ready
							log.Info("OPENAIWS:listen", "CommandType", "SessionCreated")
							go wd.created()
						} else if commandType == RealtimeRealtimeRequestCommandType1(SessionUpdated) {
							// Session updated
							log.Info("OPENAIWS:listen", "CommandType", "SessionUpdated")
							wd.ready = true
							// If this is the first session update, send the initial response create
							// message to trigger the ai to start the conversation
							if !initialResponseSent {
								initialResponseSent = true
								sessionResponse := "{\"type\": \"response.create\"}"
								data := []byte(sessionResponse)
								err = wd.c.Write(ctx, websocket.MessageText, data)
								if err != nil {
									log.Error("OPENAIWS:listen", "ErrorSending", err)
								} else {
									log.Info("OPENAIWS:listen", "Status", "InitialResponseCreateSent")
								}
							}
						} else if commandType == RealtimeRealtimeRequestCommandType1(ResponseAudioDelta) {
							// Audio chunk received from the ai
							if wd.Dialog.receiveAudioFromAI() && wd.Dialog.IsEngaged() {
								// If using audio mode and the dialog is engaged, send the audio chunk to the ai
								log.Info("OPENAIWS:listen", "CommandType", "ResponseAudioDelta")
								var response responseDelta
								err := json.Unmarshal(msg, &response)
								if err != nil {
									log.Error("OPENAIWS:listen", "ErrorUnmarshalling", err)
								} else {
									log.Info("OPENAIWS:listen", "Action", "AddDialogAudio")
									// convert delta binary format to byte array
									deltastring := response.Delta
									delta, err := base64.StdEncoding.DecodeString(deltastring)
									if err != nil {
										log.Error("OPENAIWS:listen", "ErrorDecodingDelta", err)
									} else {
										// this should be re-written to use the DGTTS model
										//wd.Dialog.addDialogAudio(delta)
										//go func() {
										//	wd.Dialog.wakefile <- true
										//}()
										if !wd.Dialog.IsStreaming() {
											log.Debug("TTS:Pipe", "Event", "WakefileSend")
											wd.Dialog.wakefile <- true
											log.Debug("TTS:Pipe", "Event", "WakefileSendComplete")
										}

										log.Debug("TTS:Pipe", "Event", "WritingData")
										wd.Dialog.addDialogAudio(delta)
										log.Debug("TTS:Pipe", "Event", "DataWritten")
									}
								}
							} else {
								log.Info("OPENAIWS:listen", "CommandType", "ResponseAudioDelta", "Status", "Ignored")
							}
						} else if commandType == RealtimeRealtimeRequestCommandType1(ResponseTextDelta) {
							// Text chunk received from the ai. These come as individual words, super annoying.
							log.Debug("OPENAIWS:listen", "CommandType", "ResponseTextDelta")
							var response responseDelta
							err := json.Unmarshal(msg, &response)
							if err != nil {
								log.Error("OPENAIWS:listen", "ErrorUnmarshalling", err)
							} else if wd.Dialog.IsEngaged() {
								log.Debug("OPENAIWS:listen", "Action", "AddDialogText", "Delta", response.Delta)
								wd.Dialog.addDialogText(response.Delta)
								if wd.Dialog.isTerminated() {
									responseText := wd.Dialog.flushDialogText()
									responseText = strings.TrimSpace(responseText)
									log.Info("OPENAIWS:listen", "ResponseText", responseText)
									wd.dialogTextCallBack(dialogid, responseText)
								}
							} else {
								log.Info("OPENAIWS:listen", "Status", "NotEngaged")
							}
						} else if commandType == RealtimeRealtimeRequestCommandType1(ResponseTextDone) {
							// End of text chunk indication, flush the text buffer
							log.Info("OPENAIWS:listen", "CommandType", "ResponseTextDone")
							if wd.Dialog.IsEngaged() {
								responseText := wd.Dialog.flushDialogText()
								if responseText != "" {
									responseText = strings.TrimSpace(responseText)
									log.Info("OPENAIWS:listen", "ResponseText", responseText)
									if wd.Dialog.IsEngaged() {
										wd.dialogTextCallBack(dialogid, responseText)
									}
								}
								wd.dialogTextFlushCallBack(dialogid, "")
							} else {
								log.Info("OPENAIWS:listen", "Status", "NotEngaged")
							}
						} else if commandType == RealtimeRealtimeRequestCommandType1(ResponseCreated) {
							log.Info("OPENAIWS:listen", "CommandType", "ResponseCreated")
						} else if commandType == RealtimeRealtimeRequestCommandType1(ResponseOutputItemAdded) {
							log.Info("OPENAIWS:listen", "CommandType", "ResponseOutputItemAdded")
						} else if commandType == RealtimeRealtimeRequestCommandType1(ResponseOutputItemDone) {
							log.Info("OPENAIWS:listen", "CommandType", "ResponseOutputItemDone")
						} else if commandType == RealtimeRealtimeRequestCommandType1(ResponseContentPartAdded) {
							log.Debug("OPENAIWS:listen", "CommandType", "ResponseContentPartAdded")
						} else if commandType == RealtimeRealtimeRequestCommandType1(ResponseAudioDone) {
							// End of audio chunk indication, add a dead packet
							log.Info("OPENAIWS:listen", "CommandType", "ResponseAudioDone")
							if wd.Dialog.receiveAudioFromAI() {
								wd.Dialog.addDeadPacket()
							}
						} else if commandType == RealtimeRealtimeRequestCommandType1(InputAudioBufferSpeechStarted) {
							// VAD indication from the ai service
							log.Info("OPENAIWS:listen", "CommandType", "InputAudioBufferSpeechStarted")
							if wd.Dialog.receiveAudioFromAI() {
								// Reset the pipe to clear out any buffered audio
								wd.Dialog.resetPipe(true)
							} else if wd.Dialog.getMode() == MODE_HYBRID {
								// Reset the dialog text buffer
								_ = wd.Dialog.flushDialogText()
								log.Info("OPENAIWS:listen", "Status", "TruncatedText")
								// invoke a callback to the controller to indicate speech was detected
								wd.dialogSpeechDetectedCallBack(dialogid, "")
							}
						} else if commandType == RealtimeRealtimeRequestCommandType1(InputAudioBufferSpeechStopped) {
							// VAD end indication from the ai service
							log.Info("OPENAIWS:listen", "CommandType", "InputAudioBufferSpeechStopped")
						} else if commandType == RealtimeRealtimeRequestCommandType1(ResponseAudioTranscriptDelta) {
							log.Debug("OPENAIWS:listen", "Message", string(msg))
						} else if commandType == RealtimeRealtimeRequestCommandType1(ResponseAudioTranscriptDone) {
							// transcript of the audio chunk received from the ai, we should correlate this into
							// a presentable conversation
							var response responseTranscript
							err := json.Unmarshal(msg, &response)
							if err != nil {
								log.Error("OPENAIWS:listen", "ErrorUnmarshalling", err)
							} else {
								log.Info("OPENAIWS:listen", "ResponseAudioTranscript", response.Transcript)
							}
						} else if commandType == RealtimeRealtimeRequestCommandType1(ResponseFunctionCallArgumentsDone) {
							// What we are driving towards, the ai has indicated a chosen function call.
							// Pass this back to the controller to to process
							log.Debug("OPENAIWS:listen", "Message", string(msg))
							var response responseFunctionCall
							err := json.Unmarshal(msg, &response)
							if err != nil {
								log.Error("OPENAIWS:listen", "ErrorUnmarshalling", err)
							} else {
								log.Info("OPENAIWS:listen", "FunctionName", response.FunctionName, "Arguments", response.Arguments)
								arguments := ""
								if response.Arguments != "" && response.Arguments != "{}" {
									arguments = response.Arguments
								}
								if wd.Dialog.IsStreaming() {
									log.Info("OPENAIWS:listen", "Status", "WaitingForStreamToComplete")
									go func() {
										for wd.Dialog.IsStreaming() {
											time.Sleep(5 * time.Millisecond)
										}
										log.Info("OPENAIWS:listen", "Status", "StreamComplete")
										sdc.dialogComplete(dialogid, response.FunctionName, arguments, "", "")
									}()
								} else {
									sdc.dialogComplete(dialogid, response.FunctionName, arguments, "", "")
								}
							}
						} else if commandType == RealtimeRealtimeRequestCommandType1(ResponseContentPartDone) {
							log.Debug("OPENAIWS:listen", "Message", string(msg))
						} else if commandType == RealtimeRealtimeRequestCommandType1(ResponseOutputItemDone) {
							log.Debug("OPENAIWS:listen", "Message", string(msg))
						} else if commandType == RealtimeRealtimeRequestCommandType1(ResponseDone) {
							log.Debug("OPENAIWS:listen", "Message", string(msg))
						} else if commandType == RealtimeRealtimeRequestCommandType1(Error) {
							log.Info("OPENAIWS:listen", "CommandType", "Error")
							log.Info("OPENAIWS:listen", "Message", string(msg))
						} else {
							log.Info("OPENAIWS:listen", "CommandType", "UnHandled")
							log.Debug("OPENAIWS:listen", "Message", string(msg))
						}
					}
				}
			} else if mtype == websocket.MessageBinary {
				log.Info("OPENAIWS:listen", "MessageReceived", "Binary")
			} else {
				log.Error("OPENAIWS:listen", "MessageReceived", "Unknown")
			}

		}
	}
}

func (wd *websocketDialog) update(tools string, instructions string) {
	log.Info("OPENAIWS:listen", "CommandType", "SessionCreated")

	modalities := " "
	audioSettings := " "

	if wd.Dialog.getMode() == MODE_AUDIO {
		modalities = " \"modalities\": [\"audio\", \"text\"], "
		//audioSettings = "\"input_audio_format\": \"g711_ulaw\", \"output_audio_format\": \"g711_ulaw\", \"input_audio_transcription\": {\"model\": \"whisper-1\"}, \"turn_detection\": {\"threshold\": 0.4, \"silence_duration_ms\": 600, \"type\": \"server_vad\"}, "
		audioSettings = "\"output_audio_format\": \"g711_ulaw\", \"input_audio_format\": \"g711_ulaw\", \"input_audio_transcription\": {\"model\": \"whisper-1\"}, \"turn_detection\": {\"threshold\": 0.4, \"silence_duration_ms\": 600, \"type\": \"server_vad\"}, "
	} else if wd.Dialog.getMode() == MODE_TEXT {
		modalities = " \"modalities\": [\"text\"], "
		audioSettings = ""
	} else if wd.Dialog.getMode() == MODE_HYBRID {
		modalities = " \"modalities\": [\"text\"], "
		audioSettings = "\"input_audio_format\": \"g711_ulaw\", \"input_audio_transcription\": {\"model\": \"whisper-1\"}, \"turn_detection\": {\"threshold\": 0.4, \"silence_duration_ms\": 600, \"type\": \"server_vad\"}, "
	}

	sessionUpdate := "{\"type\": \"session.update\",\"session\": {\"voice\": \"alloy\", \"temperature\": 0.7," + modalities + "\"instructions\": \"" + instructions + "\", " + audioSettings + "\"tools\":[" + tools + "]}}"

	log.Info("OPENAIWS:listen", "SendingUpdate", sessionUpdate)
	data := []byte(sessionUpdate)
	ctx := context.Background()
	err := wd.c.Write(ctx, websocket.MessageText, data)
	if err != nil {
		log.Error("OPENAIWS:listen", "ErrorSending", err)
	} else {
		log.Info("OPENAIWS:listen", "Status", "SessionUpdateSent")
	}
}

type OPENAIWebsocketDialogController struct {
	dialogs           map[string]*websocketDialog
	defaultCapability string
	uri               string
	authString        string
	mode              string // audio, text, hybrid

	dialogComplete         openaiDialogCompleteCallBack
	dialogContinue         openaiDialogControllerCallBack
	dialogContinueComplete openaiDialogControllerCallBack
	dialogSpeechDetected   openaiDialogControllerCallBack
	dialogError            openaiDialogControllerCallBack
}

func CreateOPENAIWebsocketDialogController(defaultCapabilityFile string, mode string) (*OPENAIWebsocketDialogController, bool) {
	sdc := &OPENAIWebsocketDialogController{}
	sdc.mode = mode
	ok := sdc.initialize(defaultCapabilityFile)
	return sdc, ok
}

func (sdc *OPENAIWebsocketDialogController) initialize(defaultCapabilityFile string) bool {
	sdc.dialogs = map[string]*websocketDialog{}
	return true
}

func (sdc *OPENAIWebsocketDialogController) SetCallbacks(cbi OPENAIDialogControllerCallBackHander) {
	sdc.dialogComplete = cbi.HandleDialogComplete
	sdc.dialogContinue = cbi.HandleDialogContinue
	sdc.dialogContinueComplete = cbi.HandleDialogContinueComplete
	sdc.dialogSpeechDetected = cbi.HandleDialogSpeechDetected
	sdc.dialogError = cbi.HandleDialogError
}

// audio complete callback
func (sdc *OPENAIWebsocketDialogController) AudioComplete(dialogid string, playbackid string) bool {
	log.Info("OPENAIWS:AudioComplete", "DialogID", dialogid, "PlaybackID", playbackid)
	return true
}

// handle a new dialog indication from the voicebot
func (sdc *OPENAIWebsocketDialogController) NewDialog(dialogid string, udproxy *vproxy.IOProxy) bool {
	dw := sdc.getDialogWebsocket(dialogid)
	if dw == nil {
		// create a new dialog
		dialog := openaiDialog{udpproxy: udproxy}
		dialog.initialize(sdc.AudioComplete, sdc.mode)
		sdc.addDialog(dialogid, &dialog)
		return true
	}
	return false
}

// handle a dialog completion from the voicebot
func (sdc *OPENAIWebsocketDialogController) DialogComplete(dialogid string) bool {
	log.Info("OPENAIWS:DialogComplete", "DialogID", dialogid)
	dialog := sdc.getDialog(dialogid)
	if dialog != nil {
		sdc.removeDialog(dialogid)
		return true
	}
	return false
}

func (sdc *OPENAIWebsocketDialogController) UpdateDialog(dialogid string, tools string, instructions string) bool {
	log.Info("OPENAIWS:DialogComplete", "DialogID", dialogid)
	dialog := sdc.dialogs[dialogid]
	if dialog != nil {
		go func() {
			log.Info("OPENAIWS:UpdateDialog", "Event", "WaitingForSession")
			// wait for the dialog to be ready
			<-dialog.sessionCreated
			dialog.update(tools, instructions)
		}()
		return true
	}
	return false
}

// VAD detected
func (sdc *OPENAIWebsocketDialogController) ExternalVADStart(dialogid string, prompt string) bool {
	return true
}

func (sdc *OPENAIWebsocketDialogController) ExternalVADEnd(dialogid string, prompt string) bool {
	return true
}

func (sdc *OPENAIWebsocketDialogController) ExternalVADText(dialogid string, data string, level string) (int, error) {
	dialog := sdc.getDialogWebsocket(dialogid)
	if dialog != nil {
		log.Info("OPENAIWS:ExternalVADText", "DialogID", dialogid, "Data", data, "Level", level)
		return dialog.WriteText(data, level)
	} else {
		log.Error("OPENAIWS:ExternalVADText", "DialogID", dialogid, "Error", "DialogNotFound")
	}

	return 0, nil
}

// disengage the ai for a particular dialog
func (sdc *OPENAIWebsocketDialogController) Disengage(dialogid string) bool {
	dialog := sdc.getDialog(dialogid)
	if dialog != nil {
		dialog.Disengage()
		return true
	}
	return false
}

// engage the ai for a particular dialog
func (sdc *OPENAIWebsocketDialogController) Engage(dialogid string) bool {
	dialog := sdc.getDialog(dialogid)
	if dialog != nil {
		dialog.Engage()
		return true
	}
	return false
}

func (sdc *OPENAIWebsocketDialogController) addDialog(dialogid string, dialog *openaiDialog) {
	if sdc.dialogs[dialogid] != nil {
		return
	}
	// create a new websocket dialog
	sdc.dialogs[dialogid] = &websocketDialog{}
	sdc.dialogs[dialogid].initialize(sdc, dialog, dialogid)
	sdc.dialogs[dialogid].Dialog.Start(sdc.dialogs[dialogid])
}

func (sdc *OPENAIWebsocketDialogController) getDialog(dialogid string) *openaiDialog {
	return sdc.dialogs[dialogid].Dialog
}

func (sdc *OPENAIWebsocketDialogController) getDialogWebsocket(dialogid string) *websocketDialog {
	return sdc.dialogs[dialogid]
}

func (sdc *OPENAIWebsocketDialogController) removeDialog(dialogid string) {
	log.Info("OPENAIWS:removeDialog", "DialogID", dialogid)
	// close the websocket connection
	sdc.dialogs[dialogid].terminate()
	delete(sdc.dialogs, dialogid)
}
