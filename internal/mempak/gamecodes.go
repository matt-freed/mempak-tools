// SPDX-License-Identifier: GPL-3.0-or-later

package mempak

import (
	_ "embed"
	"strings"
	"sync"
)

// gameCodesTSV maps retail game codes to titles. It is generated from the
// No-Intro N64 DAT by scripts/gamecodes.py.
//
//go:embed gamecodes.tsv
var gameCodesTSV string

var gameNames = sync.OnceValue(func() map[string]string {
	m := make(map[string]string, 1024)
	for line := range strings.Lines(gameCodesTSV) {
		line = strings.TrimRight(line, "\r\n")
		if line == "" || line[0] == '#' {
			continue
		}
		code, name, ok := strings.Cut(line, "\t")
		if ok {
			m[code] = name
		}
	}
	return m
})

// GameName returns the retail title for a 4-byte game code, such as
// "Vigilante 8 (USA)" for NV8E. ok is false for codes not in the table:
// unreleased games, homebrew, cheat devices, or a game that writes a code
// other than its cartridge ID.
func GameName(code []byte) (name string, ok bool) {
	name, ok = gameNames()[string(code)]
	return name, ok
}
