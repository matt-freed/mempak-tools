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
		Short: "Show the layout and bytes of a pak image or note file",
		Long: "Show the layout of a pak image ('inspect pak') or note file ('inspect note'),\n" +
			"then hex dump it part by part. With -f, the parts are split into their fields,\n" +
			"each with its bytes and what they mean, where that is known.",
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

// pakSections are the sections 'inspect pak' can be limited to.
var pakSections = []string{mempak.SectionID, mempak.SectionTOC, mempak.SectionNotes, mempak.SectionData}

// sectionFields returns the decoded fields of a section, or nil for the data
// blocks, which are only shown as hex since their layout is up to each game.
func sectionFields(img *mempak.Image, s mempak.Section) []mempak.Field {
	switch s.Kind {
	case mempak.SectionID:
		return img.IDSectorFields()
	case mempak.SectionTOC:
		return img.TOCFields(s.Offset / mempak.BlockSize)
	case mempak.SectionNotes:
		return img.NoteTableFields()
	}
	return nil
}

func newInspectPakCmd() *cobra.Command {
	var fields bool
	cmd := &cobra.Command{
		Use:   "pak PAKFILE [SECTION]",
		Short: "Show the layout and bytes of a pak image",
		Long: "Show the layout of a pak image, then hex dump it section by section: the ID\n" +
			"sector, the two TOCs, the note table and the data blocks, each run of blocks\n" +
			"labelled with the note that owns it. Offsets are from the start of the image.\n\n" +
			"With -f, each section is split into its fields, each with its bytes and what\n" +
			"they mean, where that is known. The data blocks, whose layout is up to each\n" +
			"game, are still shown as one dump per run of blocks.\n\n" +
			"With SECTION, show only that section, without the layout:\n" +
			"  id     sector 0: the label and the four ID block copies\n" +
			"  toc    sectors 1-2: the TOC and its backup\n" +
			"  notes  sectors 3-4: the note table\n" +
			"  data   blocks 5-127\n\n" +
			"An image without a valid filesystem is still shown, but the command exits\n" +
			"with status 1, like 'mempak ls'.",
		Args:      args(cobra.RangeArgs(1, 2)),
		ValidArgs: pakSections,
		RunE: func(cmd *cobra.Command, a []string) error {
			only := ""
			if len(a) == 2 {
				only = a[1]
				if !slices.Contains(pakSections, only) {
					return usagef("unknown section '%s' (sections: %s)", only, strings.Join(pakSections, ", "))
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
			case multiBank && only == mempak.SectionID:
				// Sector 0 has the same layout on every pak, so it can be shown.
				fmt.Fprintf(os.Stderr, "Warning: %v\n", valid)
				valid = nil
			case multiBank:
				return valid // its layout differs; the sections would be wrong
			case valid != nil:
				fmt.Fprintln(stdout, "Mempak invalid (not formatted or corrupted)")
			case only == "" || only == mempak.SectionID:
				warnDamagedID(img)
			}

			var sections []mempak.Section
			for _, s := range img.Sections() {
				if only == "" || s.Kind == only {
					sections = append(sections, s)
				}
			}
			heading := func(s mempak.Section) string {
				return fmt.Sprintf("0x%04x-0x%04x  %s", s.Offset, s.Offset+len(s.Data)-1, s.Label)
			}
			// The layout first, then each section's bytes, as 'inspect note'
			// shows its summary first. With one section the layout would
			// only repeat the headings below, so it is left out.
			if only == "" {
				fmt.Fprintln(stdout)
				for _, s := range sections {
					fmt.Fprintln(stdout, heading(s))
				}
			}
			for _, s := range sections {
				fmt.Fprintf(stdout, "\n%s:\n", heading(s))
				if f := sectionFields(img, s); fields && f != nil {
					err = printFields(f)
				} else {
					err = mempak.HexdumpAt(stdout, s.Data, s.Offset)
				}
				if err != nil {
					return err
				}
			}
			if valid != nil {
				return errSilent
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&fields, "fields", "f", false, "split each section into its fields, with each field's bytes")
	return cmd
}

// printFields prints decoded fields as an indented table: offset, size in
// bytes, name and what the bytes mean (if anything is known), with sub-fields indented under their field. Each
// field without sub-fields is followed by its bytes; a field with sub-fields
// has its bytes shown under them, which cover all of it.
func printFields(fields []mempak.Field) error {
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
			line := fmt.Sprintf("  %s0x%04x %3d  %-*s  %s", indent, f.Offset, len(f.Data), w-2*depth, f.Name, f.Value)
			fmt.Fprintln(stdout, strings.TrimRight(line, " "))
			if len(f.Fields) > 0 {
				if err := print(f.Fields, depth+1); err != nil {
					return err
				}
				continue
			}
			var dump strings.Builder
			mempak.HexdumpAt(&dump, f.Data, f.Offset)
			for line := range strings.Lines(dump.String()) {
				// Line the dump up under the field's name.
				if _, err := fmt.Fprintf(stdout, "  %s            %s", indent, line); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return print(fields, 0)
}

func newInspectNoteCmd() *cobra.Command {
	var fields bool
	cmd := &cobra.Command{
		Use:   "note NOTEFILE",
		Short: "Show what a note file holds, and its bytes",
		Long: "Show the name, game and publisher codes and size of a note file, as written by\n" +
			"'mempak extract', then hex dump its 32-byte header and its data. With -f, the\n" +
			"header is split into its fields, each with its bytes and what they mean, where\n" +
			"that is known.",
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

			fmt.Fprintln(stdout, "\nHeader:")
			if fields {
				err = printFields(mempak.NoteFileHeaderFields(b))
			} else {
				err = mempak.Hexdump(stdout, n.Header[:])
			}
			if err != nil {
				return err
			}
			fmt.Fprintln(stdout, "\nData (offsets from the start of the data):")
			return mempak.Hexdump(stdout, data)
		},
	}
	cmd.Flags().BoolVarP(&fields, "fields", "f", false, "split the header into its fields, with each field's bytes")
	return cmd
}
