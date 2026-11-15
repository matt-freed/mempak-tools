// SPDX-License-Identifier: GPL-3.0-or-later

package hid

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Linux backend: hidraw, discovered through sysfs (no libudev).

const busUSB = 0x03

// ioctl request numbers: _IOC(_IOC_READ|_IOC_WRITE, 'H', nr, len).
func hidiocSFeature(n int) uintptr { return 0xC0004806 | uintptr(n)<<16 }
func hidiocGFeature(n int) uintptr { return 0xC0004807 | uintptr(n)<<16 }

func enumerate(vendorID, productID uint16) ([]DeviceInfo, error) {
	nodes, err := filepath.Glob("/sys/class/hidraw/hidraw*")
	if err != nil {
		return nil, err
	}
	var out []DeviceInfo
	for _, node := range nodes {
		info, ok := hidrawInfo(node)
		if !ok || info.VendorID != vendorID || (productID != 0 && info.ProductID != productID) {
			continue
		}
		out = append(out, info)
	}
	return out, nil
}

// hidrawInfo reads the description of /sys/class/hidraw/<node>.
func hidrawInfo(node string) (DeviceInfo, bool) {
	hidDev, err := filepath.EvalSymlinks(filepath.Join(node, "device"))
	if err != nil {
		return DeviceInfo{}, false
	}
	uevent := readUevent(filepath.Join(hidDev, "uevent"))

	// HID_ID=0003:0000289B:00000038 (bus:vendor:product)
	parts := strings.Split(uevent["HID_ID"], ":")
	if len(parts) != 3 {
		return DeviceInfo{}, false
	}
	bus, err1 := strconv.ParseUint(parts[0], 16, 32)
	vid, err2 := strconv.ParseUint(parts[1], 16, 32)
	pid, err3 := strconv.ParseUint(parts[2], 16, 32)
	if err1 != nil || err2 != nil || err3 != nil || bus != busUSB {
		return DeviceInfo{}, false
	}

	info := DeviceInfo{
		Path:      "/dev/" + filepath.Base(node),
		VendorID:  uint16(vid),
		ProductID: uint16(pid),
		Product:   uevent["HID_NAME"],
		Serial:    uevent["HID_UNIQ"],
		Interface: -1,
	}

	// The parent of the HID device is the USB interface, and its parent the
	// USB device.
	usbIntf := filepath.Dir(hidDev)
	usbDev := filepath.Dir(usbIntf)
	if n, err := strconv.ParseUint(readAttr(usbIntf, "bInterfaceNumber"), 16, 8); err == nil {
		info.Interface = int(n)
	}
	if n, err := strconv.ParseUint(readAttr(usbDev, "bcdDevice"), 16, 16); err == nil {
		info.Release = uint16(n)
	}
	if s := readAttr(usbDev, "product"); s != "" {
		info.Product = s
	}
	if info.Serial == "" {
		info.Serial = readAttr(usbDev, "serial")
	}
	return info, true
}

func readAttr(dir, name string) string {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readUevent(path string) map[string]string {
	m := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	for _, line := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			m[k] = v
		}
	}
	return m
}

type linuxDevice struct {
	fd int
}

func open(path string) (Device, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.EACCES) {
			return nil, fmt.Errorf("open %s: %w (install scripts/10-raphnet.rules to allow access without root)", path, err)
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return &linuxDevice{fd: fd}, nil
}

func (d *linuxDevice) ioctl(req uintptr, b []byte) (int, error) {
	if len(b) == 0 {
		return 0, errors.New("hid: empty report buffer")
	}
	for {
		n, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(d.fd), req, uintptr(unsafe.Pointer(&b[0])))
		if errno == unix.EINTR {
			continue
		}
		if errno != 0 {
			return 0, errno
		}
		return int(n), nil
	}
}

func (d *linuxDevice) SendFeatureReport(b []byte) (int, error) {
	return d.ioctl(hidiocSFeature(len(b)), b)
}

func (d *linuxDevice) GetFeatureReport(b []byte) (int, error) {
	return d.ioctl(hidiocGFeature(len(b)), b)
}

func (d *linuxDevice) Close() error {
	return unix.Close(d.fd)
}
