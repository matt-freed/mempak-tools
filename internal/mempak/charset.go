// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2007-2024 Raphael Assenat (gcn64tools); Go port (C) 2026 Matt Freed.

package mempak

// n64Charset maps the N64 note-name character set, starting at code 0x0F,
// to Unicode.
var n64Charset = []rune(" 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ!\"#'*+,-./:=?@" +
	"。゛゜ァィゥェォッャュョヲンアイウエオ" +
	"カキクケコサシスセソタチツテトナニヌネノハヒフヘホマミムメモヤユヨラリルレロワ" +
	"ガギグゲゴザジズゼゾダヂヅデドバビブベボパピプペポ")

const charsetBase = 0x0F

// decodeChar converts one N64 character to Unicode. Codes outside the
// character set (including the 0x00 padding) become a space.
func decodeChar(c byte) rune {
	i := int(c) - charsetBase
	if i >= 0 && i < len(n64Charset) {
		return n64Charset[i]
	}
	return ' '
}
