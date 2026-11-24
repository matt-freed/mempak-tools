// SPDX-License-Identifier: GPL-3.0-or-later

package mempak

import (
	"bytes"
	"fmt"
	"strings"
)

// Field is one decoded field of an image or note file, for display. Fields
// may hold sub-fields, such as the parts of an ID block; the sub-fields then
// cover all of the field's bytes.
type Field struct {
	Offset int    // offset of Data in the image or file
	Data   []byte // the field's bytes
	Name   string
	Value  string // the decoded value, or a summary of the sub-fields
	Fields []Field
}

// field returns the Field for the size bytes of b at offset off.
func field(b []byte, off, size int, name, value string, sub ...Field) Field {
	return Field{Offset: off, Data: b[off : off+size], Name: name, Value: value, Fields: sub}
}

// bytesValue summarises a run of bytes with no known meaning: "all 0xNN" when
// every byte is the same, else the bytes in hex, cut short after 8 bytes when
// there are more than 16.
func bytesValue(b []byte) string {
	if len(b) > 1 && bytes.Count(b, b[:1]) == len(b) {
		return fmt.Sprintf("all 0x%02x", b[0])
	}
	if len(b) > 16 {
		return hexBytes(b[:8]) + " …"
	}
	return hexBytes(b)
}

func hexBytes(b []byte) string {
	s := make([]string, len(b))
	for i, c := range b {
		s[i] = fmt.Sprintf("%02x", c)
	}
	return strings.Join(s, " ")
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
	out := []Field{field(d, 0, idBlockSize, "label", bytesValue(h[:idBlockSize]))}
	for off := idBlockSize; off < BlockSize; off += idBlockSize {
		b := h[off : off+idBlockSize]
		copyNum := -1
		for i, o := range idBlockOffsets {
			if o == off {
				copyNum = i
			}
		}
		if copyNum < 0 {
			out = append(out, field(d, off, idBlockSize, "unused", bytesValue(b)))
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

	dev := b[0x19]
	devValue := fmt.Sprintf("0x%02x (bit 0 set: controller pak)", dev)
	if dev&1 == 0 {
		devValue = fmt.Sprintf("0x%02x (bit 0 clear: not a controller pak)", dev)
	}

	sum := idBlockSum(b)
	c1 := uint16(b[0x1C])<<8 | uint16(b[0x1D])
	c2 := uint16(b[0x1E])<<8 | uint16(b[0x1F])
	c1Value := fmt.Sprintf("0x%04x (ok)", c1)
	if c1 != sum {
		c1Value = fmt.Sprintf("0x%04x (bad: the words at 0x00-0x1b sum to 0x%04x)", c1, sum)
	}
	c2Value := fmt.Sprintf("0x%04x (ok)", c2)
	if want := 0xFFF2 - c1; c2 != want {
		c2Value = fmt.Sprintf("0x%04x (bad: 0xfff2 - checksum 1 is 0x%04x)", c2, want)
	}

	return []Field{
		field(d, off, 0x18, "serial number", bytesValue(b[:0x18])),
		field(d, off+0x18, 1, "unused", fmt.Sprintf("0x%02x", b[0x18])),
		field(d, off+0x19, 1, "device ID", devValue),
		field(d, off+0x1A, 1, "bank count", bankValue),
		field(d, off+0x1B, 1, "unused", fmt.Sprintf("0x%02x", b[0x1B])),
		field(d, off+0x1C, 2, "checksum 1", c1Value),
		field(d, off+0x1E, 2, "checksum 2", c2Value),
	}
}

// noteHeaderFields decodes the 32-byte note-table entry at offset off of d,
// which is an image or a note file (noteFile), where the inode holds the
// 0xCAFE marker instead of a block number.
func noteHeaderFields(d []byte, off int, noteFile bool) []Field {
	h := d[off : off+noteHeaderSize]

	game := CodeString(h[0:4])
	if name, ok := GameName(h[0:4]); ok {
		game += " (" + name + ")"
	}

	inode := int(h[6])<<8 | int(h[7])
	inodeValue := fmt.Sprintf("0x%04x (block %d)", inode, inode)
	switch {
	case noteFile && inode == noteFileMarker:
		inodeValue = "0xcafe (note file marker; set when the note is inserted)"
	case noteFile:
		inodeValue = fmt.Sprintf("0x%04x (not the 0xcafe note file marker)", inode)
	case inode < blockValidFirst || inode > blockValidLast:
		inodeValue = fmt.Sprintf("0x%04x (not a data block: damaged)", inode)
	}

	ext := fmt.Sprintf("none (%s)", hexBytes(h[0x0C:0x10]))
	if h[0x0C] != 0 {
		ext = fmt.Sprintf("'%c' (%s)", decodeChar(h[0x0C]), hexBytes(h[0x0C:0x10]))
	}

	var name strings.Builder
	for _, c := range h[0x10:0x20] {
		name.WriteRune(decodeChar(c))
	}

	return []Field{
		field(d, off, 4, "game code", game),
		field(d, off+0x04, 2, "publisher code", CodeString(h[4:6])),
		field(d, off+0x06, 2, "start inode", inodeValue),
		field(d, off+0x08, 1, "status", fmt.Sprintf("0x%02x", h[0x08])),
		field(d, off+0x09, 1, "unused", fmt.Sprintf("0x%02x", h[0x09])),
		field(d, off+0x0A, 2, "unused", bytesValue(h[0x0A:0x0C])),
		field(d, off+0x0C, 4, "extension", ext),
		field(d, off+0x10, 16, "name", fmt.Sprintf("\"%s\"", strings.TrimRight(name.String(), " "))),
	}
}

// NoteFileHeaderFields decodes the 32-byte header of a note file.
func NoteFileHeaderFields(b []byte) []Field {
	return noteHeaderFields(b, 0, true)
}
