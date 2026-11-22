// SPDX-License-Identifier: GPL-3.0-or-later

package mempak

import "testing"

func TestGameName(t *testing.T) {
	for _, tc := range []struct {
		code, name string
		ok         bool
	}{
		{"NV8E", "Vigilante 8 (USA)", true},
		{"NSME", "Super Mario 64 (USA)", true},
		{"CZLE", "Legend of Zelda, The - Ocarina of Time (USA)", true},
		{"NTDE", "", false},             // O.D.T. (USA) only exists as a prototype
		{"\x3b\xad\xd1\xe5", "", false}, // GameShark
	} {
		name, ok := GameName([]byte(tc.code))
		if name != tc.name || ok != tc.ok {
			t.Errorf("GameName(%q) = %q, %v; want %q, %v", tc.code, name, ok, tc.name, tc.ok)
		}
	}
	if n := len(gameNames()); n < 800 {
		t.Errorf("game code table has %d entries, want at least 800", n)
	}
}
