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

// saveImage writes img back to path.
func saveImage(img *mempak.Image, path string) error {
	if err := img.SaveFile(path); err != nil {
		return fmt.Errorf("could not write mempak file '%s': %w", path, err)
	}
	return nil
}

// warnDamagedID tells the user when some copies of the pak's ID block are
// damaged. The pak still works while one copy is intact.
func warnDamagedID(img *mempak.Image) {
	damaged, err := img.DamagedIDBlocks()
	if err != nil || len(damaged) == 0 {
		return
	}
	names := make([]string, len(damaged))
	for i, d := range damaged {
		names[i] = fmt.Sprint(d)
	}
	fmt.Fprintf(os.Stderr, "Warning: %d of 4 ID block copies are damaged (copy %s; 0 is the primary). "+
		"The pak is still readable from the remaining copies.\n", len(damaged), strings.Join(names, ", "))
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
			"Images are 32 KiB raw .mpk files.",
		Args: args(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, a []string) error {
			img, err := loadImage(a[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "Image: %s\n", a[0])
			if img.WrongSize() {
				fmt.Fprintf(os.Stderr, "Warning: '%s' is not 32 KiB; showing its first 32 KiB as a raw pak image\n", a[0])
			}

			free, err := img.FreeBlocks()
			if errors.Is(err, mempak.ErrMultiBank) {
				return err
			}
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
				if !n.Used {
					fmt.Fprintln(stdout, "Free")
					continue
				}
				size := fmt.Sprintf("(%3d blocks)", n.Blocks)
				if !n.Valid {
					size = "(damaged: broken block chain)"
				}
				fmt.Fprintf(stdout, "%s %s", padRight(n.Name(), 20), size)
				if verbose {
					fmt.Fprintf(stdout, "  game %s  publisher %s", code(n.GameCode()), code(n.Publisher()))
				}
				fmt.Fprintln(stdout)
			}
			recommendRepair(img, a[0])
			return nil
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "also show game and publisher codes")
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
			if !n.Used {
				return fmt.Errorf("note %d is already free", slot)
			}
			if !n.Valid {
				return fmt.Errorf("note %d is damaged (broken block chain), so its blocks cannot be traced; "+
					"use 'mempak repair' to remove it and free its blocks", slot)
			}
			fmt.Fprintf(stdout, "Deleting note %d: %s (%d blocks)\n", slot, n.RawName(), n.Blocks)
			if err := img.DeleteNote(slot); err != nil {
				return fmt.Errorf("error deleting note: %w", err)
			}
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
	var slotStr string
	var overwrite bool
	cmd := &cobra.Command{
		Use:   "insert PAKFILE NOTEFILE",
		Short: "Import a note file",
		Long: "Import a note file into a mempak image. The image file is modified in place.\n" +
			"Without -s the note goes to the first free slot. With -s N, if slot N already\n" +
			"holds a note you are asked before it is replaced, unless --overwrite is given.",
		Args: args(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, a []string) error {
			flags := cmd.Flags()
			slot := -1
			if flags.Changed("slot") {
				var err error
				if slot, err = parseSlot(slotStr); err != nil {
					return err
				}
			} else if overwrite {
				return usagef("--overwrite needs a slot (-s N)")
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
				if old, err := img.Note(slot); err == nil && old.Used {
					what := "note"
					if !old.Valid {
						what = "damaged note"
					}
					if !overwrite && !confirm(cmd.Context(), fmt.Sprintf("Slot %d holds %s '%s'. Overwrite it?", slot, what, old.Name())) {
						return errors.New("cancelled")
					}
					fmt.Fprintf(stdout, "Overwriting note %d\n", slot)
				}
			}
			used, err := img.InsertNote(header, data, slot)
			if err != nil {
				return fmt.Errorf("could not import note '%s': %w", a[1], err)
			}
			fmt.Fprintf(stdout, "Note imported into slot %d\n", used)
			return saveImage(img, a[0])
		},
	}
	f := cmd.Flags()
	f.StringVarP(&slotStr, "slot", "s", "", "write to note slot `N` (0-15)")
	f.BoolVarP(&overwrite, "overwrite", "o", false, "replace the note already in the -s slot without asking")
	return cmd
}

func newFormatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "format OUTFILE",
		Short: "Create an empty image",
		Long:  "Create a new, empty (formatted) 32 KiB .mpk image file.",
		Args:  args(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, a []string) error {
			if err := mempak.New().SaveFile(a[0]); err != nil {
				return fmt.Errorf("could not write mempak file '%s': %w", a[0], err)
			}
			fmt.Fprintf(stdout, "Wrote empty mempak image '%s'\n", a[0])
			return nil
		},
	}
}
