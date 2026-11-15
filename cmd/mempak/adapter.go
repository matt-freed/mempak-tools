// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2007-2024 Raphael Assenat (gcn64tools); Go port (C) 2026 Matt Freed.

package main

// 'mempak adapter ...': every command that talks to the raphnet adapter.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/matt-freed/mempak-tools/internal/mempak"
	"github.com/matt-freed/mempak-tools/internal/raphnet"
)

// adapterOpts are the options shared by the commands that open an adapter.
type adapterOpts struct {
	serial  string
	channel string
	verbose bool
	yes     bool
}

func (o *adapterOpts) register(cmd *cobra.Command, withYes bool) {
	f := cmd.Flags()
	f.StringVarP(&o.serial, "serial", "s", "", "use the adapter with this `SERIAL` (default: first adapter found)")
	f.StringVarP(&o.channel, "channel", "c", "", "controller port `N` to use, starting at 0 (default 0)")
	f.BoolVarP(&o.verbose, "verbose", "v", false, "verbose USB/adapter output")
	if withYes {
		f.BoolVarP(&o.yes, "yes", "y", false, "do not ask for confirmation")
	}
}

func setVerbose(v bool) {
	if v {
		raphnet.Verbose = stdout
	}
}

func permissionHint() string {
	if runtime.GOOS == "linux" {
		return " Do you have permissions? See scripts/10-raphnet.rules."
	}
	return ""
}

// session is an open adapter with its controller polling suspended.
type session struct {
	*raphnet.Adapter
	channel int
}

// openAdapter opens the adapter chosen by the options and checks the channel.
func openAdapter(o *adapterOpts) (*session, error) {
	channel := 0
	if o.channel != "" {
		var err error
		if channel, err = parseUint("channel", o.channel, 255); err != nil {
			return nil, err
		}
	}
	setVerbose(o.verbose)

	adapters, err := raphnet.List()
	if err != nil {
		return nil, fmt.Errorf("listing adapters: %w", err)
	}
	var found *raphnet.Info
	for i := range adapters {
		if o.serial == "" || adapters[i].Serial == o.serial {
			found = &adapters[i]
			break
		}
	}
	if found == nil {
		if o.serial != "" {
			return nil, fmt.Errorf("no adapter with serial '%s' found. Try 'mempak adapter detect'", o.serial)
		}
		return nil, errors.New("no adapter found. Is it plugged in? Try 'mempak adapter detect'")
	}
	if channel >= found.Channels {
		return nil, fmt.Errorf("channel %d out of range: adapter '%s' has %d port(s) (0-%d)",
			channel, found.Product, found.Channels, found.Channels-1)
	}

	a, err := raphnet.Open(*found)
	if err != nil {
		return nil, fmt.Errorf("error opening adapter '%s' (serial %s): %w.%s", found.Product, found.Serial, err, permissionHint())
	}
	fmt.Fprintf(os.Stderr, "Using adapter '%s' (serial %s), channel %d\n", found.Product, found.Serial, channel)

	// The adapter would otherwise keep polling the controller between pak
	// accesses.
	a.SuspendPolling(true)
	return &session{Adapter: a, channel: channel}, nil
}

// Close resumes polling and closes the adapter.
func (s *session) Close() {
	s.SuspendPolling(false)
	s.Adapter.Close()
}

// transferError explains a failed pak transfer.
func transferError(err error) error {
	if errors.Is(err, context.Canceled) {
		return errors.New("cancelled")
	}
	return err
}

func (s *session) readPak(ctx context.Context, label string) (*mempak.Image, error) {
	cb, done := progress(label)
	img, err := s.ReadPak(ctx, s.channel, cb)
	done()
	return img, transferError(err)
}

func (s *session) writePak(ctx context.Context, label string, img *mempak.Image) error {
	cb, done := progress(label)
	err := s.WritePak(ctx, s.channel, &img.Data, cb)
	done()
	return transferError(err)
}

// verify reads the pak back and compares it with img.
func (s *session) verify(ctx context.Context, img *mempak.Image) error {
	got, err := s.readPak(ctx, "Verifying")
	if err != nil {
		return err
	}
	for i := range got.Data {
		if got.Data[i] != img.Data[i] {
			return fmt.Errorf("verify failed: first mismatch at 0x%04x (wrote 0x%02x, read 0x%02x)", i, img.Data[i], got.Data[i])
		}
	}
	fmt.Fprintln(os.Stderr, "Verify OK")
	return nil
}

func newAdapterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "adapter COMMAND",
		Short: "Commands that talk to a raphnet adapter",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, a []string) error {
			if len(a) > 0 {
				return usagef("unknown adapter command '%s'", a[0])
			}
			return helpOnly(cmd, a)
		},
	}
	cmd.AddCommand(newDetectCmd(), newReadCmd(), newWriteCmd(), newEraseCmd())
	return cmd
}

func newDetectCmd() *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "detect",
		Short: "Show adapters, ports, controllers and mempak presence",
		Long: "List connected raphnet adapters, their firmware, and for each port\n" +
			"the connected controller and whether a mempak is present.",
		Args: args(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, a []string) error {
			setVerbose(verbose)
			adapters, err := raphnet.List()
			if err != nil {
				return fmt.Errorf("listing adapters: %w", err)
			}
			if len(adapters) == 0 {
				fmt.Fprintln(stdout, "No adapter found.")
				return errSilent
			}
			for i, inf := range adapters {
				fmt.Fprintf(stdout, "Adapter %d: %s\n", i+1, inf.Product)
				fmt.Fprintf(stdout, "  Serial:   %s\n", inf.Serial)
				fmt.Fprintf(stdout, "  USB ID:   %04x:%04x\n", inf.VendorID, inf.ProductID)
				if verbose {
					fmt.Fprintf(stdout, "  Path:     %s\n", inf.Path)
				}
				detectAdapter(inf)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "verbose USB/adapter output")
	return cmd
}

func detectAdapter(inf raphnet.Info) {
	a, err := raphnet.Open(inf)
	if err != nil {
		fmt.Fprintf(stdout, "  Status:   could not open device (%v)%s\n", err, permissionHint())
		return
	}
	defer a.Close()

	if v, err := a.Version(); err == nil {
		fmt.Fprintf(stdout, "  Firmware: %s\n", v)
	}
	fmt.Fprintf(stdout, "  Ports:    %d\n", a.Channels)
	blockIO := "no"
	if a.Features&raphnet.FeatureBlockIO != 0 {
		blockIO = "yes"
	}
	fmt.Fprintf(stdout, "  Block IO: %s\n", blockIO)

	for ch := range a.Channels {
		fmt.Fprintf(stdout, "    Port %d: ", ch)
		probe := true
		if a.Features&raphnet.FeatureControllerType != 0 {
			if t, err := a.ControllerType(ch); err == nil {
				fmt.Fprint(stdout, raphnet.ControllerName(t))
				probe = raphnet.IsN64Controller(t)
			} else {
				fmt.Fprint(stdout, "Unknown controller")
			}
		} else {
			fmt.Fprint(stdout, "Controller type not reported by this firmware")
		}
		if probe {
			a.SuspendPolling(true)
			status := "present"
			if a.DetectPak(ch) != nil {
				status = "not detected"
			}
			fmt.Fprintf(stdout, ", mempak: %s", status)
			a.SuspendPolling(false)
		}
		fmt.Fprintln(stdout)
	}
}

func newReadCmd() *cobra.Command {
	var o adapterOpts
	var format string
	cmd := &cobra.Command{
		Use:   "read [OUTFILE]",
		Short: "Read the mempak to an image file",
		Long: "Read the whole mempak and save it to OUTFILE.\n" +
			"Without OUTFILE, a hex dump is printed to stdout.\n\n" + formatsHelp,
		Args: args(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, a []string) error {
			var outfile string
			var f mempak.Format
			if len(a) == 1 {
				outfile = a[0]
				var err error
				if f, err = outputFormat(format, outfile); err != nil {
					return err
				}
			}

			s, err := openAdapter(&o)
			if err != nil {
				return err
			}
			img, err := s.readPak(cmd.Context(), "Reading")
			s.Close()
			if err != nil {
				return err
			}

			if outfile == "" {
				return mempak.Hexdump(stdout, img.Data[:])
			}
			if err := img.SaveFile(outfile, f); err != nil {
				return fmt.Errorf("error writing file '%s': %w", outfile, err)
			}
			fmt.Fprintf(os.Stderr, "Wrote '%s' (%s format)\n", outfile, f)
			if img.Validate() != nil {
				fmt.Fprintln(os.Stderr, "Warning: the pak contents are not a valid mempak filesystem (unformatted or corrupted)")
			}
			return nil
		},
	}
	o.register(cmd, false)
	cmd.Flags().StringVarP(&format, "format", "f", "", "output `FMT` (default: from extension, else mpk)")
	return cmd
}

func newWriteCmd() *cobra.Command {
	var o adapterOpts
	var verify bool
	cmd := &cobra.Command{
		Use:   "write INFILE",
		Short: "Write an image file to the mempak",
		Long:  "Write a mempak image (.mpk/.n64) to the mempak, replacing all its contents.",
		Args:  args(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, a []string) error {
			img, err := loadImage(a[0])
			if err != nil {
				return err
			}
			if img.Validate() != nil {
				fmt.Fprintf(os.Stderr, "Warning: '%s' is not a valid mempak filesystem (unformatted or corrupted)\n", a[0])
			}

			s, err := openAdapter(&o)
			if err != nil {
				return err
			}
			defer s.Close()

			if !o.yes && !confirm(cmd.Context(), "This will overwrite ALL data on the mempak. Continue?") {
				return errors.New("cancelled")
			}
			if err := s.writePak(cmd.Context(), "Writing", img); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Wrote '%s' to mempak\n", a[0])
			if verify {
				return s.verify(cmd.Context(), img)
			}
			return nil
		},
	}
	o.register(cmd, true)
	cmd.Flags().BoolVar(&verify, "verify", false, "read the pak back afterwards and compare")
	return cmd
}

func newEraseCmd() *cobra.Command {
	var o adapterOpts
	var fillFF bool
	cmd := &cobra.Command{
		Use:   "erase",
		Short: "Erase the mempak",
		Long: "Erase the mempak. By default a blank, formatted filesystem is written so\n" +
			"the pak is immediately usable by games.",
		Args: args(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, a []string) error {
			ctx := cmd.Context()
			s, err := openAdapter(&o)
			if err != nil {
				return err
			}
			defer s.Close()

			if !o.yes && !confirm(cmd.Context(), "This will erase ALL data on the mempak. Continue?") {
				return errors.New("cancelled")
			}

			if !fillFF {
				blank := mempak.New()
				if err := s.writePak(ctx, "Erasing", blank); err != nil {
					return err
				}
				if err := s.verify(ctx, blank); err != nil {
					return err
				}
				fmt.Fprintln(os.Stderr, "Mempak erased (blank formatted filesystem written)")
				return nil
			}

			if err := s.DetectPak(s.channel); err != nil {
				return err
			}
			cb, done := progress("Filling")
			err = s.FillPak(ctx, s.channel, 0xFF, cb)
			done()
			if err != nil {
				return transferError(err)
			}
			cb, done = progress("Verifying")
			err = s.VerifyFill(ctx, s.channel, 0xFF, cb)
			done()
			if err != nil {
				return transferError(err)
			}
			fmt.Fprintln(os.Stderr, "Mempak filled with 0xFF")
			return nil
		},
	}
	o.register(cmd, true)
	cmd.Flags().BoolVar(&fillFF, "fill-ff", false, "instead, overwrite every byte with 0xFF and verify\n(raw wipe; the pak will need formatting before use)")
	return cmd
}
