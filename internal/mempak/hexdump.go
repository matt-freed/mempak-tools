// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2007-2024 Raphael Assenat (gcn64tools); Go port (C) 2026 Matt Freed.

package mempak

import (
	"fmt"
	"io"
	"unicode"
)

// Hexdump writes data as lines of 32 bytes: the offset, the bytes in hex and
// the bytes as ASCII.
func Hexdump(w io.Writer, data []byte) error { return HexdumpAt(w, data, 0) }

// HexdumpAt is Hexdump with offsets starting at base, for dumping part of a
// larger buffer.
func HexdumpAt(w io.Writer, data []byte, base int) error {
	for off := 0; off < len(data); off += 32 {
		line := data[off:min(off+32, len(data))]
		if _, err := fmt.Fprintf(w, "%04x: ", base+off); err != nil {
			return err
		}
		for _, b := range line {
			fmt.Fprintf(w, "%02x ", b)
		}
		fmt.Fprint(w, "    ")
		ascii := make([]byte, len(line))
		for i, b := range line {
			if b < 0x80 && unicode.IsPrint(rune(b)) {
				ascii[i] = b
			} else {
				ascii[i] = '.'
			}
		}
		if _, err := fmt.Fprintf(w, "%s\n", ascii); err != nil {
			return err
		}
	}
	return nil
}
