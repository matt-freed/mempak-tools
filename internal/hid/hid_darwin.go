// SPDX-License-Identifier: GPL-3.0-or-later

package hid

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// macOS backend: IOKit's IOHIDManager / IOHIDDevice, called through purego
// so no cgo is needed.

const (
	kCFStringEncodingUTF8   = 0x08000100
	kCFNumberSInt32Type     = 3
	kCFNumberSInt64Type     = 4
	kIOHIDReportTypeFeature = 2
	kIOHIDOptionsTypeNone   = 0
	kIOHIDOptionsTypeSeize  = 1
	kIOReturnSuccess        = 0
	devSrvsPrefix           = "DevSrvsID:"
	coreFoundationPath      = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"
	ioKitPath               = "/System/Library/Frameworks/IOKit.framework/IOKit"
)

var (
	loadOnce sync.Once
	loadErr  error

	cfTypeDictionaryKeyCallBacks   uintptr
	cfTypeDictionaryValueCallBacks uintptr

	cfRelease                 func(cf uintptr)
	cfGetTypeID               func(cf uintptr) uint
	cfStringGetTypeID         func() uint
	cfNumberGetTypeID         func() uint
	cfStringCreateWithCString func(alloc uintptr, cstr *byte, enc uint32) uintptr
	cfStringGetCString        func(s uintptr, buf unsafe.Pointer, size int, enc uint32) bool
	cfNumberCreate            func(alloc uintptr, typ int, value unsafe.Pointer) uintptr
	cfNumberGetValue          func(n uintptr, typ int, value unsafe.Pointer) bool
	cfDictionaryCreateMutable func(alloc uintptr, capacity int, keyCB, valueCB uintptr) uintptr
	cfDictionarySetValue      func(d, key, value uintptr)
	cfSetGetCount             func(s uintptr) int
	cfSetGetValues            func(s uintptr, values unsafe.Pointer)

	ioHIDManagerCreate                func(alloc uintptr, options uint32) uintptr
	ioHIDManagerSetDeviceMatching     func(mgr, dict uintptr)
	ioHIDManagerCopyDevices           func(mgr uintptr) uintptr
	ioHIDDeviceGetProperty            func(dev, key uintptr) uintptr
	ioHIDDeviceGetService             func(dev uintptr) uint32
	ioHIDDeviceCreate                 func(alloc uintptr, service uint32) uintptr
	ioHIDDeviceOpen                   func(dev uintptr, options uint32) int32
	ioHIDDeviceClose                  func(dev uintptr, options uint32) int32
	ioHIDDeviceSetReport              func(dev uintptr, typ int, id int, report unsafe.Pointer, length int) int32
	ioHIDDeviceGetReport              func(dev uintptr, typ int, id int, report unsafe.Pointer, length *int) int32
	ioRegistryEntryGetRegistryEntryID func(entry uint32, id *uint64) int32
	ioRegistryEntryIDMatching         func(id uint64) uintptr
	ioServiceGetMatchingService       func(mainPort uint32, matching uintptr) uint32
	ioObjectRelease                   func(obj uint32) int32
	ioRegistryEntrySearchCFProperty   func(entry uint32, plane *byte, key, alloc uintptr, options uint32) uintptr
)

func load() error {
	loadOnce.Do(func() {
		defer func() {
			// RegisterLibFunc panics on a missing symbol.
			if r := recover(); r != nil {
				loadErr = fmt.Errorf("hid: loading IOKit: %v", r)
			}
		}()
		cf, err := purego.Dlopen(coreFoundationPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("hid: %w", err)
			return
		}
		iokit, err := purego.Dlopen(ioKitPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("hid: %w", err)
			return
		}
		if cfTypeDictionaryKeyCallBacks, err = purego.Dlsym(cf, "kCFTypeDictionaryKeyCallBacks"); err != nil {
			loadErr = err
			return
		}
		if cfTypeDictionaryValueCallBacks, err = purego.Dlsym(cf, "kCFTypeDictionaryValueCallBacks"); err != nil {
			loadErr = err
			return
		}

		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
		purego.RegisterLibFunc(&cfGetTypeID, cf, "CFGetTypeID")
		purego.RegisterLibFunc(&cfStringGetTypeID, cf, "CFStringGetTypeID")
		purego.RegisterLibFunc(&cfNumberGetTypeID, cf, "CFNumberGetTypeID")
		purego.RegisterLibFunc(&cfStringCreateWithCString, cf, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&cfStringGetCString, cf, "CFStringGetCString")
		purego.RegisterLibFunc(&cfNumberCreate, cf, "CFNumberCreate")
		purego.RegisterLibFunc(&cfNumberGetValue, cf, "CFNumberGetValue")
		purego.RegisterLibFunc(&cfDictionaryCreateMutable, cf, "CFDictionaryCreateMutable")
		purego.RegisterLibFunc(&cfDictionarySetValue, cf, "CFDictionarySetValue")
		purego.RegisterLibFunc(&cfSetGetCount, cf, "CFSetGetCount")
		purego.RegisterLibFunc(&cfSetGetValues, cf, "CFSetGetValues")

		purego.RegisterLibFunc(&ioHIDManagerCreate, iokit, "IOHIDManagerCreate")
		purego.RegisterLibFunc(&ioHIDManagerSetDeviceMatching, iokit, "IOHIDManagerSetDeviceMatching")
		purego.RegisterLibFunc(&ioHIDManagerCopyDevices, iokit, "IOHIDManagerCopyDevices")
		purego.RegisterLibFunc(&ioHIDDeviceGetProperty, iokit, "IOHIDDeviceGetProperty")
		purego.RegisterLibFunc(&ioHIDDeviceGetService, iokit, "IOHIDDeviceGetService")
		purego.RegisterLibFunc(&ioHIDDeviceCreate, iokit, "IOHIDDeviceCreate")
		purego.RegisterLibFunc(&ioHIDDeviceOpen, iokit, "IOHIDDeviceOpen")
		purego.RegisterLibFunc(&ioHIDDeviceClose, iokit, "IOHIDDeviceClose")
		purego.RegisterLibFunc(&ioHIDDeviceSetReport, iokit, "IOHIDDeviceSetReport")
		purego.RegisterLibFunc(&ioHIDDeviceGetReport, iokit, "IOHIDDeviceGetReport")
		purego.RegisterLibFunc(&ioRegistryEntryGetRegistryEntryID, iokit, "IORegistryEntryGetRegistryEntryID")
		purego.RegisterLibFunc(&ioRegistryEntryIDMatching, iokit, "IORegistryEntryIDMatching")
		purego.RegisterLibFunc(&ioServiceGetMatchingService, iokit, "IOServiceGetMatchingService")
		purego.RegisterLibFunc(&ioObjectRelease, iokit, "IOObjectRelease")
		purego.RegisterLibFunc(&ioRegistryEntrySearchCFProperty, iokit, "IORegistryEntrySearchCFProperty")
	})
	return loadErr
}

// cfString returns a new CFString; the caller releases it.
func cfString(s string) uintptr {
	b := append([]byte(s), 0)
	return cfStringCreateWithCString(0, &b[0], kCFStringEncodingUTF8)
}

func cfNumber(v int32) uintptr {
	return cfNumberCreate(0, kCFNumberSInt32Type, unsafe.Pointer(&v))
}

func stringProperty(dev uintptr, key string) string {
	k := cfString(key)
	defer cfRelease(k)
	v := ioHIDDeviceGetProperty(dev, k) // not owned
	if v == 0 || cfGetTypeID(v) != cfStringGetTypeID() {
		return ""
	}
	buf := make([]byte, 1024)
	if !cfStringGetCString(v, unsafe.Pointer(&buf[0]), len(buf), kCFStringEncodingUTF8) {
		return ""
	}
	if i := strings.IndexByte(string(buf), 0); i >= 0 {
		buf = buf[:i]
	}
	return string(buf)
}

func intProperty(dev uintptr, key string) (int64, bool) {
	k := cfString(key)
	defer cfRelease(k)
	v := ioHIDDeviceGetProperty(dev, k)
	if v == 0 || cfGetTypeID(v) != cfNumberGetTypeID() {
		return 0, false
	}
	var n int64
	if !cfNumberGetValue(v, kCFNumberSInt64Type, unsafe.Pointer(&n)) {
		return 0, false
	}
	return n, true
}

// searchIntProperty looks up key on a registry entry and its parents, which
// is where the USB interface number lives for most devices.
func searchIntProperty(entry uint32, key string) (int64, bool) {
	k := cfString(key)
	defer cfRelease(k)
	plane := []byte("IOService\x00")
	const iterateRecursivelyParents = 1 | 2
	v := ioRegistryEntrySearchCFProperty(entry, &plane[0], k, 0, iterateRecursivelyParents)
	if v == 0 {
		return 0, false
	}
	defer cfRelease(v)
	var n int64
	if cfGetTypeID(v) != cfNumberGetTypeID() || !cfNumberGetValue(v, kCFNumberSInt64Type, unsafe.Pointer(&n)) {
		return 0, false
	}
	return n, true
}

func enumerate(vendorID, productID uint16) ([]DeviceInfo, error) {
	if err := load(); err != nil {
		return nil, err
	}

	mgr := ioHIDManagerCreate(0, kIOHIDOptionsTypeNone)
	if mgr == 0 {
		return nil, errors.New("hid: IOHIDManagerCreate failed")
	}
	defer cfRelease(mgr)

	match := cfDictionaryCreateMutable(0, 0, cfTypeDictionaryKeyCallBacks, cfTypeDictionaryValueCallBacks)
	defer cfRelease(match)
	setInt := func(key string, v int32) {
		k, n := cfString(key), cfNumber(v)
		cfDictionarySetValue(match, k, n)
		cfRelease(k)
		cfRelease(n)
	}
	setInt("VendorID", int32(vendorID))
	if productID != 0 {
		setInt("ProductID", int32(productID))
	}
	ioHIDManagerSetDeviceMatching(mgr, match)

	set := ioHIDManagerCopyDevices(mgr)
	if set == 0 {
		return nil, nil // no matching devices
	}
	defer cfRelease(set)

	devs := make([]uintptr, cfSetGetCount(set))
	if len(devs) == 0 {
		return nil, nil
	}
	cfSetGetValues(set, unsafe.Pointer(&devs[0]))

	var out []DeviceInfo
	for _, dev := range devs {
		var id uint64
		if ioRegistryEntryGetRegistryEntryID(ioHIDDeviceGetService(dev), &id) != kIOReturnSuccess {
			continue
		}
		info := DeviceInfo{
			Path:      devSrvsPrefix + strconv.FormatUint(id, 10),
			Product:   stringProperty(dev, "Product"),
			Serial:    stringProperty(dev, "SerialNumber"),
			Interface: -1,
		}
		if v, ok := intProperty(dev, "VendorID"); ok {
			info.VendorID = uint16(v)
		}
		if v, ok := intProperty(dev, "ProductID"); ok {
			info.ProductID = uint16(v)
		}
		if v, ok := intProperty(dev, "VersionNumber"); ok {
			info.Release = uint16(v)
		}
		if v, ok := intProperty(dev, "bInterfaceNumber"); ok {
			info.Interface = int(v)
		} else if v, ok := searchIntProperty(ioHIDDeviceGetService(dev), "bInterfaceNumber"); ok {
			info.Interface = int(v)
		}
		out = append(out, info)
	}
	return out, nil
}

type darwinDevice struct {
	dev uintptr
}

func open(path string) (Device, error) {
	if err := load(); err != nil {
		return nil, err
	}
	idStr, ok := strings.CutPrefix(path, devSrvsPrefix)
	if !ok {
		return nil, fmt.Errorf("hid: invalid device path %q", path)
	}
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("hid: invalid device path %q", path)
	}

	// IOServiceGetMatchingService consumes the matching dictionary.
	service := ioServiceGetMatchingService(0, ioRegistryEntryIDMatching(id))
	if service == 0 {
		return nil, fmt.Errorf("hid: device %s not found", path)
	}
	defer ioObjectRelease(service)

	dev := ioHIDDeviceCreate(0, service)
	if dev == 0 {
		return nil, fmt.Errorf("hid: cannot create device for %s", path)
	}
	// Open exclusively, like hidapi does by default, then fall back to
	// shared access.
	if ret := ioHIDDeviceOpen(dev, kIOHIDOptionsTypeSeize); ret != kIOReturnSuccess {
		if ret = ioHIDDeviceOpen(dev, kIOHIDOptionsTypeNone); ret != kIOReturnSuccess {
			cfRelease(dev)
			return nil, fmt.Errorf("hid: cannot open %s (IOReturn 0x%08x)", path, uint32(ret))
		}
	}
	return &darwinDevice{dev: dev}, nil
}

func (d *darwinDevice) SendFeatureReport(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, errors.New("hid: empty report buffer")
	}
	id, data := int(b[0]), b
	if id == 0 {
		data = b[1:] // unnumbered reports are sent without the ID byte
	}
	if len(data) == 0 {
		return 0, errors.New("hid: empty report")
	}
	if ret := ioHIDDeviceSetReport(d.dev, kIOHIDReportTypeFeature, id, unsafe.Pointer(&data[0]), len(data)); ret != kIOReturnSuccess {
		return 0, fmt.Errorf("hid: set feature report failed (IOReturn 0x%08x)", uint32(ret))
	}
	return len(b), nil
}

func (d *darwinDevice) GetFeatureReport(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, errors.New("hid: empty report buffer")
	}
	id, data := int(b[0]), b
	if id == 0 {
		data = b[1:]
	}
	if len(data) == 0 {
		return 0, errors.New("hid: empty report buffer")
	}
	n := len(data)
	if ret := ioHIDDeviceGetReport(d.dev, kIOHIDReportTypeFeature, id, unsafe.Pointer(&data[0]), &n); ret != kIOReturnSuccess {
		return 0, fmt.Errorf("hid: get feature report failed (IOReturn 0x%08x)", uint32(ret))
	}
	if id == 0 {
		n++ // count the report ID byte, like the other backends
	}
	return n, nil
}

func (d *darwinDevice) Close() error {
	ioHIDDeviceClose(d.dev, kIOHIDOptionsTypeNone)
	cfRelease(d.dev)
	return nil
}
