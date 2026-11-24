// SPDX-License-Identifier: GPL-3.0-or-later

package mempak

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
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
	for _, off := range idBlockOffsets {
		if !idBlockValid(img.sector(0)[off : off+idBlockSize]) {
			t.Fatalf("format template ID block at 0x%02X fails its checksums", off)
		}
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

// setIDChecksums recomputes the two checksums of an ID block.
func setIDChecksums(b []byte) {
	var sum uint16
	for i := 0; i < 0x1C; i += 2 {
		sum += uint16(b[i])<<8 | uint16(b[i+1])
	}
	c2 := 0xFFF2 - sum
	b[0x1C], b[0x1D], b[0x1E], b[0x1F] = byte(sum>>8), byte(sum), byte(c2>>8), byte(c2)
}

// setBanks sets the bank count in all four ID block copies.
func setBanks(img *Image, n byte) {
	for _, off := range idBlockOffsets {
		b := img.sector(0)[off : off+idBlockSize]
		b[idBankCount] = n
		setIDChecksums(b)
	}
}

func TestMultiBank(t *testing.T) {
	img := New()
	if n, err := img.Banks(); err != nil || n != 1 {
		t.Fatalf("standard pak: banks = %d, %v", n, err)
	}

	setBanks(img, 4)
	if n, _ := img.Banks(); n != 4 {
		t.Errorf("banks = %d, want 4", n)
	}
	if err := img.Validate(); !errors.Is(err, ErrMultiBank) {
		t.Errorf("Validate: %v", err)
	}
	if _, err := img.Note(0); !errors.Is(err, ErrMultiBank) {
		t.Errorf("Note: %v", err)
	}

	// A 128 KiB dump of a 4-bank pak loads as raw data (WrongSize); its
	// first 32 KiB must not be read as a whole pak.
	b := append(bytes.Clone(img.Data[:]), make([]byte, 3*Size)...)
	if err := Decode(b).Validate(); !errors.Is(err, ErrMultiBank) {
		t.Errorf("128 KiB 4-bank dump: %v", err)
	}

	setBanks(img, 0) // read as 1
	if err := img.Validate(); err != nil {
		t.Errorf("bank count 0: %v", err)
	}
}

func TestIDBlockFallback(t *testing.T) {
	img := New()
	if d, err := img.DamagedIDBlocks(); err != nil || len(d) != 0 {
		t.Fatalf("fresh pak: damaged = %v, %v", d, err)
	}

	h := img.sector(0)
	h[0x20+0x1C] ^= 0xFF // primary: bad checksum 1
	// Backup 2: valid checksums, but a different serial than the others.
	h[0x80] = 0x12
	setIDChecksums(h[0x80 : 0x80+idBlockSize])

	if err := img.Validate(); err != nil {
		t.Fatalf("pak with intact backups rejected: %v", err)
	}
	// Backup 1 is the first intact copy, so backup 2 counts as damaged too.
	if d, _ := img.DamagedIDBlocks(); !slices.Equal(d, []int{0, 2}) {
		t.Errorf("damaged = %v, want [0 2]", d)
	}

	for _, off := range idBlockOffsets {
		h[off+0x1E] ^= 0xFF // bad checksum 2 everywhere
	}
	if err := img.Validate(); !errors.Is(err, ErrNotFormatted) {
		t.Fatalf("all ID blocks damaged: %v", err)
	}

	// Four matching copies are not enough: they must also pass the checksums.
	img = New()
	clear(img.sector(0))
	if err := img.Validate(); !errors.Is(err, ErrNotFormatted) {
		t.Fatalf("zeroed ID sector: %v", err)
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

func TestImageFile(t *testing.T) {
	img := New()
	insert(t, img, testNote(2, 1), -1)
	path := filepath.Join(t.TempDir(), "pak.mpk")
	if err := img.SaveFile(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.WrongSize() || got.Data != img.Data {
		t.Fatal("round trip through a .mpk file failed")
	}
	if b, _ := os.ReadFile(path); len(b) != Size {
		t.Fatalf("saved %d bytes, want %d", len(b), Size)
	}

	// Any other size (DexDrive .n64, old 128 KiB MPK4, truncated files...) is
	// read as raw data for inspection, and can't be saved back.
	for _, n := range []int{0, 1000, 0x9040, 4 * Size} {
		b := make([]byte, n)
		copy(b, img.Data[:])
		got := Decode(b)
		if !got.WrongSize() {
			t.Errorf("%d-byte file not flagged as the wrong size", n)
		}
		if err := got.SaveFile(path); !errors.Is(err, ErrWrongSize) {
			t.Errorf("saving a %d-byte file's image: %v", n, err)
		}
	}
	if New().WrongSize() {
		t.Error("New() flagged as the wrong size")
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

// TestFormatTOC checks that a blank TOC matches factory paks: reserved entries
// 0-4 are 0, every data entry is free, and the checksum (0x71) is the same
// whether it is summed over entries 5-127 or over every byte from offset 2.
func TestFormatTOC(t *testing.T) {
	img := New()
	for _, s := range []int{1, 2} {
		sec := img.sector(s)
		if sec[1] != 0x71 {
			t.Errorf("TOC %d checksum = 0x%02x, want 0x71", s, sec[1])
		}
		if sec[0] != 0 {
			t.Errorf("TOC %d byte 0 = 0x%02x, want 0", s, sec[0])
		}
		for i := 1; i < blockValidFirst; i++ {
			if sec[2*i] != 0 || sec[2*i+1] != 0 {
				t.Errorf("TOC %d reserved entry %d = %02x %02x, want 00 00", s, i, sec[2*i], sec[2*i+1])
			}
		}
		for i := blockValidFirst; i <= blockValidLast; i++ {
			if sec[2*i] != 0 || sec[2*i+1] != blockEmpty {
				t.Errorf("TOC %d entry %d = %02x %02x, want 00 03", s, i, sec[2*i], sec[2*i+1])
			}
		}
		var sum byte
		for _, b := range sec[2:] {
			sum += b
		}
		if sum != sec[1] {
			t.Errorf("TOC %d: sum of bytes 2..255 = 0x%02x, want stored checksum 0x%02x", s, sum, sec[1])
		}
	}
	if n, err := img.FreeBlocks(); err != nil || n != TotalBlocks {
		t.Errorf("FreeBlocks = %d, %v; want %d", n, err, TotalBlocks)
	}
}

func TestReadNoteFile(t *testing.T) {
	note := testNote(3, 0)
	n, data, err := ReadNoteFile(note)
	if err != nil {
		t.Fatal(err)
	}
	if string(n.GameCode()) != "NSME" || string(n.Publisher()) != "01" || n.Name() != "TEST" {
		t.Errorf("got game %q publisher %q name %q", n.GameCode(), n.Publisher(), n.Name())
	}
	if n.Blocks != 3 || !bytes.Equal(data, note[noteHeaderSize:]) {
		t.Errorf("got %d blocks, %d data bytes", n.Blocks, len(data))
	}
	if !bytes.Equal(n.Header[:], note[:noteHeaderSize]) {
		t.Error("header not kept verbatim")
	}
	if _, _, err := ReadNoteFile(note[:100]); err == nil {
		t.Error("partial block accepted")
	}
}

func TestSections(t *testing.T) {
	img := New()
	insert(t, img, testNote(2, 0), -1) // blocks 5-6
	insert(t, img, testNote(1, 1), -1) // block 7
	// A damaged note: slot 1 loses its start block, so block 7 is orphaned.
	img.noteHeader(1)[6], img.noteHeader(1)[7] = 0, 0

	got := img.Sections()
	want := []string{
		"Sector 0: ID sector",
		"Sector 1: TOC",
		"Sector 2: backup TOC",
		"Sectors 3-4: note table",
		// Blocks are linked in reverse, so block 5 holds the note's last part.
		"Blocks 5-6: note 0 'TEST', parts 2-1 of 2",
		"Block 7: used, but not part of a readable note",
		"Blocks 8-127: free",
	}
	var labels []string
	total := 0
	for _, s := range got {
		labels = append(labels, s.Label)
		if s.Offset != total {
			t.Errorf("%s: offset %#x, want %#x", s.Label, s.Offset, total)
		}
		total += len(s.Data)
	}
	if !slices.Equal(labels, want) {
		t.Errorf("labels:\n got %q\nwant %q", labels, want)
	}
	if total != Size {
		t.Errorf("sections cover %d bytes, want %d", total, Size)
	}

	for i := BlockSize; i < 3*BlockSize; i++ {
		img.Data[i] = 0xFF // both TOCs fail their checksum
	}
	got = img.Sections()
	if l := got[len(got)-1].Label; l != "Blocks 5-127: data (no valid TOC, so the notes' blocks are unknown)" {
		t.Errorf("no TOC: last label %q", l)
	}
	if got[1].Label != "Sector 1: TOC (bad checksum)" {
		t.Errorf("no TOC: %q", got[1].Label)
	}
}

func TestIDSectorFields(t *testing.T) {
	img := New()
	got := img.IDSectorFields()
	type row struct {
		off   int
		name  string
		value string
		subs  int
	}
	want := []row{
		{0x00, "label", "81 01 02 03 04 05 06 07 …", 0},
		{0x20, "ID block (primary)", "in use", 7},
		{0x40, "unused", "all 0x00", 0},
		{0x60, "ID block (backup 1)", "identical to the copy in use", 0},
		{0x80, "ID block (backup 2)", "identical to the copy in use", 0},
		{0xA0, "unused", "all 0x00", 0},
		{0xC0, "ID block (backup 3)", "identical to the copy in use", 0},
		{0xE0, "unused", "all 0x00", 0},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d fields, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Offset != w.off || len(g.Data) != 32 || g.Name != w.name || g.Value != w.value || len(g.Fields) != w.subs {
			t.Errorf("field %d: got %#x %q %q (%d sub-fields), want %#x %q %q (%d)",
				i, g.Offset, g.Name, g.Value, len(g.Fields), w.off, w.name, w.value, w.subs)
		}
	}
	primary := map[string]string{}
	for _, f := range got[1].Fields {
		primary[f.Name] = f.Value
	}
	for name, v := range map[string]string{
		"device ID":  "0xff (bit 0 set: controller pak)",
		"bank count": "1 (32 KiB)",
		"checksum 1": "0x6625 (ok)",
		"checksum 2": "0x99cd (ok)",
	} {
		if primary[name] != v {
			t.Errorf("primary %s: got %q, want %q", name, primary[name], v)
		}
	}

	// Damage the primary: backup 1 takes over, and the primary is expanded.
	img.Data[0x20] ^= 0xFF
	got = img.IDSectorFields()
	if got[1].Value != "damaged: bad checksum" || len(got[1].Fields) != 7 {
		t.Errorf("damaged primary: %q, %d sub-fields", got[1].Value, len(got[1].Fields))
	}
	if v := got[1].Fields[5].Value; v != "0x6625 (bad: the words at 0x00-0x1b sum to 0x6725)" {
		t.Errorf("damaged primary checksum 1: %q", v)
	}
	if got[3].Value != "in use" || got[4].Value != "identical to the copy in use" {
		t.Errorf("backups: %q, %q", got[3].Value, got[4].Value)
	}
}

func TestNoteFileHeaderFields(t *testing.T) {
	note := testNote(1, 0)
	note[0x0C] = 0x1A // extension 'A'
	got := NoteFileHeaderFields(note)
	want := []struct {
		off   int
		size  int
		name  string
		value string
	}{
		{0x00, 4, "game code", "NSME (Super Mario 64 (USA))"},
		{0x04, 2, "publisher code", "01"},
		{0x06, 2, "start inode", "0xcafe (note file marker; set when the note is inserted)"},
		{0x08, 1, "status", "0x00"},
		{0x09, 1, "unused", "0x00"},
		{0x0A, 2, "unused", "all 0x00"},
		{0x0C, 4, "extension", "'A' (1a 00 00 00)"},
		{0x10, 16, "name", `"TEST"`},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d fields, want %d", len(got), len(want))
	}
	total := 0
	for i, w := range want {
		g := got[i]
		if g.Offset != w.off || len(g.Data) != w.size || g.Name != w.name || g.Value != w.value {
			t.Errorf("field %d: got %#x+%d %q %q, want %#x+%d %q %q",
				i, g.Offset, len(g.Data), g.Name, g.Value, w.off, w.size, w.name, w.value)
		}
		total += len(g.Data)
	}
	if total != noteHeaderSize {
		t.Errorf("fields cover %d bytes, want %d", total, noteHeaderSize)
	}

	gs := NoteFileHeaderFields(gameSharkNote())
	if gs[0].Value != "3badd1e5" || gs[1].Value != "fade" {
		t.Errorf("GameShark codes: %q, %q", gs[0].Value, gs[1].Value)
	}
}

// TestIDSectorFieldsCover checks that the sub-fields of every expanded ID
// block cover all of its bytes, so 'inspect pak -x id' shows every byte.
func TestIDSectorFieldsCover(t *testing.T) {
	img := New()
	img.Data[0x80] ^= 1 // damage backup 2, so it is expanded too
	next := 0
	var walk func([]Field)
	walk = func(fs []Field) {
		for _, f := range fs {
			if len(f.Fields) > 0 {
				walk(f.Fields)
				continue
			}
			if f.Offset != next {
				t.Errorf("%s at %#x, want %#x", f.Name, f.Offset, next)
			}
			if !bytes.Equal(f.Data, img.Data[f.Offset:f.Offset+len(f.Data)]) {
				t.Errorf("%s: data does not match the image", f.Name)
			}
			next = f.Offset + len(f.Data)
		}
	}
	walk(img.IDSectorFields())
	if next != BlockSize {
		t.Errorf("fields end at %#x, want %#x", next, BlockSize)
	}
}
