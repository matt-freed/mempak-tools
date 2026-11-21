// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2007-2024 Raphael Assenat (gcn64tools); Go port (C) 2026 Matt Freed.

// Command mempak manages N64 controller paks through a raphnet USB adapter,
// and the notes stored in pak image files.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// Exit codes.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// usageError marks errors in the command line, which exit with status 2.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error {
	return usageError{fmt.Sprintf(format, args...)}
}

// errSilent exits with an error status without printing anything more.
var errSilent = errors.New("")

// errSilentUsage exits with the usage status without printing anything more.
var errSilentUsage = usageError{}

// args wraps a cobra positional-argument check so failures count as usage
// errors.
func args(check cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, a []string) error {
		if err := check(cmd, a); err != nil {
			return usageError{err.Error()}
		}
		return nil
	}
}

// helpOnly makes a parent command print its help and fail with a usage error
// when run without a subcommand.
func helpOnly(cmd *cobra.Command, a []string) error {
	cmd.Help()
	return errSilentUsage
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "mempak",
		Short: "N64 controller pak (mempak) tools for raphnet USB adapters",
		Long: "mempak " + version + " - N64 controller pak (mempak) tools for raphnet USB adapters\n\n" +
			"Adapter commands talk to the connected raphnet adapter. All other\n" +
			"commands work offline on 32 KiB .mpk image files.",
		Version:           version,
		SilenceErrors:     true,
		SilenceUsage:      true,
		RunE:              helpOnly,
		CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
	}
	root.SetVersionTemplate("mempak {{.Version}}\n")
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return usageError{err.Error()}
	})
	root.AddCommand(
		newAdapterCmd(),
		newLsCmd(),
		newRmCmd(),
		newExtractCmd(),
		newInsertCmd(),
		newFormatCmd(),
		newRepairCmd(),
		&cobra.Command{
			Use:   "version",
			Short: "Show the version",
			Args:  args(cobra.NoArgs),
			Run: func(cmd *cobra.Command, a []string) {
				fmt.Fprintf(stdout, "mempak %s\n", version)
			},
		},
	)
	return root
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		// After the first Ctrl-C, which lets a transfer stop cleanly and
		// resume the adapter's polling, a second one kills the program.
		<-ctx.Done()
		stop()
	}()

	root := newRootCmd()
	root.SetOut(stdout)
	cmd, err := root.ExecuteContextC(ctx)
	flushStdout()
	if err == nil {
		return exitOK
	}

	var uerr usageError
	isUsage := errors.As(err, &uerr) || strings.HasPrefix(err.Error(), "unknown command")
	if msg := err.Error(); msg != "" {
		fmt.Fprintln(os.Stderr, "Error:", msg)
		if isUsage {
			fmt.Fprintf(os.Stderr, "Run '%s --help' for usage.\n", cmd.CommandPath())
		}
	}
	if isUsage {
		return exitUsage
	}
	return exitError
}

func main() {
	os.Exit(run())
}
