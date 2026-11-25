// SPDX-License-Identifier: GPL-3.0-or-later

package mempak

import (
	"bytes"
	"crypto/rand"
)

// SerialSize is the size in bytes of a pak's serial number, the first field
// of its ID block.
const SerialSize = 24

// Serial is a pak's serial number. Nintendo's library only compares it, to
// notice when a different pak is inserted, so any value unique to the pak
// works.
type Serial [SerialSize]byte

// RandomSerial returns a serial number of random bytes.
func RandomSerial() Serial {
	var s Serial
	rand.Read(s[:]) // never fails: crypto/rand panics if it can't read
	return s
}

// Text returns the serial as text, if it is printable ASCII followed only by
// zero bytes, as SetSerial stores a text serial; ok is false otherwise.
func (s Serial) Text() (text string, ok bool) {
	t := bytes.TrimRight(s[:], "\x00")
	if len(t) == 0 {
		return "", false
	}
	for _, b := range t {
		if b < 0x20 || b > 0x7E {
			return "", false
		}
	}
	return string(t), true
}

// Serial returns the serial number from the ID block copy in use.
func (img *Image) Serial() (Serial, error) {
	var s Serial
	i, err := img.idBlock()
	if err != nil {
		return s, err
	}
	copy(s[:], img.sector(0)[idBlockOffsets[i]:])
	return s, nil
}

// SetSerial gives the pak a new serial number. The ID block copy in use gets
// the new serial and new checksums, and is written to all four copies, so
// damaged copies are rewritten too. Its other fields (device ID, bank count)
// are kept. It fails with ErrNotFormatted if no ID block copy is intact.
func (img *Image) SetSerial(s Serial) error {
	i, err := img.idBlock()
	if err != nil {
		return err
	}
	h := img.sector(0)
	id := bytes.Clone(h[idBlockOffsets[i] : idBlockOffsets[i]+idBlockSize])
	copy(id, s[:])
	sum := idBlockSum(id)
	inv := 0xFFF2 - sum
	id[0x1C], id[0x1D], id[0x1E], id[0x1F] = byte(sum>>8), byte(sum), byte(inv>>8), byte(inv)
	for _, off := range idBlockOffsets {
		copy(h[off:off+idBlockSize], id)
	}
	return nil
}
