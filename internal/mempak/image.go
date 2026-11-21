// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2007-2024 Raphael Assenat (gcn64tools); Go port (C) 2026 Matt Freed.

// Package mempak reads, writes and edits N64 Controller Pak (mempak) images
// and the filesystem stored inside them.
package mempak

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// Size is the size in bytes of a mempak, and of a .mpk image file.
	Size = 0x8000
	// NumNotes is the number of note slots in the note table.
	NumNotes = 16
)

// ErrWrongSize means an image read from a file that was not exactly Size
// bytes is being saved. Only 32 KiB raw .mpk images are supported.
var ErrWrongSize = errors.New("not a 32 KiB .mpk image, so it can be inspected but not modified")

// Image is a mempak image: the raw 32 KiB contents of a pak. This is the only
// image format the tool supports (.mpk).
type Image struct {
	Data [Size]byte

	wrongSize bool // decoded from a file that wasn't exactly Size bytes
	fileSize  int  // that file's size
}

// New returns an image holding a blank, formatted filesystem.
func New() *Image {
	img := &Image{}
	img.FormatFS()
	return img
}

// WrongSize reports whether the image came from a file that wasn't exactly
// 32 KiB. Such an image holds the file's first 32 KiB (zero-padded), so it
// can be inspected, but it can't be saved.
func (img *Image) WrongSize() bool { return img.wrongSize }

// Decode reads a .mpk image: the raw 32 KiB contents of a pak. A file of any
// other size is read as raw data, like gcn64tools does, and flagged by
// WrongSize so it is never saved back.
func Decode(b []byte) *Image {
	img := &Image{wrongSize: len(b) != Size, fileSize: len(b)}
	copy(img.Data[:], b)
	return img
}

// LoadFile reads an image file.
func LoadFile(path string) (*Image, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Decode(b), nil
}

// SaveFile writes the image to path as a 32 KiB .mpk file. The file is
// replaced atomically, so a failed write never leaves a truncated image
// behind.
func (img *Image) SaveFile(path string) error {
	if img.WrongSize() {
		return fmt.Errorf("%w (the file was %d bytes)", ErrWrongSize, img.fileSize)
	}
	return writeFileAtomic(path, bytes.Clone(img.Data[:]))
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
