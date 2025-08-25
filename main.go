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
package main

import (
	"os"

	"github.com/asterisk/AsteriskVoiceBridge/voicebot"
	"golang.org/x/exp/slog"
)

var log = slog.New(slog.NewTextHandler(os.Stderr, nil))

func main() {
	log.Info("No rest for the wicked.")
	vb, ok := voicebot.CreateVoiceBot("tango", "conversationalai", "en-US")
	if !ok {
		log.Error("Failed to create VoiceBot")
		return
	}

	vb.GoBotGo()
}
