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

package voicebot

import (
	"encoding/json"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/asterisk/AsteriskVoiceBridge/ariman"
	"github.com/asterisk/AsteriskVoiceBridge/deepgram"
	"github.com/asterisk/AsteriskVoiceBridge/google"
	"github.com/asterisk/AsteriskVoiceBridge/rclocal"
	"github.com/asterisk/AsteriskVoiceBridge/vproxy"
)

const (
	// How many seconds of audio to buffer
	BUFSIZE_SECONDS = 60
	// How many seconds to wait for the user to input enough a text segment for the ai
	WAITFORUSER_SECONDS = 10
	// How many seconds to wait for the ai to respond
	WAITFORAI_SECONDS = 15
	// How many seconds to wait after the ai has responded
	WAITAFTERAI_SECONDS = 65
	// operating mode
	OPMODE = "text" // "hybrid", "text", "audio"
)

var log = slog.New(slog.NewTextHandler(os.Stderr, nil))

/********** voicebotCall ******************************************/
type voicebotCall struct {
	commandWord string
	info        ariman.CallInfo

	udpProxy *vproxy.IOProxy

	listen, ismuted, closing bool

	delayedCommands []chan bool
}

func (vbc *voicebotCall) initialize() {
	vbc.listen = false
	vbc.ismuted = false
	vbc.closing = false
	vbc.delayedCommands = make([]chan bool, 0)
}

func newVoiceBotCall(ci ariman.CallInfo, commandWord string) (voicebotCall, bool) {
	call := voicebotCall{info: ci, commandWord: commandWord}
	call.initialize()
	return call, true
}

func (vbc *voicebotCall) addDelayedCommand(cncl chan bool) {
	vbc.delayedCommands = append(vbc.delayedCommands, cncl)
}

func (vbc *voicebotCall) removeDelayedCommand(cncl chan bool) {
	for i, c := range vbc.delayedCommands {
		if c == cncl {
			vbc.delayedCommands = append(vbc.delayedCommands[:i], vbc.delayedCommands[i+1:]...)
			break
		}
	}
}

func (vbc *voicebotCall) cancelDelayedCommands() {
	for _, cncl := range vbc.delayedCommands {
		log.Info("BOT:cancelDelayedCommands", "callid", vbc.info.CallID, "status", "Cancelling Delayed Command")

		go func() {
			cncl <- true
			vbc.removeDelayedCommand(cncl)
		}()
	}
}

func (vbc *voicebotCall) setUDPProxy(proxy *vproxy.IOProxy) {
	vbc.udpProxy = proxy
}

func (vbc *voicebotCall) getUDPProxy() *vproxy.IOProxy {
	return vbc.udpProxy
}

func (vbc *voicebotCall) getID() string {
	return vbc.info.CallID
}

func (vbc *voicebotCall) getDialKey() string {
	return vbc.info.AppKey
}

func (vbc *voicebotCall) setLocalListen(toggle bool) {
	vbc.listen = toggle
}

func (vbc *voicebotCall) getLocalListen() bool {
	return vbc.listen
}

func (vbc *voicebotCall) mute() {
	vbc.ismuted = true
}

func (vbc *voicebotCall) unmute() {
	vbc.ismuted = false
}

func (vbc *voicebotCall) muted() bool {
	return vbc.ismuted
}

func (vbc *voicebotCall) setClosing() {
	vbc.closing = true
}

func (vbc *voicebotCall) isClosing() bool {
	return vbc.closing
}

/* commands in the map need to take this format */
// type vbCommand func(VoiceBot, string, string) bool

/* Dummy command function to map to a reserved but unused command. */
func commandDummy(v *VoiceBot, callid string, parameters string) bool {
	// v.doathing()
	return true
}

type commandDialPlanParams struct {
	Scheme   string `json:"scheme"`
	UserInfo string `json:"userinfo"`
	Host     string `json:"host"`
	Port     string `json:"port"`
}

func CommandDialPlan(v *VoiceBot, callid string, parameters string) bool {
	log.Info("BOT:CommandDialPlan", "callid", callid, "parameters", parameters)

	var decodedParams commandDialPlanParams
	// Unmarshal the JSON string into the map
	err := json.Unmarshal([]byte(parameters), &decodedParams)
	if err != nil {
		log.Error("BOT:CommandDialPlan", "callid", callid, "error", "Failed to unmarshal parameters")
		return false
	}

	if decodedParams.UserInfo == "" {
		log.Error("BOT:CommandDialPlan", "callid", callid, "error", "UserInfo not found")
		return false
	}

	if decodedParams.Host == "" {
		log.Error("BOT:CommandDialPlan", "callid", callid, "error", "Host not found")
		return false
	}

	var uri string
	if decodedParams.Scheme == "sip" {
		uri = "PJSIP/gobot-ob-proxy/" + decodedParams.Scheme + ":" + decodedParams.UserInfo + "@" + decodedParams.Host
		if decodedParams.Port != "" {
			uri += ":" + decodedParams.Port
		}
	} else if decodedParams.Scheme != "" {
		uri = "LOCAL/" + decodedParams.Scheme + ":" + decodedParams.UserInfo + "@" + decodedParams.Host
	} else {
		uri = "LOCAL/" + decodedParams.UserInfo + "@" + decodedParams.Host
	}

	log.Info("BOT:CommandDialPlan", "callid", callid, "AddingURI", uri)

	v.AddURItoCall(callid, uri)

	return true
}

func CommandDialPlanTransfer(v *VoiceBot, callid string, parameters string) bool {
	log.Info("BOT:CommandDialPlanTransfer", "callid", callid, "parameters", parameters)

	var decodedParams commandDialPlanParams
	// Unmarshal the JSON string into the map
	err := json.Unmarshal([]byte(parameters), &decodedParams)
	if err != nil {
		log.Error("BOT:CommandDialPlanTransfer", "callid", callid, "error", "Failed to unmarshal parameters")
		return false
	}

	if decodedParams.UserInfo == "" {
		log.Error("BOT:CommandDialPlanTransfer", "callid", callid, "error", "UserInfo not found")
		return false
	}

	if decodedParams.Host == "" {
		log.Error("BOT:CommCommandDialPlanTransferandDialPlan", "callid", callid, "error", "Host not found")
		return false
	}

	var uri string

	uri = "sip:" + decodedParams.UserInfo + "@" + decodedParams.Host
	if decodedParams.Port != "" {
		uri += ":" + decodedParams.Port
	}

	log.Info("BOT:CommandDialPlanTransfer", "callid", callid, "ContinuingToURI", uri)

	v.SendCalltoURI(callid, uri)

	return true
}

type commandDialPlanContinueParams struct {
	Context   string `json:"context"`
	Extension string `json:"extension"`
	Priority  string `json:"priority"`
}

func CommandDialPlanContinue(v *VoiceBot, callid string, parameters string) bool {
	log.Info("BOT:CommandDialPlan", "callid", callid, "parameters", parameters)

	var decodedParams commandDialPlanContinueParams
	// Unmarshal the JSON string into the map
	err := json.Unmarshal([]byte(parameters), &decodedParams)
	if err != nil {
		log.Error("BOT:CommandDialPlanContinue", "callid", callid, "error", "Failed to unmarshal parameters")
		return false
	}

	if decodedParams.Context == "" {
		log.Error("BOT:CommandDialPlanContinue", "callid", callid, "error", "Context not found")
		return false
	}

	if decodedParams.Extension == "" {
		log.Error("BOT:CommandDialPlanContinue", "callid", callid, "error", "Extension not found")
		return false
	}

	if decodedParams.Priority == "0" {
		decodedParams.Priority = "1"
	}

	priority, err := strconv.Atoi(decodedParams.Priority)
	if err != nil {
		log.Error("BOT:CommandDialPlanContinue", "callid", callid, "error", "Failed to convert priority to int")
		return false
	}

	var uri string

	uri = decodedParams.Extension + "@" + decodedParams.Context + "," + decodedParams.Priority

	log.Info("BOT:CommandDialPlanContinue", "callid", callid, "ContinuingToURI", uri)

	v.ContinueCall(callid, decodedParams.Context, decodedParams.Extension, priority)

	return true
}

type commandFilePlayParams struct {
	Language string `json:"language"`
	File     string `json:"file"`
}

func CommandEngageAI(v *VoiceBot, callid string, parameters string) bool {
	log.Info("BOT:CommandEngageAI", "callid", callid, "parameters", parameters)
	// Translation is always engaged when STT is active
	useGoogle := os.Getenv("USE_GOOGLE_STT_TTS") == "true"
	if useGoogle {
		if v.googleTTSProvider != nil {
			v.googleTTSProvider.Engage(callid)
		}
	} else {
		if v.ttsprovider != nil {
			v.ttsprovider.Engage(callid)
		}
	}
	return true
}

func CommandDisengageAI(v *VoiceBot, callid string, parameters string) bool {
	log.Info("BOT:CommandDisengageAI", "callid", callid, "parameters", parameters)
	// Translation is always engaged when STT is active
	useGoogle := os.Getenv("USE_GOOGLE_STT_TTS") == "true"
	if useGoogle {
		if v.googleTTSProvider != nil {
			v.googleTTSProvider.Disengage(callid)
		}
	} else {
		if v.ttsprovider != nil {
			v.ttsprovider.Disengage(callid)
		}
	}
	v.CancelText(callid)
	return true
}

func CommandFilePlay(v *VoiceBot, callid string, parameters string) bool {
	log.Info("BOT:CommandFilePlay", "callid", callid, "parameters", parameters)

	var decodedParams commandFilePlayParams
	// Unmarshal the JSON string into the map
	err := json.Unmarshal([]byte(parameters), &decodedParams)
	if err != nil {
		log.Error("BOT:CommandFilePlay", "callid", callid, "error", "Failed to unmarshal parameters")
		return false
	}

	if decodedParams.File == "" {
		log.Error("BOT:CommandFilePlay", "callid", callid, "error", "File not found")
		return false
	}

	v.PlayFile(callid, decodedParams.File, decodedParams.Language)

	return true
}

func CommandFuncClear(v *VoiceBot, callid string, parameters string) bool {
	log.Info("BOT:CommandFuncClear", "callid", callid, "parameters", parameters)
	//v.chatController.ClearDynamicCapability(callid)
	return true
}

type commandReadTextParams struct {
	Text string `json:"text"`
}

func CommandHangupCall(v *VoiceBot, callid string, parameters string) bool {
	log.Info("BOT:CommandHangupCall", "callid", callid, "parameters", parameters)
	v.HangupCall(callid)
	return true
}

func CommandClearBuffers(v *VoiceBot, callid string, parameters string) bool {
	log.Info("BOT:CommandClearBuffers", "callid", callid, "parameters", parameters)
	_, ok := v.getCall(callid)
	if !ok {
		log.Error("BOT:CommandClearBuffers", "callid", callid, "error", "Call not found")
		return false
	}

	return true
}

/* End of command functions */

/********** VoiceBot **********************************************/
type VoiceBot struct {
	commandWord     string
	defaultmode     string
	defaultlanguage string
	callMap         map[string]*voicebotCall
	commandMap      map[string]func(*VoiceBot, string, string) bool

	ariController *ariman.Connector

	// Deepgram providers (original)
	ttsprovider *deepgram.TTSDeepgram
	sttprovider *deepgram.DeepgramSTTProvider

	// Google providers (new)
	googleTTSProvider *google.TTSGoogle
	googleSTTProvider *google.GoogleSTTProvider

	remoteCommander   *rclocal.StudioCommander     //RC
	translateProvider *google.GoogleTranslateProvider // Translation provider
}

func CreateVoiceBot(commandWord string, defaultmode string, defaultlanguage string) (*VoiceBot, bool) {

	vb := VoiceBot{commandWord: commandWord, defaultmode: defaultmode, defaultlanguage: defaultlanguage}
	vb.initializeCommands()

	vb.callMap = make(map[string]*voicebotCall)

	http_url := "http://" + ariman.AST_ADD + ":8088/ari"
	ws_url := "ws://" + ariman.AST_ADD + ":8088/ari/events"

	aricontroller, ok := ariman.CreateConnector("voicebot", "asterisk", "asterisk", http_url, ws_url)
	if ok {
		vb.ariController = aricontroller
	} else {
		return nil, false
	}

	commander, ok := rclocal.CreateStudioCommander()
	if ok {
		vb.remoteCommander = commander
	} else {
		return nil, false
	}

	// Check which STT/TTS provider to use
	useGoogle := os.Getenv("USE_GOOGLE_STT_TTS") == "true"

	if OPMODE == "text" {
		if useGoogle {
			sttprovider, ok := google.CreateProvider("ulaw")
			if ok {
				vb.googleSTTProvider = sttprovider
			} else {
				return nil, false
			}
		} else {
			sttprovider, ok := deepgram.CreateProvider("ulaw")
			if ok {
				vb.sttprovider = sttprovider
			} else {
				return nil, false
			}
		}
	}

	if OPMODE == "text" || OPMODE == "hybrid" {
		if useGoogle {
			ttsprovider, ok := google.CreateTTSGoogle()
			if ok {
				vb.googleTTSProvider = ttsprovider
			} else {
				return nil, false
			}
		} else {
			ttsprovider, ok := deepgram.CreateTTSDeepgram()
			if ok {
				vb.ttsprovider = ttsprovider
			} else {
				return nil, false
			}
		}
	}

	// Create Google Translate provider
	translateProvider, ok := google.NewGoogleTranslateProvider()
	if ok {
		vb.translateProvider = translateProvider
		// Set translation callback
		vb.translateProvider.SetTranslationCallback(vb.HandleTranslationResults)
	} else {
		log.Error("Failed to create Google Translate provider")
		return nil, false
	}

	// set the callbacks once we have created all providers
	vb.ariController.SetCallbacks(vb)

	if OPMODE == "text" {
		if useGoogle {
			vb.googleSTTProvider.SetCallbacks(vb)
		} else {
			vb.sttprovider.SetCallbacks(vb)
		}
	}
	if OPMODE == "text" || OPMODE == "hybrid" {
		if useGoogle {
			vb.googleTTSProvider.SetCallbacks(vb)
		} else {
			vb.ttsprovider.SetCallbacks(vb)
		}
	}
	vb.remoteCommander.SetCallBacks(vb)

	return &vb, true
}

func (v *VoiceBot) GoBotGo() {
	v.ariController.Connect()
}

// Helper functions to get the correct STT/TTS provider
func (v *VoiceBot) getSTTProvider() interface{} {
	useGoogle := os.Getenv("USE_GOOGLE_STT_TTS") == "true"
	if useGoogle {
		return v.googleSTTProvider
	}
	return v.sttprovider
}

func (v *VoiceBot) getTTSProvider() interface{} {
	useGoogle := os.Getenv("USE_GOOGLE_STT_TTS") == "true"
	if useGoogle {
		return v.googleTTSProvider
	}
	return v.ttsprovider
}

func (v *VoiceBot) initializeCommands() {
	v.commandMap = map[string]func(*VoiceBot, string, string) bool{
		"engage_ai":          CommandEngageAI,
		"disengage_ai":       CommandDisengageAI,
		"recording_enable":   commandDummy,
		"recording_disable":  commandDummy,
		"play_file":          CommandFilePlay,
		"mute_enable":        commandDummy,
		"mute_disable":       commandDummy,
		"on_hold_enable":     commandDummy,
		"on_hold_disable":    commandDummy,
		"hang_up_call":       CommandHangupCall,
		"detach_call":        commandDummy,
		"dial_number":        commandDummy,
		"dial_plan":          CommandDialPlan,
		"dial_plan_transfer": CommandDialPlanTransfer,
		"dial_plan_continue": CommandDialPlanContinue,
		"call_contact":       commandDummy,
		"set_status":         commandDummy,
		"clear_functions":    CommandFuncClear,
	}
}

func VBDoCommand(v VoiceBot, callid string, command string, parameters string) bool {
	return v.DoCommand(callid, command, parameters)
}

func (v VoiceBot) ProcessLocal(callid string, text string) bool {
	// capability not currentl available
	//v.processTextLocally(callid, text, "passive")
	return true
}

func (v *VoiceBot) DoCommand(callid string, command string, parameters string) bool {
	commandfunc, ok := v.getCommand(command)
	if ok {
		ok = commandfunc(v, callid, parameters)
	}
	return ok
}

func (v *VoiceBot) addDelayedCommand(callid string, cncl chan bool) {
	call, ok := v.getCall(callid)
	if ok {
		call.addDelayedCommand(cncl)
	}
}

func (v *VoiceBot) removeDelayedCommand(callid string, cncl chan bool) {
	call, ok := v.getCall(callid)
	if ok {
		call.removeDelayedCommand(cncl)
	}
}

func (v *VoiceBot) cancelDelayedCommands(callid string) {
	call, ok := v.getCall(callid)
	if ok {
		call.cancelDelayedCommands()
	}
}

func (v *VoiceBot) AddURItoCall(callid string, uri string) bool {
	_, ok := v.getCall(callid)
	if !ok {
		log.Error("BOT:AddURItoCall", "callid", callid, "error", "Call not found")
		return false
	}

	ok = v.ariController.AddURItoCall(callid, uri)
	if !ok {
		log.Error("BOT:AddURItoCall", "callid", callid, "error", "Failed to add URI to call")
		return false
	}

	return true
}

func (v *VoiceBot) RemoveURIfromCall(callid string, uri string) bool {
	return true
}

func (v *VoiceBot) SendCalltoURI(callid string, uri string) bool {
	_, ok := v.getCall(callid)
	if !ok {
		log.Error("BOT:SendCalltoURI", "callid", callid, "error", "Call not found")
		return false
	}

	ok = v.ariController.SendCalltoURI(callid, uri)
	if !ok {
		log.Error("BOT:SendCalltoURI", "callid", callid, "error", "Failed to send call to URI")
		return false
	}

	return true
}

func (v *VoiceBot) ContinueCall(callid string, context string, extension string, priority int) bool {
	_, ok := v.getCall(callid)
	if !ok {
		log.Error("BOT:ContinueCall", "callid", callid, "error", "Call not found")
		return false
	}

	ok = v.ariController.ContinueCall(callid, context, extension, priority)
	if !ok {
		log.Error("BOT:ContinueCall", "callid", callid, "error", "Failed to continue call")
		return false
	}

	return true
}

func (v *VoiceBot) PlayFile(callid string, file string, language string) bool {
	_, ok := v.getCall(callid)
	if !ok {
		log.Error("BOT:PlayFile", "callid", callid, "error", "Call not found")
		return false
	}

	ok = v.ariController.PlayFile(callid, file, language)
	if !ok {
		log.Error("BOT:PlayFile", "callid", callid, "error", "Failed to play file")
		return false
	}

	return true
}

func (v *VoiceBot) getCommand(command string) (func(*VoiceBot, string, string) bool, bool) {
	elem, ok := v.commandMap[command]
	if ok {
		return elem, true
	}
	return nil, false
}

func (v VoiceBot) addCall(call *voicebotCall) {
	v.callMap[call.getID()] = call
}

func (v VoiceBot) getCall(callid string) (*voicebotCall, bool) {
	elem, ok := v.callMap[callid]
	if ok {
		return elem, true
	}
	return elem, false
}

func (v VoiceBot) terminateCall(callid string) {
	log.Info("BOT:terminateCall", "callid", callid)

	call, ok := v.getCall(callid)
	if !ok {
		log.Error("BOT:terminateCall", "callid", callid, "error", "Call not found")
	}

	call.setClosing()
	v.cancelDelayedCommands(callid)

	// Translation doesn't need dialog completion
	log.Info("BOT:terminateCall", "Status", "TranslationComplete")

	useGoogle := os.Getenv("USE_GOOGLE_STT_TTS") == "true"

	if OPMODE == "text" {
		var sttend bool
		if useGoogle {
			sttend = v.googleSTTProvider.EndCall(callid)
		} else {
			sttend = v.sttprovider.EndCall(callid)
		}
		if sttend {
			log.Info("BOT:terminateCall", "Status", "STTStopSuccess")
		} else {
			log.Info("BOT:terminateCall", "Status", "STTStopFailed")
		}
	}

	if OPMODE == "text" || OPMODE == "hybrid" {
		var ttsend bool
		if useGoogle {
			ttsend = v.googleTTSProvider.EndCall(callid)
		} else {
			ttsend = v.ttsprovider.EndCall(callid)
		}
		if ttsend {
			log.Info("BOT:terminateCall", "Status", "TTSStopSuccess")
		} else {
			log.Info("BOT:terminateCall", "Status", "TTSStopFailed")
		}
	}

	log.Info("BOT:terminateCall", "callid", callid, "status", "Complete")
}

func (v *VoiceBot) removeCall(callid string) {
	delete(v.callMap, callid)
}

func HandleCallEvent(voicebot VoiceBot, channelid string, event string) {
	voicebot.handleCallEvent(channelid, event)
}

func (v VoiceBot) handleCallEvent(channelid string, event string) {
	return
}

func (v VoiceBot) HandleNewCall(ci ariman.CallInfo) bool {

	log.Info("BOT:HandleNewCall", "callid", ci.CallID)
	log.Info("BOT:HandleNewCall", "appkey", ci.AppKey)
	log.Info("BOT:HandleNewCall", "src_media_address", ci.SrcMediaAddress)
	log.Info("BOT:HandleNewCall", "src_media_port", strconv.Itoa(ci.SrcMediaPort))
	log.Info("BOT:HandleNewCall", "dest_media_address", ci.DestMediaAddress)
	log.Info("BOT:HandleNewCall", "dest_media_port", strconv.Itoa(ci.DestMediaPort))

	vbcall, ok := newVoiceBotCall(ci, v.commandWord)
	if !ok {
		return ok
	}
	vbcall.setLocalListen(true)

	udpProxy, ok := vproxy.CreateRTPPProxy(ci.DestMediaAddress, ci.DestMediaPort, ci.SrcMediaAddress, ci.SrcMediaPort)
	if !ok {
		return ok
	}

	vbcall.setUDPProxy(udpProxy)

	// Add call to callMap early to avoid race conditions with STT callbacks
	v.addCall(&vbcall)

	// Check which STT/TTS provider to use
	useGoogle := os.Getenv("USE_GOOGLE_STT_TTS") == "true"

	if OPMODE == "text" {
		if useGoogle {
			ok = v.googleSTTProvider.NewCall(vbcall.getID(), v.defaultmode, v.defaultlanguage, v.commandWord, udpProxy)
		} else {
			ok = v.sttprovider.NewCall(vbcall.getID(), v.defaultmode, v.defaultlanguage, v.commandWord, udpProxy)
		}
		if !ok {
			v.removeCall(vbcall.getID())
			udpProxy.Stop()
			return ok
		}
	}

	if OPMODE == "text" || OPMODE == "hybrid" {
		if useGoogle {
			ok = v.googleTTSProvider.NewCall(vbcall.getID(), udpProxy)
		} else {
			ok = v.ttsprovider.NewCall(vbcall.getID(), udpProxy)
		}
		if !ok {
			if OPMODE == "text" {
				if useGoogle {
					v.googleSTTProvider.EndCall(vbcall.getID())
				} else {
					v.sttprovider.EndCall(vbcall.getID())
				}
			}
			v.removeCall(vbcall.getID())
			udpProxy.Stop()
			return ok
		}
	}

	// Translation doesn't need dialog creation - it's always ready

	ok = v.remoteCommander.NewCall(vbcall.getID(), ci.Vars)
	if !ok {
		// Translation doesn't need dialog completion
		if OPMODE == "text" {
			if useGoogle {
				v.googleTTSProvider.EndCall(vbcall.getID())
				v.googleSTTProvider.EndCall(vbcall.getID())
			} else {
				v.ttsprovider.EndCall(vbcall.getID())
				v.sttprovider.EndCall(vbcall.getID())
			}
		} else if OPMODE == "hybrid" {
			if useGoogle {
				v.googleTTSProvider.EndCall(vbcall.getID())
			} else {
				v.ttsprovider.EndCall(vbcall.getID())
			}
		}
		v.removeCall(vbcall.getID())
		udpProxy.Stop()
		return ok
	}

	callinfojson, err := json.Marshal(ci.Vars)
	callinfotext := ""
	if err != nil {
		log.Error("BOT:HandleNewCall", "callid", ci.CallID, "error", "Failed to marshal call info")
	} else {
		callinfotext = string(callinfojson)
		log.Info("BOT:HandleNewCall", "callid", ci.CallID, "callinfo", callinfotext)
	}

	name := ci.Vars["CALLERIDNAME"]
	if name != "" {
		log.Info("BOT:HandleNewCall", "HELLONAME", name)
	}

	return ok
}

func (v VoiceBot) HandleEndCall(ci ariman.CallInfo) bool {
	log.Info("BOT:HandleEndCall", "callid", ci.CallID)
	v.EndCall(ci.CallID)
	return true
}

func (v VoiceBot) EndCall(callid string) bool {
	log.Info("BOT:EndCall", "callid", callid)
	call, ok := v.getCall(callid)
	if ok {
		log.Info("BOT:EndCall", "callid", callid, "status", "Stopping RTP Proxy")
		call.getUDPProxy().Stop()
	} else {
		log.Info("BOT:EndCall", "callid", callid, "status", "Call not found")
	}
	v.terminateCall(callid)
	v.removeCall(callid)
	return true
}

func (v VoiceBot) HandleCallEvent(ci ariman.CallInfo, event string) bool {
	log.Info("BOT:HandleCallEvent", "callid", ci.CallID, "event", event)
	return true
}

func (v VoiceBot) HandleExternalEvent(ci ariman.CallInfo, event string, tag string) bool {
	log.Info("BOT:HandleExternalEvent", "callid", ci.CallID, "event", event, "tag", tag)
	return true
}

func (v VoiceBot) HandleDtmfEvent(ci ariman.CallInfo, dtmf string) bool {
	log.Info("BOT:HandleDtmfEvent", "callid", ci.CallID, "digit", dtmf)
	return v.processRemoteDTMF(ci.CallID, dtmf)
}

func (v VoiceBot) SendText(callid string, text string) {
	log.Debug("BOT:SendText", "callid", callid, "text", text)
	if v.ttsprovider == nil {
		log.Error("BOT:SendText", "callid", callid, "error", "TTS provider not initialized")
		return
	}

	//call, ok := v.getCall(callid)
	_, ok := v.getCall(callid)
	if !ok {
		log.Error("BOT:SendText", "callid", callid, "error", "Call not found")
		return
	}

	// A little text sanitation
	//remove all \", \n and - characters
	text = strings.Replace(text, "\"", "", -1)
	text = strings.Replace(text, "\n", " ", -1)
	text = strings.Replace(text, "-", " ", -1)
	text = strings.Replace(text, "<", " ", -1)
	text = strings.Replace(text, ">", " ", -1)
	text = strings.Replace(text, "[", " ", -1)
	text = strings.Replace(text, "]", " ", -1)
	text = strings.Replace(text, "...", ".", -1)
	text = strings.Replace(text, "*", " ", -1)
	text = strings.Replace(text, "_", " ", -1)
	text = strings.Map(func(r rune) rune {
		if r > 31 && r < 127 {
			return r
		}
		return -1
	}, text)

	// FreePBX sounds like freex when spoken by the tts engine
	text = strings.Replace(text, "FreePBX", "Free PBX", -1)

	// The average word is 4.7 characters and the average speaking rate is 150 words per minute
	// this gives us 4.7*150/60 = 11.75 characters per second * BUFSIZE_SECONDS seconds = maxlen
	var maxlen int
	truncatedMessage := "... Sorry I was rambling a bit and had to cut that short.  Please continue."
	maxlen = 11.75*BUFSIZE_SECONDS - len(truncatedMessage)
	//maxlen = 11.75*BUFSIZE_SECONDS - 1
	if len(text) > maxlen {
		log.Info("TTS:SendText", "Event", "TruncatingText")
		text = text[:maxlen]
		// add notification
		text = text + truncatedMessage
	}

	useGoogle := os.Getenv("USE_GOOGLE_STT_TTS") == "true"
	if useGoogle {
		v.googleTTSProvider.AddText(callid, text, "voicebot", "en-US")
	} else {
		v.ttsprovider.AddText(callid, text, "voicebot", "en-US")
	}
}

func (v VoiceBot) FlushText(callid string) {
	log.Debug("BOT:FlushText", "callid", callid)
	if v.ttsprovider == nil {
		log.Error("BOT:FlushText", "callid", callid, "error", "TTS provider not initialized")
		return
	}
	_, ok := v.getCall(callid)
	if !ok {
		log.Error("BOT:FlushText", "callid", callid, "error", "Call not found")
		return
	}
	useGoogle := os.Getenv("USE_GOOGLE_STT_TTS") == "true"
	if useGoogle {
		v.googleTTSProvider.FlushResponseText(callid)
	} else {
		v.ttsprovider.FlushResponseText(callid)
	}
}

func (v VoiceBot) CancelText(callid string) {
	log.Debug("BOT:CancelText", "callid", callid)
	useGoogle := os.Getenv("USE_GOOGLE_STT_TTS") == "true"
	if useGoogle {
		if v.googleTTSProvider == nil {
			log.Error("BOT:CancelText", "callid", callid, "error", "Google TTS provider not initialized")
			return
		}
	} else {
		if v.ttsprovider == nil {
			log.Error("BOT:CancelText", "callid", callid, "error", "TTS provider not initialized")
			return
		}
	}
	_, ok := v.getCall(callid)
	if !ok {
		log.Error("BOT:CancelText", "callid", callid, "error", "Call not found")
		return
	}
	if useGoogle {
		v.googleTTSProvider.CancelText(callid)
	} else {
		v.ttsprovider.CancelText(callid)
	}
}

func (v VoiceBot) HangupCall(callid string) {
	log.Info("BOT:HangupCall", "callid", callid)
	_, ok := v.getCall(callid)
	if !ok {
		log.Error("BOT:HangupCall", "error", "Call Not Found")
		return
	}

	if v.ariController == nil {
		log.Error("BOT:HangupCall", "callid", callid, "error", "ARI controller not initialized")
		return
	}
	v.ariController.HangupCall(callid)
}

func (v *VoiceBot) ClearBuffers(callid string) {
	_, ok := v.getCall(callid)
	if !ok {
		log.Error("BOT:ClearBuffers", "callid", callid, "error", "CallOrProviderNotFound")
		return
	}
}

// DeepgramSTTCallBackHandler Interface
func (v VoiceBot) HandleTranscriptResults(callid string, text string, level string) bool {

	log.Info("BOT:HandleTranscriptResults", "callid", callid, "text", text, "level", level)

	_, ok := v.getCall(callid)
	if !ok {
		log.Error("BOT:HandleTranscriptResults", "callid", callid, "error", "Call not found")
		return ok
	}

	if level != "passive" {
		if level == "conversationalai-vad-start" {
			// Cancel any ongoing TTS
			useGoogle := os.Getenv("USE_GOOGLE_STT_TTS") == "true"
			if useGoogle {
				if v.googleTTSProvider != nil {
					v.googleTTSProvider.CancelText(callid)
				}
			} else {
				v.ttsprovider.CancelText(callid)
			}
		} else {
			// Translate the text and send to TTS
			if v.translateProvider != nil {
				v.translateProvider.TranslateText(callid, text)
			} else {
				log.Error("BOT:HandleTranscriptResults", "callid", callid, "error", "Translation provider not available")
			}
		}
	}

	return ok
}

// HandleTranslationResults handles translation results and sends to TTS
func (v VoiceBot) HandleTranslationResults(callid string, translatedText string, sourceLanguage string, targetLanguage string) bool {
	log.Info("BOT:HandleTranslationResults", 
		"callid", callid, 
		"translated", translatedText, 
		"source", sourceLanguage, 
		"target", targetLanguage)

	_, ok := v.getCall(callid)
	if !ok {
		log.Error("BOT:HandleTranslationResults", "callid", callid, "error", "Call not found")
		return false
	}

	// Send translated text to TTS
	useGoogle := os.Getenv("USE_GOOGLE_STT_TTS") == "true"
	if useGoogle {
		if v.googleTTSProvider != nil {
			v.googleTTSProvider.AddText(callid, translatedText, "translation", targetLanguage)
		} else {
			log.Error("BOT:HandleTranslationResults", "callid", callid, "error", "Google TTS provider not available")
			return false
		}
	} else {
		v.ttsprovider.AddText(callid, translatedText, "translation", targetLanguage)
	}

	return true
}

// END DeepgramSTTCallBackHandler Interface

// Translation doesn't use dialog completion - this is kept for compatibility
func (v VoiceBot) HandleDialogComplete(dialogid string, funcname string, parameters string, dialog string, prompt string) bool {
	log.Info("BOT:HandleDialogComplete", "dialogid", dialogid, "funcname", funcname, "parameters", parameters, "dialog", dialog, "prompt", prompt)
	// Translation doesn't need dialog completion - return true to indicate success
	return true
}

func (v VoiceBot) HandleDialogContinue(dialogid string, prompt string) bool {
	log.Info("BOT:HandleDialogContinue", "dialogid", dialogid, "prompt", prompt)
	v.SendText(dialogid, prompt)
	return true
}

func (v VoiceBot) HandleDialogContinueComplete(dialogid string, prompt string) bool {
	log.Info("BOT:HandleDialogContinueComplete", "dialogid", dialogid)
	v.FlushText(dialogid)
	return true
}

func (v VoiceBot) HandleDialogSpeechDetected(dialogid string, prompt string) bool {
	log.Info("BOT:HandleDialogSpeechDetected", "dialogid", dialogid)
	// For translation, cancel any ongoing TTS when speech is detected
	useGoogle := os.Getenv("USE_GOOGLE_STT_TTS") == "true"
	var isStreaming bool
	if useGoogle {
		if v.googleTTSProvider != nil {
			isStreaming = v.googleTTSProvider.IsStreaming(dialogid)
		}
	} else {
		if v.ttsprovider != nil {
			isStreaming = v.ttsprovider.IsStreaming(dialogid)
		}
	}
	if isStreaming {
		log.Info("BOT:HandleDialogSpeechDetected", "dialogid", dialogid, "status", "CancelingTextPlayback")
		v.CancelText(dialogid)
	}
	return true
}

func (v VoiceBot) HandleDialogError(dialogid string, err string) bool {
	log.Error("BOT:HandleDialogError", "dialogid", dialogid, "error", err)
	return true
}

// END AIDialogControllerCallBackHander Interface

// StudioCommanderCallBackHandler Interface
func (v VoiceBot) HandleDialogCommands(dialogid string, commands []rclocal.VoiceBotCommand) bool {
	log.Info("BOT:HandleDialogCommands", "callid", dialogid)
	for _, command := range commands {
		log.Info("BOT:HandleDialogCommands", "callid", dialogid, "command", command.Command, "parameters", command.Parameters)
		combytes, err := json.Marshal(command.Parameters)
		if err != nil {
			log.Error("BOT:HandleDialogCommands", "callid", dialogid, "error", "Failed to marshal command parameters")
			return false
		}

		if command.Wait != 0 {
			cancelDelayed := make(chan bool)
			v.addDelayedCommand(dialogid, cancelDelayed)
			go v.DoDelayedCommand(dialogid, command.Command, string(combytes), command.Wait, cancelDelayed)
		} else {
			go v.DoCommand(dialogid, command.Command, string(combytes))
		}
	}
	return true
}

func (v VoiceBot) HandleDialogFunctions(dialogid string, functions []rclocal.StudioTool, prompt string) bool {
	log.Info("BOT:HandleDialogFunctions", "callid", dialogid)
	// Translation doesn't use dialog functions - this is kept for compatibility
	for _, function := range functions {
		log.Info("BOT:HandleDialogFunctions", "callid", dialogid, "function", function.Tool.Name, "parameters", function.Tool.Parameters)
	}
	log.Info("BOT:HandleDialogFunctions", "callid", dialogid, "status", "TranslationMode - FunctionsIgnored")
	return true
}

func (v *VoiceBot) DoDelayedCommand(callid string, command string, parameters string, delay int, cncl chan bool) {
	log.Info("BOT:DoDelayedCommand", "callid", callid, "command", command, "parameters", parameters, "delay", delay)
	select {
	case <-cncl:
		log.Info("BOT:DoDelayedCommand", "callid", callid, "status", "Cancelled")
		v.removeDelayedCommand(callid, cncl)
		return
	case <-time.After(time.Duration(delay) * time.Second):
		v.removeDelayedCommand(callid, cncl)
		v.DoCommand(callid, command, parameters)
	}
}

func ConvertStudioToolsToOpenAITools(tools []rclocal.StudioTool) (bool, string) {
	var sngaiTools string
	for _, tool := range tools {
		toolbytes, err := json.Marshal(tool.Tool)
		if err != nil {
			log.Error("BOT:ConvertStudioToolsToOpenAITools", "error", "Failed to marshal tool")
			return false, ""
		} else {
			log.Debug("BOT:ConvertStudioToolsToOpenAITools", "tool", string(toolbytes))
		}
		sngaiTools += string(toolbytes)
		sngaiTools += ","
	}
	// remove trailing comma
	sngaiTools = sngaiTools[:len(sngaiTools)-1]
	return true, sngaiTools
}

// END StudioCommanderCallBackHandler Interface

func (v VoiceBot) processRemoteCommand(callid string, command string, parameters string, transcript string) bool {
	log.Debug("BOT:processRemoteCommand", "callid", callid, "command", command, "parameters", parameters)
	if v.remoteCommander == nil {
		log.Error("BOT:processRemoteCommand", "callid", callid, "error", "Remote command processor not initialized")
		return false
	}

	var callerinfo string
	call, ok := v.getCall(callid)
	if ok {
		callerinfo = call.info.Vars["CALLERIDNAME"] + " - " + call.info.Vars["CALLERIDNUM"]
	}

	return v.remoteCommander.ProcessFunction(callid, command, parameters, transcript, callerinfo)
}

func (v VoiceBot) processRemoteDTMF(callid string, commanddtmf string) bool {
	log.Debug("BOT:processRemoteDTMF", "callid", callid, "commanddtmf", commanddtmf)
	if v.remoteCommander == nil {
		log.Error("BOT:processRemoteDTMF", "callid", callid, "error", "Remote command processor not initialized")
		return false
	}

	var callerinfo string
	call, ok := v.getCall(callid)
	if ok {
		callerinfo = call.info.Vars["CALLERIDNAME"] + " - " + call.info.Vars["CALLERIDNUM"]
		ok = v.remoteCommander.ProcessDTMF(callid, commanddtmf, callerinfo)
		if ok {
			// stop audio?
			v.cancelDelayedCommands(callid)
		}
	}

	return ok
}

func (v VoiceBot) HandlePlaybackComplete(callid string, playbackid string) bool {
	_, ok := v.getCall(callid)
	if !ok {
		return false
	}
	log.Info("BOT:HandlePlaybackComplete", "callid", callid, "playbackid", playbackid)
	return true
}

func (v VoiceBot) handleChannelHangup(callid string) {

}
