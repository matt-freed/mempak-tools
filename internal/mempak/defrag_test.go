// SPDX-License-Identifier: GPL-3.0-or-later

package mempak

import (
	"bytes"
	"errors"
	"slices"
	"testing"
)

// setEntry sets a TOC entry in both TOC copies and fixes their checksums.
func setEntry(img *Image, block, v int) {
	for _, s := range []int{1, 2} {
		var tc toc
		copy(tc[:], img.sector(s))
		tc.setNext(block, v)
		tc[1] = tc.checksum()
		copy(img.sector(s), tc[:])
	}
}

func TestDefrag(t *testing.T) {
	img := New()
	a, b, c, d := testNote(2, 1), testNote(3, 2), testNote(1, 3), testNote(4, 4)
	insert(t, img, a, 1) // blocks 5-6, slot 1 (slot 0 stays empty)
	insert(t, img, b, 2) // blocks 7-9
	insert(t, img, c, 4) // block 10, slot 4 (slot 3 stays empty)
	if err := img.DeleteNote(2); err != nil {
		t.Fatal(err)
	}
	insert(t, img, d, 6) // blocks 7-9, 11
	for blk := 20; blk < numSectors; blk++ {
		img.sector(blk)[0] = 0xAA // stale data in free blocks
	}
	freeBefore, _ := img.FreeBlocks()

	r, err := img.Defrag()
	if err != nil {
		t.Fatal(err)
	}
	if !r.Changed || len(r.Notes) != 3 || r.UsedBlocks != 7 || r.FreeBlocks != TotalBlocks-7 {
		t.Fatalf("report = %+v", r)
	}
	if err := img.Validate(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(img.sector(1), img.sector(2)) {
		t.Fatal("TOC copies differ")
	}
	if free, _ := img.FreeBlocks(); free != freeBefore {
		t.Fatalf("free = %d, want %d", free, freeBefore)
	}

	// Slots 0-2 hold A, C, D in their old order, with packed ascending chains.
	var tc toc
	copy(tc[:], img.sector(1))
	want := []struct {
		note  []byte
		chain []int
	}{{a, []int{5, 6}}, {c, []int{7}}, {d, []int{8, 9, 10, 11}}}
	for slot, w := range want {
		out, err := img.ExportNote(slot)
		if err != nil || !bytes.Equal(out, w.note) {
			t.Fatalf("slot %d: note differs (%v)", slot, err)
		}
		n, _ := img.Note(slot)
		if chain, _ := tc.chain(n.inode()); !slices.Equal(chain, w.chain) {
			t.Fatalf("slot %d chain = %v, want %v", slot, chain, w.chain)
		}
	}
	for slot := 3; slot < NumNotes; slot++ {
		if !bytes.Equal(img.noteHeader(slot), make([]byte, noteHeaderSize)) {
			t.Fatalf("slot %d not cleared", slot)
		}
	}
	for blk := 12; blk < numSectors; blk++ {
		if !bytes.Equal(img.sector(blk), make([]byte, BlockSize)) {
			t.Fatalf("free block %d not zeroed", blk)
		}
	}
	if m := r.Notes[2]; m.OldSlot != 6 || m.NewSlot != 2 || !slices.Equal(m.OldBlocks, []int{7, 8, 9, 11}) || m.NewFirst != 8 {
		t.Fatalf("move = %+v", m)
	}

	before := img.Data
	if r, err := img.Defrag(); err != nil || r.Changed || img.Data != before {
		t.Fatalf("second defrag changed the image: %+v, %v", r, err)
	}
	if r, err := New().Defrag(); err != nil || r.Changed {
		t.Fatalf("blank image: %+v, %v", r, err)
	}
}

func TestDefragRefusesDamage(t *testing.T) {
	base := New()
	insert(t, base, testNote(2, 0), -1) // blocks 5-6
	insert(t, base, testNote(2, 1), -1) // blocks 7-8

	damaged := *base
	setEntry(&damaged, 6, 6) // loop
	orphan := *base
	setEntry(&orphan, 20, blockLast)
	crossed := *base
	setEntry(&crossed, 8, 5) // note 1 runs into note 0: 7 -> 8 -> 5 -> 6

	for name, tc := range map[string]struct {
		img  Image
		want error
	}{
		"damaged note": {damaged, ErrNeedsRepair},
		"orphan block": {orphan, ErrNeedsRepair},
		"cross-linked": {crossed, ErrCorrupt},
	} {
		img := tc.img
		before := img.Data
		if _, err := img.Defrag(); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
		if img.Data != before {
			t.Errorf("%s: image changed", name)
		}
	}
}
