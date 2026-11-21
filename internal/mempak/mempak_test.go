// SPDX-License-Identifier: GPL-3.0-or-later

package mempak

import (
	"bytes"
	"errors"
	"testing"
)

// testNote returns a note file like the one tests/offline.sh builds:
// game "NSME", publisher "01", name "TEST", nblocks of data.
func testNote(nblocks int, seed byte) []byte {
	b := make([]byte, noteHeaderSize+nblocks*BlockSize)
	copy(b, []byte{'N', 'S', 'M', 'E', '0', '1', 0xCA, 0xFE})
	copy(b[0x10:], []byte{0x2d, 0x1e, 0x2c, 0x2d}) // TEST
	for i := noteHeaderSize; i < len(b); i++ {
		b[i] = byte(i)*7 + seed
	}
	return b
}

func insert(t *testing.T, img *Image, note []byte, slot int) int {
	t.Helper()
	h, d, err := ParseNoteFile(note)
	if err != nil {
		t.Fatal(err)
	}
	used, err := img.InsertNote(h, d, slot)
	if err != nil {
		t.Fatal(err)
	}
	return used
}

func TestNewIsValid(t *testing.T) {
	img := New()
	if err := img.Validate(); err != nil {
		t.Fatal(err)
	}
	if free, _ := img.FreeBlocks(); free != TotalBlocks {
		t.Fatalf("free = %d", free)
	}
	// What 'adapter erase --fill-ff' leaves behind.
	var wiped Image
	for i := range wiped.Data {
		wiped.Data[i] = 0xFF
	}
	if err := wiped.Validate(); !errors.Is(err, ErrNotFormatted) {
		t.Fatalf("0xFF-filled image: %v", err)
	}
}

func TestTOCFallback(t *testing.T) {
	img := New()
	img.Data[BlockSize+1] ^= 0xFF // break TOC 1's checksum
	if err := img.Validate(); err != nil {
		t.Fatalf("TOC 2 should be used: %v", err)
	}
	img.Data[2*BlockSize+1] ^= 0xFF
	if err := img.Validate(); err == nil {
		t.Fatal("both TOCs broken but still valid")
	}
}

func TestInsertExtractDelete(t *testing.T) {
	img := New()
	note := testNote(2, 3)

	if used := insert(t, img, note, -1); used != 0 {
		t.Fatalf("auto slot = %d", used)
	}
	if used := insert(t, img, testNote(3, 9), 5); used != 5 {
		t.Fatalf("slot = %d", used)
	}
	if free, _ := img.FreeBlocks(); free != TotalBlocks-5 {
		t.Fatalf("free = %d", free)
	}

	n, err := img.Note(0)
	if err != nil || !n.Valid || n.Blocks != 2 || n.Name() != "TEST" {
		t.Fatalf("note 0 = %+v, %v (name %q)", n, err, n.Name())
	}
	if got := n.RawName(); got != "TEST            . " {
		t.Fatalf("raw name %q", got)
	}

	out, err := img.ExportNote(0)
	if err != nil || !bytes.Equal(out, note) {
		t.Fatalf("export differs: %v", err)
	}

	// Replacing an existing note frees its blocks.
	insert(t, img, note, 5)
	if free, _ := img.FreeBlocks(); free != TotalBlocks-4 {
		t.Fatalf("free after replace = %d", free)
	}

	if err := img.DeleteNote(0); err != nil {
		t.Fatal(err)
	}
	if err := img.DeleteNote(0); !errors.Is(err, ErrSlotEmpty) {
		t.Fatalf("double delete: %v", err)
	}
	if free, _ := img.FreeBlocks(); free != TotalBlocks-2 {
		t.Fatalf("free after delete = %d", free)
	}
	if out, _ := img.ExportNote(5); !bytes.Equal(out, note) {
		t.Fatal("note 5 changed")
	}
}

func TestBlockAllocationOrder(t *testing.T) {
	// The lowest free blocks are taken and linked in reverse, as in
	// libdragon: a 3-block note on a blank pak uses 7 -> 6 -> 5.
	img := New()
	insert(t, img, testNote(3, 0), -1)
	n, _ := img.Note(0)
	if n.inode() != 7 {
		t.Fatalf("inode = %d", n.inode())
	}
	var tc toc
	copy(tc[:], img.sector(1))
	chain, err := tc.chain(n.inode())
	if err != nil || len(chain) != 3 || chain[1] != 6 || chain[2] != 5 {
		t.Fatalf("chain = %v, %v", chain, err)
	}
	if !bytes.Equal(img.sector(1), img.sector(2)) {
		t.Fatal("TOC copies differ")
	}
}

func TestNoSpace(t *testing.T) {
	img := New()
	insert(t, img, testNote(100, 0), -1)
	h, d, _ := ParseNoteFile(testNote(24, 0))
	if _, err := img.InsertNote(h, d, -1); !errors.Is(err, ErrNoSpace) {
		t.Fatalf("got %v", err)
	}
	img = New()
	for i := range NumNotes {
		insert(t, img, testNote(1, byte(i)), -1)
	}
	h, d, _ = ParseNoteFile(testNote(1, 0))
	if _, err := img.InsertNote(h, d, -1); !errors.Is(err, ErrNoSpace) {
		t.Fatalf("all slots used: %v", err)
	}
}

func TestCorruptChain(t *testing.T) {
	img := New()
	insert(t, img, testNote(2, 0), -1)
	for _, s := range []int{1, 2} {
		sec := img.sector(s)
		sec[6<<1|1] = 6 // block 6 points to itself: a loop
		var tc toc
		copy(tc[:], sec)
		sec[1] = tc.checksum()
	}
	if n, _ := img.Note(0); n.Valid {
		t.Fatal("looping note reported valid")
	}
	if _, err := img.NoteData(0); err == nil {
		t.Fatal("NoteData of a looping note succeeded")
	}
}

func TestParseNoteFileErrors(t *testing.T) {
	note := testNote(1, 0)
	bad := bytes.Clone(note)
	bad[7] = 0
	if _, _, err := ParseNoteFile(bad); err == nil {
		t.Error("missing 0xCAFE marker accepted")
	}
	if _, _, err := ParseNoteFile(note[:100]); err == nil {
		t.Error("partial block accepted")
	}
	bad = bytes.Clone(note)
	bad[4], bad[5] = 0, 0 // no publisher code: it would read back as a free slot
	if _, _, err := ParseNoteFile(bad); !errors.Is(err, errNoCodes) {
		t.Errorf("zero publisher code: %v", err)
	}
	bad = bytes.Clone(note)
	copy(bad, []byte{0, 0, 0, 0})
	if _, _, err := ParseNoteFile(bad); !errors.Is(err, errNoCodes) {
		t.Errorf("zero game code: %v", err)
	}
	h, d, _ := ParseNoteFile(note)
	h[4], h[5] = 0, 0
	if _, err := New().InsertNote(h, d, -1); !errors.Is(err, errNoCodes) {
		t.Errorf("InsertNote with zero publisher code: %v", err)
	}
}

// gameSharkNote is a note with the non-ASCII codes Datel's GameShark writes.
func gameSharkNote() []byte {
	b := testNote(1, 9)
	copy(b, []byte{0x3B, 0xAD, 0xD1, 0xE5, 0xFA, 0xDE})
	return b
}

func TestAnyGameCodeIsANote(t *testing.T) {
	img := New()
	if slot := insert(t, img, gameSharkNote(), -1); slot != 0 {
		t.Fatalf("GameShark note went to slot %d", slot)
	}
	n, err := img.Note(0)
	if err != nil || !n.Used || !n.Valid || n.Blocks != 1 {
		t.Fatalf("GameShark note = %+v, %v", n, err)
	}
	out, err := img.ExportNote(0)
	if err != nil || !bytes.Equal(out, gameSharkNote()) {
		t.Fatalf("GameShark note export: %v", err)
	}
}

// damage makes the note in slot unreadable: its start block points nowhere.
func damage(img *Image, slot int) {
	h := img.noteHeader(slot)
	h[6], h[7] = 0, 0
}

func TestDamagedNote(t *testing.T) {
	img := New()
	insert(t, img, testNote(2, 0), -1)
	damage(img, 0)

	n, err := img.Note(0)
	if err != nil || !n.Used || n.Valid {
		t.Fatalf("damaged note = %+v, %v", n, err)
	}
	if _, err := img.NoteData(0); !errors.Is(err, ErrCorrupt) {
		t.Errorf("NoteData: %v", err)
	}
	if _, err := img.ExportNote(0); !errors.Is(err, ErrCorrupt) {
		t.Errorf("ExportNote: %v", err)
	}
	if err := img.DeleteNote(0); !errors.Is(err, ErrCorrupt) {
		t.Errorf("DeleteNote: %v", err)
	}
	if _, err := img.NoteData(1); !errors.Is(err, ErrSlotEmpty) {
		t.Errorf("NoteData of a free slot: %v", err)
	}

	// Replacing it on purpose works; its blocks can't be traced, so they stay used.
	insert(t, img, testNote(1, 1), 0)
	if n, _ := img.Note(0); !n.Valid || n.Blocks != 1 {
		t.Fatalf("replacement note = %+v", n)
	}
	if free, _ := img.FreeBlocks(); free != TotalBlocks-3 {
		t.Errorf("free = %d, want %d", free, TotalBlocks-3)
	}
}

// The first free slot must skip every slot that is in use, even ones this
// tool can't read, instead of overwriting them.
func TestInsertSkipsUsedSlots(t *testing.T) {
	img := New()
	insert(t, img, gameSharkNote(), -1)
	insert(t, img, testNote(1, 1), -1)
	damage(img, 1)
	before := bytes.Clone(img.Data[noteTableOffset : noteTableOffset+2*noteHeaderSize])

	if slot := insert(t, img, testNote(1, 2), -1); slot != 2 {
		t.Fatalf("inserted into slot %d, want 2", slot)
	}
	if !bytes.Equal(img.Data[noteTableOffset:noteTableOffset+2*noteHeaderSize], before) {
		t.Error("headers of slots 0-1 changed")
	}
}

func TestFormats(t *testing.T) {
	img := New()
	insert(t, img, testNote(2, 1), -1)
	img.Comments[0] = "hello world"

	for _, f := range []Format{FormatMPK, FormatMPK4, FormatN64} {
		b, err := img.Encode(f)
		if err != nil {
			t.Fatal(err)
		}
		got := Decode(b)
		if got.Format != f || got.Data != img.Data {
			t.Errorf("%v: round trip failed (format %v)", f, got.Format)
		}
		if f == FormatN64 && got.Comments != img.Comments {
			t.Errorf("comments = %q", got.Comments)
		}
	}
	if Decode(make([]byte, 1000)).Format != FormatInvalid {
		t.Error("odd-sized file should have an invalid format")
	}
	if _, err := img.Encode(FormatInvalid); err == nil {
		t.Error("encoding an invalid format succeeded")
	}
	if FormatForFilename("x.N64") != FormatN64 || FormatForFilename("x.mpk") != FormatMPK {
		t.Error("FormatForFilename")
	}
	if f, err := ParseFormat("MPK4"); err != nil || f != FormatMPK4 {
		t.Error("ParseFormat")
	}
}

func TestCharset(t *testing.T) {
	cases := map[byte]rune{0x00: ' ', 0x0F: ' ', 0x10: '0', 0x1A: 'A', 0x34: '!', 0x41: '@',
		0x42: '。', 0x47: 'ゥ', 0x94: 'ポ', 0x95: ' ', 0xFF: ' ',
		0x37: '\'', 0x43: '゛', 0x44: '゜'}
	for c, want := range cases {
		if got := decodeChar(c); got != want {
			t.Errorf("decodeChar(0x%02x) = %q, want %q", c, got, want)
		}
	}
}
