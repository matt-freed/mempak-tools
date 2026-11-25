// SPDX-License-Identifier: GPL-3.0-or-later

package mempak

import (
	"bytes"
	"errors"
	"testing"
)

func TestSetSerial(t *testing.T) {
	img := New()
	insert(t, img, testNote(3, 1), -1)
	before := img.Data

	var s Serial
	copy(s[:], "MY PAK 1")
	// Damage backup 2: SetSerial should rewrite it along with the others.
	img.sector(0)[idBlockOffsets[2]+5] ^= 0xFF
	if err := img.SetSerial(s); err != nil {
		t.Fatal(err)
	}

	got, err := img.Serial()
	if err != nil || got != s {
		t.Fatalf("Serial() = %q, %v; want %q", got, err, s)
	}
	if text, ok := got.Text(); !ok || text != "MY PAK 1" {
		t.Fatalf("Text() = %q, %v", text, ok)
	}
	h := img.sector(0)
	for i, off := range idBlockOffsets {
		b := h[off : off+idBlockSize]
		if !idBlockValid(b) {
			t.Errorf("ID block copy %d fails its checksums", i)
		}
		// Device ID, bank count and the unused bytes are kept.
		if !bytes.Equal(b[0x18:0x1C], formatHeader[0x38:0x3C]) {
			t.Errorf("ID block copy %d: bytes 0x18-0x1b = % x", i, b[0x18:0x1C])
		}
	}
	if d, _ := img.DamagedIDBlocks(); len(d) != 0 {
		t.Errorf("damaged copies after SetSerial: %v", d)
	}
	// Nothing outside the ID blocks changes.
	for i := range img.Data {
		inID := false
		for _, off := range idBlockOffsets {
			inID = inID || (i >= off && i < off+idBlockSize)
		}
		if !inID && img.Data[i] != before[i] {
			t.Fatalf("byte 0x%04x changed", i)
		}
	}
	if err := img.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSetSerialUnformatted(t *testing.T) {
	var img Image
	if err := img.SetSerial(RandomSerial()); !errors.Is(err, ErrNotFormatted) {
		t.Fatalf("SetSerial on a blank image: %v", err)
	}
}

func TestSerialText(t *testing.T) {
	tmpl, _ := New().Serial()
	var nul, ctrl, mid Serial
	ctrl[0] = 'A'
	ctrl[1] = '\n'
	copy(mid[:], "AB")
	mid[5] = 'C' // a zero byte inside the text
	for _, s := range []Serial{tmpl, nul, ctrl, mid} {
		if text, ok := s.Text(); ok {
			t.Errorf("% x read as text %q", s, text)
		}
	}
	if RandomSerial() == RandomSerial() {
		t.Error("two random serials are equal")
	}
}
