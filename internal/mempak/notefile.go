// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2007-2024 Raphael Assenat (gcn64tools); Go port (C) 2026 Matt Freed.

package mempak

import (
	"errors"
)

// Note files use the format of bryc's mempak editor
// (https://github.com/bryc/mempak): the 32-byte note header with its inode
// set to 0xCAFE, followed by the note data.
const noteFileMarker = 0xCAFE

// ParseNoteFile splits a note file into its header and data. The header's
// inode is reset to a valid value so it can be passed to InsertNote.
func ParseNoteFile(b []byte) (header [noteHeaderSize]byte, data []byte, err error) {
	if len(b) < noteHeaderSize || int(b[6])<<8|int(b[7]) != noteFileMarker {
		return header, nil, errors.New("input file does not appear to be in a supported format")
	}
	copy(header[:], b)
	header[6], header[7] = 0x00, blockValidFirst

	if !headerValid(header[:]) {
		return header, nil, errors.New("error loading note (invalid)")
	}
	data = b[noteHeaderSize:]
	if len(data) == 0 || len(data)%BlockSize != 0 {
		return header, nil, errors.New("invalid note file size")
	}
	return header, data, nil
}

// ExportNote returns the note in slot as a note file.
func (img *Image) ExportNote(slot int) ([]byte, error) {
	n, err := img.Note(slot)
	if err != nil {
		return nil, err
	}
	if !n.Valid {
		return nil, ErrSlotEmpty
	}
	data, err := img.NoteData(slot)
	if err != nil {
		return nil, err
	}
	out := append([]byte(nil), n.Header[:]...)
	out[6], out[7] = noteFileMarker>>8, noteFileMarker&0xFF
	return append(out, data...), nil
}
