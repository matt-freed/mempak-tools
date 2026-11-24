// SPDX-License-Identifier: GPL-3.0-or-later

package mempak

import "fmt"

// Kinds of Section.
const (
	SectionID    = "id"    // sector 0
	SectionTOC   = "toc"   // sectors 1 and 2
	SectionNotes = "notes" // the note table, sectors 3 and 4
	SectionData  = "data"  // a run of data blocks
)

// Section is a labelled part of an image, for display.
type Section struct {
	Kind   string // SectionID, SectionTOC, SectionNotes or SectionData
	Label  string
	Offset int // offset of Data in the image
	Data   []byte
}

// Sections splits the whole image into labelled parts, in file order: the ID
// sector, the two TOC copies, the note table, and runs of data blocks. A run
// is a stretch of free blocks, or consecutive blocks holding consecutive parts
// of one note, counting up or down. Blocks that the TOC marks as used but
// that no readable note reaches (such as those of a damaged note) are
// labelled as such. Without a
// valid TOC, the data blocks are a single unlabelled section.
func (img *Image) Sections() []Section {
	sec := func(label string, first, last int) Section {
		kind := SectionData
		switch first {
		case 0:
			kind = SectionID
		case 1, 2:
			kind = SectionTOC
		case 3:
			kind = SectionNotes
		}
		return Section{kind, label, first * BlockSize, img.Data[first*BlockSize : (last+1)*BlockSize]}
	}
	tocLabel := func(n int, label string) string {
		var t toc
		copy(t[:], img.sector(n))
		if t.checksum() != t[1] {
			label += " (bad checksum)"
		}
		return label
	}
	out := []Section{
		sec("Sector 0: ID sector", 0, 0),
		sec(tocLabel(1, "Sector 1: TOC"), 1, 1),
		sec(tocLabel(2, "Sector 2: backup TOC"), 2, 2),
		sec("Sectors 3-4: note table", 3, 4),
	}
	t, _, err := img.readTOC()
	if err != nil {
		return append(out, sec(fmt.Sprintf("Blocks %d-%d: data (no valid TOC, so the notes' blocks are unknown)",
			blockValidFirst, blockValidLast), blockValidFirst, blockValidLast))
	}

	runs, names := img.blockRuns(&t)
	for _, r := range runs {
		label := fmt.Sprintf("Block %d", r.first)
		if r.last > r.first {
			label = fmt.Sprintf("Blocks %d-%d", r.first, r.last)
		}
		out = append(out, sec(label+": "+r.label(names), r.first, r.last))
	}
	return out
}

// Owners of a block, for blockRun.slot.
const (
	runFree   = -1
	runOrphan = -2 // used in the TOC, but not part of a readable note
)

// blockRun is a stretch of data blocks: free blocks, blocks the TOC marks as
// used that no readable note reaches, or consecutive blocks holding
// consecutive parts (1-based, counting up or down) of one note.
type blockRun struct {
	first, last         int
	slot                int // the note's slot, runFree or runOrphan
	firstPart, lastPart int // the parts in blocks first and last
	parts               int // the note's number of blocks
}

// blockRuns splits blocks 5-127 into runs, following the chains in t. It
// also returns the names of the readable notes, by slot.
func (img *Image) blockRuns(t *toc) ([]blockRun, []string) {
	type owner struct{ slot, part, parts int }
	var owners [numSectors]owner
	for b := blockValidFirst; b <= blockValidLast; b++ {
		owners[b].slot = runOrphan
		if t.next(b) == blockEmpty {
			owners[b].slot = runFree
		}
	}
	names := make([]string, NumNotes)
	for slot := range NumNotes {
		n := img.note(t, slot)
		if !n.Valid {
			continue
		}
		names[slot] = n.Name()
		blocks, _ := t.chain(n.inode())
		for i, b := range blocks {
			if owners[b].slot == runOrphan { // a block two notes share goes to the first
				owners[b] = owner{slot, i + 1, len(blocks)}
			}
		}
	}

	var runs []blockRun
	for first := blockValidFirst; first <= blockValidLast; {
		// A note's run may count up or down: inserted notes are linked in
		// reverse, so their parts are in descending block order.
		o, last, step := owners[first], first, 0
		for last < blockValidLast {
			next := owners[last+1]
			if next.slot != o.slot {
				break
			}
			if o.slot >= 0 {
				d := next.part - owners[last].part
				if (d != 1 && d != -1) || (step != 0 && d != step) {
					break
				}
				step = d
			}
			last++
		}
		runs = append(runs, blockRun{first, last, o.slot, o.part, owners[last].part, o.parts})
		first = last + 1
	}
	return runs, names
}

// label describes what a run holds, such as "note 0 'TEST', parts 2-1 of 2".
func (r blockRun) label(names []string) string {
	switch r.slot {
	case runFree:
		return "free"
	case runOrphan:
		return "used, but not part of a readable note"
	}
	part := fmt.Sprintf("part %d", r.firstPart)
	if r.last > r.first {
		part = fmt.Sprintf("parts %d-%d", r.firstPart, r.lastPart)
	}
	return fmt.Sprintf("note %d '%s', %s of %d", r.slot, names[r.slot], part, r.parts)
}
