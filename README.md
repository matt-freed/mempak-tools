# mempak tools

A small command-line tool for N64 Controller Paks (mempaks). It does two separate jobs:

- **Adapter tools** (`mempak adapter …`): read, write and erase real memory pak hardware through a
  [raphnet](https://www.raphnet.net/) USB adapter, copying it to or from a `.mpk` image file.
  - This is a focused extraction of the mempak features from raphnet's [gcn64tools](https://www.raphnet.net/programmation/gcn64tools/index_en.php), ported to Go. The GUI and support for other controllers and accessories have been removed.
  - Why: If you only need the mempak functionality of the adapter, this Go port provides easier cross-compilation and a single static binary, with no additional libraries to install. 
- **Offline tools** (every other command): work on `.mpk` image files on your computer,
  with no adapter needed. List, extract, insert and delete saves (notes), create blank
  images, inspect an image byte by byte, and repair or defragment a damaged one.

The two fit together: read a pak to an image, edit the image offline, and write it back.
The offline tools work on any raw 32 KiB `.mpk` image, whatever made it.

Many thanks to raphnet for designing the adapter hardware and its firmware, and for releasing gcn64tools as open source. 

## Usage

Commands that talk to the adapter are grouped under `mempak adapter`. All other
commands work offline on image files.

```
mempak adapter detect [-v]                                Show adapters, ports, controllers and mempak presence
mempak adapter read [OPTIONS] [OUTFILE]                   Read the mempak to an image file (hex dump if no file)
mempak adapter write [OPTIONS] [-y] [--verify] INFILE     Write an image file to the mempak
mempak adapter erase [OPTIONS] [-y] [--fill-ff]           Erase the mempak

mempak ls [-v] PAKFILE                                    List notes
mempak rm PAKFILE SLOT                                    Delete a note
mempak extract PAKFILE SLOT OUTFILE                       Export a note to a note file
mempak inspect pak [-f] [-s N] PAKFILE [SECTION]          Show an image's layout, then hex dump it by section
mempak inspect note [-f] NOTEFILE                         Show a note file's name, codes and size, then hex dump it
mempak insert [-s N [-o]] PAKFILE NOTEFILE                Import a note file
mempak format OUTFILE                                     Create an empty image
mempak repair [-y] PAKFILE                                Repair a damaged image (keeps PAKFILE.bak)
mempak defrag PAKFILE                                     Pack notes together, cosmetic only (keeps PAKFILE.bak)
```

Run `mempak COMMAND --help` or `mempak adapter COMMAND --help` for each command's options.

### Adapter options

`OPTIONS` above stands for any of `-s`, `-c` and `-v`, which `read`, `write` and `erase` share.

| Option | Meaning |
|---|---|
| `-s, --serial SERIAL` | Choose an adapter by serial number. By default the first adapter found is used. |
| `-c, --channel N` | Controller port, starting at 0. The default is 0. |
| `-v, --verbose` | Show detailed USB output (also accepted by `detect`, which then shows device paths too). |
| `-y, --yes` | Skip the confirmation prompt (`write`, `erase`). |
| `--verify` | Read the pak back after `write` and compare it with the file. |
| `--fill-ff` | Make `erase` fill the pak with 0xFF instead of writing a blank filesystem. |

By default, `erase` writes a blank formatted filesystem and checks it by reading
it back, so games can use the pak straight away. `--fill-ff` wipes every byte
instead, and a game will then need to format the pak before using it.

### Image files

The only supported image format is `.mpk`: the raw 32 KiB contents of a pak, byte for byte.
Other formats, such as DexDrive `.n64` files and 128 KiB "MPK4" files, are not
supported, because their real layouts can't be checked without the original
hardware or files. A file that isn't exactly 32 KiB can still be listed with `ls`
(with a warning), but nothing will modify it or write it to a pak.

For the byte-level layout of paks, image files and note files, see the
[Controller Pak format spec](docs/mempak-format.md).

### Note options

Note slots are numbered 0–15, as shown by `mempak ls`.

| Option | Meaning |
|---|---|
| `ls -v, --verbose` | Also show each note's game code (its last character is the region) and publisher code, and the game's title when the code is a known retail cartridge. |
| `inspect pak PAKFILE` | Show the image's layout (the ID sector, the two TOCs, the note table and the data blocks, each run of blocks labelled with the note and which of its parts it holds, or as free), then hex dump each section. Each row of the layout names its section (`id`, `toc`, `notes` or `data`), which is the SECTION argument that shows just that part. Offsets are from the start of the image. The output is long, but the layout comes first, so `\| head -20` shows just that. |
| `inspect pak -f, --fields` | Split each section into its fields, each followed by its bytes and labelled with what those bytes mean, where that is known (a checksum is "ok" or says what it should be; a field with no known meaning, such as the serial number, has no label). The ID sector, both TOCs (each run of entries with its chain, such as `12→13→…→73, then end`) and the note table (each used slot split into game code, publisher, start inode and so on) are decoded; the data blocks, whose layout is up to each game, are still dumped whole. |
| `inspect pak PAKFILE SECTION` | Show only one section, without the layout: `id` (sector 0), `toc` (the TOC and its backup), `notes` (the note table) or `data` (the data blocks). Works with or without `-f`. |
| `inspect pak -s, --slot N` | Show only what belongs to the note in slot N: its note-table entry, its TOC entries (the backup TOC's too, where they differ from the TOC in use) and its data blocks. Combine with `-f`, and with SECTION (`toc`, `notes` or `data`) to narrow it further. A damaged note shows only its note-table entry, since its blocks can't be found. |
| `inspect note -f, --fields` | Show the note file's 32-byte header field by field (game code, publisher, start inode, status, extension, name), each followed by its bytes and labelled with what they mean where that is known, instead of as one dump. |
| `insert -s, --slot N` | Write the note to slot N. If the slot already holds a note, you are asked before it is replaced. |
| `insert -o, --overwrite` | With `-s`, replace an existing note without asking. |

Without `-s`, `insert` puts the note in the first free slot. In scripts, where there is
nobody to answer the question, use `--overwrite` to replace a note: an unanswered prompt
counts as "no" and the command fails.

### Repairing an image

When `ls` finds damage that can be repaired, it ends its listing with a
recommendation to run `mempak repair` and the list of repairs.

`mempak repair PAKFILE` checks for and fixes:

- **Damaged ID block copies:** rewritten from an intact copy.
- **A damaged or out-of-date TOC copy:** rewritten from the valid copy.
- **Damaged notes** (shown as `damaged` by `ls`): removed.
- **Orphaned blocks** (marked used but belonging to no note): freed.

It lists the repairs and asks before changing anything (`-y` skips the
question). The original file is first saved as `PAKFILE.bak`; if that file
already exists, `repair` stops, so an earlier backup is never overwritten.

Limitations:

- **Damaged notes are removed, not recovered.** Their data, and that of freed
  blocks, is kept only in `PAKFILE.bak`.
- **Only the filesystem is checked.** Save data inside a note has no checksum on
  the pak, so corrupt data in a readable note goes unnoticed (the game's own
  checks may still reject it).
- **Some damage can't be repaired:** no intact ID block copy, neither TOC copy
  passing its checksum, or a multi-bank pak. `repair` then changes nothing.
- **Cross-linked chains** (two notes sharing a block) are not detected.
- **Deleted notes** cannot be recovered.

To repair a pak, read it, repair the image, and write it back:

```sh
mempak adapter read pak.mpk
mempak repair pak.mpk
mempak adapter write --verify pak.mpk
```

### Defragmenting an image

`mempak defrag PAKFILE` tidies an image, in place:

- **Notes move up to the lowest slots**, keeping their order (slot numbers shown
  by `ls` can change).
- **Note data is packed** into contiguous blocks from block 5, in slot order,
  each note linked in ascending order.
- **Free blocks and unused slots are zeroed.**

The original file is first saved as `PAKFILE.bak`; if that file already exists,
`defrag` stops. A damaged image is refused, so run `mempak repair` first.

**This is purely cosmetic and won't make anything faster.** The pak is
random-access memory and games follow a note's block chain wherever it goes. A
defragmented image is just tidier to look at with `mempak inspect pak`.

**Caution: free blocks are zeroed.** Deleting a note only marks its blocks free,
so free blocks can still hold the data of old deleted saves. `defrag` erases that
data from the image. Only `PAKFILE.bak` keeps a copy.

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
- `internal/mempak/` handles 32 KiB pak images, the pak filesystem and note files.
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
`mempak.c`. So some rules in this tool, such as how blocks are allocated for
an inserted note, are libdragon conventions rather than part of Nintendo's
format. The [format spec](docs/mempak-format.md) marks where that applies.
