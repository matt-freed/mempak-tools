// SPDX-License-Identifier: GPL-3.0-or-later

package hid

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows backend: hid.dll plus DeviceIoControl feature-report requests.

var (
	hidDLL                    = windows.NewLazySystemDLL("hid.dll")
	procHidDGetHidGuid        = hidDLL.NewProc("HidD_GetHidGuid")
	procHidDGetAttributes     = hidDLL.NewProc("HidD_GetAttributes")
	procHidDGetProductString  = hidDLL.NewProc("HidD_GetProductString")
	procHidDGetSerialString   = hidDLL.NewProc("HidD_GetSerialNumberString")
	procHidDGetPreparsedData  = hidDLL.NewProc("HidD_GetPreparsedData")
	procHidDFreePreparsedData = hidDLL.NewProc("HidD_FreePreparsedData")
	procHidPGetCaps           = hidDLL.NewProc("HidP_GetCaps")
)

const (
	// CTL_CODE(FILE_DEVICE_KEYBOARD, 100, METHOD_IN_DIRECT / METHOD_OUT_DIRECT, FILE_ANY_ACCESS)
	ioctlHIDSetFeature = 0x000B0191
	ioctlHIDGetFeature = 0x000B0192

	hidpStatusSuccess = 0x00110000
)

type hiddAttributes struct {
	Size          uint32
	VendorID      uint16
	ProductID     uint16
	VersionNumber uint16
	_             uint16
}

type hidpCaps struct {
	Usage                     uint16
	UsagePage                 uint16
	InputReportByteLength     uint16
	OutputReportByteLength    uint16
	FeatureReportByteLength   uint16
	Reserved                  [17]uint16
	NumberLinkCollectionNodes uint16
	NumberInputButtonCaps     uint16
	NumberInputValueCaps      uint16
	NumberInputDataIndices    uint16
	NumberOutputButtonCaps    uint16
	NumberOutputValueCaps     uint16
	NumberOutputDataIndices   uint16
	NumberFeatureButtonCaps   uint16
	NumberFeatureValueCaps    uint16
	NumberFeatureDataIndices  uint16
}

func openHandle(path string, access uint32) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
}

func hidString(proc *windows.LazyProc, h windows.Handle) string {
	buf := make([]uint16, 256)
	r, _, _ := proc.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2))
	if r == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// interfaceFromPath extracts the USB interface number from a path such as
// \\?\hid#vid_289b&pid_0038&mi_01#...
func interfaceFromPath(path string) int {
	lower := strings.ToLower(path)
	i := strings.Index(lower, "&mi_")
	if i < 0 || len(lower) < i+6 {
		return -1
	}
	n, err := strconv.ParseUint(lower[i+4:i+6], 16, 8)
	if err != nil {
		return -1
	}
	return int(n)
}

func enumerate(vendorID, productID uint16) ([]DeviceInfo, error) {
	if err := hidDLL.Load(); err != nil {
		return nil, err
	}
	var guid windows.GUID
	procHidDGetHidGuid.Call(uintptr(unsafe.Pointer(&guid)))

	paths, err := windows.CM_Get_Device_Interface_List("", &guid, windows.CM_GET_DEVICE_INTERFACE_LIST_PRESENT)
	if err != nil {
		return nil, fmt.Errorf("hid: listing devices: %w", err)
	}

	var out []DeviceInfo
	for _, path := range paths {
		// Access 0 is enough to read attributes, even for devices that are
		// opened exclusively elsewhere (keyboards, mice).
		h, err := openHandle(path, 0)
		if err != nil {
			continue
		}
		attr := hiddAttributes{Size: uint32(unsafe.Sizeof(hiddAttributes{}))}
		r, _, _ := procHidDGetAttributes.Call(uintptr(h), uintptr(unsafe.Pointer(&attr)))
		if r == 0 || attr.VendorID != vendorID || (productID != 0 && attr.ProductID != productID) {
			windows.CloseHandle(h)
			continue
		}
		out = append(out, DeviceInfo{
			Path:      path,
			VendorID:  attr.VendorID,
			ProductID: attr.ProductID,
			Release:   attr.VersionNumber,
			Product:   hidString(procHidDGetProductString, h),
			Serial:    hidString(procHidDGetSerialString, h),
			Interface: interfaceFromPath(path),
		})
		windows.CloseHandle(h)
	}
	return out, nil
}

type windowsDevice struct {
	h          windows.Handle
	featureLen int
}

func open(path string) (Device, error) {
	if err := hidDLL.Load(); err != nil {
		return nil, err
	}
	h, err := openHandle(path, windows.GENERIC_READ|windows.GENERIC_WRITE)
	if err != nil {
		return nil, fmt.Errorf("hid: open %s: %w", path, err)
	}

	var pp uintptr
	if r, _, _ := procHidDGetPreparsedData.Call(uintptr(h), uintptr(unsafe.Pointer(&pp))); r == 0 {
		windows.CloseHandle(h)
		return nil, errors.New("hid: HidD_GetPreparsedData failed")
	}
	var caps hidpCaps
	status, _, _ := procHidPGetCaps.Call(pp, uintptr(unsafe.Pointer(&caps)))
	procHidDFreePreparsedData.Call(pp)
	if status != hidpStatusSuccess {
		windows.CloseHandle(h)
		return nil, errors.New("hid: HidP_GetCaps failed")
	}
	return &windowsDevice{h: h, featureLen: int(caps.FeatureReportByteLength)}, nil
}

// buffer returns b padded or truncated to the device's feature report
// length, which Windows requires.
func (d *windowsDevice) buffer(b []byte) []byte {
	if d.featureLen == 0 || len(b) == d.featureLen {
		return b
	}
	buf := make([]byte, d.featureLen)
	copy(buf, b)
	return buf
}

func (d *windowsDevice) SendFeatureReport(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, errors.New("hid: empty report buffer")
	}
	buf := d.buffer(b)
	var n uint32
	if err := windows.DeviceIoControl(d.h, ioctlHIDSetFeature, &buf[0], uint32(len(buf)), nil, 0, &n, nil); err != nil {
		return 0, fmt.Errorf("hid: set feature report: %w", err)
	}
	return len(b), nil
}

func (d *windowsDevice) GetFeatureReport(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, errors.New("hid: empty report buffer")
	}
	buf := d.buffer(b)
	var n uint32
	if err := windows.DeviceIoControl(d.h, ioctlHIDGetFeature, &buf[0], uint32(len(buf)), &buf[0], uint32(len(buf)), &n, nil); err != nil {
		return 0, fmt.Errorf("hid: get feature report: %w", err)
	}
	if &buf[0] != &b[0] {
		copy(b, buf)
	}
	// Without numbered reports the count excludes the report ID byte.
	if b[0] == 0 {
		n++
	}
	return min(int(n), len(b)), nil
}

func (d *windowsDevice) Close() error {
	return windows.CloseHandle(d.h)
}
