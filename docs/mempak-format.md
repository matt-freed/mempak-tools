# N64 Controller Pak (mempak) format

This describes the Controller Pak filesystem, the image file formats and the
note file format, plus how `mempak-tools` (`internal/mempak`) handles each
part. Notes headed **This tool** describe the tool's own behaviour where it
goes beyond the format or deliberately ignores part of it.

All multi-byte integers are **big-endian**.

**References**

- Nintendo N64 Programming Manual, section 26.3 "Controller Pak":
  <https://ultra64.ca/files/documentation/online-manuals/man/pro-man/pro26/index26.3.html>
- n64brew wiki, Controller Pak/Filesystem:
  <https://n64brew.dev/wiki/Controller_Pak/Filesystem>

### Terminology

| This document | Also called                          |
|---------------|--------------------------------------|
| sector, block | page                                 |
| TOC (table of contents) | I-NODE area, FAT, index table |
| note table    | game note information, directory     |
| note          | game note, file                      |

## 1. Pak layout

A Controller Pak holds **32 768 bytes (0x8000)** of SRAM: 128 sectors of
**256 bytes**.

| Sector | Offset            | Contents                                   |
|--------|-------------------|--------------------------------------------|
| 0      | `0x0000`–`0x00FF` | ID sector: label and ID blocks             |
| 1      | `0x0100`–`0x01FF` | TOC, primary                               |
| 2      | `0x0200`–`0x02FF` | TOC, backup                                |
| 3–4    | `0x0300`–`0x04FF` | Note table (16 entries × 32 bytes)         |
| 5–127  | `0x0500`–`0x7FFF` | Note data, **123 blocks**                  |

Sectors 0–4 (1 280 bytes) are reserved for the filesystem. Data blocks are
numbered by their sector number, so valid data block numbers run from **5 to
127 (`0x05`–`0x7F`)**. The pak holds at most **16 notes**, and each note takes
a whole number of blocks.

Nintendo's library transfers data in **32-byte units**: offsets and lengths
passed to `osPfsReadWriteFile` must be multiples of 32. The adapter protocol
uses the same 32-byte unit.

The SRAM can be accessed at any address, so a note's blocks don't need to be
next to each other or in order. The filesystem has no notion of defragmenting.

### Larger (banked) paks

Third-party paks can hold several 32 KiB **banks**; Nintendo's library supports
up to 62. The bank count is stored in the ID block (section 2). Each TOC page
covers 128 pages, so a pak with `n` banks needs `n` TOC pages plus `n` backup
pages, and the note table moves up after them. For a 512 KiB pak (16 banks,
2048 pages):

| Pages     | Contents            |
|-----------|---------------------|
| 0         | ID sector           |
| 1–16      | TOC                 |
| 17–32     | Backup TOC          |
| 33–34     | Note table          |
| 35–2047   | Note data           |

Each TOC page stores its own checksum in its first entry, so the first page of
every bank after the first can never hold data.

**This tool** only supports single-bank 32 KiB paks. It reads the bank count
from the ID block in use (a count of `0` is read as `1`). If the count is more
than 1, every command that reads the filesystem fails with "multi-bank controller
paks are not supported", instead of misreading the first 32 KiB as a whole pak.

## 2. Sector 0: ID sector

| Offset | Size | Contents              |
|--------|------|-----------------------|
| `0x00` | 32   | Label                 |
| `0x20` | 32   | ID block (primary)    |
| `0x40` | 32   | unused                |
| `0x60` | 32   | ID block (backup 1)   |
| `0x80` | 32   | ID block (backup 2)   |
| `0xA0` | 32   | unused                |
| `0xC0` | 32   | ID block (backup 3)   |
| `0xE0` | 32   | unused                |

The **label** was never specified or used. Its contents are undefined; in
practice it holds garbage written during a repair, or the bytes `0x00`–`0x1F`
copied from example code. Many paks contain `81 01 02 … 1F` here.

### ID block

The four ID blocks are identical copies, so a damaged one can be recovered from
the others.

| Offset | Size | Contents                                                 |
|--------|------|----------------------------------------------------------|
| `0x00` | 24   | Serial number (any value unique to this pak)             |
| `0x18` | 1    | unused (`0x00` or `0xFF`; see below)                     |
| `0x19` | 1    | Device ID: bit 0 = 1 for a Controller Pak; the other bits have no known meaning (see below) |
| `0x1A` | 1    | Bank count, in 32 KiB banks (`1` for a standard pak)     |
| `0x1B` | 1    | unused (`0x00` or `0xFF`; see below)                     |
| `0x1C` | 2    | Checksum 1 = sum of the fourteen 16-bit words at `0x00`–`0x1B`, mod 0x10000 |
| `0x1E` | 2    | Checksum 2 = `0xFFF2 − checksum 1`                       |

Both values occur in these three bytes. The format template in this section,
as found on official paks, has `0xFF` in all three (`ff ff 01 ff` at
`0x18`–`0x1B`). A pak with a different serial number, and so not formatted
from that template, has `00 01 01 00` there: both unused bytes `0x00` and
device ID `0x01`. Nothing is known to read the unused bytes or the device ID's
other bits, and **this tool** neither checks them nor treats either value as
damage: `inspect pak … id` shows them as they are.

Nintendo's library reads the **serial number** when the pak is initialised
and compares it on every access. If it has changed, the call returns
`PFS_ERR_NEW_PAK`, and the game treats that as a different pak being inserted.
So restoring an image dumped from another pak gives this pak the other one's
serial number. Games can't tell the two apart, but swapping contents while a
game is running looks to it like a new pak.

`osPfsInitPak` checks the ID area and returns `PFS_ERR_ID_FATAL` when it is
damaged. Games then offer to repair it with `osPfsRepairId`; nothing repairs it
automatically.

**This tool** checks both checksums of each ID block copy, in order: primary
(`0x20`), then backups at `0x60`, `0x80` and `0xC0`. The first copy that passes
is the one in use, and the pak counts as formatted while **at least one copy is
intact**, since that's what the backups are for. If no copy passes, the pak is
reported as not formatted or corrupted. Nothing else in sector 0 is checked.

Copies that fail their checksums, or differ from the copy in use, are reported
as damaged, but the pak reads normally. `adapter read` prints a warning naming
them, and `ls` lists them among the repairs it recommends (section 5). `repair` rewrites them from the copy in use (section 5).

`mempak inspect pak -f PAKFILE id` shows sector 0 field by field, each field
followed by its bytes, so the dump is split at the field boundaries above and
covers all 256 bytes of the sector. Next to each field is only what its bytes
mean: whether a checksum is right (and if not, what it should be), what the
device ID's bit 0 says, and the bank count as a size. Fields with no known
meaning, such as the label, the serial number and the unused bytes, have
nothing there; their bytes are the whole story. The copy in use, and any copy
that fails its checksums or differs from it, is broken into the fields above;
a copy identical to the one in use is one field, with its 32 bytes under it.
The ID sector has the same layout on multi-bank paks, so `inspect pak … id`
works on them too, with a warning.


### Format template

**This tool** formats with this sector 0, as found on official paks:

```
0x00: 81 01 02 03 04 05 06 07 08 09 0a 0b 0c 0d 0e 0f   ┐ label
0x10: 10 11 12 13 14 15 16 17 18 19 1a 1b 1c 1d 1e 1f   ┘
0x20: ff ff ff ff 05 1a 5f 13 00 00 00 00 00 00 00 00   ┐ ID block
0x30: ff ff ff ff ff ff ff ff ff ff 01 ff 66 25 99 cd   ┘
0x40: 00 × 32
0x60: (ID block repeated)
0x80: (ID block repeated)
0xA0: 00 × 32
0xC0: (ID block repeated)
0xE0: 00 × 32 (implicit; the template ends at 0xDF)
```

The ID block has device ID `0xFF`, bank count `1` and valid checksums: its
words sum to `0x6625`, and `0xFFF2 − 0x6625 = 0x99CD`. Every pak formatted by
this tool gets the same serial number.

## 3. Sectors 1–2: TOC

The TOC is a 256-byte table of **128 two-byte entries**, one per sector. Entry
`i` is at bytes `2i` and `2i+1`, and its low byte (`2i+1`) is the "next block"
pointer of block `i`:

| Value         | Meaning                                         |
|---------------|-------------------------------------------------|
| `0x00`        | Reserved sector (entries 1–4)                   |
| `0x01`        | Last block of a note (end of chain)             |
| `0x03`        | Free block                                      |
| `0x05`–`0x7F` | Next block in the note's chain                  |
| anything else | Invalid (the chain is corrupt)                  |

Entries 0–4 cover the reserved sectors and are never used as pointers. Byte 0
is always `0` and byte 1 holds the checksum. On factory paks entries 1–4 are
`0`, but Nintendo's library ignores their contents.

On a 32 KiB pak the high byte of every entry is `0`.

Sector 2 is an exact copy of sector 1. Because each copy has its own checksum,
a reader can fall back to the backup if the primary is damaged.

`osPfsChecker` in Nintendo's library repairs damaged TOCs and note tables,
usually losing data. **This tool** only reads the TOC as it is; `repair`
(section 5) fixes a damaged TOC copy and frees blocks that belong to no note.

### Checksum

Byte 1 (the low byte of entry 0) is the checksum of the data entries:

```
checksum = (Σ toc[2i] + toc[2i+1] for i in 5..127) mod 256
```

Entries 1–4 are not included. On a blank pak all 123 data entries are `00 03`,
so the checksum is `0x71`.

When entries 1–4 are `0`, as on factory paks, this equals summing every byte
from offset `0x02` to the end of the page. When they hold other values, only
entries 5–127 count. Games accept a pak with `0x03` in entries 1–4 (as
other tools formats it), and they keep those values when rewriting the TOC.
 **This tool** formats with `0` there, like a
factory pak, so tools using either formula accept its output.

**This tool** sums only the low byte of each entry. Since the high bytes are
always `0` on a 32 KiB pak, the result is the same; they could only differ on a
pak that is already corrupt. The tool never writes high bytes and keeps
whatever was there.

### Choosing a TOC

1. Validate sector 0 (section 2). If that fails, the pak is not formatted.
2. Use **sector 1** if its checksum matches.
3. Otherwise use **sector 2** if its checksum matches.
4. Otherwise the pak is not formatted.

### Writing

When saving a modified TOC, the checksum is recomputed and the TOC is written
to both sectors. **This tool** writes the **alternate copy first**, then the
one that was valid, so at least one valid TOC exists at every step.

### Free space

Free space is the number of entries `5..127` whose value is `0x03`. The maximum
is 123.

### Inspecting the TOC

`mempak inspect pak -f PAKFILE toc` shows each TOC copy field by field, each
field followed by its bytes:

- byte 0 (unused) and the checksum, which is "ok" or says what it should be;
- entries 1–4 together, as the reserved sectors;
- entries 5–127, grouped into the same runs as the data blocks (section 4,
  "Following the block chain"), with the run's chain in chain order and where
  it goes next, such as `12→13→…→73, then end` or `74→75→76→77, then block 86`.
  Free runs are labelled "free". A run that no readable note reaches has each
  entry decoded on its own: "next block N", "last block of a note" or
  "invalid". Entries whose high byte is not `0` are pointed out.

Each copy is decoded from its own entries, so a stale or damaged backup shows
its own chains. A copy identical to the one in use is a single field, with its
256 bytes under it.

## 4. Sectors 3–4: note table

There are 16 note slots (0–15). Each has a 32-byte header at
`0x300 + slot × 32`. Unused entries are all zeros.

| Offset | Size | Field                                                         |
|--------|------|---------------------------------------------------------------|
| `0x00` | 4    | Game code, ASCII. The last byte (`0x03`) is the region code   |
| `0x04` | 2    | Publisher (company) code, ASCII                               |
| `0x06` | 2    | **Start inode**: first data block of the note                 |
| `0x08` | 1    | Status flags                                                  |
| `0x09` | 1    | Unused (often garbage)                                        |
| `0x0A` | 2    | Unused                                                        |
| `0x0C` | 4    | Extension, N64 charset                                        |
| `0x10` | 16   | Note name, N64 charset, padded with `0x00`                    |

- **Game code:** 4 ASCII bytes, non-zero. It is the same media code as in the
  game's ROM header, for example `NSME`. The last byte is the ROM's region
  (`E` = USA, `J` = Japan, `P` = Europe, and so on).
- **Publisher code:** 2 ASCII bytes, non-zero, assigned by Nintendo to each
  licensee. Games may only open notes with their own publisher code; other
  publishers' notes may only be deleted from the Controller Pak menu.
- **Datel GameShark** is an exception: it writes the non-ASCII game code
  `0x3BADD1E5` and publisher code `0xFADE`.
- **Status:** bit flags. Bit `0x02` should always be set. Values seen on real
  paks are `0x02` and `0x82`. Nintendo's library writes `0x02` ("occupied")
  when it creates a note, but it decides whether a slot is used from the game
  and publisher codes, not from this byte, and so does this tool. The meaning
  of bit `0x80` is not documented and has not been seen to change how a note
  behaves. 
- **Byte `0x09`:** commonly `0x03` or `0x19`.
- **Extension:** `ext[0]` is `0x00` for a game's main note and `0x10`–`0x33`
  (`0`–`9`, `A`–`Z`) for extra notes. By convention the second note uses
  `0x1A` (`A`), the third `0x1B` (`B`), and so on. `ext[1..3]` are reserved and
  normally `0x00`. The standard Controller Pak menu used by most games shows
  only `ext[0]`, but some games with their own menu, such as Ogre Battle 64,
  show all four characters. Unused characters are padded with NUL.
- **Name:** 16 characters, padded with **`0x00`** (NUL), not the `0x0F` space
  character.
- Games are expected to use a single note where possible, and notes should not
  depend on each other.

### libdragon extension

libdragon stores extra data in the unused bytes:

| Offset | Size | Contents                                                     |
|--------|------|--------------------------------------------------------------|
| `0x09` | 1    | Number of padding bytes in the note's last block, so the exact file size can be recovered |
| `0x0A` | 2    | CRC16 (polynomial `0x5935`, not reflected) over the whole note, computed with the CRC bytes set to 0 |

The padding byte is only trusted when the CRC matches.

### This tool

Game and publisher codes are printed as raw bytes. With `ls -v`, the game code
is also looked up in `internal/mempak/gamecodes.tsv`, a table of the 808 retail
cartridge codes in the No-Intro N64 DAT, generated by `scripts/gamecodes.py`. The
title is shown only on an exact 4-byte match, so prototypes, homebrew, cheat
devices and games that write a code other than their cartridge ID show no title.
Publisher codes are not translated.

The tool checks only whether the codes are zero, plus the start inode in
`0x06`–`0x07`. Any non-zero code is accepted, including GameShark's. Every other byte is copied unchanged when notes
are inserted, exported or imported. Only `ext[0]` is shown.

#### When a slot holds a note

A slot is **used** when its game code and its publisher code are **both
non-zero**, and **free** when either one is zero. A deleted note's header is
cleared to 32 zero bytes.

A used slot is one of two things:

- **A note:** the start inode is in `0x0005`–`0x007F` and the block chain
  starting there is valid (see below).
- **A damaged note:** anything else. `ls` shows it as
  `(damaged: broken block chain)`. Reading, exporting or deleting it fails with
  "filesystem is corrupted". A damaged note is **never treated as free**:
  `insert` without `-s` skips it, and `insert -s N` asks before overwriting it.
  Its blocks can't be traced, so they stay marked as used after it is
  overwritten. `repair` (section 5) removes damaged notes and frees their
  blocks.

### Following the block chain

```
blocks = [inode]
cur = inode
loop:
    n = toc[2*cur + 1]
    if n == 0x01: done
    if n == 0x03: corrupt              (a note cannot contain a free block)
    if n < 0x05 or n > 0x7F: corrupt
    cur = n; append cur to blocks
    if len(blocks) > 123: corrupt      (loop in the chain)
```

The note's data is its blocks' sectors joined in chain order. Its size is
always `len(blocks) × 256`.

`mempak inspect pak` uses the chains to label the data blocks. Each
block is labelled with the note it belongs to and its position in that note's
chain ("part 1 of 5" is the inode), or as free (`0x03` in the TOC). A block the
TOC marks as used that no readable chain reaches, such as a block of a damaged
note, is labelled "used, but not part of a readable note". Consecutive blocks
share one heading when they are free, or when they hold consecutive parts of
the same note in either direction. Inserted notes are linked in reverse, so
their runs read like "parts 5-1 of 5". Without a valid TOC, blocks 5–127 are
shown as one unlabelled section.

`mempak inspect pak -f PAKFILE notes` shows the note table one slot at a time.
A free slot (zero game or publisher code) is a single field labelled "free",
with its 32 bytes under it. A used slot is labelled with its name and size, or
"damaged: broken block chain", and split into the fields of the table above:
the game code with the game's title when known, the publisher code, the start
inode as a block number (or "not a data block: damaged"), the status and
unused bytes (shown only as bytes), the extension character and the name.
GameShark's non-ASCII codes are labelled "Datel GameShark". The data blocks
themselves are always shown as plain hex, since their layout is up to each
game.

`mempak inspect pak -s N PAKFILE` shows only what belongs to the note in slot
N: its note-table entry, its entries in the TOC in use (grouped into runs, as
above), and its data blocks. The other TOC copy is compared at the same
blocks; where its bytes differ, its entries there are shown too, each decoded
on its own, since its chain may not hold. A damaged note, or any note in an
image without a valid TOC, shows only its note-table entry, since its blocks
can't be found.

### Name display

**This tool** displays a name as the 16 name bytes followed by `.` and the
extension character, decoded with the charset below. For `ls` the trailing
padding is trimmed, and the `.EXT` part is dropped when the extension is blank.
Codes outside the table, including `0x00` padding, are shown as a space.

#### Character set

Rows are the high nibble and columns the low nibble. `0x00` is NUL (padding)
and ␠ is a space. Codes `0x01`–`0x0E` and `0x95`–`0xFF` are undefined.

|        | 0 | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | A | B | C | D | E | F |
|--------|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| `0x00` | NUL |   |   |   |   |   |   |   |   |   |   |   |   |   |   | ␠ |
| `0x10` | 0 | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | A | B | C | D | E | F |
| `0x20` | G | H | I | J | K | L | M | N | O | P | Q | R | S | T | U | V |
| `0x30` | W | X | Y | Z | ! | " | # | ' | * | + | , | - | . | / | : | = |
| `0x40` | ? | @ | 。 | ゛ | ゜ | ァ | ィ | ゥ | ェ | ォ | ッ | ャ | ュ | ョ | ヲ | ン |
| `0x50` | ア | イ | ウ | エ | オ | カ | キ | ク | ケ | コ | サ | シ | ス | セ | ソ | タ |
| `0x60` | チ | ツ | テ | ト | ナ | ニ | ヌ | ネ | ノ | ハ | ヒ | フ | ヘ | ホ | マ | ミ |
| `0x70` | ム | メ | モ | ヤ | ユ | ヨ | ラ | リ | ル | レ | ロ | ワ | ガ | ギ | グ | ゲ |
| `0x80` | ゴ | ザ | ジ | ズ | ゼ | ゾ | ダ | ヂ | ヅ | デ | ド | バ | ビ | ブ | ベ | ボ |
| `0x90` | パ | ピ | プ | ペ | ポ |   |   |   |   |   |   |   |   |   |   |   |

The Unicode characters used for display are U+0027 for `0x37`, and U+309B
(dakuten) and U+309C (handakuten) for `0x43` and `0x44`.

> **Difference from gcn64tools.** The C gcn64tools table shows a backtick for
> `0x37`, and U+212B `Å` and U+212C `ℬ` for `0x43` and `0x44` (apparently a
> typo for U+309B and U+309C). Only the display differs; the name bytes stored
> on the pak are the same.

## 5. Filesystem operations

These describe how **this tool** changes a pak.

### Format

1. Write the sector 0 template (section 2).
2. Zero sectors 3 and 4 (the note table).
3. Build a TOC with entries 0–4 set to `00 00` and entries 5–127 set to
   `00 03`, then set byte 1 to the checksum (`0x71`).
4. Write that TOC to sectors 1 and 2.

Data sectors 5–127 are left as they were. The TOC matches a factory pak, so its
checksum is valid whether it is summed from entry 5 or from offset `0x02`
(section 3).

### Delete a note

1. Zero the slot's 32-byte header.
2. Set every block in the note's chain to `0x03`.
3. Write the TOC (alternate first).

Data sectors are not cleared.

### Insert a note

Input: a 32-byte header, data that is a non-zero multiple of 256 bytes, and
either a target slot or "first free slot".

1. Reject the note if its game code or publisher code is zero. Otherwise it
   would read back as a free slot.
2. Pick a slot:
   - **Target slot:** blocks used by a readable note already in that slot count
     as free space.
   - **First free slot:** use the lowest free slot (zero game or publisher
     code). Damaged notes are skipped. If there is no free slot, fail with no
     space.
3. Fail with no space if the note needs more blocks than are free.
4. Delete the existing note in the slot if it is readable. Otherwise zero the
   header, which leaves a damaged note's blocks allocated until `repair` frees
   them.
5. **Allocate** by scanning blocks from `0x05` up, taking the lowest free blocks
   and **linking them in reverse**:
   ```
   inode = 0x01
   for i in 0x05..0x7F while taken < n:
       if toc[i] == 0x03:
           toc[i] = inode
           inode = i
   ```
   The **highest** block taken becomes the start inode, and the chain runs
   downward to the lowest block, which holds `0x01`. For example, three free
   blocks 5, 6, 7 give the chain `7 → 6 → 5 → end`.
6. Copy the data to the blocks in chain order.
7. Write the TOC (alternate first).
8. Write the header **as given**, except bytes `0x06`–`0x07`, which become the
   new start inode.

### Repair

`mempak repair PAKFILE` does what it can for a damaged image, much like
`osPfsChecker` and `osPfsRepairId` in Nintendo's library:

1. **ID blocks:** every copy that fails its checksums, or differs from the
   first intact copy, is rewritten from that copy.
2. **TOC:** if the two TOC copies differ (one fails its checksum, or is out of
   date), both are rewritten from the copy in use, as chosen in section 3.
3. **Damaged notes** (used slots whose chain is broken, section 4) are
   removed: their header is zeroed.
4. **Orphaned blocks:** every block in `0x05`–`0x7F` whose TOC entry isn't
   `0x03` (free) and that isn't part of a readable note's chain is set to
   `0x03`. This frees the blocks of removed notes, including the parts of a
   broken chain that could still be followed, and blocks left behind by
   overwriting a damaged note. When several damaged notes exist, their blocks
   can't be told apart, so all of them are freed together.

`ls` runs the same repair on a copy of the image. If it would change anything,
`ls` ends with a recommendation to run `mempak repair`, listing the repairs.
This also reveals damage `ls` doesn't otherwise show, such as a mismatched TOC
copy or orphaned blocks.

It can't help when no ID block copy is intact, when neither TOC copy passes its
checksum, or on a multi-bank pak; it then changes nothing. Cross-linked chains
(two readable notes sharing a block) aren't detected. Neither are TOC entries
0–4, the reserved entries.

Removed notes and freed blocks lose their data, so the command lists the
repairs and asks before changing anything (`-y` skips the question). It first
copies the original file, byte for byte, to `PAKFILE.bak`, and refuses to run
if that file already exists.

## 6. Image files

**This tool** supports one image format: **`.mpk`**, the 32 768 raw pak bytes,
nothing else. Every command that writes an image writes this format, whatever
the file's extension.

A file of any other size is read as raw data: its first 32 KiB, zero-padded if
it is shorter. `ls` lists it with a warning, so it can be inspected, but `rm`,
`insert` and `adapter write` refuse it, and nothing saves it back.

### Formats that are not supported

- **N64 (DexDrive):** files starting with `123-456-STD`, with a 256-byte comment
  per note slot at `0x40` and the pak data at `0x1040`. 
- **MPK4:** a 128 KiB file written as four copies of the 32 KiB pak, and any 64,
  96 or 128 KiB file read using only its first 32 KiB. Real 128 KiB files could
  be dumps of switch-selected "4x" paks, holding four different paks, or of true
  multi-bank paks (section 1).

Such files are handled as the wrong size. A multi-bank dump is still
recognised from its ID block and rejected as multi-bank (section 1).

## 7. Note files

A single note is exported in the format used by
[bryc's mempak editor](https://github.com/bryc/mempak):

| Offset | Size      | Contents                                                 |
|--------|-----------|----------------------------------------------------------|
| `0x00` | 32        | The note-table header, with bytes `0x06`–`0x07` = `CA FE` |
| `0x20` | n × 256   | Note data, blocks in chain order                         |

### Export

Copy the slot's header, replace the inode with `0xCAFE`, then append the data.

### Inspect

`mempak inspect note` applies the import checks below, shows the main header
fields, and then hex dumps the header and the data. With `-f`, the header is
shown field by field instead, using the note-table layout in section 4, each
field followed by its bytes. The header is the file's own, so the start inode
shows the `CA FE` marker. The data is always one dump, with offsets from the
start of the data.

### Import

A note file is accepted only if **all** of these are true:

1. It is at least 32 bytes and bytes `0x06`–`0x07` are `CA FE`.
2. Its game code and publisher code are both non-zero. The inode is then set to
   `0x0005` as a placeholder until `insert` allocates blocks.
3. The data after the header is a non-zero multiple of 256 bytes.

The header is then inserted as described in section 5, keeping all its other
bytes verbatim.
