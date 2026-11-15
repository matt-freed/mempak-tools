// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2007-2024 Raphael Assenat (gcn64tools); Go port (C) 2026 Matt Freed.

// Package raphnet talks to raphnet USB controller adapters over HID feature
// reports, and implements N64 controller pak access on top of them.
package raphnet

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/matt-freed/mempak-tools/internal/hid"
)

// Verbose, when set, receives debugging output about USB traffic.
var Verbose io.Writer

func logf(format string, args ...any) {
	if Verbose != nil {
		fmt.Fprintf(Verbose, format+"\n", args...)
	}
}

const (
	defaultReportSize = 63
	legacyReportSize  = 40 // firmware older than 3.4
	exchangeTimeout   = time.Second
	sendAttempts      = 2
)

// Info describes a connected adapter.
type Info struct {
	Path      string
	Product   string
	Serial    string
	VendorID  uint16
	ProductID uint16
	// Release is the USB device release number; use Adapter.Version for the
	// full firmware version.
	Release uint16
	// Channels is the number of controller ports.
	Channels int
	// Features is updated with what the firmware reports once the adapter
	// is opened.
	Features Features

	reportSize int
}

// List returns the connected adapters that gcn64tools supports.
func List() ([]Info, error) {
	logf("Start listing")
	devs, err := hid.Enumerate(VendorID, 0)
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, d := range devs {
		logf("Considering 0x%04x:0x%04x (interface %d)", d.VendorID, d.ProductID, d.Interface)
		m, ok := findModel(d.ProductID, d.Interface)
		if !ok {
			continue
		}
		if d.Product == "" || d.Serial == "" {
			logf("Warning: Skipping device without product string or serial")
			continue
		}
		out = append(out, Info{
			Path:       d.Path,
			Product:    d.Product,
			Serial:     d.Serial,
			VendorID:   d.VendorID,
			ProductID:  d.ProductID,
			Release:    d.Release,
			Channels:   m.channels,
			Features:   m.features,
			reportSize: m.reportSize,
		})
	}
	return out, nil
}

// Adapter is an open adapter.
type Adapter struct {
	Info
	dev        hid.Device
	reportSize int
}

// Open opens the adapter described by info.
func Open(info Info) (*Adapter, error) {
	logf("Opening device path: '%s'", info.Path)
	dev, err := hid.Open(info.Path)
	if err != nil {
		return nil, err
	}
	a, err := newAdapter(info, dev)
	if err != nil {
		dev.Close()
		return nil, err
	}
	return a, nil
}

func newAdapter(info Info, dev hid.Device) (*Adapter, error) {
	a := &Adapter{Info: info, dev: dev, reportSize: info.reportSize}
	if a.reportSize == 0 {
		a.reportSize = defaultReportSize
		if info.Features&(FeatureBlockIO|featureDynamic) == 0 {
			logf("Pre-3.4 version detected. Setting report size to %d bytes", legacyReportSize)
			a.reportSize = legacyReportSize
		}
	}
	if info.Features&featureDynamic != 0 {
		if err := a.readFeatures(); err != nil {
			return nil, fmt.Errorf("failed to query features: %w", err)
		}
	}
	return a, nil
}

// readFeatures asks the firmware which requests it supports.
func (a *Adapter) readFeatures() error {
	reply, err := a.exchange([]byte{rqGetSupportedRequests})
	if err != nil {
		return err
	}
	supported := reply[1:]
	if bytes.IndexByte(supported, rqBlockIO) >= 0 {
		a.Features |= FeatureBlockIO
	}
	if bytes.IndexByte(supported, rqGetControllerType) >= 0 {
		a.Features |= FeatureControllerType
	}
	return nil
}

// Close closes the adapter.
func (a *Adapter) Close() error {
	return a.dev.Close()
}

// exchange sends a command and waits for the adapter's reply.
func (a *Adapter) exchange(cmd []byte) ([]byte, error) {
	if len(cmd) > a.reportSize {
		return nil, errors.New("command too long")
	}
	// Report ID 0 (the adapter has only one report), then the command.
	buf := make([]byte, a.reportSize+1)
	copy(buf[1:], cmd)

	var err error
	for range sendAttempts {
		if _, err = a.dev.SendFeatureReport(buf); err == nil {
			break
		}
		logf("send feature report: retry")
	}
	if err != nil {
		return nil, fmt.Errorf("could not send feature report: %w", err)
	}

	// The reply is polled for; an empty report means it is not ready yet.
	start := time.Now()
	for {
		clear(buf)
		n, err := a.dev.GetFeatureReport(buf)
		if err != nil {
			return nil, fmt.Errorf("could not read feature report: %w", err)
		}
		if n > 1 {
			logf("Exchange done (%d ms)", time.Since(start).Milliseconds())
			return buf[1:n], nil
		}
		if time.Since(start) > exchangeTimeout {
			return nil, errors.New("adapter exchange timeout")
		}
	}
}

// SuspendPolling stops (or resumes) the adapter's own polling of the
// controller, which would otherwise interleave with pak accesses.
func (a *Adapter) SuspendPolling(suspend bool) error {
	var v byte
	if suspend {
		v = 1
	}
	_, err := a.exchange([]byte{rqSuspendPolling, v})
	return err
}

// Version returns the firmware version string.
func (a *Adapter) Version() (string, error) {
	reply, err := a.exchange([]byte{rqGetVersion})
	if err != nil {
		return "", err
	}
	v := reply[1:]
	if i := bytes.IndexByte(v, 0); i >= 0 {
		v = v[:i]
	}
	return string(v), nil
}

// ControllerType returns the type of controller connected to a port. See
// ControllerName and IsN64Controller.
func (a *Adapter) ControllerType(channel int) (int, error) {
	reply, err := a.exchange([]byte{rqGetControllerType, byte(channel)})
	if err != nil {
		return 0, err
	}
	if len(reply) < 3 {
		return 0, errors.New("short controller type reply")
	}
	return int(reply[2]), nil
}

// rawSI sends a raw joybus (SI) command to a port and returns the answer.
func (a *Adapter) rawSI(channel int, tx []byte) ([]byte, error) {
	cmd := append([]byte{rqRawSICommand, byte(channel), byte(len(tx))}, tx...)
	reply, err := a.exchange(cmd)
	if err != nil {
		return nil, err
	}
	if len(reply) < 3 {
		return nil, errors.New("short SI reply")
	}
	n := int(reply[2])
	if 3+n > len(reply) {
		return nil, fmt.Errorf("SI reply length %d exceeds report", n)
	}
	return reply[3 : 3+n], nil
}
