#!/usr/bin/env bash
# Offline round-trip test for the image-file commands. No adapter needed.
# Usage: tests/offline.sh path/to/mempak
set -euo pipefail

MEMPAK=${1:?usage: offline.sh path/to/mempak}
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
cd "$WORK"

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "ok - $*"; }

# Byte-compare two files. Uses cmp when available; otherwise falls back to
# cksum (coreutils), since some environments (e.g. a minimal MSYS2) lack cmp.
same_file() {
	if command -v cmp >/dev/null 2>&1; then
		cmp -s "$1" "$2"
	else
		[ "$(cksum < "$1")" = "$(cksum < "$2")" ]
	fi
}

# Build a 2-block note file in the bryc/mempak export format:
# 32-byte note header with inode 0xCAFE, followed by 2 x 256 data bytes.
#   game code "NSM", region 'E', publisher "01", name "TEST" (N64 charset)
make_note() {
	{
		printf '\x4e\x53\x4d\x45'          # game code + region
		printf '\x30\x31'                  # publisher
		printf '\xca\xfe'                  # inode marker for exported notes
		printf '\x00%.0s' {1..8}           # status, reserved, extension
		printf '\x2d\x1e\x2c\x2d'          # "TEST"
		printf '\x00%.0s' {1..12}
		local i
		for ((i = 0; i < 512; i++)); do
			printf "\\x$(printf '%02x' $(( (i * 7 + 3) & 0xff )))"
		done
	} > "$1"
}

make_note test.note
[ "$(wc -c < test.note | tr -d ' ')" = 544 ] || fail "fixture note has wrong size"

# format
"$MEMPAK" format blank.mpk >/dev/null
[ "$(wc -c < blank.mpk | tr -d ' ')" = 32768 ] || fail "format: mpk should be 32768 bytes"
# .mpk is the only format: other extensions don't change what is written
"$MEMPAK" format blank.n64 >/dev/null
[ "$(wc -c < blank.n64 | tr -d ' ')" = 32768 ] || fail "format: every output should be a 32768-byte mpk"
# -f (mpk4/n64) was removed: it should be a usage error
rc=0; "$MEMPAK" format -f n64 x.n64 >/dev/null 2>&1 || rc=$?
[ "$rc" = 2 ] || fail "format -f should be a usage error (exit 2), got $rc"
pass "format"

# ls on blank
"$MEMPAK" ls blank.mpk > ls.txt
grep -q "Blocks used: 0 / 123" ls.txt || fail "ls: blank pak should have 0 used blocks"
[ "$(grep -c 'Free' ls.txt)" = 16 ] || fail "ls: blank pak should have 16 free notes"
pass "ls blank"

# insert (first free slot)
cp blank.mpk pak.mpk
"$MEMPAK" insert pak.mpk test.note > /dev/null
"$MEMPAK" ls pak.mpk > ls.txt
grep -q "Blocks used: 2 / 123" ls.txt || fail "insert: expected 2 used blocks"
grep -q "Note  0: TEST" ls.txt || fail "insert: note 0 should be TEST"
pass "insert"

# ls -v shows codes and the game title from the code table
"$MEMPAK" ls -v pak.mpk > ls.txt
grep -q "Note  0: TEST .*  game NSME  publisher 01  Super Mario 64 (USA)$" ls.txt ||
	fail "ls -v: note 0 should show codes and title"
pass "ls -v"

# inspect pak: the layout, then a hex dump of each section
"$MEMPAK" inspect pak pak.mpk > inspect.txt || fail "inspect pak failed"
grep -q "^0x0000-0x00ff  id       Sector 0: ID sector$" inspect.txt || fail "inspect pak: ID sector missing"
grep -q "^0x0300-0x04ff  notes    Sectors 3-4: note table$" inspect.txt || fail "inspect pak: note table missing"
grep -q "^0x0500-0x06ff  data     Blocks 5-6: note 0 'TEST', parts 1-2 of 2$" inspect.txt || fail "inspect pak: note blocks missing"
grep -q "^0x0700-0x7fff  data     Blocks 7-127: free$" inspect.txt || fail "inspect pak: free blocks missing"
grep -q "^Offsets        Section  Contents$" inspect.txt || fail "inspect pak: layout header missing"
grep -q "^To show one section: mempak inspect pak pak.mpk SECTION \[-f\]$" inspect.txt || fail "inspect pak: section hint missing"
grep -q "^0x0100-0x01ff  toc      Sector 1: TOC$" inspect.txt || fail "inspect pak: TOC should be labelled with its section"
grep -q "^0x0500-0x06ff  Blocks 5-6: note 0 'TEST', parts 1-2 of 2:$" inspect.txt || fail "inspect pak: section dump heading missing"
[ "$(grep -n -m1 '^0000: ' inspect.txt | cut -d: -f1)" -gt "$(grep -n -m1 'Blocks 7-127: free$' inspect.txt | cut -d: -f1)" ] || fail "inspect pak: hex dump should follow the layout"
grep -q "^0020: " inspect.txt || fail "inspect pak: sector 0 should be dumped as one block"
grep -q "^7fe0: " inspect.txt || fail "inspect pak: dump should reach the end of the image"
grep -q "ID block (primary)" inspect.txt && fail "inspect pak: fields shown without -f"
"$MEMPAK" ls -x pak.mpk >/dev/null 2>&1 && fail "ls -x was removed and should fail"
"$MEMPAK" inspect pak -x pak.mpk >/dev/null 2>&1 && fail "inspect pak -x was replaced and should fail"
head -c 32768 /dev/zero | LC_ALL=C tr '\0' '\377' > ff.mpk
[ "$(wc -c < ff.mpk | tr -d " ")" = 32768 ] || fail "fixture ff.mpk has wrong size"
if "$MEMPAK" inspect pak ff.mpk > inspect.txt; then fail "inspect pak: unformatted image should fail"; fi
grep -q "Blocks 5-127: data (no valid TOC" inspect.txt || fail "inspect pak: unformatted image should still be shown"
rc=0; "$MEMPAK" inspect >/dev/null 2>&1 || rc=$?
[ "$rc" = 2 ] || fail "inspect without a subcommand should be a usage error (exit 2), got $rc"
pass "inspect pak"

# inspect pak -f: fields with their bytes; sections without fields stay one dump
"$MEMPAK" inspect pak -f pak.mpk > inspect.txt || fail "inspect pak -f failed"
grep -q "^  0x0020  32  ID block (primary)   in use$" inspect.txt || fail "inspect pak -f: primary should be in use"
grep -q "^    0x003a   1  bank count         1 (32 KiB)$" inspect.txt || fail "inspect pak -f: bank count missing"
grep -A1 "^    0x003c   2  checksum 1         ok$" inspect.txt | grep -q "^                003c: 66 25 " || fail "inspect pak -f: checksum 1 bytes should follow the field"
grep -A1 "^  0x0060  32  ID block (backup 1)  identical to the copy in use$" inspect.txt | grep -q "^              0060: ff ff ff ff " || fail "inspect pak -f: a one-line copy should show its 32 bytes"
grep -q "^0020: " inspect.txt && fail "inspect pak -f: sector 0 should be split by field"
grep -q "^0100: " inspect.txt && fail "inspect pak -f: the TOC should be split by field"
grep -Eq "^  0x0101 +1 +checksum +ok$" inspect.txt || fail "inspect pak -f: TOC checksum missing"
grep -Eq "^  0x010a +4 +blocks 5-6 +note 0 'TEST', parts 1-2 of 2: 5→6, then end$" inspect.txt || fail "inspect pak -f: TOC chain missing"
grep -Eq "^  0x0200 256 +TOC +identical to the copy in use \(sector 1\)$" inspect.txt || fail "inspect pak -f: identical backup TOC should be one field"
grep -Eq "^  0x0300 +32 +slot 0 +'TEST', 2 blocks$" inspect.txt || fail "inspect pak -f: note table slot 0 missing"
grep -Eq "^    0x0306 +2 +start inode +block 5$" inspect.txt || fail "inspect pak -f: slot 0 start inode missing"
grep -Eq "^  0x0320 +32 +slot 1 +free$" inspect.txt || fail "inspect pak -f: free slot missing"
grep -q "^7fe0: " inspect.txt || fail "inspect pak -f: data blocks should still be dumped"
grep -Eq "^    0x0020 +24 +serial number$" inspect.txt || fail "inspect pak -f: a field with no known meaning should have no value"
pass "inspect pak -f"

# inspect pak SECTION: only that section, with or without -f
"$MEMPAK" inspect pak pak.mpk id > inspect.txt || fail "inspect pak id failed"
[ "$(grep -c '^0x' inspect.txt)" = 1 ] || fail "inspect pak id: should show only sector 0, without the layout"
grep -q "^00e0: " inspect.txt || fail "inspect pak id: sector 0 not dumped"
grep -q "0100: " inspect.txt && fail "inspect pak id: should show only sector 0"
"$MEMPAK" inspect pak pak.mpk toc > inspect.txt
grep -q "^0x0100-0x01ff  Sector 1: TOC:$" inspect.txt || fail "inspect pak toc: TOC missing"
grep -q "^0x0200-0x02ff  Sector 2: backup TOC:$" inspect.txt || fail "inspect pak toc: backup TOC missing"
[ "$(grep -c '^0x' inspect.txt)" = 2 ] || fail "inspect pak toc: should show only the two TOCs, without the layout"
"$MEMPAK" inspect pak pak.mpk data > inspect.txt
grep -q "Sector" inspect.txt && fail "inspect pak data: should show only data blocks"
grep -q "^0x0700-0x7fff  Blocks 7-127: free:$" inspect.txt || fail "inspect pak data: free blocks missing"
grep -q "^0x0700-0x7fff  data" inspect.txt && fail "inspect pak data: layout should be left out"
grep -q "To show one section" inspect.txt && fail "inspect pak data: section hint should be left out"
cp pak.mpk id.mpk
printf '\x00' | dd of=id.mpk bs=1 seek=$((0x9c)) conv=notrunc 2>/dev/null  # break backup 2's checksum
"$MEMPAK" inspect pak -f id.mpk id > inspect.txt 2>/dev/null
grep -q "ID block (backup 2)  damaged: bad checksum" inspect.txt || fail "inspect pak -f id: damaged copy not reported"
grep -q "checksum 1         bad: should be 0x6625, the sum of the words at 0x00-0x1b" inspect.txt || fail "inspect pak -f id: bad checksum not explained"
grep -q "Sector 1" inspect.txt && fail "inspect pak -f id: should show only sector 0"
rc=0; "$MEMPAK" inspect pak pak.mpk bogus >/dev/null 2>&1 || rc=$?
[ "$rc" = 2 ] || fail "inspect pak with an unknown section should be a usage error (exit 2), got $rc"
"$MEMPAK" format dmg.mpk >/dev/null
"$MEMPAK" insert dmg.mpk test.note >/dev/null
"$MEMPAK" insert dmg.mpk test.note >/dev/null
printf '\x00\x00' | dd of=dmg.mpk bs=1 seek=$((0x306)) conv=notrunc 2>/dev/null  # slot 0: start block lost
"$MEMPAK" inspect pak -f dmg.mpk notes > inspect.txt
grep -Eq "slot 0 +'TEST', damaged: broken block chain" inspect.txt || fail "inspect pak -f notes: damaged note not reported"
grep -Eq "start inode +not a data block: damaged" inspect.txt || fail "inspect pak -f notes: bad start inode not explained"
"$MEMPAK" inspect pak -f dmg.mpk toc > inspect.txt
grep -Eq "blocks 5-6 +used, but not part of a readable note" inspect.txt || fail "inspect pak -f toc: orphaned blocks not reported"
grep -Eq "^    0x010a +2 +block 5 +next block 6$" inspect.txt || fail "inspect pak -f toc: orphaned entries not decoded"
pass "inspect pak SECTION"

# inspect pak -s N: only what belongs to one note
"$MEMPAK" inspect pak pak.mpk -s 0 > inspect.txt || fail "inspect pak -s failed"
grep -Eq "^0x0300-0x031f +notes +Slot 0: 'TEST', 2 blocks$" inspect.txt || fail "inspect pak -s: note-table entry missing"
grep -Eq "^0x010a-0x010d +toc +Sector 1: TOC entries of blocks 5-6$" inspect.txt || fail "inspect pak -s: TOC entries missing"
grep -Eq "^0x0500-0x06ff +data +Blocks 5-6: note 0 'TEST', parts 1-2 of 2$" inspect.txt || fail "inspect pak -s: data blocks missing"
grep -q "Sector 0" inspect.txt && fail "inspect pak -s: should not show the ID sector"
grep -q "backup TOC" inspect.txt && fail "inspect pak -s: an identical backup TOC should not be shown"
grep -q "^To show one section: mempak inspect pak pak.mpk -s 0 SECTION \[-f\]$" inspect.txt || fail "inspect pak -s: section hint missing"
"$MEMPAK" inspect pak pak.mpk -s 0 toc -f > inspect.txt
grep -Eq "^  0x010a +4 +blocks 5-6 +note 0 'TEST', parts 1-2 of 2: 5→6, then end$" inspect.txt || fail "inspect pak -s toc -f: chain missing"
grep -q "Slot 0" inspect.txt && fail "inspect pak -s toc: should show only TOC entries"
cp pak.mpk bk.mpk
printf '\x03' | dd of=bk.mpk bs=1 seek=$((0x20d)) conv=notrunc 2>/dev/null  # backup TOC: block 6 free
"$MEMPAK" inspect pak bk.mpk -s 0 -f toc > inspect.txt
grep -Eq "Sector 2: backup TOC \(bad checksum\) entries of blocks 5-6:$" inspect.txt || fail "inspect pak -s: differing backup TOC entries missing"
grep -Eq "^    0x020c +2 +block 6 +free$" inspect.txt || fail "inspect pak -s: differing backup entry not decoded"
"$MEMPAK" inspect pak dmg.mpk -s 0 > inspect.txt || fail "inspect pak -s on a damaged note should succeed"
grep -q "block chain can't be followed" inspect.txt || fail "inspect pak -s: damaged note not explained"
if "$MEMPAK" inspect pak pak.mpk -s 3 >/dev/null 2>&1; then fail "inspect pak -s on a free slot should fail"; fi
rc=0; "$MEMPAK" inspect pak pak.mpk -s 16 >/dev/null 2>&1 || rc=$?
[ "$rc" = 2 ] || fail "inspect pak -s 16 should be a usage error (exit 2), got $rc"
rc=0; "$MEMPAK" inspect pak pak.mpk -s 0 id >/dev/null 2>&1 || rc=$?
[ "$rc" = 2 ] || fail "inspect pak -s with id should be a usage error (exit 2), got $rc"
pass "inspect pak -s"

# insert into a specific slot
"$MEMPAK" insert -s 5 pak.mpk test.note > /dev/null
"$MEMPAK" ls pak.mpk | grep -q "Note  5: TEST" || fail "insert -s 5: note 5 should be TEST"
# an occupied slot needs confirmation or --overwrite
if echo n | "$MEMPAK" insert -s 5 pak.mpk test.note >/dev/null 2>&1; then fail "insert -s onto a used slot should fail when declined"; fi
echo y | "$MEMPAK" insert -s 5 pak.mpk test.note > /dev/null 2>&1 || fail "insert -s onto a used slot should succeed when confirmed"
"$MEMPAK" insert -s 5 --overwrite pak.mpk test.note < /dev/null > /dev/null || fail "insert -s --overwrite should not ask"
"$MEMPAK" ls pak.mpk | grep -q "Blocks used: 4 / 123" || fail "insert -s: overwriting should free the old note's blocks"
if "$MEMPAK" insert --overwrite pak.mpk test.note >/dev/null 2>&1; then fail "--overwrite without -s should be rejected"; fi
pass "insert --slot / --overwrite"

# extract and compare
"$MEMPAK" extract pak.mpk 5 out.note > /dev/null
same_file test.note out.note || fail "extract: exported note differs from original"
if "$MEMPAK" extract pak.mpk 3 bad.note >/dev/null 2>&1; then fail "extract of empty slot should fail"; fi
if "$MEMPAK" extract pak.mpk 16 bad.note >/dev/null 2>&1; then fail "extract of slot 16 should fail"; fi
pass "extract"

# inspect a note file
"$MEMPAK" inspect note test.note > inspect.txt || fail "inspect note failed"
grep -q "Name:      TEST" inspect.txt || fail "inspect note: wrong name"
grep -q "Game code: NSME (" inspect.txt || fail "inspect note: wrong game code or no title"
grep -q "Publisher: 01" inspect.txt || fail "inspect note: wrong publisher"
grep -q "Size:      2 blocks (512 bytes of data)" inspect.txt || fail "inspect note: wrong size"
grep -q "^0000: 4e 53 4d 45 30 31 ca fe" inspect.txt || fail "inspect note: header should be dumped"
grep -q "^01e0: " inspect.txt || fail "inspect note: data not dumped"
grep -q "start inode" inspect.txt && fail "inspect note: fields shown without -f"
"$MEMPAK" inspect note -f test.note > inspect.txt
grep -q "^  0x0006   2  start inode     note file marker" inspect.txt || fail "inspect note -f: header fields missing"
grep -A1 "^  0x0000   4  game code" inspect.txt | grep -q "^              0000: 4e 53 4d 45 " || fail "inspect note -f: game code bytes should follow the field"
grep -q "^  0x0010  16  name            \"TEST\"$" inspect.txt || fail "inspect note -f: name field missing"
grep -q "^01e0: " inspect.txt || fail "inspect note -f: data not dumped"
if "$MEMPAK" inspect note blank.mpk >/dev/null 2>&1; then fail "inspect of a non-note file should fail"; fi
pass "inspect note"

# rm
"$MEMPAK" rm pak.mpk 0 > /dev/null
"$MEMPAK" ls pak.mpk > ls.txt
grep -q "Note  0: Free" ls.txt || fail "rm: note 0 should be free"
grep -q "Blocks used: 2 / 123" ls.txt || fail "rm: expected 2 used blocks"
if "$MEMPAK" rm pak.mpk 0 >/dev/null 2>&1; then fail "rm of a free note should fail"; fi
pass "rm"

# a file that isn't 32 KiB can be listed (with a warning) but not modified
{ cat pak.mpk; printf '\x00%.0s' {1..16}; } > big.mpk
"$MEMPAK" ls big.mpk > ls.txt 2> err.txt || fail "ls: wrong-size file should still list"
grep -q "not 32 KiB" err.txt || fail "ls: wrong-size file should warn"
cp big.mpk big-before.mpk
if "$MEMPAK" rm big.mpk 0 >/dev/null 2>&1; then fail "rm: wrong-size file should not be modified"; fi
same_file big.mpk big-before.mpk || fail "rm: wrong-size file changed"
if "$MEMPAK" convert pak.mpk x.mpk >/dev/null 2>&1; then fail "convert was removed and should fail"; fi
pass "wrong-size image"

# repair: a damaged note is removed, its blocks freed, and the original kept as .bak
"$MEMPAK" format rep.mpk >/dev/null
"$MEMPAK" insert rep.mpk test.note >/dev/null
"$MEMPAK" insert rep.mpk test.note >/dev/null
printf '\x00\x00' | dd of=rep.mpk bs=1 seek=$((0x306)) conv=notrunc 2>/dev/null  # slot 0: start block lost
cp rep.mpk rep-damaged.mpk
"$MEMPAK" ls rep.mpk > ls.txt
grep -q "damaged" ls.txt || fail "repair: setup should leave a damaged note"
grep -q "Run 'mempak repair rep.mpk'" ls.txt || fail "ls: damaged image should recommend repair"
if echo n | "$MEMPAK" repair rep.mpk >/dev/null 2>&1; then fail "repair: declining should fail"; fi
same_file rep.mpk rep-damaged.mpk || fail "repair: declining should change nothing"
[ ! -e rep.mpk.bak ] || fail "repair: declining should not write a backup"
"$MEMPAK" repair -y rep.mpk > repair.txt || fail "repair -y failed"
grep -q "Note 0 'TEST' is damaged" repair.txt || fail "repair: damaged note not reported"
grep -q "A backup of the original file will be created first: 'rep.mpk.bak'" repair.txt || fail "repair: backup file not announced"
grep -q "damaged notes are removed, not recovered" repair.txt || fail "repair: limitations not shown"
"$MEMPAK" repair --help | grep -q "Limitations:" || fail "repair --help: limitations not shown"
same_file rep.mpk.bak rep-damaged.mpk || fail "repair: backup should hold the original file"
"$MEMPAK" ls rep.mpk > ls.txt
grep -q "Blocks used: 2 / 123" ls.txt || fail "repair: the damaged note's blocks should be freed"
grep -q "Note  0: Free" ls.txt || fail "repair: damaged note should be removed"
grep -q "Note  1: TEST" ls.txt || fail "repair: readable note should be kept"
grep -q "mempak repair" ls.txt && fail "ls: repaired image should not recommend repair"
"$MEMPAK" repair rep.mpk | grep -q "needs no repair" || fail "repair: repaired image should need no repair"
cp rep-damaged.mpk rep.mpk
if "$MEMPAK" repair -y rep.mpk >/dev/null 2>&1; then fail "repair: should refuse to overwrite an existing backup"; fi
same_file rep.mpk rep-damaged.mpk || fail "repair: refusing should change nothing"
pass "repair"

# argument validation
if "$MEMPAK" rm pak.mpk abc >/dev/null 2>&1; then fail "non-numeric note id should be rejected"; fi
if "$MEMPAK" format -f bogus x.mpk >/dev/null 2>&1; then fail "unknown flag should be rejected"; fi
if "$MEMPAK" nosuchcmd >/dev/null 2>&1; then fail "unknown command should fail"; fi
"$MEMPAK" --help >/dev/null
"$MEMPAK" adapter --help >/dev/null
"$MEMPAK" adapter read --help >/dev/null
"$MEMPAK" repair --help >/dev/null
pass "argument handling"

echo "All offline tests passed"
