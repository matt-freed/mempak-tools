// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2007-2024 Raphael Assenat (gcn64tools); Go port (C) 2026 Matt Freed.

package mempak

import (
	"errors"
	"fmt"
	"strings"
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

	if !slotUsed(header[:]) {
		return header, nil, errNoCodes
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
	if !n.Used {
		return nil, ErrSlotEmpty
	}
	data, err := img.NoteData(slot) // fails with ErrCorrupt for a damaged note
	if err != nil {
		return nil, err
	}
	out := append([]byte(nil), n.Header[:]...)
	out[6], out[7] = noteFileMarker>>8, noteFileMarker&0xFF
	return append(out, data...), nil
}

// ReadNoteFile parses a note file for display. It applies the same checks as
// ParseNoteFile, but the returned Note keeps the file's header verbatim,
// including its 0xCAFE inode. Its Slot is -1.
func ReadNoteFile(b []byte) (Note, []byte, error) {
	_, data, err := ParseNoteFile(b)
	if err != nil {
		return Note{}, nil, err
	}
	n := Note{Slot: -1, Blocks: len(data) / BlockSize, Used: true, Valid: true}
	copy(n.Header[:], b)
	return n, data, nil
}

// FileName returns a file name for the note's note file: its slot as two
// digits, a dash and its name, such as "05-MARIO KART 64.note". The slot
// keeps names unique, even for two notes with the same name. Characters that
// some file systems reject (/ \ : * ? " < > |) become '_', and leading or
// trailing spaces and dots are dropped (Windows drops trailing ones itself).
func (n *Note) FileName() string {
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, n.Name())
	name = strings.Trim(name, " .")
	if name == "" {
		return fmt.Sprintf("%02d.note", n.Slot)
	}
	return fmt.Sprintf("%02d-%s.note", n.Slot, name)
}
