// SPDX-License-Identifier: GPL-3.0-or-later

package mempak

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
)

// repair runs Repair and checks the result is a healthy image that a second
// repair leaves alone.
func repair(t *testing.T, img *Image) RepairReport {
	t.Helper()
	r, err := img.Repair()
	if err != nil {
		t.Fatal(err)
	}
	if err := img.Validate(); err != nil {
		t.Fatalf("repaired image invalid: %v", err)
	}
	if d, _ := img.DamagedIDBlocks(); len(d) > 0 {
		t.Errorf("ID blocks %v still damaged", d)
	}
	if !bytes.Equal(img.sector(1), img.sector(2)) {
		t.Error("TOC copies differ after repair")
	}
	again := *img
	if r2, err := again.Repair(); err != nil || r2.Changed() || again.Data != img.Data {
		t.Errorf("second repair changed something: %+v, %v", r2, err)
	}
	return r
}

func TestRepairHealthy(t *testing.T) {
	img := New()
	insert(t, img, testNote(2, 0), -1)
	before := img.Data
	if r := repair(t, img); r.Changed() || img.Data != before {
		t.Errorf("healthy image changed: %+v", r)
	}
}

func TestRepairIDBlocks(t *testing.T) {
	img := New()
	h := img.sector(0)
	h[0x20+0x1E] ^= 0xFF // primary: bad checksum
	h[0xC0] = 0x12       // backup 3: intact, but a different serial
	setIDChecksums(h[0xC0 : 0xC0+idBlockSize])
	want := bytes.Clone(h[0x60 : 0x60+idBlockSize])

	r := repair(t, img)
	if r.IDSource != 1 || !slices.Equal(r.IDFixed, []int{0, 3}) {
		t.Errorf("report = %+v", r)
	}
	for _, off := range idBlockOffsets {
		if !bytes.Equal(h[off:off+idBlockSize], want) {
			t.Errorf("ID block at 0x%02X not rewritten from backup 1", off)
		}
	}
}

func TestRepairTOC(t *testing.T) {
	img := New()
	insert(t, img, testNote(2, 0), -1)
	img.sector(2)[1] ^= 0xFF // backup TOC: bad checksum
	if r := repair(t, img); r.TOCFixed != 2 {
		t.Errorf("TOCFixed = %d, want 2", r.TOCFixed)
	}

	img.sector(1)[1] ^= 0xFF // primary TOC: bad checksum
	if r := repair(t, img); r.TOCFixed != 1 {
		t.Errorf("TOCFixed = %d, want 1", r.TOCFixed)
	}
	if n, _ := img.Note(0); !n.Valid || n.Blocks != 2 {
		t.Errorf("note lost while repairing the TOC: %+v", n)
	}
}

func TestRepairDamagedNotes(t *testing.T) {
	img := New()
	insert(t, img, testNote(2, 0), -1) // slot 0: blocks 5-6
	insert(t, img, testNote(3, 1), -1) // slot 1: blocks 7-9
	insert(t, img, testNote(1, 2), -1) // slot 2: block 10
	keep, _ := img.NoteData(1)

	damage(img, 0) // start block lost: blocks 5-6 become orphans
	// Slot 2's only block now points at a free block: a broken chain.
	for _, s := range []int{1, 2} {
		var tc toc
		copy(tc[:], img.sector(s))
		tc.setNext(10, 0x7F)
		tc[1] = tc.checksum()
		copy(img.sector(s), tc[:])
	}

	r := repair(t, img)
	var slots []int
	for _, n := range r.DamagedNotes {
		slots = append(slots, n.Slot)
	}
	if !slices.Equal(slots, []int{0, 2}) || !slices.Equal(r.FreedBlocks, []int{5, 6, 10}) {
		t.Errorf("report = %+v", r)
	}
	for _, s := range []int{0, 2} {
		if n, _ := img.Note(s); n.Used {
			t.Errorf("damaged note %d not removed", s)
		}
	}
	if got, _ := img.NoteData(1); !bytes.Equal(got, keep) {
		t.Error("readable note 1 changed")
	}
	if free, _ := img.FreeBlocks(); free != TotalBlocks-3 {
		t.Errorf("free = %d, want %d", free, TotalBlocks-3)
	}
}

// Overwriting a damaged note leaves its blocks allocated; repair gets them back.
func TestRepairAfterOverwrite(t *testing.T) {
	img := New()
	insert(t, img, testNote(4, 0), -1)
	damage(img, 0)
	insert(t, img, testNote(1, 1), 0)
	if free, _ := img.FreeBlocks(); free != TotalBlocks-5 {
		t.Fatalf("setup: free = %d", free)
	}
	r := repair(t, img)
	if len(r.FreedBlocks) != 4 || len(r.DamagedNotes) != 0 {
		t.Errorf("report = %+v", r)
	}
	if free, _ := img.FreeBlocks(); free != TotalBlocks-1 {
		t.Errorf("free = %d, want %d", free, TotalBlocks-1)
	}
}

func TestRepairImpossible(t *testing.T) {
	img := New()
	for _, off := range idBlockOffsets {
		img.sector(0)[off+0x1C] ^= 0xFF
	}
	before := img.Data
	if _, err := img.Repair(); !errors.Is(err, ErrNotFormatted) || img.Data != before {
		t.Errorf("no intact ID block: %v (changed: %v)", err, img.Data != before)
	}

	img = New()
	img.sector(1)[1] ^= 0xFF
	img.sector(2)[1] ^= 0xFF
	before = img.Data
	if _, err := img.Repair(); !errors.Is(err, ErrNotFormatted) || img.Data != before {
		t.Errorf("no valid TOC: %v (changed: %v)", err, img.Data != before)
	}

	img = New()
	setBanks(img, 4)
	if _, err := img.Repair(); !errors.Is(err, ErrMultiBank) {
		t.Errorf("multi-bank: %v", err)
	}
}

func TestRepairReportText(t *testing.T) {
	r := RepairReport{IDSource: 1, IDFixed: []int{2}, FreedBlocks: []int{9}}
	want := "ID block copy 2 is damaged: rewrite it from copy 1\n" +
		"Block 9 is marked used but belongs to no readable note: free it\n"
	if got := r.String(); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	r = RepairReport{IDFixed: []int{0, 3}, FreedBlocks: []int{5, 6}}
	if got := r.String(); !strings.Contains(got, "copies 0, 3 are") || !strings.Contains(got, "2 blocks") {
		t.Errorf("plural forms: %q", got)
	}
}
