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
	
	// Get language from environment variable, default to en-US for translation
	language := os.Getenv("STT_LANGUAGE")
	if language == "" {
		language = "en-US" // Default to English for phone translation
	}
	
	log.Info("Using STT language", "language", language)
	
	vb, ok := voicebot.CreateVoiceBot("tango", "conversationalai", language)
	if !ok {
		log.Error("Failed to create VoiceBot")
		return
	}

	vb.GoBotGo()
}
