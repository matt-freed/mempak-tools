// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2007-2024 Raphael Assenat (gcn64tools); Go port (C) 2026 Matt Freed.
// The address and data CRCs are adapted from libdragon (public domain).

package raphnet

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/matt-freed/mempak-tools/internal/mempak"
)

const (
	// pakBlock is the size of one pak read or write.
	pakBlock   = 0x20
	ioRetries  = 5
	probeAddr  = 0x8000 // accessory-identification area, outside the memory
	writeProbe = 0xFE
)

var (
	// ErrNoPak means no controller pak was detected (no controller, no pak,
	// or a rumble pak).
	ErrNoPak = errors.New("no mempak detected (is a controller with a pak plugged in?)")
	// ErrIO means the pak did not answer correctly, even after retries.
	ErrIO = errors.New("I/O error communicating with the pak")
)

// Progress is called after each 32-byte block with the number of bytes done.
type Progress func(done int)

// addressCRC returns addr with the 5-bit address CRC in its low bits.
func addressCRC(addr uint16) uint16 {
	table := [16]uint16{0x0, 0x0, 0x0, 0x0, 0x0, 0x15, 0x1F, 0x0B, 0x16, 0x19, 0x07, 0x0E, 0x1C, 0x0D, 0x1A, 0x01}
	addr &^= 0x1F
	var crc uint16
	for i := 15; i >= 5; i-- {
		if addr>>i&1 != 0 {
			crc ^= table[i]
		}
	}
	return addr | crc&0x1F
}

// dataCRC returns the 8-bit CRC of a block of pak data.
func dataCRC(data []byte) byte {
	var crc byte
	for i := 0; i <= len(data); i++ {
		for bit := 7; bit >= 0; bit-- {
			var x byte
			if crc&0x80 != 0 {
				x = 0x85
			}
			crc <<= 1
			if i < len(data) && data[i]&(1<<bit) != 0 {
				crc |= 1
			}
			crc ^= x
		}
	}
	return crc
}

// expansionWrite writes 32 bytes to the accessory port and returns the data
// CRC the accessory answers with. addr must already include its CRC.
func (a *Adapter) expansionWrite(channel int, addr uint16, data []byte) (byte, error) {
	rx, err := a.rawSI(channel, append([]byte{n64ExpansionWrite, byte(addr >> 8), byte(addr)}, data...))
	if err != nil {
		return 0, err
	}
	if len(rx) != 1 {
		return 0, fmt.Errorf("expansion write returned %d bytes instead of 1", len(rx))
	}
	return rx[0], nil
}

// expansionRead reads from the accessory port. addr must already include its
// CRC. The answer is 32 data bytes followed by their CRC.
func (a *Adapter) expansionRead(channel int, addr uint16) ([]byte, error) {
	return a.rawSI(channel, []byte{n64ExpansionRead, byte(addr >> 8), byte(addr)})
}

// DetectPak checks that a controller pak (and not a rumble pak) is plugged
// into the controller on channel.
func (a *Adapter) DetectPak(channel int) error {
	caps, err := a.rawSI(channel, []byte{n64GetCapabilities})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoPak, err)
	}
	if len(caps) != 3 || caps[2]&0x01 == 0 {
		return ErrNoPak // no controller, or nothing in its accessory port
	}

	// Writing to 0x8000 selects the accessory type on rumble paks. Write
	// 0xFE then 0x80, reading back after each (method from libdragon).
	addr := addressCRC(probeAddr)
	probe := func(v byte) (byte, error) {
		crc, err := a.expansionWrite(channel, addr, bytes.Repeat([]byte{v}, pakBlock))
		if err != nil {
			return 0, err
		}
		if v == writeProbe && crc != dataCRC(bytes.Repeat([]byte{v}, pakBlock)) {
			return 0, errors.New("bad write CRC")
		}
		rx, err := a.expansionRead(channel, addr)
		if err != nil {
			return 0, err
		}
		if len(rx) == 0 {
			return 0, errors.New("empty read")
		}
		return rx[0], nil
	}

	first, err := probe(writeProbe)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoPak, err)
	}
	second, err := probe(0x80)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoPak, err)
	}

	// Values read back after writing 0xFE:
	//   official rumble pak, Yobo rumble pak, Yobo mempak: 0x00
	//   unknown "super memory card 1000": 0xFE
	// The simple libdragon method mistakes that last card for a rumble pak,
	// so it is caught here.
	if first == writeProbe {
		return nil
	}
	// Values read back after writing 0x80:
	//   official rumble pak, Yobo rumble pak: 0x80 (and the card above)
	//   Yobo mempak: 0x00
	if second == 0x80 {
		return ErrNoPak // rumble pak
	}
	return nil
}

// ReadBlock reads 32 bytes of pak memory at addr.
func (a *Adapter) ReadBlock(channel int, addr uint16) ([pakBlock]byte, error) {
	var block [pakBlock]byte
	rx, err := a.expansionRead(channel, addressCRC(addr))
	if err != nil {
		return block, err
	}
	if len(rx) != pakBlock+1 {
		return block, fmt.Errorf("short read at address 0x%04x (%d bytes)", addr, len(rx))
	}
	copy(block[:], rx)
	if crc := dataCRC(block[:]); crc != rx[pakBlock] {
		return block, fmt.Errorf("bad CRC reading address 0x%04x. Expected 0x%02x, got 0x%02x", addr, crc, rx[pakBlock])
	}
	return block, nil
}

// WriteBlock writes 32 bytes of pak memory at addr.
func (a *Adapter) WriteBlock(channel int, addr uint16, data []byte) error {
	if len(data) != pakBlock {
		return fmt.Errorf("block must be %d bytes", pakBlock)
	}
	crc, err := a.expansionWrite(channel, addressCRC(addr), data)
	if err != nil {
		return err
	}
	if want := dataCRC(data); crc != want {
		return fmt.Errorf("bad CRC writing address 0x%04x. Expected 0x%02x, got 0x%02x", addr, want, crc)
	}
	return nil
}

func retry(op func() error) error {
	var err error
	for range ioRetries {
		if err = op(); err == nil {
			return nil
		}
		logf("retrying: %v", err)
	}
	return fmt.Errorf("%w: %v", ErrIO, err)
}

// ReadPak reads the whole pak.
func (a *Adapter) ReadPak(ctx context.Context, channel int, progress Progress) (*mempak.Image, error) {
	if err := a.DetectPak(channel); err != nil {
		return nil, err
	}
	img := &mempak.Image{}
	for addr := 0; addr < mempak.Size; addr += pakBlock {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		err := retry(func() error {
			block, err := a.ReadBlock(channel, uint16(addr))
			copy(img.Data[addr:], block[:])
			return err
		})
		if err != nil {
			return nil, err
		}
		if progress != nil {
			progress(addr + pakBlock)
		}
	}
	return img, nil
}

// WritePak writes data (a whole pak image) to the pak.
func (a *Adapter) WritePak(ctx context.Context, channel int, data *[mempak.Size]byte, progress Progress) error {
	if err := a.DetectPak(channel); err != nil {
		return err
	}
	for addr := 0; addr < mempak.Size; addr += pakBlock {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := retry(func() error {
			return a.WriteBlock(channel, uint16(addr), data[addr:addr+pakBlock])
		})
		if err != nil {
			return err
		}
		if progress != nil {
			progress(addr + pakBlock)
		}
	}
	return nil
}

// FillPak overwrites every byte of the pak with v. The caller should have
// checked for a pak with DetectPak.
func (a *Adapter) FillPak(ctx context.Context, channel int, v byte, progress Progress) error {
	block := bytes.Repeat([]byte{v}, pakBlock)
	for addr := 0; addr < mempak.Size; addr += pakBlock {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.WriteBlock(channel, uint16(addr), block); err != nil {
			return fmt.Errorf("error writing to mempak: %w", err)
		}
		if progress != nil {
			progress(addr + pakBlock)
		}
	}
	return nil
}

// VerifyFill checks that every byte of the pak is v.
func (a *Adapter) VerifyFill(ctx context.Context, channel int, v byte, progress Progress) error {
	want := bytes.Repeat([]byte{v}, pakBlock)
	for addr := 0; addr < mempak.Size; addr += pakBlock {
		if err := ctx.Err(); err != nil {
			return err
		}
		got, err := a.ReadBlock(channel, uint16(addr))
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}
		if !bytes.Equal(got[:], want) {
			return fmt.Errorf("verify failed at 0x%04x\nExpected: % x\nReadback: % x", addr, want, got[:])
		}
		if progress != nil {
			progress(addr + pakBlock)
		}
	}
	return nil
}
