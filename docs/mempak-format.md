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
| TOC           | I-NODE area, FAT, index table        |
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

**This tool** only supports single-bank 32 KiB paks.

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
| `0x18` | 1    | unused (normally `0`)                                    |
| `0x19` | 1    | Device ID: bit 0 = 1 for a Controller Pak; other bits normally 0 but may be garbage |
| `0x1A` | 1    | Bank count, in 32 KiB banks (`1` for a standard pak)     |
| `0x1B` | 1    | unused (normally `0`)                                    |
| `0x1C` | 2    | Checksum 1 = sum of the fourteen 16-bit words at `0x00`–`0x1B`, mod 0x10000 |
| `0x1E` | 2    | Checksum 2 = `0xFFF2 − checksum 1`                       |

Nintendo's library reads the **serial number** when the pak is initialised
and compares it on every access. If it has changed, the call returns
`PFS_ERR_NEW_PAK`, and the game treats that as a different pak being inserted.
So restoring an image dumped from another pak gives this pak the other one's
serial number. Games can't tell the two apart, but swapping contents while a
game is running looks to it like a new pak.

`osPfsInitPak` checks the ID area and returns `PFS_ERR_ID_FATAL` when it is
damaged. Games then offer to repair it with `osPfsRepairId`; nothing repairs it
automatically.

**This tool** considers a pak formatted only if the first 16 bytes of the four
ID blocks match:

```
h[0x20:0x30] == h[0x60:0x70]
h[0x80:0x90] == h[0xC0:0xD0]
h[0x20:0x30] == h[0x80:0x90]
```

It doesn't verify the ID block checksums, and nothing else in sector 0 is
checked.

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
usually losing data. **This tool** doesn't repair anything: a broken chain just
makes the slot appear empty (section 4).

### Checksum

Byte 1 (the low byte of entry 0) is the checksum of the data entries:

```
checksum = (Σ toc[2i] + toc[2i+1] for i in 5..127) mod 256
```

Entries 1–4 are not included. On a blank pak all 123 data entries are `00 03`,
so the checksum is `0x71`.

When entries 1–4 are `0`, as on factory paks, this equals summing every byte
from offset `0x02` to the end of the page. When they hold other values, only
entries 5–127 count. Games accept a pak with `0x03` in entries 1–4 (as this
tool and gcn64tools format it), and they keep those values when rewriting the
TOC. This has been verified on hardware.

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
  paks are `0x02` and `0x82`.
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

Game and publisher codes are printed as raw bytes. Only byte `0x03` and bytes
`0x06`–`0x07` are validated. Every other byte is copied unchanged when notes
are inserted, exported or imported. Only `ext[0]` is shown.

#### Region codes

A header is valid only if byte `0x03` is one of:

`0x00`, `0x37`, `0x41`, `0x44`, `0x45`, `0x46`, `0x49`, `0x4A`, `0x50`,
`0x53`, `0x55`, `0x58`, `0x59`

(as ASCII: `7 A D E F I J P S U X Y`, plus `0x00`). This list comes from
libdragon, not from the format itself. GameShark notes (region byte `0xE5`)
fail the check, so the tool treats them as empty slots and refuses to import
them.

#### When a slot holds a note

A slot holds a note when **all** of these are true:

1. The start inode is in `0x0005`–`0x007F`.
2. The region code is in the list above.
3. The block chain starting at that inode is valid (see below).

If 1 or 2 fails, the slot is empty. If 3 fails, the slot is also shown as empty,
and reading its data fails with "filesystem is corrupted".

Empty slots are cleared to 32 zero bytes when a note is deleted.

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
3. Build a TOC with all 128 entries set to `00 03`, then set byte 1 to the
   checksum (`0x71`).
4. Write that TOC to sectors 1 and 2.

Data sectors 5–127 are left as they were. Step 3 copies gcn64tools: factory
paks have `0` in entries 1–4 instead, but games ignore those entries
(section 3).

### Delete a note

1. Zero the slot's 32-byte header.
2. Set every block in the note's chain to `0x03`.
3. Write the TOC (alternate first).

Data sectors are not cleared.

### Insert a note

Input: a 32-byte header, data that is a non-zero multiple of 256 bytes, and
either a target slot or "first free slot".

1. Reject the note if the header's region code is invalid.
2. Pick a slot:
   - **Target slot:** blocks used by a note already in that slot count as free
     space.
   - **First free slot:** use the lowest slot whose header fails the
     inode/region check. If there is none, fail with no space.
3. Fail with no space if the note needs more blocks than are free.
4. Delete the existing note in the slot if there is one; otherwise zero the
   header.
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

## 6. Image file formats

### Detection (loading)

Detection checks file size first, then content:

| Condition                                | Format    | Notes                                    |
|------------------------------------------|-----------|------------------------------------------|
| size = 32 768                            | **MPK**   | Raw pak                                  |
| size = 65 536, 98 304 or 131 072         | **MPK4**  | Only the first 32 kB is used             |
| starts with ASCII `123-456-STD`          | **N64**   | DexDrive, see below                      |
| anything else                            | *Invalid* | First 32 kB copied as raw, zero-padded   |

An image loaded as *Invalid* can be read and listed, but in-place edits (`rm`,
`insert`) refuse to save it rather than guess a format.

### Choosing an output format

The format comes from the extension: `*.n64` (any case) writes **N64**, and
everything else, including `.mpk`, writes **MPK** (32 kB). MPK4 is only written
when asked for explicitly (`mpk`, `mpk4`, `n64`).

### MPK

The 32 768 raw pak bytes, nothing else.

### MPK4

The 32 kB pak repeated **4 times** (131 072 bytes). When reading a 2×, 3× or 4×
file, only the first copy is used.

### N64 (DexDrive)

| Offset            | Size   | Contents                                              |
|-------------------|--------|-------------------------------------------------------|
| `0x0000`          | 11     | Magic `123-456-STD` (ASCII)                           |
| `0x000B`–`0x003F` | 53     | Header fields; written as zeros, ignored on read      |
| `0x0040`          | 16×256 | One comment per note slot, slot `i` at `0x40 + i×256` |
| `0x1040`          | 32 768 | Raw pak data                                          |

Total size when written: **`0x9040` (36 928) bytes**.

Comments are NUL-terminated byte strings. When reading, each 256-byte field is
cut at the first NUL. When writing, at most **255 bytes** are stored, so a
terminating NUL is always left. Comments belong to slots, not to note data: the
tool clears a slot's comment when the note is deleted or replaced. When reading,
truncated files are tolerated: missing comments stay empty and missing data
bytes stay zero.

## 7. Note files

A single note is exported in the format used by
[bryc's mempak editor](https://github.com/bryc/mempak):

| Offset | Size      | Contents                                                 |
|--------|-----------|----------------------------------------------------------|
| `0x00` | 32        | The note-table header, with bytes `0x06`–`0x07` = `CA FE` |
| `0x20` | n × 256   | Note data, blocks in chain order                         |

### Export

Copy the slot's header, replace the inode with `0xCAFE`, then append the data.

### Import

A note file is accepted only if **all** of these are true:

1. It is at least 32 bytes and bytes `0x06`–`0x07` are `CA FE`.
2. After setting the inode to `0x0005` as a placeholder, the header passes the
   inode/region check, so the region code must be valid.
3. The data after the header is a non-zero multiple of 256 bytes.

The header is then inserted as described in section 5, keeping all its other
bytes verbatim.
