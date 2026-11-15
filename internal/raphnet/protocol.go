// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2007-2024 Raphael Assenat (gcn64tools); Go port (C) 2026 Matt Freed.

package raphnet

// Adapter requests (the first byte of a command), from gcn64tools requests.h.
const (
	rqSuspendPolling       = 0x03
	rqGetVersion           = 0x04
	rqGetControllerType    = 0x06
	rqGetSupportedRequests = 0xF0
	rqGetSupportedModes    = 0xF1
	rqGetSupportedCfg      = 0xF2
	rqGetSupportedMappings = 0xF3
	rqRawSICommand         = 0x80
	rqBlockIO              = 0x81
)

// N64 joybus commands.
const (
	n64GetCapabilities = 0x00
	n64ExpansionRead   = 0x02
	n64ExpansionWrite  = 0x03
)

// Controller types reported by rqGetControllerType.
const (
	ctlN64    = 1
	ctlN64New = 104
)

// IsN64Controller reports whether a controller type is an N64 controller,
// which is the only kind that takes a pak.
func IsN64Controller(t int) bool { return t == ctlN64 || t == ctlN64New }

var controllerNames = map[int]string{
	0: "No controller", 100: "No controller",
	1: "N64 Controller", 104: "N64 Controller",
	2: "GC Controller", 105: "GC Controller",
	3: "GC Keyboard", 141: "GC Keyboard",
	5:   "N64 mouse",
	101: "Classic controller",
	102: "SNES controller",
	103: "NES controller",
	106: "Megadrive controller",
	107: "SMS controller",
	108: "NTT Data controller",
	109: "SNES mouse",
	110: "PC engine controller",
	111: "PC engine 6 button controller",
	112: "Nunchuk",
	113: "Classic controller pro",
	114: "Taiko drum",
	115: "XE-1AP",
	116: "Saturn Controller",
	117: "Saturn Analog Controller",
	118: "Saturn Mouse",
	119: "PSX Digital",
	120: "PSX Analog",
	121: "NEGCON",
	122: "PSX Mouse",
	123: "Megadrive 6 button controller",
	124: "SMS Paddle Control",
	125: "Megadrive mouse",
	126: "MSX 2-button controller",
	127: "Guitar controller",
	128: "uDraw tablet",
	129: "3DO controller",
	130: "Wii drum controller",
	131: "Wii turntable",
	132: "Famicom gamepad",
	133: "Famicom keyboard",
	134: "Famicom network controller",
	135: "MSX Mouse",
	139: "Neo Geo controller",
	140: "N64 Keyboard",
	142: "2-button PC joystick",
	143: "4-button PC joystick",
	144: "Drawsome tablet",
	145: "Dreamcast Controller",
	146: "Dreamcast Arcade Stick",
	147: "Dreamcast mouse",
	148: "Dreamcast keyboard",
	149: "Dreamcast Steering Wheel",
	150: "PCFX Controller",
	151: "Intellivision Controller",
	152: "Virtual Boy Controller",
	153: "PSX Analog Joystick",
	154: "Famicom trackball",
}

// ControllerName returns a readable name for a controller type.
func ControllerName(t int) string {
	if n, ok := controllerNames[t]; ok {
		return n
	}
	return "Unknown"
}
