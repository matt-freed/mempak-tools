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
			"Images are 32 KiB raw .mpk files. To see the image's layout and bytes, use\n" +
			"'mempak inspect pak'.",
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
					fmt.Fprintf(stdout, "  game %s  publisher %s", mempak.CodeString(n.GameCode()), mempak.CodeString(n.Publisher()))
					if name, ok := mempak.GameName(n.GameCode()); ok {
						fmt.Fprintf(stdout, "  %s", name)
					}
				}
				fmt.Fprintln(stdout)
			}
			recommendRepair(img, a[0])
			return nil
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "also show game and publisher codes, and the game's title when known")
	return cmd
}

func newRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm PAKFILE SLOT",
		Short: "Delete a note",
		Long: "Delete the note in slot SLOT (0-15, as shown by 'mempak ls') from a mempak image.\n" +
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
		Use:   "extract PAKFILE SLOT OUTFILE",
		Short: "Export a note to a note file",
		Long:  "Export the note in slot SLOT (0-15, as shown by 'mempak ls') to a standalone note file.",
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

func newDefragCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "defrag PAKFILE",
		Short: "Defragment an image",
		Long: "Defragment a mempak image, in place:\n" +
			"  - notes move up to the lowest slots, keeping their order (slot numbers\n" +
			"    shown by 'mempak ls' can change)\n" +
			"  - their data is packed from block 5 up in slot order, each note in\n" +
			"    contiguous blocks linked in ascending order\n" +
			"  - free blocks and unused slots are zeroed\n\n" +
			"This is purely cosmetic: it doesn't make the pak any faster. Games follow a\n" +
			"note's block chain wherever it goes, and the pak has no seek time.\n\n" +
			"The original file is first copied to PAKFILE.bak, which must not exist yet.\n" +
			"A damaged image is refused; run 'mempak repair' first.",
		Args: args(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, a []string) error {
			path := a[0]
			orig, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("could not load mempak file '%s': %w", path, err)
			}
			img := mempak.Decode(orig)
			if img.WrongSize() {
				return fmt.Errorf("cannot defragment '%s': %w", path, mempak.ErrWrongSize)
			}
			report, err := img.Defrag()
			if err != nil {
				return fmt.Errorf("cannot defragment '%s': %w", path, err)
			}
			if !report.Changed {
				fmt.Fprintf(stdout, "'%s' is already defragmented\n", path)
				return nil
			}
			backup := path + ".bak"
			if _, err := os.Lstat(backup); err == nil {
				return fmt.Errorf("backup file '%s' already exists; move it away first", backup)
			}

			for _, m := range report.Notes {
				if !m.Moved() {
					continue
				}
				slot := fmt.Sprint(m.OldSlot)
				if m.NewSlot != m.OldSlot {
					slot = fmt.Sprintf("%d -> %d", m.OldSlot, m.NewSlot)
				}
				fmt.Fprintf(stdout, "Note %s '%s': blocks %s -> %s\n", slot, m.Name,
					blockRanges(m.OldBlocks), blockRange(m.NewFirst, len(m.OldBlocks)))
			}
			fmt.Fprintf(stdout, "A backup of the original file will be created first: '%s'\n", backup)
			if err := writeBackup(path, backup, orig); err != nil {
				return err
			}
			if err := saveImage(img, path); err != nil {
				return err
			}
			where := ""
			if report.UsedBlocks > 0 {
				where = " in blocks " + blockRange(5, report.UsedBlocks)
			}
			fmt.Fprintf(stdout, "Defragmented '%s': %d notes%s, %d free blocks zeroed (original saved as '%s')\n",
				path, len(report.Notes), where, report.FreeBlocks, backup)
			return nil
		},
	}
}

// blockRange formats n blocks starting at first as "5-8", or "5" for one.
func blockRange(first, n int) string {
	if n == 1 {
		return fmt.Sprint(first)
	}
	return fmt.Sprintf("%d-%d", first, first+n-1)
}

// blockRanges formats a chain as its runs of consecutive blocks, such as
// "40-42, 9". A reverse-linked run is written high to low ("12-8").
func blockRanges(blocks []int) string {
	var parts []string
	for i := 0; i < len(blocks); {
		j, step := i+1, 0
		if j < len(blocks) && (blocks[j] == blocks[i]+1 || blocks[j] == blocks[i]-1) {
			step = blocks[j] - blocks[i]
			for j < len(blocks) && blocks[j] == blocks[j-1]+step {
				j++
			}
		}
		if j-i == 1 {
			parts = append(parts, fmt.Sprint(blocks[i]))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", blocks[i], blocks[j-1]))
		}
		i = j
	}
	return strings.Join(parts, ", ")
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
