// SPDX-License-Identifier: GPL-3.0-or-later

package raphnet

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/matt-freed/mempak-tools/internal/mempak"
)

// fakeAdapter simulates raphnet firmware with an N64 controller and a pak
// on channel 0. It answers feature reports like the real adapter.
type fakeAdapter struct {
	t          *testing.T
	pak        [mempak.Size]byte
	probe      byte // last value written to 0x8000
	rumble     bool
	reply      []byte
	suspended  bool
	badReads   int // corrupt the CRC of this many reads
	neverReply bool
}

func (f *fakeAdapter) SendFeatureReport(b []byte) (int, error) {
	cmd := b[1:]
	switch cmd[0] {
	case rqGetSupportedRequests:
		f.reply = []byte{cmd[0], rqSuspendPolling, rqGetVersion, rqGetControllerType, rqRawSICommand, rqBlockIO}
	case rqGetVersion:
		f.reply = append([]byte{cmd[0]}, "3.6.1\x00"...)
	case rqSuspendPolling:
		f.suspended = cmd[1] != 0
		f.reply = []byte{cmd[0]}
	case rqGetControllerType:
		f.reply = []byte{cmd[0], cmd[1], ctlN64New}
	case rqRawSICommand:
		rx := f.si(cmd[3 : 3+int(cmd[2])])
		f.reply = append([]byte{cmd[0], cmd[1], byte(len(rx))}, rx...)
	default:
		f.t.Fatalf("unexpected request 0x%02x", cmd[0])
	}
	return len(b), nil
}

func (f *fakeAdapter) si(tx []byte) []byte {
	switch tx[0] {
	case n64GetCapabilities:
		return []byte{0x05, 0x00, 0x01} // pak present
	case n64ExpansionRead:
		addr := uint16(tx[1])<<8 | uint16(tx[2])
		if addressCRC(addr) != addr {
			f.t.Fatalf("bad address CRC 0x%04x", addr)
		}
		addr &^= 0x1F
		var data []byte
		if addr >= mempak.Size {
			v := byte(0)
			if f.rumble && f.probe == 0x80 {
				v = 0x80
			}
			data = bytes.Repeat([]byte{v}, pakBlock)
		} else {
			data = bytes.Clone(f.pak[addr : addr+pakBlock])
		}
		crc := dataCRC(data)
		if f.badReads > 0 {
			f.badReads--
			crc++
		}
		return append(data, crc)
	case n64ExpansionWrite:
		addr := (uint16(tx[1])<<8 | uint16(tx[2])) &^ 0x1F
		data := tx[3:]
		if addr >= mempak.Size {
			f.probe = data[0]
		} else {
			copy(f.pak[addr:], data)
		}
		return []byte{dataCRC(data)}
	}
	f.t.Fatalf("unexpected SI command 0x%02x", tx[0])
	return nil
}

func (f *fakeAdapter) GetFeatureReport(b []byte) (int, error) {
	if f.neverReply {
		return 1, nil
	}
	clear(b)
	n := copy(b[1:], f.reply)
	return n + 1, nil
}

func (f *fakeAdapter) Close() error { return nil }

func newFake(t *testing.T) (*Adapter, *fakeAdapter) {
	t.Helper()
	f := &fakeAdapter{t: t}
	m, _ := findModel(0x0060, 1)
	a, err := newAdapter(Info{ProductID: 0x0060, Channels: m.channels, Features: m.features}, f)
	if err != nil {
		t.Fatal(err)
	}
	return a, f
}

func TestCRCGolden(t *testing.T) {
	// Values produced by the original C implementation.
	addrs := map[uint16]uint16{0x0000: 0x0000, 0x0020: 0x0035, 0x01e0: 0x01f7, 0x8000: 0x8001,
		0x7fe0: 0x7fec, 0x1234: 0x1230, 0xffe0: 0xffed}
	for in, want := range addrs {
		if got := addressCRC(in); got != want {
			t.Errorf("addressCRC(0x%04x) = 0x%04x, want 0x%04x", in, got, want)
		}
	}
	seq := make([]byte, 32)
	for i := range seq {
		seq[i] = byte(i*7 + 3)
	}
	datas := []struct {
		data []byte
		want byte
	}{
		{bytes.Repeat([]byte{0x00}, 32), 0x00},
		{bytes.Repeat([]byte{0xff}, 32), 0x0a},
		{bytes.Repeat([]byte{0xfe}, 32), 0xe1},
		{bytes.Repeat([]byte{0x80}, 32), 0xb8},
		{seq, 0x8e},
	}
	for _, d := range datas {
		if got := dataCRC(d.data); got != d.want {
			t.Errorf("dataCRC(% x) = 0x%02x, want 0x%02x", d.data[:4], got, d.want)
		}
	}
}

func TestInfoAndFeatures(t *testing.T) {
	a, _ := newFake(t)
	if a.Features&FeatureControllerType == 0 || a.Features&FeatureBlockIO == 0 {
		t.Errorf("features = %b", a.Features)
	}
	if v, err := a.Version(); err != nil || v != "3.6.1" {
		t.Errorf("Version() = %q, %v", v, err)
	}
	if ct, err := a.ControllerType(0); err != nil || !IsN64Controller(ct) {
		t.Errorf("ControllerType() = %d, %v", ct, err)
	}
	if err := a.SuspendPolling(true); err != nil {
		t.Fatal(err)
	}
}

func TestReadWriteRoundTrip(t *testing.T) {
	a, f := newFake(t)
	ctx := context.Background()

	img := mempak.New()
	for i := 0x500; i < 0x900; i++ {
		img.Data[i] = byte(i)
	}
	var last int
	if err := a.WritePak(ctx, 0, &img.Data, func(done int) { last = done }); err != nil {
		t.Fatal(err)
	}
	if last != mempak.Size {
		t.Errorf("progress ended at %d", last)
	}
	if f.pak != img.Data {
		t.Fatal("pak contents differ from written image")
	}

	f.badReads = 2 + 3 // 2 used by DetectPak, then 3 retried block reads
	got, err := a.ReadPak(ctx, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Data != img.Data {
		t.Fatal("read back differs")
	}

	f.badReads = ioRetries + 2 // DetectPak does 2 reads that ignore the CRC
	if _, err := a.ReadPak(ctx, 0, nil); !errors.Is(err, ErrIO) {
		t.Fatalf("ReadPak with persistent CRC errors: %v", err)
	}
}

func TestFill(t *testing.T) {
	a, f := newFake(t)
	ctx := context.Background()
	if err := a.FillPak(ctx, 0, 0xFF, nil); err != nil {
		t.Fatal(err)
	}
	if err := a.VerifyFill(ctx, 0, 0xFF, nil); err != nil {
		t.Fatal(err)
	}
	f.pak[0x1234] = 0
	if err := a.VerifyFill(ctx, 0, 0xFF, nil); err == nil {
		t.Fatal("VerifyFill should fail")
	}
}

func TestDetectRumble(t *testing.T) {
	a, f := newFake(t)
	if err := a.DetectPak(0); err != nil {
		t.Fatalf("mempak: %v", err)
	}
	f.rumble = true
	if err := a.DetectPak(0); !errors.Is(err, ErrNoPak) {
		t.Fatalf("rumble pak: %v", err)
	}
}

func TestCancel(t *testing.T) {
	a, _ := newFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.ReadPak(ctx, 0, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestExchangeTimeout(t *testing.T) {
	a, f := newFake(t)
	f.neverReply = true
	if _, err := a.Version(); err == nil {
		t.Fatal("expected timeout")
	}
}
