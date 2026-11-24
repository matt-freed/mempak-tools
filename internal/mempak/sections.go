// SPDX-License-Identifier: GPL-3.0-or-later

package mempak

import "fmt"

// Section is a labelled part of an image, for display.
type Section struct {
	Label  string
	Offset int // offset of Data in the image
	Data   []byte
}

// Sections splits the whole image into labelled parts, in file order: the ID
// sector, the two TOC copies, the note table, and runs of data blocks. A run
// is a stretch of free blocks, or consecutive blocks holding consecutive parts
// of one note, counting up or down. Blocks that the TOC marks as used but that no readable note
// reaches (such as those of a damaged note) are labelled as such. Without a
// valid TOC, the data blocks are a single unlabelled section.
func (img *Image) Sections() []Section {
	sec := func(label string, first, last int) Section {
		return Section{label, first * BlockSize, img.Data[first*BlockSize : (last+1)*BlockSize]}
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

	const (
		free   = -1
		orphan = -2 // used in the TOC, but not part of a readable note
	)
	type owner struct{ slot, part, parts int }
	var owners [numSectors]owner
	for b := blockValidFirst; b <= blockValidLast; b++ {
		owners[b].slot = orphan
		if t.next(b) == blockEmpty {
			owners[b].slot = free
		}
	}
	names := make([]string, NumNotes)
	for slot := range NumNotes {
		n := img.note(&t, slot)
		if !n.Valid {
			continue
		}
		names[slot] = n.Name()
		blocks, _ := t.chain(n.inode())
		for i, b := range blocks {
			if owners[b].slot == orphan { // a block two notes share goes to the first
				owners[b] = owner{slot, i + 1, len(blocks)}
			}
		}
	}

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
		label := fmt.Sprintf("Block %d", first)
		if last > first {
			label = fmt.Sprintf("Blocks %d-%d", first, last)
		}
		switch o.slot {
		case free:
			label += ": free"
		case orphan:
			label += ": used, but not part of a readable note"
		default:
			part := fmt.Sprintf("part %d", o.part)
			if last > first {
				part = fmt.Sprintf("parts %d-%d", o.part, owners[last].part)
			}
			label += fmt.Sprintf(": note %d '%s', %s of %d", o.slot, names[o.slot], part, o.parts)
		}
		out = append(out, sec(label, first, last))
		first = last + 1
	}
	return out
}
