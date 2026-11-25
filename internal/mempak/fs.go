// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2007-2024 Raphael Assenat (gcn64tools); Go port (C) 2026 Matt Freed.
// The filesystem logic is adapted from libdragon's mempak.c (public domain).

package mempak

import (
	"bytes"
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
	// ErrMultiBank means the pak's ID block declares more than one 32 KiB
	// bank. Such a pak has a single filesystem spanning all its banks, with a
	// larger TOC and the note table after it, which this package can't read.
	ErrMultiBank = errors.New("multi-bank controller paks are not supported, only standard 32 KiB paks")
	// ErrCorrupt means a note's block chain is broken.
	ErrCorrupt = errors.New("mempak filesystem is corrupted")
	// ErrNoSpace means there are not enough free blocks or note slots.
	ErrNoSpace = errors.New("not enough space in mempak")
	// ErrSlotEmpty means the note slot holds no note.
	ErrSlotEmpty = errors.New("note slot is empty")
	// ErrBadSlot means the note slot number is out of range.
	ErrBadSlot = errors.New("note slot out of range")

	errNoCodes = errors.New("note has a zero game code or publisher code")
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

// idBlockOffsets are the positions in sector 0 of the four copies of the ID
// block: the primary copy, then three backups.
var idBlockOffsets = [...]int{0x20, 0x60, 0x80, 0xC0}

const (
	idBlockSize = 32
	idBankCount = 0x1A // offset of the bank count in an ID block
)

// idBlockValid checks an ID block's two checksums. Checksum 1 (at 0x1C) is
// the sum of the fourteen 16-bit words before it; checksum 2 (at 0x1E) is
// 0xFFF2 minus checksum 1.
func idBlockValid(b []byte) bool {
	c1 := uint16(b[0x1C])<<8 | uint16(b[0x1D])
	c2 := uint16(b[0x1E])<<8 | uint16(b[0x1F])
	return idBlockSum(b) == c1 && c2 == 0xFFF2-c1
}

// idBlockSum returns what checksum 1 of an ID block should be: the sum of
// its fourteen 16-bit words before the checksums.
func idBlockSum(b []byte) uint16 {
	var sum uint16
	for i := 0; i < 0x1C; i += 2 {
		sum += uint16(b[i])<<8 | uint16(b[i+1])
	}
	return sum
}

// idBlock returns the index (0-3) of the first ID block copy with valid
// checksums. Like the console, a pak stays usable while any copy is intact.
func (img *Image) idBlock() (int, error) {
	h := img.sector(0)
	for i, off := range idBlockOffsets {
		if idBlockValid(h[off : off+idBlockSize]) {
			return i, nil
		}
	}
	return 0, ErrNotFormatted
}

// Banks returns the number of 32 KiB banks declared by the ID block in use.
// A count of 0 is read as 1.
func (img *Image) Banks() (int, error) {
	i, err := img.idBlock()
	if err != nil {
		return 0, err
	}
	return max(int(img.sector(0)[idBlockOffsets[i]+idBankCount]), 1), nil
}

// DamagedIDBlocks returns the ID block copies (0 = primary, 1-3 = backups)
// that have bad checksums or differ from the copy in use. They don't stop the
// pak from being read, but the pak has less protection left.
func (img *Image) DamagedIDBlocks() ([]int, error) {
	good, err := img.idBlock()
	if err != nil {
		return nil, err
	}
	h := img.sector(0)
	ref := h[idBlockOffsets[good] : idBlockOffsets[good]+idBlockSize]
	var damaged []int
	for i, off := range idBlockOffsets {
		if !bytes.Equal(h[off:off+idBlockSize], ref) {
			damaged = append(damaged, i)
		}
	}
	return damaged, nil
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
	banks, err := img.Banks()
	if err != nil {
		return 0, err
	}
	if banks > 1 {
		return 0, fmt.Errorf("%w (this pak has %d banks, %d KiB)", ErrMultiBank, banks, banks*Size/1024)
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
	// Entries 0-4 (the reserved sectors) stay 0, as on factory paks. Games
	// ignore them, but tools that checksum from byte 2 rather than from
	// entry 5 need them to be 0.
	var t toc
	for i := blockValidFirst; i <= blockValidLast; i++ {
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
	// Used is true when the slot is taken: its game and publisher codes are
	// both non-zero.
	Used bool
	// Valid is true for a used slot whose block chain is readable. A slot that
	// is Used but not Valid is damaged: it must not be treated as free.
	Valid bool
}

// GameCode returns the 4-byte game code. Its last byte is the region.
func (n *Note) GameCode() []byte { return n.Header[0:4] }

// Publisher returns the 2-byte publisher code.
func (n *Note) Publisher() []byte { return n.Header[4:6] }

func (n *Note) inode() int { return int(n.Header[6])<<8 | int(n.Header[7]) }

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

// slotUsed reports whether a note-table entry is taken. Nintendo's library
// treats an entry as free when its game code or publisher code is zero (the
// manual gives both ranges as starting at 1). The start block and the region
// are not part of this test: an entry with a bad start block is a damaged
// note, not a free slot, and any game code is allowed (e.g. GameShark's
// 0x3BADD1E5).
func slotUsed(h []byte) bool {
	game := int(h[0])<<24 | int(h[1])<<16 | int(h[2])<<8 | int(h[3])
	publisher := int(h[4])<<8 | int(h[5])
	return game != 0 && publisher != 0
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
	n.Used = slotUsed(n.Header[:])
	if !n.Used {
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
	if !n.Used {
		return nil, ErrSlotEmpty
	}
	blocks, err := t.chain(n.inode())
	if err != nil {
		return nil, err // a damaged note
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
	if !n.Used {
		return ErrSlotEmpty
	}
	blocks, err := t.chain(n.inode())
	if err != nil {
		return err // a damaged note: its blocks cannot be traced to free them
	}
	clear(img.noteHeader(slot))
	for _, b := range blocks {
		t.setNext(b, blockEmpty)
	}
	img.writeTOC(&t, valid)
	return nil
}

// MoveNote moves the note in slot from to slot to. If to holds a note, the
// two notes swap slots. Only the two 32-byte headers are exchanged: each
// header's inode still points to the same blocks, so no data moves and the
// TOC is unchanged. A damaged note can be moved too, since its chain is never
// followed. Moving a note to its own slot changes nothing.
func (img *Image) MoveNote(from, to int) error {
	if from < 0 || from >= NumNotes || to < 0 || to >= NumNotes {
		return ErrBadSlot
	}
	if _, err := img.validTOC(); err != nil {
		return err
	}
	if !slotUsed(img.noteHeader(from)) {
		return ErrSlotEmpty
	}
	var tmp [noteHeaderSize]byte
	a, b := img.noteHeader(from), img.noteHeader(to)
	copy(tmp[:], a)
	copy(a, b)
	copy(b, tmp[:])
	return nil
}

// InsertNote stores a note with the given 32-byte header and data, which
// must be a whole number of blocks.
//
// With slot set to -1 the note goes to the first free slot. Otherwise it goes
// to that slot, replacing any note already there. A damaged note in that slot
// is overwritten too, but its blocks stay allocated since its chain cannot be
// followed. The header is stored as is apart from the inode, so the note keeps
// its game code, publisher code and name; both codes must be non-zero, or the
// note would read back as a free slot.
//
// It returns the slot used.
func (img *Image) InsertNote(header [noteHeaderSize]byte, data []byte, slot int) (int, error) {
	if slot < -1 || slot >= NumNotes {
		return 0, ErrBadSlot
	}
	if len(data) == 0 || len(data)%BlockSize != 0 {
		return 0, fmt.Errorf("note data is not a whole number of blocks")
	}
	if !slotUsed(header[:]) {
		return 0, errNoCodes
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
		}
	} else {
		slot = -1
		for i := range NumNotes {
			if !slotUsed(img.noteHeader(i)) {
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
		// The slot is free, or holds a damaged note the caller chose to
		// overwrite.
		clear(img.noteHeader(slot))
	}

	t, valid, err := img.readTOC()
	if err != nil {
		return 0, err
	}

	// Take the lowest free blocks and link them in ascending order: the first
	// block taken is the note's first block (its inode), and each block points
	// to the next one taken. libdragon and gcn64tools link them in reverse;
	// both are valid chains. Ascending order matches what games wrote on a
	// real pak, and makes an image's blocks read in the same order as the
	// note file's data (see docs/mempak-format.md, "Insert a note").
	inode, prev, taken := 0, 0, 0
	for i := blockValidFirst; i <= blockValidLast && taken < nblocks; i++ {
		if t.next(i) == blockEmpty {
			if taken == 0 {
				inode = i
			} else {
				t.setNext(prev, i)
			}
			prev = i
			taken++
		}
	}
	if taken < nblocks {
		return 0, ErrNoSpace
	}
	t.setNext(prev, blockLast)

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
