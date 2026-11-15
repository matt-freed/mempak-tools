// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2007-2024 Raphael Assenat (gcn64tools); Go port (C) 2026 Matt Freed.

package main

// Offline commands: they work on image files and never touch the adapter.

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matt-freed/mempak-tools/internal/mempak"
)

func loadImage(path string) (*mempak.Image, error) {
	img, err := mempak.LoadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not load mempak file '%s': %w", path, err)
	}
	return img, nil
}

// saveImage writes img back in its own format.
func saveImage(img *mempak.Image, path string) error {
	if err := img.SaveFile(path, img.Format); err != nil {
		return fmt.Errorf("could not write mempak file '%s': %w", path, err)
	}
	return nil
}

// code formats a game or publisher code: as text if printable, else in hex.
func code(b []byte) string {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return fmt.Sprintf("%x", b)
		}
	}
	return string(b)
}

// padRight pads s with spaces to width terminal columns. The kana of the
// N64 character set are full-width and take two columns.
func padRight(s string, width int) string {
	w := 0
	for _, r := range s {
		if r >= 0x3000 {
			w += 2
		} else {
			w++
		}
	}
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func newLsCmd() *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "ls PAKFILE",
		Short: "List notes",
		Long: "List the notes (saves) stored in a mempak image.\n" +
			"Raw (.mpk, .bin) and DexDrive (.n64) images are accepted.",
		Args: args(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, a []string) error {
			img, err := loadImage(a[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "Image: %s (%s format)\n", a[0], img.Format)

			free, err := img.FreeBlocks()
			if err != nil {
				fmt.Fprintln(stdout, "Mempak invalid (not formatted or corrupted)")
				return errSilent
			}
			fmt.Fprintf(stdout, "Blocks used: %d / %d\n\n", mempak.TotalBlocks-free, mempak.TotalBlocks)

			for slot := range mempak.NumNotes {
				fmt.Fprintf(stdout, "Note %2d: ", slot)
				n, err := img.Note(slot)
				if err != nil {
					fmt.Fprintln(stdout, "Error!")
					continue
				}
				if !n.Valid {
					fmt.Fprintln(stdout, "Free")
					continue
				}
				fmt.Fprintf(stdout, "%s (%3d blocks)", padRight(n.Name(), 20), n.Blocks)
				if verbose {
					fmt.Fprintf(stdout, "  game %s  publisher %s", code(n.GameCode()), code(n.Publisher()))
				}
				if c := img.Comments[slot]; c != "" {
					fmt.Fprintf(stdout, "  { %s }", c)
				}
				fmt.Fprintln(stdout)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "also show game code, publisher code and region")
	return cmd
}

func newRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm PAKFILE NOTE",
		Short: "Delete a note",
		Long: "Delete note NOTE (0-15, as shown by 'mempak ls') from a mempak image.\n" +
			"The image file is modified in place.",
		Args: args(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, a []string) error {
			slot, err := parseSlot(a[1])
			if err != nil {
				return err
			}
			img, err := loadImage(a[0])
			if err != nil {
				return err
			}
			n, err := img.Note(slot)
			if err != nil {
				return fmt.Errorf("could not get note %d: %w", slot, err)
			}
			if !n.Valid {
				return fmt.Errorf("note %d is already free", slot)
			}
			fmt.Fprintf(stdout, "Deleting note %d: %s (%d blocks)\n", slot, n.RawName(), n.Blocks)
			if err := img.DeleteNote(slot); err != nil {
				return fmt.Errorf("error deleting note: %w", err)
			}
			img.Comments[slot] = "" // a deleted note no longer owns its comment
			return saveImage(img, a[0])
		},
	}
}

func newExtractCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "extract PAKFILE NOTE OUTFILE",
		Short: "Export a note to a note file",
		Long:  "Export note NOTE (0-15, as shown by 'mempak ls') to a standalone note file.",
		Args:  args(cobra.ExactArgs(3)),
		RunE: func(cmd *cobra.Command, a []string) error {
			slot, err := parseSlot(a[1])
			if err != nil {
				return err
			}
			img, err := loadImage(a[0])
			if err != nil {
				return err
			}
			note, err := img.ExportNote(slot)
			if err != nil {
				return fmt.Errorf("could not export note %d: %w", slot, err)
			}
			if err := os.WriteFile(a[2], note, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "Exported note %d to '%s'\n", slot, a[2])
			return nil
		},
	}
}

func newInsertCmd() *cobra.Command {
	var slotStr, dstStr, comment string
	cmd := &cobra.Command{
		Use:   "insert PAKFILE NOTEFILE",
		Short: "Import a note file",
		Long: "Import a note file into a mempak image. The image file is modified in place.\n" +
			"Without -d or -s the note goes to the first free slot.",
		Args: args(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, a []string) error {
			flags := cmd.Flags()
			if flags.Changed("slot") && flags.Changed("dst") {
				return usagef("use either -d or -s, not both")
			}
			slot, replaceOnly := -1, false
			var err error
			switch {
			case flags.Changed("slot"):
				slot, err = parseSlot(slotStr)
			case flags.Changed("dst"):
				slot, err = parseSlot(dstStr)
				replaceOnly = true
			}
			if err != nil {
				return err
			}
			if len(comment) > mempak.MaxCommentLen {
				return usagef("comment too long (%d characters max.)", mempak.MaxCommentLen)
			}

			img, err := loadImage(a[0])
			if err != nil {
				return err
			}
			b, err := os.ReadFile(a[1])
			if err != nil {
				return err
			}
			header, data, err := mempak.ParseNoteFile(b)
			if err != nil {
				return fmt.Errorf("could not import note '%s': %w", a[1], err)
			}
			if slot >= 0 {
				if old, err := img.Note(slot); err == nil && old.Valid {
					fmt.Fprintf(stdout, "Overwriting note %d\n", slot)
				}
			}
			used, err := img.InsertNote(header, data, slot, replaceOnly)
			if errors.Is(err, mempak.ErrSlotEmpty) {
				err = fmt.Errorf("no note in slot %d", slot)
			}
			if err != nil {
				return fmt.Errorf("could not import note '%s': %w", a[1], err)
			}
			fmt.Fprintf(stdout, "Note imported into slot %d\n", used)

			// The slot may previously have held another note's comment.
			img.Comments[used] = ""
			if flags.Changed("comment") {
				if img.Format != mempak.FormatN64 {
					fmt.Fprintf(os.Stderr, "Warning: Ignoring comment since it cannot be stored in %s format. Use the n64 format instead.\n", img.Format)
				} else {
					img.Comments[used] = comment
				}
			}
			return saveImage(img, a[0])
		},
	}
	f := cmd.Flags()
	f.StringVarP(&slotStr, "slot", "s", "", "write to note slot `N` (0-15), empty or not")
	f.StringVarP(&dstStr, "dst", "d", "", "overwrite note slot `N` (0-15); the slot must not be empty")
	f.StringVarP(&comment, "comment", "c", "", "note comment `TEXT` (only stored in .n64 images)")
	return cmd
}

func newFormatCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "format OUTFILE",
		Short: "Create an empty image",
		Long:  "Create a new, empty (formatted) mempak image file.\n\n" + formatsHelp,
		Args:  args(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, a []string) error {
			f, err := outputFormat(format, a[0])
			if err != nil {
				return err
			}
			if err := mempak.New().SaveFile(a[0], f); err != nil {
				return fmt.Errorf("could not write mempak file '%s': %w", a[0], err)
			}
			fmt.Fprintf(stdout, "Wrote empty mempak image '%s' (%s format)\n", a[0], f)
			return nil
		},
	}
	cmd.Flags().StringVarP(&format, "format", "f", "", "output `FMT` (default: from extension, else mpk)")
	return cmd
}

func newConvertCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "convert INFILE OUTFILE",
		Short: "Convert between image formats",
		Long:  "Convert a mempak image between formats.\n\n" + formatsHelp,
		Args:  args(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, a []string) error {
			f, err := outputFormat(format, a[1])
			if err != nil {
				return err
			}
			img, err := loadImage(a[0])
			if err != nil {
				return err
			}
			if err := img.SaveFile(a[1], f); err != nil {
				return fmt.Errorf("could not write mempak file '%s': %w", a[1], err)
			}
			fmt.Fprintf(stdout, "Converted '%s' (%s) to '%s' (%s)\n", a[0], img.Format, a[1], f)
			return nil
		},
	}
	cmd.Flags().StringVarP(&format, "format", "f", "", "output `FMT` (default: from extension, else mpk)")
	return cmd
}
