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
"$MEMPAK" format blank.n64 >/dev/null
"$MEMPAK" format -f mpk4 blank4.mpk >/dev/null
[ "$(wc -c < blank4.mpk | tr -d ' ')" = 131072 ] || fail "format: mpk4 should be 131072 bytes"
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

# insert into a specific slot
"$MEMPAK" insert -s 5 pak.mpk test.note > /dev/null
"$MEMPAK" ls pak.mpk | grep -q "Note  5: TEST" || fail "insert -s 5: note 5 should be TEST"
# -d requires an existing note
if "$MEMPAK" insert -d 9 pak.mpk test.note >/dev/null 2>&1; then fail "insert -d onto empty slot should fail"; fi
pass "insert --slot / --dst"

# extract and compare
"$MEMPAK" extract pak.mpk 5 out.note > /dev/null
cmp -s test.note out.note || fail "extract: exported note differs from original"
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

# convert round trip mpk -> n64 (with comment) -> mpk
"$MEMPAK" convert pak.mpk pak.n64 > /dev/null
"$MEMPAK" insert -s 1 -c "hello world" pak.n64 test.note > /dev/null
"$MEMPAK" ls pak.n64 | grep -q "{ hello world }" || fail "n64 comment not stored"
"$MEMPAK" convert pak.n64 back.mpk > /dev/null
[ "$(wc -c < back.mpk | tr -d ' ')" = 32768 ] || fail "convert: mpk should be 32768 bytes"
"$MEMPAK" extract back.mpk 1 rt.note > /dev/null
cmp -s test.note rt.note || fail "convert: note changed across mpk->n64->mpk"
pass "convert"

# argument validation
if "$MEMPAK" rm pak.mpk abc >/dev/null 2>&1; then fail "non-numeric note id should be rejected"; fi
if "$MEMPAK" format -f bogus x.mpk >/dev/null 2>&1; then fail "unknown format should be rejected"; fi
if "$MEMPAK" nosuchcmd >/dev/null 2>&1; then fail "unknown command should fail"; fi
"$MEMPAK" --help >/dev/null
"$MEMPAK" adapter --help >/dev/null
"$MEMPAK" adapter read --help >/dev/null
pass "argument handling"

echo "All offline tests passed"
