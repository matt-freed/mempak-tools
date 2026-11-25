// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matt-freed/mempak-tools/internal/mempak"
)

func newSerialCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serial COMMAND",
		Short: "Show or set an image's serial number",
		Long: "Show ('serial show') or set ('serial set') the serial number of a mempak image.\n\n" +
			"The serial number is 24 bytes in the pak's ID block. Games only compare it,\n" +
			"to notice that a different pak was inserted, so any value works. Paks\n" +
			"formatted from the same template (including by 'mempak format') share one\n" +
			"serial; giving each pak its own lets you tell their images apart.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, a []string) error {
			if len(a) > 0 {
				return usagef("unknown serial command '%s'", a[0])
			}
			return helpOnly(cmd, a)
		},
	}
	cmd.AddCommand(newSerialShowCmd(), newSerialSetCmd())
	return cmd
}

func newSerialShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show PAKFILE",
		Short: "Show an image's serial number",
		Long: "Show the serial number of a mempak image: 48 hex digits, followed by the\n" +
			"text in quotes when it is text (as 'mempak serial set' stores it).",
		Args: args(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, a []string) error {
			img, err := loadImage(a[0])
			if err != nil {
				return err
			}
			serial, err := img.Serial()
			if err != nil {
				return fmt.Errorf("'%s': %w", a[0], err)
			}
			fmt.Fprintf(stdout, "Serial: %s\n", formatSerial(serial))
			return nil
		},
	}
}

func newSerialSetCmd() *cobra.Command {
	var random, isHex bool
	cmd := &cobra.Command{
		Use:   "set PAKFILE (SERIAL | --random)",
		Short: "Set an image's serial number",
		Long: "Set the serial number of a mempak image. The image file is modified in place.\n\n" +
			"SERIAL is text of up to 24 printable ASCII characters, padded with zero\n" +
			"bytes, or with --hex, exactly 24 bytes as 48 hex digits (spaces and colons\n" +
			"are ignored). --random sets 24 random bytes instead.\n\n" +
			"All four ID block copies are rewritten, so damaged copies are repaired too.\n" +
			"Write the image back with 'mempak adapter write' to change the pak itself.\n" +
			"A game that is running when the serial changes sees a different pak.",
		Args: args(cobra.RangeArgs(1, 2)),
		RunE: func(cmd *cobra.Command, a []string) error {
			path := a[0]
			given := len(a) == 2
			switch {
			case random && given:
				return usagef("give either SERIAL or --random, not both")
			case random && isHex:
				return usagef("--hex needs a SERIAL, not --random")
			case !random && !given:
				return usagef("give a SERIAL, or --random")
			}
			var serial mempak.Serial
			if random {
				serial = mempak.RandomSerial()
			} else {
				var err error
				if serial, err = parseSerial(a[1], isHex); err != nil {
					return err
				}
			}

			img, err := loadImage(path)
			if err != nil {
				return err
			}
			if img.WrongSize() {
				return fmt.Errorf("cannot set the serial of '%s': %w", path, mempak.ErrWrongSize)
			}
			old, err := img.Serial()
			if err != nil {
				return fmt.Errorf("cannot set the serial of '%s': %w", path, err)
			}
			damaged, _ := img.DamagedIDBlocks()
			if old == serial && len(damaged) == 0 {
				fmt.Fprintf(stdout, "'%s' already has serial %s\n", path, formatSerial(serial))
				return nil
			}
			if err := img.SetSerial(serial); err != nil {
				return fmt.Errorf("cannot set the serial of '%s': %w", path, err)
			}
			if err := saveImage(img, path); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "Old serial: %s\nNew serial: %s\n", formatSerial(old), formatSerial(serial))
			if len(damaged) > 0 {
				fmt.Fprintf(stdout, "Damaged ID block copies %s were rewritten\n", joinInts(damaged))
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&random, "random", "r", false, "set a random serial number")
	f.BoolVar(&isHex, "hex", false, "read SERIAL as 24 bytes in hex")
	return cmd
}

// parseSerial reads a serial given on the command line: text, or with isHex,
// 24 bytes in hex.
func parseSerial(s string, isHex bool) (mempak.Serial, error) {
	var serial mempak.Serial
	if isHex {
		digits := strings.NewReplacer(" ", "", ":", "").Replace(s)
		b, err := hex.DecodeString(digits)
		if err != nil || len(b) != mempak.SerialSize {
			return serial, usagef("serial '%s' is not %d bytes in hex (%d hex digits)", s, mempak.SerialSize, 2*mempak.SerialSize)
		}
		copy(serial[:], b)
		return serial, nil
	}
	if s == "" || len(s) > mempak.SerialSize {
		return serial, usagef("serial '%s' must be 1 to %d characters", s, mempak.SerialSize)
	}
	for _, r := range s {
		if r < 0x20 || r > 0x7E {
			return serial, usagef("serial '%s' must be printable ASCII; use --hex for other bytes", s)
		}
	}
	copy(serial[:], s)
	return serial, nil
}

// formatSerial shows a serial in hex, and as text too when it is text.
func formatSerial(s mempak.Serial) string {
	out := hex.EncodeToString(s[:])
	if text, ok := s.Text(); ok {
		out += fmt.Sprintf(" (\"%s\")", text)
	}
	return out
}

// joinInts formats numbers as "1, 2, 3".
func joinInts(v []int) string {
	s := make([]string, len(v))
	for i, n := range v {
		s[i] = fmt.Sprint(n)
	}
	return strings.Join(s, ", ")
}
