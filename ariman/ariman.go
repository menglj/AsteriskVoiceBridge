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

package ariman

import (
	"context"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CyCoreSystems/ari/v6"
	"github.com/CyCoreSystems/ari/v6/client/native"
	"github.com/CyCoreSystems/ari/v6/rid"
	"github.com/rotisserie/eris"
	"golang.org/x/exp/slog"
)

const (
	RTP_MODE_ULAW = "ulaw"
	ENCODING_ALAW = "alaw"
	ENCODING_L16  = "slin16"

	ENCODING = RTP_MODE_ULAW // ENCODING_L16

	AST_ADD  = "127.0.0.1" // "172.18.0.2" || "127.0.0.1" // set to container IP if using docker
	HOST_ADD = "127.0.0.1" // "172.18.0.1" || "127.0.0.1" // set to host IP if using docker
)

var ariApp = "voicebot"

var log = slog.New(slog.NewTextHandler(os.Stderr, nil))

func GetCallID(channelID string) string {
	idSlice := strings.Split(channelID, "-")
	return idSlice[0]
}

func FORMAT() string {
	return ENCODING
}

/**************************** CallInfo ****************************/
type CallInfo struct {
	CallID           string            `json:"call_id"`
	AppKey           string            `json:"app_key"`
	SrcMediaAddress  string            `json:"src_media_address"`
	SrcMediaPort     int               `json:"src_media_port"`
	DestMediaAddress string            `json:"dest_media_address"`
	DestMediaPort    int               `json:"dest_media_port"`
	Vars             map[string]string `json:"variables"`
}

/**************************** serviceCall ****************************/
type serviceCall struct {
	info                                           CallInfo
	silencePlayback                                *ari.PlaybackHandle
	botBridge, callBridge                          *ari.BridgeHandle
	internalChannel, snoopChannel, externalChannel *ari.ChannelHandle
	callChannels                                   map[string]*ari.ChannelHandle
	writable, incall, terminating                  bool
	CTX                                            context.Context
	cancelSilence                                  chan bool
	continued                                      bool
}

func (sc *serviceCall) initialize() {
	sc.callBridge = nil
	sc.callChannels = make(map[string]*ari.ChannelHandle)
	sc.cancelSilence = make(chan bool)
	sc.incall = false
	sc.terminating = false
	sc.continued = false
}

func (sc *serviceCall) setTerminating() {
	sc.terminating = true
}

func (sc *serviceCall) isTerminating() bool {
	return sc.terminating
}

func newBotCall(bridge *ari.BridgeHandle, internalChannel *ari.ChannelHandle,
	snoopChannel *ari.ChannelHandle, externalChannel *ari.ChannelHandle, writable bool,
	dstMediaAddress string, dstMediaPort int,
	srcMediaAddress string, srcMediaPort int,
	context context.Context) (*serviceCall, bool) {

	callinfo := CallInfo{CallID: internalChannel.ID(), AppKey: "voicebotdemo", DestMediaAddress: dstMediaAddress, DestMediaPort: dstMediaPort, SrcMediaAddress: srcMediaAddress, SrcMediaPort: srcMediaPort}
	callinfo.Vars = make(map[string]string)

	CallerNumber, err := internalChannel.GetVariable("CALLERID(num)")
	if err != nil {
		log.Error("ARI:failed to get caller number", "error", err)
	} else {
		log.Info("ARI:Callerinfo", "Number", CallerNumber)
		callinfo.Vars["CALLERIDNUM"] = CallerNumber
	}

	CallerName, err := internalChannel.GetVariable("CALLERID(name)")
	if err != nil {
		log.Error("ARI:failed to get caller number", "error", err)
	} else {
		log.Info("ARI:Callerinfo", "Name", CallerName)
		callinfo.Vars["CALLERIDNAME"] = CallerName
	}

	Extension, err := internalChannel.GetVariable("EXTEN")
	if err != nil {
		log.Error("ARI:failed to get extension", "error", err)
	} else {
		log.Info("ARI:Callerinfo", "Extension", Extension)
		callinfo.Vars["EXTEN"] = Extension
	}

	Context, err := internalChannel.GetVariable("CONTEXT")
	if err != nil {
		log.Error("ARI:failed to get context", "error", err)
	} else {
		log.Info("ARI:Callerinfo", "Context", Context)
		callinfo.Vars["CONTEXT"] = Context
	}

	call := &serviceCall{info: callinfo,
		botBridge:       bridge,
		internalChannel: internalChannel,
		snoopChannel:    snoopChannel,
		externalChannel: externalChannel,
		writable:        writable,
		CTX:             context,
	}
	call.initialize()
	return call, true
}

func (sc serviceCall) getID() string {
	return sc.info.CallID
}

func (sc serviceCall) getDialKey() string {
	return sc.info.AppKey
}

func (sc serviceCall) getDestMediaPort() int {
	return sc.info.DestMediaPort
}

func (sc serviceCall) getDestMediaAddress() string {
	return sc.info.DestMediaAddress
}

func (sc serviceCall) getSrcMediaPort() int {
	return sc.info.SrcMediaPort
}

func (sc serviceCall) getSrcMediaAddress() string {
	return sc.info.SrcMediaAddress
}

func (sc serviceCall) getInfo() CallInfo {
	return sc.info
}

// TODO:  add end event handling and context so we can restart the audio if required.
func (sc *serviceCall) startSilence() bool {
	// log.Info("ARI:startSilence", "Status", "SilenceStarting")
	playHandle, err := sc.internalChannel.Play(sc.internalChannel.Key().ID, "sound:longsilence")
	if err != nil {
		log.Error("ARI:failed to play silence on user channel", "error", err)
		return false
	}
	// log.Info("ARI:startSilence", "Status", "SilenceStarted")
	sc.silencePlayback = playHandle
	return true
}

func maintainSilence(ctx context.Context, sc *serviceCall, wg *sync.WaitGroup) {
	// log.Info("ARI:maintainSilence", "Status", "StartingSilence")

	if sc == nil {
		log.Error("ARI:maintainSilence", "Status", "ServiceCallNotSet")
		return
	}

	if sc.silencePlayback == nil {
		if !sc.startSilence() {
			log.Error("ARI:maintainSilence", "Status", "FailedToStartSilence")
			return
		}
	} else {
		log.Info("ARI:maintainSilence", "Status", "SilenceAlreadyPlaying")
		return
	}

	// subscribe to silence playback events
	finishedSub := sc.silencePlayback.Subscribe(ari.Events.PlaybackFinished)
	defer finishedSub.Cancel()

	wg.Done()

	cancelled := false

	for {
		select {
		case <-ctx.Done():
			log.Info("ARI:maintainSilence", "Status", "ContextCancelled")
			return
		case <-sc.cancelSilence:
			log.Info("ARI:maintainSilence", "Status", "Cancelled")
			cancelled = true
			continue
			//return
		case <-finishedSub.Events():
			//log.Info("silence playback complete")
			if !cancelled {
				//log.Info("Restarting silence")
				// log.Info("ARI:maintainSilence", "Status", "RestartingSilence")
				sc.startSilence()
			} else {
				log.Info("ARI:maintainSilence", "Status", "NotRestartingSilence")
				sc.silencePlayback = nil
				return
			}
		}
	}
}

func (sc *serviceCall) stopSilence() bool {
	if sc.silencePlayback == nil {
		log.Info("ARI:stopSilence", "Status", "SilenceNotFound")
		return false
	}
	sc.cancelSilence <- true
	err := sc.silencePlayback.Stop()
	if err != nil {
		log.Error("ARI:failed to stop silence playback", "error", err)
		return false
	}

	//sc.silencePlayback = nil
	log.Info("ARI:stopSilence", "Status", "SilenceStopped")
	return true
}

func (sc *serviceCall) startSilenceIfEmpty() {
	if len(sc.callChannels) == 0 && !sc.isTerminating() {
		log.Info("ARI:startSilenceIfEmpty", "Status", "StartingSilence")
		wg := new(sync.WaitGroup)
		wg.Add(1)
		go maintainSilence(sc.CTX, sc, wg)
		wg.Wait()
	}
}

func (sc *serviceCall) restartSilenceIfEmpty() {
	if len(sc.callChannels) == 0 && !sc.isTerminating() {
		log.Info("ARI:restartSilenceIfEmpty", "Status", "StartingSilence")
		wg := new(sync.WaitGroup)
		wg.Add(1)
		go maintainSilence(sc.CTX, sc, wg)
		wg.Wait()
	}
}

func (sc *serviceCall) removeCallBridgeIfEmpty() {
	if len(sc.callChannels) == 0 {
		if sc.callBridge != nil {
			sc.callBridge.Delete()
			sc.callBridge = nil
			sc.incall = false
		}
	}
}

func (sc *serviceCall) addCallChannel(tag string, callChannel *ari.ChannelHandle) {
	sc.callChannels[tag] = callChannel
}

func (sc *serviceCall) removeCallChannel(tag string) {
	delete(sc.callChannels, tag)
}

func (sc *serviceCall) getCallChannel(tag string) *ari.ChannelHandle {
	return sc.callChannels[tag]
}

/**************************** Connector ****************************/

type ServiceCallHandler func(ci CallInfo) bool
type ServiceCallEventHandler func(ci CallInfo, event string) bool
type ServiceCallExternalEventHandler func(ci CallInfo, event string, tag string) bool
type DigitEventCallHandler func(ci CallInfo, digit string) bool

type Connector struct {
	application, username, password, url, wsurl string
	newCallfunc                                 ServiceCallHandler
	endCallfunc                                 ServiceCallHandler
	eventCallfunc                               ServiceCallEventHandler
	eventURICallfunc                            ServiceCallExternalEventHandler
	digitEventfunc                              DigitEventCallHandler
	ariClient                                   ari.Client
	ctx                                         context.Context
	extMediaPorts                               map[int]bool
	calls                                       map[string]*serviceCall
}

// Get a free port for external media
func (c *Connector) getPort() (int, bool) {
	min := 9900
	max := 9999
	runs := (max - min)

	rand.Seed(time.Now().UnixNano())
	chosen := rand.Intn(max-min+1) + min

	for c.extMediaPorts[chosen] && runs > 0 {
		rand.Seed(time.Now().UnixNano())
		chosen = rand.Intn(max-min+1) + min
		runs--
	}

	if runs == 0 {
		return 0, false
	}

	c.extMediaPorts[chosen] = true

	return chosen, true
}

func (c *Connector) releasePort(port int) {
	c.extMediaPorts[port] = false
}

type ARIEventHandler interface {
	HandleNewCall(ci CallInfo) bool
	HandleEndCall(ci CallInfo) bool
	HandleCallEvent(ci CallInfo, event string) bool
	HandleExternalEvent(ci CallInfo, event string, tag string) bool
	HandleDtmfEvent(ci CallInfo, digit string) bool
}

func CreateConnector(app string, username string, password string, url string, wsurl string) (*Connector, bool) {

	con := Connector{application: app,
		username: username,
		password: password,
		url:      url,
		wsurl:    wsurl,
	}
	con.extMediaPorts = make(map[int]bool)
	con.calls = make(map[string]*serviceCall)
	return &con, true
}

func (c *Connector) SetCallbacks(cbi ARIEventHandler) {
	c.newCallfunc = cbi.HandleNewCall
	c.endCallfunc = cbi.HandleEndCall
	c.eventCallfunc = cbi.HandleCallEvent
	c.eventURICallfunc = cbi.HandleExternalEvent
	c.digitEventfunc = cbi.HandleDtmfEvent
}

func manageUserChannelSubscriptions(conn *Connector, c *ari.ChannelHandle, wg *sync.WaitGroup) {

	log.Info("ARI:manageUserChannelSubscriptions", "channel", c.ID())
	defer c.Hangup()

	call, ok := conn.getCallByID(GetCallID(c.ID()))
	if !ok {
		log.Error("ARI:manageUserChannelSubscriptions", "failed to get call by ID", c.ID())
		return
	}

	// subscribe to the channel hangup events
	hangupSub := c.Subscribe(ari.Events.ChannelDestroyed)
	defer hangupSub.Cancel()
	endSub := c.Subscribe(ari.Events.StasisEnd)
	defer endSub.Cancel()

	// subscribe to channel dtmf events
	dtmfSub := c.Subscribe(ari.Events.ChannelDtmfReceived)
	defer dtmfSub.Cancel()

	wg.Done()

	for {
		select {
		case <-conn.ctx.Done():
			log.Info("ARI:manageUserChannelSubscriptions", "exitContext", c.ID())
			return
		case <-hangupSub.Events():
			log.Info("ARI:manageUserChannelSubscriptions", "channelDestroyed", c.ID())
			return
		case <-endSub.Events():
			log.Info("ARI:manageUserChannelSubscriptions", "exitStasis", c.ID())
			if call.continued {
				conn.eventURICallfunc(call.getInfo(), "exitstasis", "continue")
			} else {
				conn.eventURICallfunc(call.getInfo(), "exitstasis", "normal")
				go conn.endCallfunc(call.getInfo())
				conn.terminateCall(GetCallID(c.ID()), false)
				conn.removeCall(GetCallID(c.ID()))
			}
			return
		case digit, ok := <-dtmfSub.Events():
			if !ok {
				log.Error("ARI:manageUserChannelSubscriptions", "dtmfSubClosed", c.ID())
				return
			} else {
				log.Info("ARI:manageUserChannelSubscriptions", "digit", digit.(*ari.ChannelDtmfReceived).Digit)
				conn.digitEventfunc(call.getInfo(), digit.(*ari.ChannelDtmfReceived).Digit)
			}
		}
	}
}

func manageExternalChannelSubscriptions(conn *Connector, c *ari.ChannelHandle, tag string, wg *sync.WaitGroup) {

	log.Info("ARI:manageExternalChannelSubscriptions", "channel", c.ID())
	defer c.Hangup()

	call, ok := conn.getCallByID(GetCallID(c.ID()))
	if !ok {
		log.Error("ARI:manageExternalChannelSubscriptions", "failed to get call by ID", c.ID())
		return
	}

	// subscribe to the channel hangup event
	hangupSub := c.Subscribe(ari.Events.ChannelDestroyed)
	defer hangupSub.Cancel()

	endSub := c.Subscribe(ari.Events.StasisEnd)
	defer endSub.Cancel()

	wg.Done()

	for {
		select {
		case <-conn.ctx.Done():
			log.Info("ARI:manageExternalChannelSubscriptions", "exitContext", c.ID())
			return
		case <-hangupSub.Events():
			log.Info("ARI:manageExternalChannelSubscriptions", "channelDestroyed", c.ID())
			return
		case <-endSub.Events():
			log.Info("ARI:manageExternalChannelSubscriptions", "exitStasis", c.ID())
			conn.eventURICallfunc(call.getInfo(), "exitstasis", tag)
			call.removeCallChannel(tag)
			//call.startSilenceIfEmpty()
			call.removeCallBridgeIfEmpty()
			return
		}
	}
}

func handleNewCall(c *Connector, userChannel *ari.ChannelHandle) bool {

	// assume we are able to determine the Asterisk side external media address and port
	var writable bool = true
	log.Info("ARI:handleNewCall", "UserChan", userChannel.ID())

	// Create a snoop channel based on the user channel
	snoopid := userChannel.ID() + "-snoop"
	snoopopts := &ari.SnoopOptions{App: ariApp, Spy: "in", Whisper: "out"}
	snoopChannel, err := userChannel.Snoop(snoopid, snoopopts)
	if err != nil {
		log.Error("ARI:failed to snoop channel", "error", err)
		return false
	}
	log.Info("ARI:handleNewCall", "SnoopChan", snoopChannel.ID())

	// Find a free port from our range for the Application side of the external media
	freePort, ok := c.getPort()
	if !ok {
		log.Error("ARI:failed to get free port")
		return false
	}

	// Create an external media channel based on the user channel
	mediaurl := HOST_ADD + ":" + strconv.Itoa(freePort)
	extid := userChannel.ID() + "-external"
	log.Info("ARI:handleNewCall", "MediaFormat", FORMAT())
	log.Info("ARI:handleNewCall", "ExternalHost", mediaurl)
	extopts := ari.ExternalMediaOptions{App: ariApp, ExternalHost: mediaurl, ChannelID: extid, Format: FORMAT()}

	externalChannel, err := userChannel.ExternalMedia(extopts)
	if err != nil {
		log.Error("ARI:failed to create external channel", "error", err)
		c.releasePort(freePort)
		return false
	}

	// Get the remote RTP address and port from the external channel
	astPortString, err := externalChannel.GetVariable("UNICASTRTP_LOCAL_PORT")
	if err != nil {
		log.Error("ARI:failed to get remote rtp port", "error", err)
		writable = false
	}
	astPort, err := strconv.Atoi(astPortString)
	if err != nil {
		log.Error("ARI:failed to get remote rtp port", "error", err)
		writable = false
	}
	log.Info("ARI:handleNewCall", "RemoteRTPport", astPort)

	astAdd, err := externalChannel.GetVariable("UNICASTRTP_LOCAL_ADDRESS")
	if err != nil {
		log.Error("ARI:failed to get remote rtp address", "error", err)
		writable = false
	}
	log.Info("ARI:handleNewCall", "RemoteRTPaddress", astAdd)

	// Create a bridge for service side of the call
	botBridge, err := ensureBridge(nil, c.ctx, c.ariClient, snoopChannel.Key())
	if err != nil {
		log.Error("ARI:failed to create service bridge", "error", err)
		c.releasePort(freePort)
		return false
	}

	// Add the snooping channel to the bridge
	if err := botBridge.AddChannel(snoopChannel.Key().ID); err != nil {
		log.Error("ARI:failed to add snoop channel to bridge", "error", err)
		c.releasePort(freePort)
		return false
	}

	// Add the external media channel to the bridge
	if err := botBridge.AddChannel(externalChannel.Key().ID); err != nil {
		log.Error("ARI:failed to add external channel to bridge", "error", err)
		c.releasePort(freePort)
		return false
	}

	// If we are not able to get the remote RTP address and port, we will not be able to write audio to the user channel
	// In that case set some defaults
	if !writable {
		astAdd = AST_ADD
		astPort = 0
	}

	// Create a new call
	bc, ok := newBotCall(botBridge, userChannel, snoopChannel, externalChannel, writable, astAdd, astPort /*AST_ADD*/, HOST_ADD, freePort, c.ctx)
	if !ok {
		log.Error("ARI:Failed to create new call")
		c.releasePort(freePort)
		return false
	}

	// Add the call to the Connector
	ok = c.addCall(bc)
	if !ok {
		log.Error("ARI:Failed to add call or call already added to Connector")
		c.releasePort(freePort)
		return false
	}

	// Subscribe to the new call's user channel events
	wg := new(sync.WaitGroup)
	wg.Add(1)
	go manageUserChannelSubscriptions(c, userChannel, wg)
	bc.startSilenceIfEmpty()

	// Call back to creator's new call hander
	ok = c.newCallfunc(bc.getInfo())
	if !ok {
		log.Error("ARI:Failed to add call to Connector, terminating!")
		c.terminateCall(userChannel.ID(), true)
		c.removeCall(userChannel.ID())
		return false
	}

	return true
}

// Connector interface methods
func (c *Connector) Connect() bool {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.ctx = ctx

	log.Info("ARI:Connect", "Application", c.application, "Username", c.username, "URL", c.url, "WSURL", c.wsurl)

	ac, err := native.Connect(&native.Options{
		Application:  c.application,
		Logger:       log.With("ariman"),
		Username:     c.username,
		Password:     c.password,
		URL:          c.url,
		WebsocketURL: c.wsurl,
	})

	if err != nil {
		log.Error("ARI:Contact", "Status", "Failed to build ARI client", "error", err)
		return false
	}

	c.ariClient = ac
	sub := c.ariClient.Bus().Subscribe(nil, "StasisStart")
	log.Info("ARI:Connect", "Status", "Connected to ARI")

	ever := true
	for ever {
		select {
		case e := <-sub.Events():

			v := e.(*ari.StasisStart)
			log.Info("ARI:Connect", "NewChannelInStasis", v.Channel.ID)
			chanID := v.Channel.ID
			var chanIDSuffix string
			rootChanID := strings.Split(chanID, "-")[0]
			if strings.Contains(chanID, "-") {
				chanIDSuffix = strings.Split(chanID, "-")[1]
			}

			dialkey := v.Channel.Dialplan.Exten + "@" + v.Channel.Dialplan.Context
			log.Info("ARI:Connect", "DialKey", dialkey)
			if dialkey == "8888@from_router" || dialkey == "613@voice-ai-service" || dialkey == "614@voice-ai-service" || dialkey == "615@voice-ai-service" || dialkey == "616@voice-ai-service" {
				log.Info("ARI:Connect", "DialKey", "Matched")
				go handleNewCall(c, c.ariClient.Channel().Get(v.Key(ari.ChannelKey, v.Channel.ID)))
			} else if chanIDSuffix == "call" {
				// hopefully we have a call bridge for this call channel
				call, ok := c.getCallByID(rootChanID)
				if !ok || call.callBridge == nil {
					log.Error("ARI:Connect", "NoCallorNoCallBridge", rootChanID)
					continue
				}
				// add the channel to the call's call bridge
				log.Info("ARI:Connect", "Status", "AddingURIChannelToBridge", "ChannelID", chanID)
				if err = call.callBridge.AddChannel(chanID); err != nil {
					log.Error("ARI:Connect", "Status", "failed to add uri channel to bridge", "error", err)
					call.removeCallChannel(chanID)
					//call.startSilenceIfEmpty()
					c.ariClient.Channel().Get(v.Key(ari.ChannelKey, chanID)).Hangup()
					log.Info("ARI:Connect", "NewChannelInStasis", chanID, "Status", "Exited")
					return false
				} else {
					log.Info("ARI:Connect", "Status", "Channel added to bridge", "ChannelID", chanID)
				}
			}

			log.Info("ARI:Connect", "NewChannelInStasis", chanID, "Status", "Exited")

		case <-ctx.Done():
			ever = false
		}
	}
	return true
}

func (c *Connector) AddURItoCall(callid string, uri string) bool {
	log.Info("ARI:AddURItoCall", "callid", callid, "uri", uri)
	call, ok := c.getCallByID(callid)
	if !ok {
		log.Error("ARI:AddURItoCall", "callid", callid, "error", "Call not found")
		return false
	}

	if c.ariClient == nil {
		log.Error("ARI:AddURItoCall", "Status", "ARI client not connected")
		return false
	}

	if c.ctx == nil {
		log.Error("ARI:AddURItoCall", "Status", "Context not set")
		return false
	}

	log.Info("ARI:AddURItoCall", "Status", "CreatingBridge")
	// Create a bridge for service side of the call
	callBridge, err := ensureBridge(call.callBridge, c.ctx, c.ariClient, call.internalChannel.Key())
	if err != nil {
		log.Error("ARI:AddURItoCall", "Status", "failed to create call bridge", "error", err)
		return false
	}

	call.callBridge = callBridge

	// Create the originate request based on the URI
	var originate = ari.OriginateRequest{
		Endpoint:  uri,
		App:       ariApp,
		Timeout:   30,
		ChannelID: call.internalChannel.ID() + "-call",
	}

	// Originate the channel
	newchannel, err := call.internalChannel.Originate(originate)
	if err != nil {
		log.Error("ARI:AddURItoCall", "Status", "failed to originate channel to uri", "error", err)
		return false
	}

	log.Info("ARI:AddURItoCall", "Status", "Channel originated", "ChannelID", newchannel.ID())
	// Add the new channel to the call.  It will be added to the call bridge on stasis start
	call.addCallChannel(uri, newchannel)

	// Subscribe to the new channel's events
	wg := new(sync.WaitGroup)
	wg.Add(1)
	go manageExternalChannelSubscriptions(c, newchannel, uri, wg)
	wg.Wait()

	log.Info("ARI:AddURItoCall", "Status", "AddingUserChannelToBridge", "ChannelID", call.internalChannel.ID())

	// Add the internal channel to the call bridge if it is not already in a call
	if !call.incall {
		// Stop the silence on the user channel as it will be bridged with at least one call channel
		// This has to be done before adding the user channel to the call bridge, or the channel won't be
		// added to the bridge until the current silence stops.
		call.stopSilence()
		// Add the user channel to the call
		if err = call.callBridge.AddChannel(call.internalChannel.Key().ID); err != nil {
			log.Error("ARI:AddURItoCall", "Status", "failed to add internal channel to bridge", "error", err)
			//call.startSilenceIfEmpty()
			return false
		} else {
			log.Info("ARI:AddURItoCall", "Status", "Internal channel added to bridge", "ChannelID", call.internalChannel.ID())
		}
		call.incall = true
	} else {
		log.Info("ARI:AddURItoCall", "Status", "Internal channel already in call", "ChannelID", call.internalChannel.ID())
	}

	log.Info("ARI:AddURItoCall", "Status", "URI added to call", "callid", callid, "uri", uri)
	return true
}

func (c *Connector) RemoveURIfromCall(callid string, uri string) bool {
	log.Info("ARI:RemoveURIfromCall", "callid", callid, "uri", uri)

	call, ok := c.getCallByID(callid)
	if !ok {
		log.Error("ARI:RemoveURIfromCall", "callid", callid, "error", "Call not found")
		return false
	}

	callchan := call.getCallChannel(uri)
	if callchan == nil {
		log.Error("ARI:RemoveURIfromCall", "callid", callid, "error", "Call channel not found")
		return false
	}

	callchan.Hangup()
	call.removeCallChannel(uri)
	call.removeCallBridgeIfEmpty()

	return true
}

func (c *Connector) SendCalltoURI(callid string, uri string) bool {
	log.Info("ARI:SendCalltoURI", "callid", callid, "uri", uri)

	call, ok := c.getCallByID(callid)
	if !ok {
		log.Error("ARI:SendCalltoURI", "callid", callid, "error", "Call not found")
		return false
	}

	if c.ariClient == nil {
		log.Error("ARI:SendCalltoURI", "Status", "ARI client not connected")
		return false
	}

	userchan := call.internalChannel
	if userchan == nil {
		log.Error("ARI:SendCalltoURI", "callid", callid, "error", "User channel not found")
		return false
	}

	// Redirect the user channel to the new URI by setting the uri as chan variables then
	// continuing the channel into the dialplan for an external transfer.
	err := userchan.SetVariable("GOBOT_TRANSFER_URI", uri)
	if err != nil {
		log.Error("ARI:SendCalltoURI", "callid", callid, "error", err)
		return false
	}

	err = userchan.Continue("gobot-transfer-out", "s", 1)
	if err != nil {
		log.Error("ARI:SendCalltoURI", "callid", callid, "error", err)
		return false
	}

	return true
}

func (c *Connector) ContinueCall(callid string, context string, extension string, priority int) bool {
	log.Info("ARI:ContinueCall", "callid", callid, "uri", extension+"@"+context+":"+strconv.Itoa(priority))

	call, ok := c.getCallByID(callid)
	if !ok {
		log.Error("ARI:ContinueCall", "callid", callid, "error", "Call not found")
		return false
	}

	if c.ariClient == nil {
		log.Error("ARI:ContinueCall", "Status", "ARI client not connected")
		return false
	}

	call.stopSilence()

	call.continued = true
	err := call.internalChannel.Continue(context, extension, priority)
	if err != nil {
		log.Error("ARI:SendCalltoURI", "callid", callid, "error", err)
		return false
	}

	// seems to not be working??
	//call.internalChannel.Exec()

	log.Info("ARI:ContinueCall", "callid", callid, "Continue", "success")

	return true
}

func (c *Connector) PlayFile(callid string, file string, language string) bool {
	log.Info("ARI:PlayFile", "callid", callid, "file", file)

	call, ok := c.getCallByID(callid)
	if !ok {
		log.Error("ARI:PlayFile", "callid", callid, "error", "Call not found")
		return false
	}

	if c.ariClient == nil {
		log.Error("ARI:PlayFile", "Status", "ARI client not connected")
		return false
	}

	call.stopSilence()
	ph, err := call.internalChannel.Play(call.internalChannel.Key().ID, "sound:"+file)
	if err != nil {
		log.Error("ARI:PlayFile", "callid", callid, "error", err)
		return false
	}

	playsub := ph.Subscribe(ari.Events.PlaybackFinished)
	defer playsub.Cancel()

	<-playsub.Events()
	log.Info("ARI:PlayFile", "callid", callid, "file", file, "Status", "PlaybackFinished")
	call.restartSilenceIfEmpty()
	return true
}

func (c *Connector) HangupCall(callid string) bool {
	log.Info("ARI:HangupCall", "callid", callid)
	return c.terminateCall(callid, true)
}

func (c *Connector) getCallByID(callid string) (*serviceCall, bool) {
	return c.calls[callid], true
}

func (c *Connector) addCall(sc *serviceCall) bool {
	c.calls[sc.getID()] = sc
	return true
}

func (c *Connector) removeCall(callid string) bool {
	call, exists := c.getCallByID(callid)
	if !exists {
		return false
	}
	c.releasePort(call.getSrcMediaPort())

	delete(c.calls, callid)
	return true
}

func (c *Connector) terminateCall(callid string, userchan bool) bool {
	log.Info("ARI:terminateCall", "callid", callid)

	sc, ok := c.getCallByID(callid)
	if !ok {
		log.Error("ARI:Failed to get call by ID")
		return false
	}

	// if userchan is set just hangup there, that will trigger another call to this
	// function to clean up the rest of the call
	if userchan {
		if sc.internalChannel != nil {
			sc.setTerminating()
			// this will stop the silence playback if it's running
			sc.stopSilence()
			sc.internalChannel.Hangup()
			log.Info("ARI:terminateCall", "ChannelHangup", sc.internalChannel.ID())
			return true
		} else {
			log.Error("ARI:terminateCall", "UserChannelHanup", "Failed")
		}
	}

	if sc.botBridge != nil {
		sc.botBridge.Delete()
		log.Info("ARI:terminateCall", "BotBridge", "Deleted")
	} else {
		log.Info("ARI:terminateCall", "BotBridge", "DoesNotExist")
	}

	if sc.externalChannel != nil {
		sc.externalChannel.Hangup()
		log.Info("ARI:terminateCall", "ChannelHangup", sc.externalChannel.ID())
	} else {
		log.Error("ARI:terminateCall", "ExternalMediaChannelHanup", "Failed")
	}

	// Hangup on all call channels
	for _, ch := range sc.callChannels {
		ch.Hangup()
	}

	// Delete the call bridge
	if sc.callBridge != nil {
		sc.callBridge.Delete()
		log.Info("ARI:terminateCall", "CallBridge", "Deleted")
	} else {
		log.Info("ARI:terminateCall", "CallBridge", "DoesNotExist")
	}

	return true
}

func ensureBridge(bridge *ari.BridgeHandle, ctx context.Context, ariClient ari.Client, src *ari.Key) (b *ari.BridgeHandle, err error) {
	if bridge != nil {
		log.Debug("Bridge already exists")
		return bridge, nil
	}

	key := src.New(ari.BridgeKey, rid.New(rid.Bridge))

	bridge, err = ariClient.Bridge().Create(key, "mixing", key.ID)
	if err != nil {
		bridge = nil
		return bridge, eris.Wrap(err, "failed to create bridge")
	}

	wg := new(sync.WaitGroup)
	wg.Add(1)
	go manageBridge(ctx, bridge, wg)
	wg.Wait()

	return bridge, nil
}

func manageBridge(ctx context.Context, h *ari.BridgeHandle, wg *sync.WaitGroup) {
	// Delete the bridge when we exit
	defer h.Delete()

	destroySub := h.Subscribe(ari.Events.BridgeDestroyed)
	defer destroySub.Cancel()

	enterSub := h.Subscribe(ari.Events.ChannelEnteredBridge)
	defer enterSub.Cancel()

	leaveSub := h.Subscribe(ari.Events.ChannelLeftBridge)
	defer leaveSub.Cancel()

	wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-destroySub.Events():
			log.Debug("bridge destroyed")
			return
		case e, ok := <-enterSub.Events():
			if !ok {
				log.Error("ARI:channel entered subscription closed")
				return
			}

			v := e.(*ari.ChannelEnteredBridge)

			log.Debug("channel entered bridge", "channel", v.Channel.Name)
		case e, ok := <-leaveSub.Events():
			if !ok {
				log.Error("ARI:channel left subscription closed")
				return
			}

			v := e.(*ari.ChannelLeftBridge)

			log.Debug("channel left bridge", "channel", v.Channel.Name)
		}
	}
}
