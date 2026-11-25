// SPDX-License-Identifier: GPL-3.0-or-later

package mempak

import (
	"errors"
	"fmt"
)

// ErrNeedsRepair means the image has damage that 'mempak repair' must fix
// before it can be defragmented.
var ErrNeedsRepair = errors.New("run 'mempak repair' first")

// DefragMove describes where Defrag put one note.
type DefragMove struct {
	OldSlot, NewSlot int
	Name             string
	// OldBlocks is the note's chain before the defrag. Its new chain is
	// NewFirst, NewFirst+1, ... NewFirst+len(OldBlocks)-1.
	OldBlocks []int
	NewFirst  int
}

// Moved reports whether the note changed slot or blocks.
func (m *DefragMove) Moved() bool {
	if m.OldSlot != m.NewSlot {
		return true
	}
	for i, b := range m.OldBlocks {
		if b != m.NewFirst+i {
			return true
		}
	}
	return false
}

// DefragReport describes what Defrag did.
type DefragReport struct {
	// Notes lists every note, in its new slot order.
	Notes []DefragMove
	// UsedBlocks is the number of blocks the notes use; they are now blocks
	// 5 to 5+UsedBlocks-1. FreeBlocks is the number of free blocks after
	// them, which are all zeroed.
	UsedBlocks, FreeBlocks int
	// Changed is false when the image was already defragmented, so Defrag
	// left every byte as it was.
	Changed bool
}

// Defrag rewrites the filesystem so the notes fill the lowest slots and the
// lowest blocks, in slot order, each note's blocks contiguous and linked in
// ascending order. Free blocks and unused slots are zeroed. Note headers and
// data are kept, apart from each header's start inode.
//
// This is cosmetic: games follow a note's chain wherever it goes, and the pak
// is random-access, so a defragmented pak is no faster.
//
// Defrag refuses an image with damaged notes or orphaned blocks (repair
// fixes those) or with notes that share blocks, and then changes nothing.
func (img *Image) Defrag() (DefragReport, error) {
	var r DefragReport
	t, valid, err := img.readTOC()
	if err != nil {
		return r, err
	}

	type entry struct {
		header [noteHeaderSize]byte
		data   []byte
		move   DefragMove
	}
	var notes []entry
	owner := make([]int, numSectors) // slot+1 of the note using each block
	for slot := range NumNotes {
		n := img.note(&t, slot)
		if !n.Used {
			continue
		}
		if !n.Valid {
			return r, fmt.Errorf("note %d '%s' is damaged (broken block chain): %w", slot, n.Name(), ErrNeedsRepair)
		}
		blocks, _ := t.chain(n.inode())
		data := make([]byte, 0, len(blocks)*BlockSize)
		for _, b := range blocks {
			if o := owner[b]; o != 0 {
				return r, fmt.Errorf("%w: notes %d and %d share block %d", ErrCorrupt, o-1, slot, b)
			}
			owner[b] = slot + 1
			data = append(data, img.sector(b)...)
		}
		notes = append(notes, entry{header: n.Header, data: data,
			move: DefragMove{OldSlot: slot, Name: n.Name(), OldBlocks: blocks}})
	}

	orphans := 0
	for b := blockValidFirst; b <= blockValidLast; b++ {
		if t.next(b) != blockEmpty && owner[b] == 0 {
			orphans++
		}
	}
	if orphans > 0 {
		return r, fmt.Errorf("%d blocks are marked used but belong to no note: %w", orphans, ErrNeedsRepair)
	}

	// Build the new layout on a copy, so img changes only once it's complete.
	out := *img
	nt := t
	for b := blockValidFirst; b <= blockValidLast; b++ {
		nt.setNext(b, blockEmpty)
	}
	next := blockValidFirst
	for slot, e := range notes {
		k := len(e.data) / BlockSize
		for i := range k {
			b := next + i
			if i == k-1 {
				nt.setNext(b, blockLast)
			} else {
				nt.setNext(b, b+1)
			}
			copy(out.sector(b), e.data[i*BlockSize:(i+1)*BlockSize])
		}
		h := e.header
		h[6], h[7] = byte(next>>8), byte(next)
		copy(out.noteHeader(slot), h[:])
		e.move.NewSlot, e.move.NewFirst = slot, next
		r.Notes = append(r.Notes, e.move)
		next += k
	}
	for slot := len(notes); slot < NumNotes; slot++ {
		clear(out.noteHeader(slot))
	}
	for b := next; b < numSectors; b++ {
		clear(out.sector(b))
	}
	out.writeTOC(&nt, valid)

	r.UsedBlocks = next - blockValidFirst
	r.FreeBlocks = numSectors - next
	r.Changed = out.Data != img.Data
	img.Data = out.Data
	return r, nil
}
