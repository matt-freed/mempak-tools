// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2007-2024 Raphael Assenat (gcn64tools); Go port (C) 2026 Matt Freed.

package raphnet

// VendorID is raphnet's USB vendor ID.
const VendorID = 0x289b

// Features is a set of adapter capabilities.
type Features uint32

const (
	// FeatureBlockIO means the adapter supports batched SI transfers.
	FeatureBlockIO Features = 1 << iota
	// FeatureControllerType means the adapter reports the controller type.
	FeatureControllerType
	// featureDynamic means the features are queried from the firmware.
	featureDynamic
)

type model struct {
	productID  uint16
	iface      int // HID interface carrying the command channel
	channels   int
	reportSize int // 0 for the default
	features   Features
}

const (
	v3     = FeatureControllerType
	v3Bio  = FeatureControllerType | FeatureBlockIO
	v35    = FeatureBlockIO | featureDynamic
	dynOnl = featureDynamic
)

// models lists the adapters gcn64tools knows how to talk to.
var models = []model{
	{0x0017, 1, 1, 0, v3}, // GC/N64 USB v3.0, 3.1.0, 3.1.1
	{0x001D, 1, 1, 0, v3}, // GC/N64 USB v3.2.0 ... v3.3.x
	{0x0020, 1, 1, 0, v3}, // GCN64->USB v3.2.1 (N64 mode)
	{0x0021, 1, 1, 0, v3}, // GCN64->USB v3.2.1 (GC mode)
	{0x0022, 1, 2, 0, v3}, // GCN64->USB v3.3.x (2x GC/N64 mode)
	{0x0030, 1, 2, 0, v3}, // GCN64->USB v3.3.0 (2x N64-only mode)
	{0x0031, 1, 2, 0, v3}, // GCN64->USB v3.3.0 (2x GC-only mode)

	{0x0032, 1, 1, 0, v3Bio}, // GC/N64 USB v3.4.x (GC/N64 mode)
	{0x0033, 1, 1, 0, v3Bio}, // GC/N64 USB v3.4.x (N64 mode)
	{0x0034, 1, 1, 0, v3Bio}, // GC/N64 USB v3.4.x (GC mode)
	{0x0035, 1, 2, 0, v3Bio}, // GC/N64 USB v3.4.x (2x GC/N64 mode)
	{0x0036, 1, 2, 0, v3Bio}, // GC/N64 USB v3.4.x (2x N64-only mode)
	{0x0037, 1, 2, 0, v3Bio}, // GC/N64 USB v3.4.x (2x GC-only mode)

	// GC/N64 USB v3.5.x
	{0x0038, 1, 1, 0, v35}, // GC/N64 mode
	{0x0039, 1, 1, 0, v35}, // N64 mode
	{0x003A, 1, 1, 0, v35}, // GC mode
	{0x003B, 2, 2, 0, v35}, // 2x GC/N64 mode
	{0x003C, 2, 2, 0, v35}, // 2x N64-only mode
	{0x003D, 2, 2, 0, v35}, // 2x GC-only mode

	// GC/N64 USB v3.6.x
	{0x0060, 1, 1, 0, v35}, // GC/N64 mode
	{0x0061, 1, 1, 0, v35}, // N64 mode
	{0x0062, 1, 1, 0, v35}, // GC mode
	{0x0063, 2, 2, 0, v35}, // 2x GC/N64 mode
	{0x0064, 2, 2, 0, v35}, // 2x N64-only mode
	{0x0065, 2, 2, 0, v35}, // 2x GC-only mode
	{0x0066, 1, 1, 0, v35}, // Keyboard mode (alternates with GC mode)
	{0x0067, 1, 1, 0, v35}, // Keyboard mode 2 (alternates with GC/N64 mode)
	{0x0068, 2, 2, 0, v35}, // Keyboard (port 2) and joystick (port 1) mode

	// GC/N64 USB v3.7.x
	{0x0090, 1, 1, 0, v35}, // GC/N64 mode
	{0x0091, 1, 1, 0, v35}, // N64 mode
	{0x0092, 1, 1, 0, v35}, // GC mode
	{0x0093, 2, 2, 0, v35}, // 2x GC/N64 mode
	{0x0094, 2, 2, 0, v35}, // 2x N64-only mode
	{0x0095, 2, 2, 0, v35}, // 2x GC-only mode
	{0x0096, 1, 1, 0, v35}, // Keyboard mode (alternates with GC mode)
	{0x0097, 1, 1, 0, v35}, // Keyboard mode 2 (alternates with GC/N64 mode)
	{0x0098, 2, 2, 0, v35}, // Keyboard (port 2) and joystick (port 1) mode
	{0x0099, 1, 1, 0, v35}, // Keyboard mode 3 (alternates with N64-only mode)

	{0x004A, 1, 1, 63, dynOnl}, // DB9 Joystick to USB adapter
	{0x004B, 1, 2, 63, dynOnl}, // DB9 Joystick to USB adapter (2 player mode)
	{0x004C, 1, 3, 63, dynOnl}, // DB9 Joystick to USB adapter (3 player mode)
	{0x004D, 1, 4, 63, dynOnl}, // DB9 Joystick to USB adapter (4 player mode)
	{0x004E, 1, 1, 63, dynOnl}, // DB9 Joystick to USB adapter (mouse mode)
	{0x004F, 1, 1, 63, dynOnl}, // DB9 Joystick to USB adapter (SMS mode)

	{0x0050, 1, 1, 63, dynOnl}, // PC Engine to USB v1.0.0
}

func findModel(productID uint16, iface int) (model, bool) {
	for _, m := range models {
		if m.productID == productID && m.iface == iface {
			return m, true
		}
	}
	return model{}, false
}
