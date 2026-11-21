// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2007-2024 Raphael Assenat (gcn64tools); Go port (C) 2026 Matt Freed.

package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/matt-freed/mempak-tools/internal/mempak"
	"github.com/matt-freed/mempak-tools/internal/raphnet"
)

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// stdout is where commands print their output. Like C stdio, it is
// buffered unless it is a terminal; flushStdout must run before exiting.
var stdout = newStdout()

func newStdout() *bufio.Writer {
	if isTerminal(os.Stdout) {
		return bufio.NewWriterSize(os.Stdout, 1) // effectively unbuffered
	}
	return bufio.NewWriter(os.Stdout)
}

func flushStdout() { stdout.Flush() }

// progress returns a callback that shows transfer progress on stderr when
// it is a terminal, and a function to call once the transfer is over.
func progress(label string) (raphnet.Progress, func()) {
	if !isTerminal(os.Stderr) {
		return nil, func() {}
	}
	update := func(done int) {
		fmt.Fprintf(os.Stderr, "\r%s: 0x%04x / 0x%04x (%3d%%)", label, done, mempak.Size, done*100/mempak.Size)
	}
	return update, func() { fmt.Fprintln(os.Stderr) }
}

// confirm asks a yes/no question on stderr; the default is no. It returns
// false if ctx is cancelled (Ctrl-C) while waiting for an answer.
func confirm(ctx context.Context, question string) bool {
	flushStdout()
	fmt.Fprintf(os.Stderr, "%s [y/N] ", question)
	answer := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		answer <- line
	}()
	select {
	case line := <-answer:
		return strings.HasPrefix(line, "y") || strings.HasPrefix(line, "Y")
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr)
		return false
	}
}

// parseUint parses a non-negative integer (decimal, 0x hex or 0 octal) of at
// most max.
func parseUint(what, s string, max int) (int, error) {
	v, err := strconv.ParseInt(s, 0, 64)
	if err != nil || v < 0 || v > int64(max) {
		return 0, usagef("invalid %s '%s' (expected 0-%d)", what, s, max)
	}
	return int(v), nil
}

func parseSlot(s string) (int, error) {
	return parseUint("note slot", s, mempak.NumNotes-1)
}
