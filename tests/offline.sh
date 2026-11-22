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
