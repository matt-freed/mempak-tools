// SPDX-License-Identifier: GPL-3.0-or-later

package main

// The inspect commands show what a pak image or a note file holds, down to
// the bytes. Like the other offline commands, they never touch the adapter.

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matt-freed/mempak-tools/internal/mempak"
)

func newInspectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect COMMAND",
		Short: "Show the layout and contents of a pak image or note file",
		Long: "Show the layout and contents of a pak image ('inspect pak') or of a note file\n" +
			"('inspect note'). With -x, both also hex dump the bytes.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, a []string) error {
			if len(a) > 0 {
				return usagef("unknown inspect command '%s'", a[0])
			}
			return helpOnly(cmd, a)
		},
	}
	cmd.AddCommand(newInspectPakCmd(), newInspectNoteCmd())
	return cmd
}

// pakSections are the sections 'inspect pak' can show field by field.
var pakSections = []string{"id"}

func newInspectPakCmd() *cobra.Command {
	var hex bool
	cmd := &cobra.Command{
		Use:   "pak PAKFILE [SECTION]",
		Short: "Show the layout of a pak image, or one section of it",
		Long: "Show the layout of a pak image: the ID sector, the two TOCs, the note table\n" +
			"and the data blocks, each run of blocks labelled with the note that owns it.\n\n" +
			"With SECTION, show that section field by field instead. Sections:\n" +
			"  id    sector 0: the label and the four ID block copies\n\n" +
			"With -x, also show the bytes behind each line: after the layout, a hex dump\n" +
			"of each section; with SECTION, the bytes of each field under it. Offsets are\n" +
			"from the start of the image.\n\n" +
			"An image without a valid filesystem is still shown, but the command exits\n" +
			"with status 1, like 'mempak ls'.",
		Args:      args(cobra.RangeArgs(1, 2)),
		ValidArgs: pakSections,
		RunE: func(cmd *cobra.Command, a []string) error {
			section := ""
			if len(a) == 2 {
				section = a[1]
				if !slices.Contains(pakSections, section) {
					return usagef("unknown section '%s' (sections: %s)", section, strings.Join(pakSections, ", "))
				}
			}
			img, err := loadImage(a[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "Image: %s\n", a[0])
			if img.WrongSize() {
				fmt.Fprintf(os.Stderr, "Warning: '%s' is not 32 KiB; showing its first 32 KiB as a raw pak image\n", a[0])
			}
			valid := img.Validate()
			multiBank := errors.Is(valid, mempak.ErrMultiBank)
			switch {
			case multiBank && section == "":
				return valid // its layout differs; the sections would be wrong
			case multiBank:
				// Sector 0 has the same layout on every pak, so it can be shown.
				fmt.Fprintf(os.Stderr, "Warning: %v\n", valid)
				valid = nil
			case valid != nil:
				fmt.Fprintln(stdout, "Mempak invalid (not formatted or corrupted)")
			case section == "":
				warnDamagedID(img) // the id section shows the damage itself
			}

			sections := img.Sections()
			heading := func(s mempak.Section) string {
				return fmt.Sprintf("0x%04x-0x%04x  %s", s.Offset, s.Offset+len(s.Data)-1, s.Label)
			}
			fmt.Fprintln(stdout)
			switch section {
			case "":
				// The layout first, then (with -x) the same sections with
				// their bytes, as 'inspect note' shows its summary first.
				for _, s := range sections {
					fmt.Fprintln(stdout, heading(s))
				}
				for _, s := range sections {
					if !hex {
						break
					}
					fmt.Fprintf(stdout, "\n%s:\n", heading(s))
					if err := mempak.HexdumpAt(stdout, s.Data, s.Offset); err != nil {
						return err
					}
				}
			case "id":
				fmt.Fprintln(stdout, heading(sections[0]))
				if err := printFields(img.IDSectorFields(), hex); err != nil {
					return err
				}
			}
			if valid != nil {
				return errSilent
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&hex, "hex", "x", false, "also show the bytes of each section, or of each field")
	return cmd
}

// printFields prints decoded fields as an indented table: offset, size in
// bytes, name and value, with sub-fields indented under their field. With
// hex, each field without sub-fields is followed by its bytes; a field with
// sub-fields has its bytes shown under them, which cover all of it.
func printFields(fields []mempak.Field, hex bool) error {
	var width func([]mempak.Field, int) int
	width = func(fs []mempak.Field, depth int) int {
		w := 0
		for _, f := range fs {
			w = max(w, 2*depth+len(f.Name), width(f.Fields, depth+1))
		}
		return w
	}
	w := width(fields, 0)
	var print func([]mempak.Field, int) error
	print = func(fs []mempak.Field, depth int) error {
		indent := strings.Repeat("  ", depth)
		for _, f := range fs {
			fmt.Fprintf(stdout, "  %s0x%04x %3d  %-*s  %s\n", indent, f.Offset, len(f.Data), w-2*depth, f.Name, f.Value)
			if len(f.Fields) > 0 {
				if err := print(f.Fields, depth+1); err != nil {
					return err
				}
				continue
			}
			if hex {
				var dump strings.Builder
				mempak.HexdumpAt(&dump, f.Data, f.Offset)
				for line := range strings.Lines(dump.String()) {
					// Line the dump up under the field's name.
					if _, err := fmt.Fprintf(stdout, "  %s            %s", indent, line); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	return print(fields, 0)
}

func newInspectNoteCmd() *cobra.Command {
	var hex bool
	cmd := &cobra.Command{
		Use:   "note NOTEFILE",
		Short: "Show what a note file holds",
		Long: "Show the name, game and publisher codes and size of a note file, as written by\n" +
			"'mempak extract'. With -x, also show its header field by field with each\n" +
			"field's bytes, then hex dump its data.",
		Args: args(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, a []string) error {
			b, err := os.ReadFile(a[0])
			if err != nil {
				return err
			}
			n, data, err := mempak.ReadNoteFile(b)
			if err != nil {
				return fmt.Errorf("could not read note '%s': %w", a[0], err)
			}
			fmt.Fprintf(stdout, "Note file: %s\n", a[0])
			fmt.Fprintf(stdout, "Name:      %s\n", n.Name())
			fmt.Fprintf(stdout, "Game code: %s", mempak.CodeString(n.GameCode()))
			if name, ok := mempak.GameName(n.GameCode()); ok {
				fmt.Fprintf(stdout, " (%s)", name)
			}
			fmt.Fprintln(stdout)
			fmt.Fprintf(stdout, "Publisher: %s\n", mempak.CodeString(n.Publisher()))
			fmt.Fprintf(stdout, "Status:    0x%02x\n", n.Header[0x08])
			fmt.Fprintf(stdout, "Size:      %d blocks (%d bytes of data)\n", n.Blocks, len(data))
			if !hex {
				return nil
			}
			fmt.Fprintln(stdout, "\nHeader:")
			if err := printFields(mempak.NoteFileHeaderFields(b), true); err != nil {
				return err
			}
			fmt.Fprintln(stdout, "\nData (offsets from the start of the data):")
			return mempak.Hexdump(stdout, data)
		},
	}
	cmd.Flags().BoolVarP(&hex, "hex", "x", false, "also show the header field by field, and hex dump the data")
	return cmd
}
