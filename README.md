# mempak

A small command-line tool for N64 Controller Paks (mempaks) using a
[raphnet](https://www.raphnet.net/) USB adapter. It can read, write and erase a
pak through the adapter, and manage the saves (notes) inside pak image files.

This is a focused extraction of the mempak features from raphnet's
[gcn64tools](https://www.raphnet.net/programmation/gcn64tools/index_en.php), ported to Go.
The GUI and support for other controllers and accessories have been removed.

Why:
 - If you only need the mempak functionality of the adapter, this Go port provides easier cross-compilation and a single static binary, with no additional libraries to install. 
 - A simpler focused CLI with some enhancements/additional features
 - Several bug fixes for a closer adherence to the n64 manual spec
 - Mostly, a fun project for me to learn about n64 mempaks

Many thanks to raphnet for designing the adapter hardware and its firmware, and for releasing gcn64tools as open source. 

## Usage

Commands that talk to the adapter are grouped under `mempak adapter`. All other
commands work offline on image files.

```
mempak adapter detect [-v]                                Show adapters, ports, controllers and mempak presence
mempak adapter read [OPTIONS] [-f FMT] [OUTFILE]          Read the mempak to an image file (hex dump if no file)
mempak adapter write [OPTIONS] [-y] [--verify] INFILE     Write an image file to the mempak
mempak adapter erase [OPTIONS] [-y] [--fill-ff]           Erase the mempak

mempak ls [-v] PAKFILE                                    List notes
mempak rm PAKFILE NOTE                                    Delete a note
mempak extract PAKFILE NOTE OUTFILE                       Export a note to a note file
mempak insert [-s N [-o]] [-c TEXT] PAKFILE NOTEFILE      Import a note file
mempak format [-f FMT] OUTFILE                            Create an empty image
mempak convert [-f FMT] INFILE OUTFILE                    Convert between image formats
```

Run `mempak COMMAND --help` or `mempak adapter COMMAND --help` for each command's options.

### Adapter options

`OPTIONS` above stands for any of `-s`, `-c` and `-v`, which `read`, `write` and `erase` share.

| Option | Meaning |
|---|---|
| `-s, --serial SERIAL` | Choose an adapter by serial number. By default the first adapter found is used. |
| `-c, --channel N` | Controller port, starting at 0. The default is 0. |
| `-v, --verbose` | Show detailed USB output (also accepted by `detect`, which then shows device paths too). |
| `-f, --format FMT` | Image format for `read` (see [Image formats](#image-formats)). |
| `-y, --yes` | Skip the confirmation prompt (`write`, `erase`). |
| `--verify` | Read the pak back after `write` and compare it with the file. |
| `--fill-ff` | Make `erase` fill the pak with 0xFF instead of writing a blank filesystem. |

By default, `erase` writes a blank formatted filesystem and checks it by reading
it back, so games can use the pak straight away. `--fill-ff` wipes every byte
instead, and a game will then need to format the pak before using it.

### Image formats

| Format | Description |
|---|---|
| `mpk` | Standard 32kB raw image. This is the default output format. |
| `mpk4` | 128kB raw image holding 4 copies of the 32kB image. |
| `n64` | DexDrive image, which can store a comment for each note. It is used automatically when the output file ends in `.n64`. |

Use `-f FMT` with `read`, `format` and `convert` to choose a different format.
Commands that edit a file in place (`rm`, `insert`) keep the file's existing format.

For the byte-level layout of paks, image files and note files, see the
[Controller Pak format spec](docs/mempak-format.md).

### Note options

Note slots are numbered 0–15, as shown by `mempak ls`.

| Option | Meaning |
|---|---|
| `ls -v, --verbose` | Also show each note's game code (its last character is the region) and publisher code. |
| `insert -s, --slot N` | Write the note to slot N. If the slot already holds a note, you are asked before it is replaced. |
| `insert -o, --overwrite` | With `-s`, replace an existing note without asking. |
| `insert -c, --comment TEXT` | Store a comment with the note (up to 255 characters; `.n64` images only, ignored with a warning otherwise). |

Without `-s`, `insert` puts the note in the first free slot. In scripts, where there is
nobody to answer the question, use `--overwrite` to replace a note: an unanswered prompt
counts as "no" and the command fails.

### Example

```sh
mempak adapter detect
mempak adapter read backup.mpk
mempak ls backup.mpk
mempak extract backup.mpk 2 vigilante8.note
mempak insert backup.mpk vigilante8.note
mempak adapter write --verify backup.mpk
```

## Downloads

Prebuilt binaries are on the [Releases page](https://github.com/matt-freed/mempak-tools/releases).
A new versioned release is published automatically after every successful build of `main`.

On macOS or Linux x86-64, you can install or update to the latest release with:

```sh
curl -fsSL https://raw.githubusercontent.com/matt-freed/mempak-tools/main/install.sh | sh
```

This installs to `~/.local/bin` (set `PREFIX` to change it) and skips the macOS quarantine
step below.

| File | Platform |
|---|---|
| `mempak-macos-universal.tar.gz` | macOS (Apple Silicon and Intel) |
| `mempak-linux-x86_64.tar.gz` | Linux x86-64 (also includes the udev rule) |
| `mempak-windows-x86_64.zip` | Windows x64 |

On macOS the binary isn't signed by a registered Apple developer, so if you download
it in a browser, macOS blocks it the first time you run it. To allow it, run:

```sh
xattr -d com.apple.quarantine mempak
```

You can also allow it under System Settings → Privacy & Security. This isn't needed if you build from source.

Release versions are numbered `<major>.<minor>.<build>`. The major and minor numbers come from
the `VERSION` file, and the build number is the CI run number.
To start a new series (for example 0.3.x), change the version there.

## Building

The only requirement is [Go](https://go.dev/dl/) (see `go.mod` for the version). No C compiler
or system libraries are needed: USB HID access is implemented in pure Go for each OS.

```sh
go build ./cmd/mempak        # produces ./mempak (mempak.exe on Windows)
go test ./...                # unit tests, no adapter needed
bash tests/offline.sh "$PWD/mempak"   # end-to-end offline tests
```

To install into `$(go env GOPATH)/bin`, run `go install ./cmd/mempak`.

Go cross-compiles to every supported platform from any machine, for example
`GOOS=windows GOARCH=amd64 go build ./cmd/mempak`.

### Linux permissions

To use the adapter without root, install the udev rule and then reconnect the adapter:

```sh
sudo cp scripts/10-raphnet.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules
```

## Layout

- `cmd/mempak/` is the command-line tool (built with [cobra](https://github.com/spf13/cobra)).
- `internal/mempak/` handles pak images (mpk, mpk4, n64), the pak filesystem and note files.
- `internal/raphnet/` talks to raphnet adapters and reads and writes paks through them.
- `internal/hid/` is a minimal cgo-free HID library: hidraw on Linux, IOKit (through
  [purego](https://github.com/ebitengine/purego)) on macOS, and hid.dll on Windows.
- `tests/offline.sh` runs the offline round-trip test against a built binary.
- `docs/mempak-format.md` describes the Controller Pak filesystem and the file formats.

## License

GPL-3.0-or-later (see `LICENSE`). The code is a Go port of raphnet's gcn64tools,
Copyright (C) 2007-2024 Raphael Assenat. The pak filesystem and CRC code are adapted from
libdragon (public domain).

[libdragon](https://github.com/DragonMinded/libdragon) is an open-source SDK for N64
homebrew, written by the community as an alternative to Nintendo's official SDK
(libultra). This tool's Controller Pak filesystem code is based on libdragon's
`mempak.c`. So some rules in this tool, such as which
region codes make a note valid, are libdragon conventions rather than part of
Nintendo's format. The [format spec](docs/mempak-format.md) marks where that applies.
