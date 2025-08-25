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

package vproxy

import (
	"crypto/rand"
	"encoding/binary"
	"log/slog"
	"os"
)

var log = slog.New(slog.NewTextHandler(os.Stderr, nil))

func generateRandomSSRCID() uint32 {
	var ssrc uint32
	binary.Read(rand.Reader, binary.BigEndian, &ssrc)
	return ssrc
}
