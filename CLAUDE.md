# mempak-tools

Go CLI for N64 Controller Paks through raphnet USB adapters. It is a port of the
mempak parts of raphnet's C gcn64tools; the C version is in git history (before the
Go port), and the README covers usage.

## Commands

```sh
go build -o mempak ./cmd/mempak
go vet ./... && go test ./...
bash tests/offline.sh "$PWD/mempak"     # needs an ABSOLUTE path: the script cd's into a temp dir
gofmt -l .                              # CI fails on unformatted files

# Every HID backend is behind build tags; check they all compile after touching internal/hid:
for t in linux/amd64 windows/amd64 darwin/arm64 freebsd/amd64; do
  CGO_ENABLED=0 GOOS=${t%/*} GOARCH=${t#*/} go vet ./... || break; done
```

## Hard rules

- **No cgo, ever.** Release binaries are built with `CGO_ENABLED=0` and cross-compiled
  from one macOS runner. macOS HID calls IOKit through `purego`; Linux uses hidraw
  ioctls + sysfs (no libudev); Windows uses `hid.dll` + `DeviceIoControl`.
- **Command output goes to the `stdout` writer** (`cmd/mempak/term.go`), never
  `fmt.Print*`/`os.Stdout`. It is buffered when stdout isn't a terminal; unbuffered
  writes die of SIGPIPE under `cmd | grep -q` with `pipefail`, which broke
  `tests/offline.sh`. Call `flushStdout()` before prompting.
- **Exit codes:** 0 ok, 1 runtime error, 2 usage error. Return `usagef(...)`/`usageError`
  for bad command lines and wrap cobra arg checks with `args(...)`; `errSilent` exits 1
  without printing.
- **Keep on-disk behaviour identical to the C tool.** Images, note files and `ls`
  output were checked byte-for-byte against the C binary; `tests/offline.sh` greps
  exact strings. Notable inherited behaviour:
  - `.mpk` output defaults to the 32kB MPK format (not MPK4); only `*.n64` picks N64.
  - Images of unrecognised size load as raw with `FormatInvalid`, so in-place edits
    (`rm`, `insert`) fail to save rather than guess a format.
  - Inserted notes keep their raw 32-byte header (only the inode changes). Blocks are
    allocated lowest-first and linked in reverse; the alternate TOC is written before
    the valid one.
  - Note files are bryc's format: header with inode `0xCAFE` + data.
- **License headers:** files ported from gcn64tools start with
  `// SPDX-License-Identifier: GPL-3.0-or-later` plus the Raphael Assenat / Go port
  copyright line; libdragon-derived code (filesystem, CRCs) notes "adapted from
  libdragon (public domain)". `internal/hid` is original code (SPDX line only); don't
  copy hidapi into it.

## Layout notes

- `internal/mempak` is pure data: images, filesystem, note files. No I/O to hardware.
- `internal/raphnet`: adapter protocol (`exchange` = send feature report, then poll
  get-feature until a non-empty reply or 1 s). Adapters are matched by PID **and**
  HID interface number in `devices.go`, so a backend that reports the wrong interface
  number makes the adapter invisible.
- `pak_test.go` has `fakeAdapter`, a simulated firmware + pak with real CRCs. Use it
  for protocol tests; CRC golden values in `TestCRCGolden` came from the C code.
- Ctrl-C cancels the context; transfers stop at the next 32-byte block and the
  deferred `session.Close()` resumes the adapter's controller polling.

## Testing with hardware

Hardware can't be tested in CI or by Claude. Ask the user to run the adapter
commands (`adapter detect -v`, `read`, `write --verify`, `erase`, `erase --fill-ff`,
Ctrl-C during `read`). As of 2026-11-15 all of these passed on macOS; the Linux and
Windows HID backends have only been compiled, not run against an adapter.

## Releases

`VERSION` holds `major.minor`; CI appends the run number and publishes a release on
every push to `main`. Package names (`mempak-macos-universal.tar.gz`,
`mempak-linux-x86_64.tar.gz`, `mempak-windows-x86_64.zip`) are what `install.sh`
downloads, so don't rename them without updating it.
