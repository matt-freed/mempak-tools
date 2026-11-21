// SPDX-License-Identifier: GPL-3.0-or-later

package mempak

import (
	"bytes"
	"fmt"
	"strings"
)

// RepairReport lists what Repair changed. Its zero value means the image
// needed no repair.
type RepairReport struct {
	// IDSource is the ID block copy (0-3) the damaged copies were rewritten
	// from, and IDFixed the copies that were rewritten.
	IDSource int
	IDFixed  []int
	// TOCFixed is the TOC sector (1 or 2) rewritten from the other copy,
	// because it failed its checksum or didn't match; 0 if neither was.
	TOCFixed int
	// DamagedNotes are the notes removed because their block chain was
	// broken. Their headers are as they were before the repair.
	DamagedNotes []Note
	// FreedBlocks are blocks that were marked used but belonged to no
	// readable note, and are now free.
	FreedBlocks []int
}

// Changed reports whether the repair changed anything.
func (r *RepairReport) Changed() bool {
	return len(r.IDFixed) > 0 || r.TOCFixed != 0 || len(r.DamagedNotes) > 0 || len(r.FreedBlocks) > 0
}

// Repair fixes what it can of a damaged filesystem, much like the console
// library's osPfsChecker and osPfsRepairId:
//
//   - ID block copies that fail their checksums, or differ from the first
//     intact copy, are rewritten from that copy.
//   - A TOC copy that fails its checksum, or differs from the valid copy, is
//     rewritten from the valid one.
//   - Damaged notes (used slots whose block chain is broken) are removed:
//     their data can't be read, so their header is cleared.
//   - Blocks that are marked used but aren't part of any readable note's
//     chain are freed. That includes the blocks of the removed notes.
//
// Data in removed notes and freed blocks is lost, so callers should keep a
// backup. Repair can't help when no ID block copy or no TOC copy is intact,
// or on multi-bank paks; it then returns the error and changes nothing.
func (img *Image) Repair() (RepairReport, error) {
	var r RepairReport
	// Validate covers both unrepairable cases: no intact ID block (or a
	// multi-bank one), and no TOC with a valid checksum.
	if err := img.Validate(); err != nil {
		return r, err
	}

	// ID blocks.
	src, _ := img.idBlock()
	h := img.sector(0)
	good := bytes.Clone(h[idBlockOffsets[src] : idBlockOffsets[src]+idBlockSize])
	r.IDSource = src
	for i, off := range idBlockOffsets {
		if !bytes.Equal(h[off:off+idBlockSize], good) {
			copy(h[off:off+idBlockSize], good)
			r.IDFixed = append(r.IDFixed, i)
		}
	}

	t, valid, _ := img.readTOC()
	if !bytes.Equal(img.sector(1), img.sector(2)) {
		r.TOCFixed = 3 - valid
	}

	// Notes: remove damaged ones, and record which blocks readable ones use.
	inUse := make([]bool, numSectors)
	for slot := range NumNotes {
		n := img.note(&t, slot)
		if !n.Used {
			continue
		}
		if !n.Valid {
			r.DamagedNotes = append(r.DamagedNotes, n)
			clear(img.noteHeader(slot))
			continue
		}
		blocks, _ := t.chain(n.inode())
		for _, b := range blocks {
			inUse[b] = true
		}
	}

	// Orphaned blocks.
	for b := blockValidFirst; b <= blockValidLast; b++ {
		if t.next(b) != blockEmpty && !inUse[b] {
			t.setNext(b, blockEmpty)
			r.FreedBlocks = append(r.FreedBlocks, b)
		}
	}

	if r.TOCFixed != 0 || len(r.FreedBlocks) > 0 {
		img.writeTOC(&t, valid)
	}
	return r, nil
}

// String describes the repairs, one per line, as actions ("rewrite", "remove",
// "free"), so it reads the same as a plan or as a record of what was done.
func (r *RepairReport) String() string {
	var b bytes.Buffer
	if len(r.IDFixed) > 0 {
		if len(r.IDFixed) == 1 {
			fmt.Fprintf(&b, "ID block copy %d is damaged: rewrite it from copy %d\n", r.IDFixed[0], r.IDSource)
		} else {
			fmt.Fprintf(&b, "ID block copies %s are damaged: rewrite them from copy %d\n", list(r.IDFixed), r.IDSource)
		}
	}
	if r.TOCFixed != 0 {
		fmt.Fprintf(&b, "TOC copy in sector %d is damaged or out of date: rewrite it from sector %d\n", r.TOCFixed, 3-r.TOCFixed)
	}
	for _, n := range r.DamagedNotes {
		fmt.Fprintf(&b, "Note %d '%s' is damaged (broken block chain): remove it\n", n.Slot, n.Name())
	}
	if len(r.FreedBlocks) == 1 {
		fmt.Fprintf(&b, "Block %d is marked used but belongs to no readable note: free it\n", r.FreedBlocks[0])
	} else if len(r.FreedBlocks) > 1 {
		fmt.Fprintf(&b, "%d blocks are marked used but belong to no readable note: free them (blocks %s)\n",
			len(r.FreedBlocks), list(r.FreedBlocks))
	}
	return b.String()
}

// list formats numbers as "1, 2, 3".
func list(v []int) string {
	s := make([]string, len(v))
	for i, n := range v {
		s[i] = fmt.Sprint(n)
	}
	return strings.Join(s, ", ")
}
