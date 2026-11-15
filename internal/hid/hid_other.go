// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !linux && !darwin && !windows

package hid

func enumerate(vendorID, productID uint16) ([]DeviceInfo, error) { return nil, ErrUnsupported }

func open(path string) (Device, error) { return nil, ErrUnsupported }
