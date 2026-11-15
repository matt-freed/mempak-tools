// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2007-2024 Raphael Assenat (gcn64tools); Go port (C) 2026 Matt Freed.

// Package mempak reads, writes and edits N64 Controller Pak (mempak) images
// and the filesystem stored inside them.
package mempak

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// Size is the size in bytes of a mempak.
	Size = 0x8000
	// NumNotes is the number of note slots in the note table.
	NumNotes = 16
	// MaxCommentLen is the longest note comment, in bytes, that an N64
	// (DexDrive) image can store.
	MaxCommentLen = 255

	dexMagic         = "123-456-STD"
	dexCommentOffset = 0x40
	dexCommentSize   = 256
	dexDataOffset    = 0x1040
)

// Format is an on-disk image format.
type Format int

const (
	FormatInvalid Format = iota
	FormatMPK            // standard 32kB raw image
	FormatMPK4           // 128kB raw image holding 4 copies of the 32kB image
	FormatN64            // DexDrive image, with a comment per note
)

func (f Format) String() string {
	switch f {
	case FormatMPK:
		return "MPK"
	case FormatMPK4:
		return "MPK4"
	case FormatN64:
		return "N64"
	case FormatInvalid:
		return "Invalid"
	}
	return "Unknown"
}

// ParseFormat parses a format name (mpk, mpk4 or n64), ignoring case.
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(s) {
	case "mpk":
		return FormatMPK, nil
	case "mpk4":
		return FormatMPK4, nil
	case "n64":
		return FormatN64, nil
	}
	return FormatInvalid, fmt.Errorf("unknown format '%s'. Use mpk, mpk4 or n64", s)
}

// FormatForFilename returns the format to use when writing name: N64 for
// *.n64 files and MPK for everything else.
func FormatForFilename(name string) Format {
	if strings.EqualFold(filepath.Ext(name), ".n64") {
		return FormatN64
	}
	return FormatMPK
}

// Image is a mempak image: the raw 32kB contents plus the per-note comments
// that the N64 format can carry.
type Image struct {
	Data     [Size]byte
	Comments [NumNotes]string
	// Format is the format the image was loaded from.
	Format Format
}

// New returns an image holding a blank, formatted filesystem.
func New() *Image {
	img := &Image{Format: FormatMPK}
	img.FormatFS()
	return img
}

// Decode parses an image file. Raw images of 1 to 4 times 32kB and DexDrive
// images are recognised. Anything else is read as a raw image, like
// gcn64tools does.
func Decode(b []byte) *Image {
	img := &Image{}
	switch {
	case len(b) == Size:
		img.Format = FormatMPK
	case len(b) == 2*Size || len(b) == 3*Size || len(b) == 4*Size:
		img.Format = FormatMPK4
	case bytes.HasPrefix(b, []byte(dexMagic)):
		img.Format = FormatN64
		for i := range img.Comments {
			off := dexCommentOffset + i*dexCommentSize
			if off >= len(b) {
				break
			}
			c := b[off:min(off+dexCommentSize, len(b))]
			if n := bytes.IndexByte(c, 0); n >= 0 {
				c = c[:n]
			}
			img.Comments[i] = string(c)
		}
		if len(b) > dexDataOffset {
			copy(img.Data[:], b[dexDataOffset:])
		}
		return img
	}
	copy(img.Data[:], b)
	return img
}

// Encode serialises the image in the given format.
func (img *Image) Encode(f Format) ([]byte, error) {
	switch f {
	case FormatMPK:
		return bytes.Clone(img.Data[:]), nil
	case FormatMPK4:
		return bytes.Repeat(img.Data[:], 4), nil
	case FormatN64:
		// Only the magic, the comments and the data are written. That is
		// all non-official tools look at.
		out := make([]byte, dexDataOffset+Size)
		copy(out, dexMagic)
		for i, c := range img.Comments {
			off := dexCommentOffset + i*dexCommentSize
			// Always leave a terminating NUL.
			copy(out[off:off+MaxCommentLen], c)
		}
		copy(out[dexDataOffset:], img.Data[:])
		return out, nil
	}
	return nil, fmt.Errorf("cannot encode format %v", f)
}

// LoadFile reads an image file.
func LoadFile(path string) (*Image, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Decode(b), nil
}

// SaveFile writes the image to path in format f. The file is replaced
// atomically, so a failed write never leaves a truncated image behind.
func (img *Image) SaveFile(path string, f Format) error {
	b, err := img.Encode(f)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b)
}

func writeFileAtomic(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		perm = fi.Mode().Perm()
	}
	if err := os.Chmod(tmp.Name(), perm); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
