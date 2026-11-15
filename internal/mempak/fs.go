// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2007-2024 Raphael Assenat (gcn64tools); Go port (C) 2026 Matt Freed.
// The filesystem logic is adapted from libdragon's mempak.c (public domain).

package mempak

import (
	"errors"
	"fmt"
	"strings"
)

const (
	// BlockSize is the size in bytes of a mempak block (sector).
	BlockSize = 256
	// TotalBlocks is the number of blocks available for note data.
	TotalBlocks = 123

	numSectors = Size / BlockSize

	// Inode values in the TOC.
	blockLast       = 0x01 // last block of a note
	blockEmpty      = 0x03 // free block
	blockValidFirst = 0x05 // first block that can hold note data
	blockValidLast  = 0x7F // last block that can hold note data

	noteTableOffset = 3 * BlockSize
	noteHeaderSize  = 32
)

var (
	// ErrNotFormatted means the image has no valid header or TOC.
	ErrNotFormatted = errors.New("mempak not formatted or corrupted")
	// ErrCorrupt means a note's block chain is broken.
	ErrCorrupt = errors.New("mempak filesystem is corrupted")
	// ErrNoSpace means there are not enough free blocks or note slots.
	ErrNoSpace = errors.New("not enough space in mempak")
	// ErrSlotEmpty means the note slot holds no note.
	ErrSlotEmpty = errors.New("note slot is empty")
	// ErrBadSlot means the note slot number is out of range.
	ErrBadSlot = errors.New("note slot out of range")
)

// sector-0 template used by format, as found on official paks.
var formatHeader = [BlockSize]byte{
	0x81, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
	0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
	0xff, 0xff, 0xff, 0xff, 0x05, 0x1a, 0x5f, 0x13, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01, 0xff, 0x66, 0x25, 0x99, 0xcd,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0xff, 0xff, 0xff, 0xff, 0x05, 0x1a, 0x5f, 0x13, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01, 0xff, 0x66, 0x25, 0x99, 0xcd,
	0xff, 0xff, 0xff, 0xff, 0x05, 0x1a, 0x5f, 0x13, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01, 0xff, 0x66, 0x25, 0x99, 0xcd,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0xff, 0xff, 0xff, 0xff, 0x05, 0x1a, 0x5f, 0x13, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01, 0xff, 0x66, 0x25, 0x99, 0xcd,
}

// toc is a copy of a TOC sector. Entry i (the "next block" pointer of
// block i) is at byte 2*i+1.
type toc [BlockSize]byte

func (t *toc) next(block int) int       { return int(t[block<<1|1]) }
func (t *toc) setNext(block int, v int) { t[block<<1|1] = byte(v) }

func (t *toc) checksum() byte {
	var sum byte
	for i := blockValidFirst; i < 128; i++ {
		sum += byte(t.next(i))
	}
	return sum
}

func (t *toc) freeBlocks() int {
	n := 0
	for i := blockValidFirst; i <= blockValidLast; i++ {
		if t.next(i) == blockEmpty {
			n++
		}
	}
	return n
}

// chain returns the blocks of the note starting at inode, in order.
func (t *toc) chain(inode int) ([]int, error) {
	if inode < blockValidFirst || inode > blockValidLast {
		return nil, ErrCorrupt
	}
	blocks := []int{inode}
	for cur := inode; len(blocks) <= TotalBlocks; {
		switch n := t.next(cur); {
		case n == blockLast:
			return blocks, nil
		case n == blockEmpty:
			return nil, ErrCorrupt // a note cannot contain free blocks
		case n < blockValidFirst || n > blockValidLast:
			return nil, ErrCorrupt
		default:
			cur = n
			blocks = append(blocks, cur)
		}
	}
	return nil, ErrCorrupt // loop in the chain
}

func (img *Image) sector(n int) []byte {
	return img.Data[n*BlockSize : (n+1)*BlockSize]
}

func (img *Image) noteHeader(slot int) []byte {
	off := noteTableOffset + slot*noteHeaderSize
	return img.Data[off : off+noteHeaderSize]
}

// validTOC returns the number of the first valid TOC sector (1 or 2).
func (img *Image) validTOC() (int, error) {
	h := img.sector(0)
	if string(h[0x20:0x30]) != string(h[0x60:0x70]) ||
		string(h[0x80:0x90]) != string(h[0xC0:0xD0]) ||
		string(h[0x20:0x30]) != string(h[0x80:0x90]) {
		return 0, ErrNotFormatted
	}
	for _, n := range []int{1, 2} {
		var t toc
		copy(t[:], img.sector(n))
		if t.checksum() == t[1] {
			return n, nil
		}
	}
	return 0, ErrNotFormatted
}

func (img *Image) readTOC() (toc, int, error) {
	var t toc
	n, err := img.validTOC()
	if err != nil {
		return t, 0, err
	}
	copy(t[:], img.sector(n))
	return t, n, nil
}

// writeTOC stores t to both TOC sectors, the alternate one first so that a
// valid TOC exists at every step.
func (img *Image) writeTOC(t *toc, valid int) {
	t[1] = t.checksum()
	copy(img.sector(3-valid), t[:])
	copy(img.sector(valid), t[:])
}

// Validate checks that the image holds a formatted filesystem.
func (img *Image) Validate() error {
	_, err := img.validTOC()
	return err
}

// FormatFS writes a blank filesystem, erasing all notes.
func (img *Image) FormatFS() {
	copy(img.sector(0), formatHeader[:])
	clear(img.sector(3))
	clear(img.sector(4))
	var t toc
	for i := range 128 {
		t.setNext(i, blockEmpty)
	}
	t[1] = t.checksum()
	copy(img.sector(1), t[:])
	copy(img.sector(2), t[:])
}

// FreeBlocks returns the number of free blocks.
func (img *Image) FreeBlocks() (int, error) {
	t, _, err := img.readTOC()
	if err != nil {
		return 0, err
	}
	return t.freeBlocks(), nil
}

// Note describes one slot of the note table.
type Note struct {
	Slot   int
	Header [noteHeaderSize]byte
	// Blocks is the number of blocks the note uses.
	Blocks int
	// Valid is false for a free (or unreadable) slot.
	Valid bool
}

// GameCode returns the 4-byte game code. Its last byte is the region.
func (n *Note) GameCode() []byte { return n.Header[0:4] }

// Publisher returns the 2-byte publisher code.
func (n *Note) Publisher() []byte { return n.Header[4:6] }

func (n *Note) region() byte { return n.Header[3] }
func (n *Note) inode() int   { return int(n.Header[6])<<8 | int(n.Header[7]) }

// RawName returns the 16-character name and the first extension character,
// joined by a dot and with their padding left in.
func (n *Note) RawName() string {
	var b strings.Builder
	for _, c := range n.Header[0x10:0x20] {
		b.WriteRune(decodeChar(c))
	}
	b.WriteByte('.')
	b.WriteRune(decodeChar(n.Header[0x0C]))
	return b.String()
}

// Name returns the note name as "NAME" or "NAME.EXT", without padding.
func (n *Note) Name() string {
	name, ext := n.RawName(), ""
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name, ext = name[:i], strings.Trim(name[i+1:], " ")
	}
	name = strings.TrimRight(name, " ")
	if ext != "" {
		return name + "." + ext
	}
	return name
}

func validRegion(r byte) bool {
	switch r {
	case 0x00, 0x37, 0x41, 0x44, 0x45, 0x46, 0x49, 0x4A, 0x50, 0x53, 0x55, 0x58, 0x59:
		return true
	}
	return false
}

// headerValid reports whether a note-table entry describes a note.
func headerValid(h []byte) bool {
	inode := int(h[6])<<8 | int(h[7])
	return inode >= blockValidFirst && inode <= blockValidLast && validRegion(h[3])
}

// Note returns the note in slot (0-15).
func (img *Image) Note(slot int) (Note, error) {
	if slot < 0 || slot >= NumNotes {
		return Note{}, ErrBadSlot
	}
	t, _, err := img.readTOC()
	if err != nil {
		return Note{}, err
	}
	return img.note(&t, slot), nil
}

func (img *Image) note(t *toc, slot int) Note {
	n := Note{Slot: slot}
	copy(n.Header[:], img.noteHeader(slot))
	if !headerValid(n.Header[:]) {
		return n
	}
	if blocks, err := t.chain(n.inode()); err == nil {
		n.Blocks = len(blocks)
		n.Valid = true
	}
	return n
}

// NoteData returns the data of the note in slot.
func (img *Image) NoteData(slot int) ([]byte, error) {
	if slot < 0 || slot >= NumNotes {
		return nil, ErrBadSlot
	}
	t, _, err := img.readTOC()
	if err != nil {
		return nil, err
	}
	n := img.note(&t, slot)
	if !n.Valid {
		return nil, ErrSlotEmpty
	}
	blocks, err := t.chain(n.inode())
	if err != nil {
		return nil, err
	}
	data := make([]byte, 0, len(blocks)*BlockSize)
	for _, b := range blocks {
		data = append(data, img.sector(b)...)
	}
	return data, nil
}

// DeleteNote frees the note in slot and its blocks.
func (img *Image) DeleteNote(slot int) error {
	if slot < 0 || slot >= NumNotes {
		return ErrBadSlot
	}
	t, valid, err := img.readTOC()
	if err != nil {
		return err
	}
	n := img.note(&t, slot)
	if !n.Valid {
		return ErrSlotEmpty
	}
	blocks, err := t.chain(n.inode())
	if err != nil {
		return err
	}
	clear(img.noteHeader(slot))
	for _, b := range blocks {
		t.setNext(b, blockEmpty)
	}
	img.writeTOC(&t, valid)
	return nil
}

// InsertNote stores a note with the given 32-byte header and data, which
// must be a whole number of blocks.
//
// With slot set to -1 the note goes to the first free slot. Otherwise it goes
// to that slot, replacing any note already there; if replaceOnly is set the
// slot must already hold a note. The header is stored as is apart from the
// inode, so the note keeps its game code, publisher code and name.
//
// It returns the slot used.
func (img *Image) InsertNote(header [noteHeaderSize]byte, data []byte, slot int, replaceOnly bool) (int, error) {
	if slot < -1 || slot >= NumNotes {
		return 0, ErrBadSlot
	}
	if len(data) == 0 || len(data)%BlockSize != 0 {
		return 0, fmt.Errorf("note data is not a whole number of blocks")
	}
	if !validRegion(header[3]) {
		return 0, fmt.Errorf("note has an invalid region code 0x%02x", header[3])
	}
	nblocks := len(data) / BlockSize

	t, _, err := img.readTOC()
	if err != nil {
		return 0, err
	}

	free := t.freeBlocks()
	if slot >= 0 {
		// Overwriting a note releases its blocks.
		if old := img.note(&t, slot); old.Valid {
			free += old.Blocks
		} else if replaceOnly {
			return 0, ErrSlotEmpty
		}
	} else {
		slot = -1
		for i := range NumNotes {
			if !headerValid(img.noteHeader(i)) {
				slot = i
				break
			}
		}
		if slot < 0 {
			return 0, fmt.Errorf("%w: no free note slot", ErrNoSpace)
		}
	}
	if nblocks > free {
		return 0, fmt.Errorf("%w: note is %d blocks and only %d free blocks in mempak", ErrNoSpace, nblocks, free)
	}

	if old := img.note(&t, slot); old.Valid {
		if err := img.DeleteNote(slot); err != nil {
			return 0, err
		}
	} else {
		// The slot may hold an unreadable entry; it is free either way.
		clear(img.noteHeader(slot))
	}

	t, valid, err := img.readTOC()
	if err != nil {
		return 0, err
	}

	// Take the lowest free blocks, linked in reverse: the last block taken
	// becomes the note's first block (its inode).
	inode, taken := blockLast, 0
	for i := blockValidFirst; i <= blockValidLast && taken < nblocks; i++ {
		if t.next(i) == blockEmpty {
			t.setNext(i, inode)
			inode = i
			taken++
		}
	}
	if taken < nblocks {
		return 0, ErrNoSpace
	}

	blocks, err := t.chain(inode)
	if err != nil {
		return 0, err
	}
	for i, b := range blocks {
		copy(img.sector(b), data[i*BlockSize:(i+1)*BlockSize])
	}
	img.writeTOC(&t, valid)

	header[6] = byte(inode >> 8)
	header[7] = byte(inode)
	copy(img.noteHeader(slot), header[:])
	return slot, nil
}
