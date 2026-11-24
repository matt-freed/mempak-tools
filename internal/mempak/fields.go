// SPDX-License-Identifier: GPL-3.0-or-later

package mempak

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
)

// Field is one decoded field of an image or note file, for display. Fields
// may hold sub-fields, such as the parts of an ID block; the sub-fields then
// cover all of the field's bytes.
type Field struct {
	Offset int    // offset of Data in the image or file
	Data   []byte // the field's bytes
	Name   string
	Value  string // what the bytes mean, or "" when nothing is known
	Fields []Field
}

// field returns the Field for the size bytes of b at offset off.
func field(b []byte, off, size int, name, value string, sub ...Field) Field {
	return Field{Offset: off, Data: b[off : off+size], Name: name, Value: value, Fields: sub}
}

// CodeString formats a game or publisher code: as text if every byte is
// printable ASCII, else in hex (as for GameShark's 0x3BADD1E5).
func CodeString(b []byte) string {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return fmt.Sprintf("%x", b)
		}
	}
	return string(b)
}

// IDSectorFields decodes sector 0: the label, the four ID block copies and
// the unused areas between them. The copy in use is broken into its fields,
// and so is any copy that is damaged or differs from it; a copy identical to
// the one in use is summarised on one line. The layout is the same on
// multi-bank paks.
func (img *Image) IDSectorFields() []Field {
	d := img.Data[:]
	h := img.sector(0)
	inUse, err := img.idBlock()
	var ref []byte
	if err == nil {
		ref = h[idBlockOffsets[inUse] : idBlockOffsets[inUse]+idBlockSize]
	}
	out := []Field{field(d, 0, idBlockSize, "label", "")}
	for off := idBlockSize; off < BlockSize; off += idBlockSize {
		b := h[off : off+idBlockSize]
		copyNum := -1
		for i, o := range idBlockOffsets {
			if o == off {
				copyNum = i
			}
		}
		if copyNum < 0 {
			out = append(out, field(d, off, idBlockSize, "unused", ""))
			continue
		}
		name := "ID block (primary)"
		if copyNum > 0 {
			name = fmt.Sprintf("ID block (backup %d)", copyNum)
		}
		valid := idBlockValid(b)
		var value string
		switch {
		case err == nil && copyNum == inUse:
			value = "in use"
		case valid && bytes.Equal(b, ref):
			out = append(out, field(d, off, idBlockSize, name, "identical to the copy in use"))
			continue
		case valid:
			value = "checksums ok, but differs from the copy in use"
		default:
			value = "damaged: bad checksum"
		}
		out = append(out, field(d, off, idBlockSize, name, value, idBlockFields(d, off)...))
	}
	return out
}

// idBlockFields decodes the ID block copy at offset off of image data d.
func idBlockFields(d []byte, off int) []Field {
	b := d[off : off+idBlockSize]
	banks := int(b[idBankCount])
	bankValue := fmt.Sprintf("%d (%d KiB)", banks, banks*Size/1024)
	switch {
	case banks == 0:
		bankValue = "0 (read as 1: 32 KiB)"
	case banks > 1:
		bankValue += "; multi-bank paks are not supported by this tool"
	}

	devValue := "controller pak (bit 0 set)"
	if b[0x19]&1 == 0 {
		devValue = "not a controller pak (bit 0 clear)"
	}

	c1 := uint16(b[0x1C])<<8 | uint16(b[0x1D])
	c2 := uint16(b[0x1E])<<8 | uint16(b[0x1F])
	c1Value := "ok"
	if sum := idBlockSum(b); c1 != sum {
		c1Value = fmt.Sprintf("bad: should be 0x%04x, the sum of the words at 0x00-0x1b", sum)
	}
	c2Value := "ok"
	if want := 0xFFF2 - c1; c2 != want {
		c2Value = fmt.Sprintf("bad: should be 0x%04x, 0xfff2 - checksum 1", want)
	}

	return []Field{
		field(d, off, 0x18, "serial number", ""),
		field(d, off+0x18, 1, "unused", ""),
		field(d, off+0x19, 1, "device ID", devValue),
		field(d, off+0x1A, 1, "bank count", bankValue),
		field(d, off+0x1B, 1, "unused", ""),
		field(d, off+0x1C, 2, "checksum 1", c1Value),
		field(d, off+0x1E, 2, "checksum 2", c2Value),
	}
}

// noteHeaderFields decodes the 32-byte note-table entry at offset off of d,
// which is an image or a note file (noteFile), where the inode holds the
// 0xCAFE marker instead of a block number.
func noteHeaderFields(d []byte, off int, noteFile bool) []Field {
	h := d[off : off+noteHeaderSize]

	game := codeValue(h[0:4], 0x3BADD1E5)
	if name, ok := GameName(h[0:4]); ok {
		game += " (" + name + ")"
	}

	inode := int(h[6])<<8 | int(h[7])
	inodeValue := fmt.Sprintf("block %d", inode)
	switch {
	case noteFile && inode == noteFileMarker:
		inodeValue = "note file marker (set to a block when the note is inserted)"
	case noteFile:
		inodeValue = "not the 0xcafe note file marker"
	case inode < blockValidFirst || inode > blockValidLast:
		inodeValue = "not a data block: damaged"
	}

	ext := "none"
	if h[0x0C] != 0 {
		ext = fmt.Sprintf("'%c'", decodeChar(h[0x0C]))
	}

	var name strings.Builder
	for _, c := range h[0x10:0x20] {
		name.WriteRune(decodeChar(c))
	}

	return []Field{
		field(d, off, 4, "game code", game),
		field(d, off+0x04, 2, "publisher code", codeValue(h[4:6], 0xFADE)),
		field(d, off+0x06, 2, "start inode", inodeValue),
		field(d, off+0x08, 1, "status", ""), // its bits' meaning is not known
		field(d, off+0x09, 1, "unused", ""),
		field(d, off+0x0A, 2, "unused", ""),
		field(d, off+0x0C, 4, "extension", ext),
		field(d, off+0x10, 16, "name", fmt.Sprintf("\"%s\"", strings.TrimRight(name.String(), " "))),
	}
}

// codeValue says what a game or publisher code means: its text when it is
// ASCII, Datel's GameShark for its known non-ASCII code (gameShark), and
// nothing otherwise.
func codeValue(b []byte, gameShark uint32) string {
	var v uint32
	for _, c := range b {
		v = v<<8 | uint32(c)
	}
	switch {
	case v == gameShark:
		return "Datel GameShark"
	case CodeString(b) == string(b):
		return string(b)
	}
	return ""
}

// NoteFileHeaderFields decodes the 32-byte header of a note file.
func NoteFileHeaderFields(b []byte) []Field {
	return noteHeaderFields(b, 0, true)
}

// TOCFields decodes one TOC copy, in sector 1 or 2. A copy identical to the
// one in use is a single field. Otherwise the copy is split into its first
// entry (an unused byte and the checksum), the reserved entries 1-4, and the
// entries of blocks 5-127, grouped into the same runs as Sections but
// following this copy's own chains, since a damaged copy may differ.
func (img *Image) TOCFields(sector int) []Field {
	d := img.Data[:]
	off := sector * BlockSize
	var t toc
	copy(t[:], img.sector(sector))
	if inUse, err := img.validTOC(); err == nil && inUse != sector && bytes.Equal(img.sector(inUse), t[:]) {
		return []Field{field(d, off, BlockSize, "TOC", fmt.Sprintf("identical to the copy in use (sector %d)", inUse))}
	}

	checksum := "ok"
	if sum := t.checksum(); sum != t[1] {
		checksum = fmt.Sprintf("bad: should be 0x%02x, the sum of entries 5-127", sum)
	}
	out := []Field{
		field(d, off, 1, "unused", ""),
		field(d, off+1, 1, "checksum", checksum),
		field(d, off+2, 2*(blockValidFirst-1), "entries 1-4", "reserved sectors 1-4, not used as pointers"),
	}
	runs, names := img.blockRuns(&t)
	for _, r := range runs {
		out = append(out, tocRunField(d, off, &t, r, names))
	}
	return out
}

// tocRunField decodes the entries of run r in TOC t, which is at offset off
// of image data d.
func tocRunField(d []byte, off int, t *toc, r blockRun, names []string) Field {
	name := fmt.Sprintf("blocks %d-%d", r.first, r.last)
	if r.first == r.last {
		name = fmt.Sprintf("block %d", r.first)
	}
	f := field(d, off+2*r.first, 2*(r.last-r.first+1), name, r.label(names))
	switch r.slot {
	case runFree:
	case runOrphan:
		// No chain to follow, so say where each entry points.
		for b := r.first; b <= r.last; b++ {
			f.Fields = append(f.Fields, field(d, off+2*b, 2, fmt.Sprintf("block %d", b), entryValue(t.next(b))))
		}
	default:
		f.Value += ": " + r.chain(t)
	}
	for b := r.first; b <= r.last; b++ {
		if t[2*b] != 0 {
			f.Value += "; some high bytes are not 0 (ignored)"
			break
		}
	}
	return f
}

// entryValue says what a TOC entry's next-block value means.
func entryValue(next int) string {
	switch {
	case next == blockLast:
		return "last block of a note"
	case next == blockEmpty:
		return "free"
	case next >= blockValidFirst && next <= blockValidLast:
		return fmt.Sprintf("next block %d", next)
	}
	return "invalid"
}

// chain shows how a note's run is linked, in chain order, and where the
// chain goes after it, such as "12→13→…→73, then end".
func (r blockRun) chain(t *toc) string {
	var blocks []int
	for b := r.first; b <= r.last; b++ {
		blocks = append(blocks, b)
	}
	if r.lastPart < r.firstPart {
		slices.Reverse(blocks)
	}
	s := make([]string, len(blocks))
	for i, b := range blocks {
		s[i] = fmt.Sprint(b)
	}
	if len(s) > 4 {
		s = []string{s[0], s[1], "…", s[len(s)-1]}
	}
	then := "end"
	if next := t.next(blocks[len(blocks)-1]); next != blockLast {
		then = fmt.Sprintf("block %d", next)
	}
	return strings.Join(s, "→") + ", then " + then
}

// NoteTableFields decodes the 16 entries of the note table. A used slot is
// split into its fields; a free one (zero game or publisher code) is one
// field.
func (img *Image) NoteTableFields() []Field {
	d := img.Data[:]
	t, _, tocErr := img.readTOC()
	var out []Field
	for slot := range NumNotes {
		off := noteTableOffset + slot*noteHeaderSize
		name := fmt.Sprintf("slot %d", slot)
		if !slotUsed(d[off : off+noteHeaderSize]) {
			out = append(out, field(d, off, noteHeaderSize, name, "free"))
			continue
		}
		var value string
		switch n := img.note(&t, slot); {
		case tocErr != nil:
			value = fmt.Sprintf("'%s' (no valid TOC, so its blocks are unknown)", n.Name())
		case !n.Valid:
			value = fmt.Sprintf("'%s', damaged: broken block chain", n.Name())
		case n.Blocks == 1:
			value = fmt.Sprintf("'%s', 1 block", n.Name())
		default:
			value = fmt.Sprintf("'%s', %d blocks", n.Name(), n.Blocks)
		}
		out = append(out, field(d, off, noteHeaderSize, name, value, noteHeaderFields(d, off, false)...))
	}
	return out
}
