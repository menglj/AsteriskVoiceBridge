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

package rclocal

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/asterisk/AsteriskVoiceBridge/voiceai"
)

const (
	URI_BASE = "http://localhost:8989/"
	// IVR Demo
	URI_COMMAND     = "ivr-root"
	URI_COMMAND_613 = "ivr-root"
	URI_EXTENSION   = ".json"
)

func getStudioURI() (bool, string) {
	return true, URI_BASE + URI_COMMAND + URI_EXTENSION
}

func getStudioURIFromKey(key string) (bool, string) {
	uri_base := URI_BASE
	uri_command := URI_COMMAND
	uri_extension := URI_EXTENSION
	if key == "613" {
		uri_command = URI_COMMAND_613
	}

	return true, uri_base + uri_command + uri_extension
}

var log = slog.New(slog.NewTextHandler(os.Stderr, nil))

// Response from the web server, allows us to automatically parse the JSON response
type StudioResponseJSON struct {
	Commands     []VoiceBotCommand `json:"commands"`
	Tools        []StudioTool      `json:"tools"`
	Instructions string            `json:"instructions"`
}

type VoiceBotCommand struct {
	Command    string            `json:"command"`
	Wait       int               `json:"wait"`
	Parameters map[string]string `json:"parameters"`
}

type StudioTool struct {
	DTMFValue   string             `json:"dtmf_equivalent"`
	URLCallback string             `json:"url"`
	PassBack    map[string]string  `json:"pass_back"`
	Tool        voiceai.OpenAITool `json:"tool"`
}

type PassBack struct {
	Parameters map[string]string
}

// A set of maps between name and DTMF value to the URL callback
type StudioFunctionCallbacks struct {
	dtmfFunctions          map[string]string
	nameFunctions          map[string]string
	dtmfPassBackParameters map[string]PassBack
	passBackParameters     map[string]PassBack
	processing, destroying bool
}

func (sf *StudioFunctionCallbacks) Build(callbacks []StudioTool) {
	sf.dtmfFunctions = make(map[string]string)
	sf.nameFunctions = make(map[string]string)
	sf.dtmfPassBackParameters = make(map[string]PassBack)
	sf.passBackParameters = make(map[string]PassBack)
	sf.processing = false
	sf.destroying = false
	for _, callback := range callbacks {
		log.Debug("RCLOCAL:Build", "Name", callback.Tool.Name, "DTMF", callback.DTMFValue, "URL", callback.URLCallback)
		sf.dtmfFunctions[callback.DTMFValue] = callback.URLCallback
		sf.nameFunctions[callback.Tool.Name] = callback.URLCallback
		log.Debug("RCLOCAL:Build", "Name", callback.Tool.Name, "DTMF", callback.DTMFValue, "PassBacks", callback.PassBack)
		pb := PassBack{Parameters: callback.PassBack}
		sf.dtmfPassBackParameters[callback.DTMFValue] = pb
		sf.passBackParameters[callback.Tool.Name] = pb
	}
}

func (sf *StudioFunctionCallbacks) ReBuild(callbacks []StudioTool) {
	clear(sf.dtmfFunctions)
	clear(sf.nameFunctions)
	for _, callback := range callbacks {
		log.Debug("RCLOCAL:ReBuild", "Name", callback.Tool.Name, "DTMF", callback.DTMFValue, "URL", callback.URLCallback)
		sf.dtmfFunctions[callback.DTMFValue] = callback.URLCallback
		sf.nameFunctions[callback.Tool.Name] = callback.URLCallback

		pb := PassBack{Parameters: callback.PassBack}
		for key, value := range pb.Parameters {
			log.Debug("RCLOCAL:ReBuild", "Name", callback.Tool.Name, "DTMF", callback.DTMFValue, "PassBackKey", key, "PassBackValue", value)
		}

		sf.dtmfPassBackParameters[callback.DTMFValue] = pb
		sf.passBackParameters[callback.Tool.Name] = pb
	}
}

func (sf *StudioFunctionCallbacks) IsProcessing() bool {
	return sf.processing
}

func (sf *StudioFunctionCallbacks) IsDestroying() bool {
	return sf.destroying
}

func (sf *StudioFunctionCallbacks) SetProcessing(toggle bool) {
	sf.processing = toggle
}

func (sf *StudioFunctionCallbacks) SetDestroying(toggle bool) {
	sf.destroying = toggle
}

func (sf StudioFunctionCallbacks) GetFunctionByName(name string) (bool, string) {
	nf, exists := sf.nameFunctions[name]
	if !exists {
		return false, ""
	}
	return true, nf
}

func (sf StudioFunctionCallbacks) GetFunctionByDTMF(dtmf string) (bool, string) {
	df, exists := sf.dtmfFunctions[dtmf]
	if !exists {
		return false, ""
	}
	return true, df
}

func (sf StudioFunctionCallbacks) GetPassBackParametersByName(name string) (bool, PassBack) {
	pb, exists := sf.passBackParameters[name]
	if !exists {
		return false, pb
	}
	return true, pb
}

func (sf StudioFunctionCallbacks) GetPassBackParametersByDTMF(dtmf string) (bool, PassBack) {
	pb, exists := sf.dtmfPassBackParameters[dtmf]
	if !exists {
		return false, pb
	}
	return true, pb
}

// Main struct for StudioCommander, which holds the URI and a map of dialogid's to StudioTool Callbacks
type studioDialogCommandsCallBack func(dialogid string, commands []VoiceBotCommand) bool
type studioDialogFunctionsCallBack func(dialogid string, functions []StudioTool, prompt string) bool

type StudioCommanderCallBackHandler interface {
	HandleDialogCommands(dialogid string, commands []VoiceBotCommand) bool
	HandleDialogFunctions(dialogid string, functions []StudioTool, prompt string) bool
}

type StudioCommander struct {
	StudioURI string
	// map of dialogid's to StudioFunctionCallbacks
	CallBacks map[string]*StudioFunctionCallbacks

	doCommands   studioDialogCommandsCallBack
	useFunctions studioDialogFunctionsCallBack
}

func (sc *StudioCommander) init() bool {
	ok, uri := getStudioURI()
	if ok {
		sc.StudioURI = uri
		sc.CallBacks = make(map[string]*StudioFunctionCallbacks)
	}
	return ok
}

func (sc *StudioCommander) SetCallBacks(handler StudioCommanderCallBackHandler) {
	sc.doCommands = handler.HandleDialogCommands
	sc.useFunctions = handler.HandleDialogFunctions
}

func CreateStudioCommander() (*StudioCommander, bool) {
	sc := StudioCommander{}
	ok := sc.init()
	return &sc, ok
}

func (sc *StudioCommander) addDialog(dialogid string) {
	sf := &StudioFunctionCallbacks{}
	sc.CallBacks[dialogid] = sf
}

func (sc *StudioCommander) addCallBacksToDialog(dialogid string, callbacks []StudioTool) bool {
	sf, exists := sc.CallBacks[dialogid]
	if !exists {
		return false
	}
	sf.Build(callbacks)
	return true
}

func (sc *StudioCommander) resetCallBacksInDialog(dialogid string, callbacks []StudioTool) bool {
	sf, exists := sc.CallBacks[dialogid]
	if !exists {
		return false
	}
	sf.ReBuild(callbacks)
	return true
}

func (sc *StudioCommander) getDialog(dialogid string) (bool, *StudioFunctionCallbacks) {
	sf, exists := sc.CallBacks[dialogid]
	if !exists {
		return false, nil
	}
	return true, sf
}

func (sc *StudioCommander) removeDialog(dialogid string) {
	delete(sc.CallBacks, dialogid)
}

func (sc *StudioCommander) NewCall(dialogid string, vars map[string]string) bool {
	ok, _ := sc.getDialog(dialogid)
	if ok {
		return false
	}
	sc.addDialog(dialogid)
	key := "610"
	if vars["EXTEN"] != "" {
		key = vars["EXTEN"]
	}
	log.Info("RCLOCAL:NewCall", "DialogID", dialogid, "Key", key)
	go sc.handleNewCall(dialogid, vars, key)
	return true
}

func (sc *StudioCommander) EndCall(dialogid string) {
	ok, _ := sc.getDialog(dialogid)
	if !ok {
		return
	}
	sc.removeDialog(dialogid)
}

func (sc *StudioCommander) ProcessDTMF(dialogid string, dtmf string, callerinfo string) bool {
	ok, sf := sc.getDialog(dialogid)
	if !ok {
		return false
	}
	ok, url := sf.GetFunctionByDTMF(dtmf)
	if !ok {
		return false
	}

	log.Info("RCLOCAL:ProcessDTMF", "DTMF", dtmf, "URL", url)
	processAsDtmf := true

	chatText := "Selected by DTMF Input:" + dtmf
	chatParams := ""
	var callbacks PassBack

	ok, callbacks = sf.GetPassBackParametersByDTMF(dtmf)
	if ok {
		// process each callback
		log.Info("RCLOCAL:ProcessDTMF", "DTMF", dtmf, "NumParameters", len(callbacks.Parameters))
		for key, value := range callbacks.Parameters {
			log.Info("RCLOCAL:ProcessDTMFPassBacks", "Key", key, "Value", value)
			if key == "dialog" {
				value = strings.ReplaceAll(value, "\n", "\\n")
				chatText = value
			} else {
				chatParams += fmt.Sprintf(`,{"%s": "%s"}`, key, value)
				processAsDtmf = false
			}
		}

		// remove leading comma
		chatParams = strings.TrimPrefix(chatParams, ",")
		chatParams = "[" + chatParams + "]"
	} else {
		log.Info("RCLOCAL:ProcessDTMF", "MSG", "NoPassBackParameters")
	}

	go sc.DoCommand(dialogid, url, chatParams, chatText, callerinfo, processAsDtmf)
	return true
}

type ChatElement struct {
	Content string `json:"content"`
	Role    string `json:"role"`
}

type ChatDialog struct {
	Dialog []ChatElement `json:"dialog"`
}

func (c ChatDialog) Summarize() string {
	var summary string
	for _, element := range c.Dialog {
		if element.Role == "user" {
			summary += "User: " + element.Content + "\\n"
		} else {
			summary += "Agent: " + element.Content + "\\n"
		}
	}
	summary = strings.Trim(summary, "\\n")

	return summary
}

func (sc *StudioCommander) ProcessFunction(dialogid string, function string, parameters string, dialogtext string, callerinfo string) bool {
	ok, sf := sc.getDialog(dialogid)
	if !ok {
		return false
	}
	ok, url := sf.GetFunctionByName(function)
	if !ok {
		return false
	}

	chatText := ""
	var callbacks PassBack
	ok, callbacks = sf.GetPassBackParametersByName(function)

	parameters = strings.TrimPrefix(parameters, "[")
	parameters = strings.TrimSuffix(parameters, "]")

	if ok {
		// process each callback
		for key, value := range callbacks.Parameters {
			if key == "dialog" {
				//chatText = "{\"dialog\": \"" + dialogtext + "\"}"
				value = strings.ReplaceAll(value, "\n", "\\n")
				chatText = value
			} else {
				parameters += fmt.Sprintf(`,{"%s": "%s"}`, key, value)
			}
		}
	} else {
		log.Info("RCLOCAL:ProcessFunction", "MSG", "NoPassBackParameters")
	}

	// remove any leading comma
	parameters = strings.TrimPrefix(parameters, ",")
	// arrayatize me captain!
	parameters = "[" + parameters + "]"

	log.Info("RCLOCAL:ProcessFunction", "Function", function, "Parameters", parameters)

	if dialogtext != "" && chatText == "" {
		var chatDialog ChatDialog
		dialogtext = "{\"dialog\":" + dialogtext + "}"
		err := json.Unmarshal([]byte(dialogtext), &chatDialog)
		if err != nil {
			log.Error("RCLOCAL:ProcessFunction", "Error", "FailedToUnmarshalDialogText")
			fmt.Println("RCLOCAL:ProcessFunction "+"DialogText: ", dialogtext)
		} else {
			chatText = chatDialog.Summarize()
		}
	}

	go sc.DoCommand(dialogid, url, parameters, chatText, callerinfo, false)
	return true
}

func (sc *StudioCommander) DoCommand(dialogid string, url string, parameters string, dialogtext string, callerinfo string, dtmfused bool) {
	ok := sc.doCommand(dialogid, url, parameters, dialogtext, callerinfo, dtmfused)
	if !ok {
		log.Error("RCLOCAL:DoCommand", "Error", "FailedToDoCommand")
	}
}

func mergeParamsAndDialogText(parameters string, dialogtext string, callerinfo string, dtmfused bool) []byte {

	returnstring := `{`

	// parameters is an array
	if parameters != "" {
		returnstring += `"parameters": ` + parameters + `, `
	}

	// dialogtext is a string
	if dialogtext != "" {
		returnstring += `"dialog": "` + dialogtext + `", `
	}

	// callerinfo is a string
	if callerinfo != "" {
		returnstring += `"callerinfo": "` + callerinfo + `"`
	}

	// dtmfused is a string pretending to be a boolean
	if dtmfused {
		returnstring += `, "dtmfused": "true"`
	} else {
		returnstring += `, "dtmfused": "false"`
	}

	returnstring += `}`

	return []byte(returnstring)
}

func (sc *StudioCommander) doCommand(dialogid string, url string, parameters string, dialogtext string, callerinfo string, dtmfused bool) bool {
	// send GET to the web server to get the call capabilities
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	data := mergeParamsAndDialogText(parameters, dialogtext, callerinfo, dtmfused)

	log.Info("RCLOCAL:doCommand", "URL", url)
	log.Info("RCLOCAL:doCommand", "Data", string(data))

	req, err := http.NewRequest("GET", url, bytes.NewBuffer(data))
	if err != nil {
		log.Error("RCLOCAL:doCommand", "ErrorCreatingRequest", err)
		return false
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Error("RCLOCAL:doCommand", "ErrorSendingRequest", err)
		return false
	}
	defer resp.Body.Close()

	rStatus := resp.Status

	if rStatus != "200 OK" {
		log.Error("RCLOCAL:doCommand", "ResponseStatus", rStatus)
		return false
	} else {
		log.Info("RCLOCAL:doCommand", "ResponseStatus", rStatus)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error("RCLOCAL:doCommand", "ErrorReadingBody", err)
		return false
	}

	var dialogResponse StudioResponseJSON
	err = json.Unmarshal(body, &dialogResponse)
	if err != nil {
		log.Error("RCLOCAL:doCommand", "ErrorUnmarshallingBody", err)
		log.Error("RCLOCAL:doCommand", "Body", string(body))
		return false
	}

	funcs := dialogResponse.Tools
	sc.resetCallBacksInDialog(dialogid, funcs)

	for _, funcCall := range dialogResponse.Tools {
		log.Debug("RCLOCAL:doCommand", "Name", funcCall.Tool.Name, "DTMF", funcCall.DTMFValue)
	}

	for _, comnd := range dialogResponse.Commands {
		log.Debug("RCLOCAL:doCommand", "Command", comnd.Command)
	}

	// send the functions to the call's chat bot
	instructions := dialogResponse.Instructions
	if len(funcs) > 0 {
		sc.useFunctions(dialogid, funcs, instructions)
	} else {
		log.Debug("RCLOCAL:doCommand", "NoFunctions", "ToUse")
	}

	// send the commands to the call's local commander
	if len(dialogResponse.Commands) > 0 {
		sc.doCommands(dialogid, dialogResponse.Commands)
	} else {
		log.Debug("RCLOCAL:doCommand", "NoCommands", "ToUse")
	}

	return true
}

func (sc *StudioCommander) handleNewCall(dialogid string, vars map[string]string, key string) {
	ok := sc.getCapabilities(dialogid, vars, key)
	if !ok {
		log.Error("RCLOCAL:handleNewCall", "Error", "FailedTogetCapabilities")
		//say something and hangup (send commands)
	}
}

func (sc *StudioCommander) getCapabilities(dialogid string, vars map[string]string, key string) bool {
	// send GET to the web server to get the call capabilities
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	var data []byte
	data, err := json.Marshal(vars)
	if err != nil {
		log.Error("RCLOCAL:getCapabilities", "ErrorMarshallingVars", err)
		data = []byte{}
	}

	ok, studioURI := getStudioURIFromKey(key)
	if !ok {
		log.Error("RCLOCAL:getCapabilities", "Error", "FailedToGetKeyedStudioURI")
		studioURI = sc.StudioURI
	}

	log.Info("RCLOCAL:getCapabilities", "URL", studioURI)
	log.Info("RCLOCAL:getCapabilities", "Data", string(data))

	req, err := http.NewRequest("GET", studioURI, bytes.NewBuffer(data))
	if err != nil {
		log.Error("RCLOCAL:getCapabilities", "ErrorCreatingRequest", err)
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Error("RCLOCAL:getCapabilities", "ErrorSendingRequest", err)
		return false
	}
	defer resp.Body.Close()

	rStatus := resp.Status
	log.Debug("RCLOCAL:getCapabilities", "ResponseStatus", rStatus)
	if rStatus != "200 OK" {
		log.Error("RCLOCAL:getCapabilities", "ResponseStatus", rStatus)
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error("RCLOCAL:getCapabilities", "ErrorReadingBody", err)
		return false
	}

	// parse the body into a json object
	log.Debug("RCLOCAL:getCapabilities", "UnmarshallingBody", "WithoutFunctionCall")
	var dialogResponse StudioResponseJSON
	err = json.Unmarshal(body, &dialogResponse)
	if err != nil {
		log.Error("RCLOCAL:getCapabilities", "ErrorUnmarshallingBody", err)
		log.Error("RCLOCAL:getCapabilities", "Body", string(body))
		return false
	}

	funcs := dialogResponse.Tools
	sc.addCallBacksToDialog(dialogid, funcs)

	for _, funcCall := range dialogResponse.Tools {
		log.Debug("RCLOCAL:getCapabilities", "Name", funcCall.Tool.Name, "DTMF", funcCall.DTMFValue)
	}

	for _, comnd := range dialogResponse.Commands {
		log.Debug("RCLOCAL:getCapabilities", "Command", comnd.Command)
	}

	// send the functions to the call's chat bot
	instructions := dialogResponse.Instructions
	if len(funcs) > 0 {
		sc.useFunctions(dialogid, funcs, instructions)
	} else {
		log.Debug("RCLOCAL:doCommand", "NoFunctions", "ToUse")
	}

	// send the commands to the call's local commander
	if len(dialogResponse.Commands) > 0 {
		sc.doCommands(dialogid, dialogResponse.Commands)
	} else {
		log.Debug("RCLOCAL:doCommand", "NoCommands", "ToUse")
	}

	return true

}
