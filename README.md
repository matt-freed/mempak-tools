# mempak

A small command-line tool for N64 Controller Paks (mempaks) using a
[raphnet](https://www.raphnet.net/) USB adapter. It can read, write and erase a
pak through the adapter, and manage the saves (notes) inside pak image files.

This is a focused extraction of the mempak features from raphnet's
[gcn64tools](https://www.raphnet.net/programmation/gcn64tools/index_en.php).
The GUI and support for other controllers and accessories have been removed.

## Usage

Commands that talk to the adapter are grouped under `mempak adapter`. All other
commands work offline on image files.

```
mempak adapter detect                 Show adapters, ports, controllers and mempak presence
mempak adapter read [OUTFILE]         Read the mempak to an image file (hex dump if no file)
mempak adapter write INFILE           Write an image file to the mempak
mempak adapter erase                  Erase the mempak

mempak ls PAKFILE                     List notes
mempak rm PAKFILE NOTE                Delete a note
mempak extract PAKFILE NOTE OUTFILE   Export a note to a note file
mempak insert PAKFILE NOTEFILE        Import a note file
mempak format OUTFILE                 Create an empty image
mempak convert INFILE OUTFILE         Convert between image formats
```

Run `mempak COMMAND --help` or `mempak adapter COMMAND --help` for each command's options.

### Adapter options

| Option | Meaning |
|---|---|
| `-s, --serial SERIAL` | Choose an adapter by serial number. By default the first adapter found is used. |
| `-c, --channel N` | Controller port, starting at 0. The default is 0. |
| `-y, --yes` | Skip the confirmation prompt (`write`, `erase`). |
| `--verify` | Read the pak back after `write` and compare it with the file. |
| `--fill-ff` | Make `erase` fill the pak with 0xFF instead of writing a blank filesystem. |
| `-v, --verbose` | Show detailed USB output. |

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

Note slots are numbered 0–15, as shown by `mempak ls`.
`mempak insert` puts the note in the first free slot unless you give one of these options:
- `-s N` writes to slot N, whether or not it is empty.
- `-d N` replaces the note in slot N, which must not be empty.

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
`project(mempak VERSION ...)` in `CMakeLists.txt`, and the build number is the CI run number.
To start a new series (for example 0.2.x), change the version there.

## Building

The only requirements are CMake (3.16 or newer) and a C compiler.
[hidapi](https://github.com/libusb/hidapi) is included in `third_party/hidapi`.

### macOS

```sh
brew install cmake        # if not already installed
cmake -S . -B build
cmake --build build
ctest --test-dir build    # offline tests, no adapter needed
sudo cmake --install build   # optional: installs to /usr/local/bin
```

### Linux

```sh
sudo apt install cmake build-essential pkg-config libudev-dev
cmake -S . -B build && cmake --build build
```

To use the adapter without root, install the udev rule and then reconnect the adapter:

```sh
sudo cp scripts/10-raphnet.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules
```

### Windows (MSYS2 / MinGW-w64)

From an MSYS2 MINGW64 shell:

```sh
pacman -S mingw-w64-x86_64-gcc mingw-w64-x86_64-cmake mingw-w64-x86_64-ninja
cmake -S . -B build -G Ninja && cmake --build build
```

The resulting `build/mempak.exe` is statically linked.

## Layout

- `src/` contains the command-line code: `main.c`, `cli.c`, `cmd_adapter.c` and `cmd_offline.c`.
- `src/lib/` contains the core library from gcn64tools `rntlib`, which handles mempak images and filesystems and talks to the adapter.
- `third_party/hidapi/` contains the vendored hidapi 0.14.0 backends for macOS, Windows and Linux (hidraw).
- `tests/offline.sh` runs the offline round-trip test through CTest.

## License

GPL-3.0-or-later (see `LICENSE`). The code is derived from gcn64tools, Copyright (C) 2007-2017 Raphael Assenat.
hidapi is used under its BSD license (`third_party/hidapi/LICENSE-bsd.txt`).
