// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matt-freed/mempak-tools/internal/mempak"
)

func newRepairCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "repair PAKFILE",
		Short: "Repair a damaged image",
		Long: "Repair the filesystem of a mempak image, in place:\n" +
			"  - damaged ID block copies are rewritten from an intact copy\n" +
			"  - a damaged or out-of-date TOC copy is rewritten from the valid one\n" +
			"  - damaged notes (broken block chain) are removed\n" +
			"  - blocks marked used but not part of any readable note are freed\n\n" +
			"The repairs are listed and you are asked before anything changes. The\n" +
			"original file is first copied to PAKFILE.bak, which must not exist yet.\n\n" +
			"Limitations:\n" +
			"  - Damaged notes are removed, not recovered. Their data, and that of freed\n" +
			"    blocks, is kept only in PAKFILE.bak.\n" +
			"  - Only the filesystem is checked. Save data inside a note has no checksum\n" +
			"    on the pak, so corrupt data in a readable note goes unnoticed (the game's\n" +
			"    own checks may still reject it).\n" +
			"  - Nothing is repaired if no ID block copy is intact, if neither TOC copy\n" +
			"    passes its checksum, or on a multi-bank pak.\n" +
			"  - Two notes sharing a block (cross-linked chains) are not detected.\n" +
			"  - Deleted notes cannot be recovered.",
		Args: args(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, a []string) error {
			path := a[0]
			orig, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("could not load mempak file '%s': %w", path, err)
			}
			img := mempak.Decode(orig)
			if img.WrongSize() {
				return fmt.Errorf("cannot repair '%s': %w", path, mempak.ErrWrongSize)
			}

			fixed := *img // repair a copy, so nothing changes until confirmed
			report, err := fixed.Repair()
			if err != nil {
				return fmt.Errorf("cannot repair '%s': %w", path, err)
			}
			if !report.Changed() {
				fmt.Fprintf(stdout, "'%s' needs no repair (only the filesystem was checked, not the save data inside notes)\n", path)
				return nil
			}

			fmt.Fprintf(stdout, "Repairs for '%s':\n", path)
			for _, line := range strings.Split(strings.TrimSpace(report.String()), "\n") {
				fmt.Fprintf(stdout, "  %s\n", line)
			}
			if len(report.DamagedNotes) > 0 || len(report.FreedBlocks) > 0 {
				fmt.Fprintln(stdout, "The data in removed notes and freed blocks will be lost from the image (the backup keeps it).")
			}

			backup := path + ".bak"
			if _, err := os.Lstat(backup); err == nil {
				return fmt.Errorf("backup file '%s' already exists; move it away first", backup)
			}
			fmt.Fprintln(stdout, "Warning: damaged notes are removed, not recovered, and only the filesystem is checked,\n"+
				"not the save data inside notes. See 'mempak repair --help' for all limitations.")
			fmt.Fprintf(stdout, "A backup of the original file will be created first: '%s'\n", backup)
			if !yes && !confirm(cmd.Context(), "Apply these repairs?") {
				return errors.New("cancelled")
			}
			if err := writeBackup(path, backup, orig); err != nil {
				return err
			}
			if err := saveImage(&fixed, path); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "Repaired '%s' (original saved as '%s')\n", path, backup)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask for confirmation")
	return cmd
}

// recommendRepair ends a listing with what 'mempak repair' would fix, if the
// image has any damage it can repair. It works on a copy; img is unchanged.
func recommendRepair(img *mempak.Image, path string) {
	if img.WrongSize() {
		return // repair refuses these
	}
	trial := *img
	report, err := trial.Repair()
	if err != nil || !report.Changed() {
		return
	}
	fmt.Fprintf(stdout, "\nThis image is damaged. Run 'mempak repair %s' to fix it (the original is kept as a backup):\n", path)
	for _, line := range strings.Split(strings.TrimSpace(report.String()), "\n") {
		fmt.Fprintf(stdout, "  %s\n", line)
	}
}

// writeBackup writes data, the original contents of path, to backup with the
// same permissions. It never overwrites an existing file.
func writeBackup(path, backup string, data []byte) error {
	perm := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		perm = fi.Mode().Perm()
	}
	f, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return fmt.Errorf("could not create backup '%s': %w", backup, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(backup)
		return fmt.Errorf("could not write backup '%s': %w", backup, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(backup)
		return fmt.Errorf("could not write backup '%s': %w", backup, err)
	}
	return nil
}
