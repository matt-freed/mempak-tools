// SPDX-License-Identifier: GPL-3.0-or-later

// Package hid is a minimal, cgo-free USB HID library: device enumeration and
// feature reports, which is all the raphnet adapters need. There is one
// backend per OS (Linux hidraw, macOS IOKit, Windows hid.dll).
package hid

import "errors"

// ErrUnsupported is returned on platforms without a HID backend.
var ErrUnsupported = errors.New("hid: not supported on this platform")

// DeviceInfo describes a HID interface found by Enumerate.
type DeviceInfo struct {
	// Path identifies the interface; pass it to Open.
	Path      string
	VendorID  uint16
	ProductID uint16
	// Release is the bcdDevice field of the USB device descriptor.
	Release uint16
	Product string
	Serial  string
	// Interface is the USB interface number, or -1 if unknown.
	Interface int
}

// Device is an open HID interface.
//
// Report buffers start with the report ID, which is 0 for devices that do
// not number their reports.
type Device interface {
	// SendFeatureReport sends b as a feature report. It returns the number
	// of bytes sent, including the report ID.
	SendFeatureReport(b []byte) (int, error)
	// GetFeatureReport reads the feature report whose ID is in b[0] into b.
	// It returns the number of bytes read, including the report ID.
	GetFeatureReport(b []byte) (int, error)
	Close() error
}

// Enumerate lists the HID interfaces of devices with the given vendor ID
// and, if non-zero, product ID.
func Enumerate(vendorID, productID uint16) ([]DeviceInfo, error) {
	return enumerate(vendorID, productID)
}

// Open opens the interface at path, as returned by Enumerate.
func Open(path string) (Device, error) {
	return open(path)
}
