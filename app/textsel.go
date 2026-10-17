package app

import (
	"strings"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// The text of a diff is selected as any text is, by dragging over it, to copy
// it (the surface is Selectable); the + of a line then comments on the lines
// selected. Which lines they are is read from the selection each frame: the
// code of each line records its paragraph as it is built, and the code cell
// the pointer went down on records the side the selection is read on, for
// the two sides side by side share the surface's one selection.

// lineRef is a line of a side of a file, which a paragraph of code shows.
type lineRef struct {
	file int
	side diffSide
	num  int
	row  int // the row of the surface it is on
}

// lineSel is the lines the text selected is on, as of the last frame.
type lineSel struct {
	on      bool
	file    int
	side    diffSide
	lo, hi  int
	used    [4]int // the selection a comment was started from, until it changes
	pinFile int
	pinSide diffSide
	// rowsOn, rowLo and rowHi are the first and last rows of the file the
	// selection reaches, from which Local Changes stages its lines.
	split        bool
	rowsOn       bool
	rowLo, rowHi int
	paragrap     map[uint64]lineRef
}

// read finds the lines among the parts of a selection, on the side of the
// code cell the selection started on.
func (s *lineSel) read(parts []ui.SelectedText) {
	s.on, s.rowsOn = false, false
	for _, p := range parts {
		ref, ok := s.paragrap[p.ID]
		if !ok || p.End <= p.Start || !s.pinned(ref) {
			continue
		}
		if !s.rowsOn {
			s.rowsOn, s.file, s.rowLo, s.rowHi = true, ref.file, ref.row, ref.row
		}
		s.rowLo, s.rowHi = min(s.rowLo, ref.row), max(s.rowHi, ref.row)
		if ref.side != s.pinSide {
			continue
		}
		if !s.on {
			s.on, s.file, s.side, s.lo, s.hi = true, ref.file, ref.side, ref.num, ref.num
			continue
		}
		s.lo, s.hi = min(s.lo, ref.num), max(s.hi, ref.num)
	}
	if s.on && s.used == [4]int{s.file, int(s.side), s.lo, s.hi} {
		s.on = false
	}
}

// pinned reports whether a line is read from the selection: it is in the file
// the pointer went down in, and in the column of it when the lines are side by
// side; in one column the old and new lines are the one selection.
func (s *lineSel) pinned(ref lineRef) bool {
	return ref.file == s.pinFile && (!s.split || ref.side == s.pinSide)
}

// press records the side of the code cell the pointer went down on, which
// the selection is then read on.
func (s *lineSel) press(file int, sd diffSide) {
	s.pinFile, s.pinSide = file, sd
}

// copied is what copying a selection puts on the clipboard: the lines of the
// side of the cell the selection started on, in the order they were built,
// as the copy of any other selectable text is.
func (s *lineSel) copied(parts []ui.SelectedText) transfer.Data {
	var out []string
	for _, p := range parts {
		ref, ok := s.paragrap[p.ID]
		if !ok || !s.pinned(ref) {
			continue
		}
		out = append(out, cut(p.Text, p.Start, p.End))
	}
	return transfer.TextData(strings.Join(out, "\n"))
}

// cut is the runes from start up to end of a text, whose parts hold rune
// offsets, not indices into the bytes.
func cut(text string, start, end int) string {
	i, j := 0, len(text)
	n := 0
	for k := range text {
		if n == start {
			i = k
		}
		if n == end {
			j = k
		}
		n++
	}
	return text[i:j]
}

// chosen reports whether line n of a side of a file is in a range of lines a
// comment is being written on.
func (rv *review) chosen(file int, sd diffSide, n int) bool {
	path := rv.files[file].path
	for _, c := range rv.comments {
		if c.path == path && c.side == sd && c.start != 0 && c.start != c.line && n >= min(c.start, c.line) && n <= max(c.start, c.line) {
			return true
		}
	}
	return false
}

// commentSelected starts a comment from the + of a line: on the lines
// selected when the line is among them, else on the line alone.
func (rv *review) commentSelected(file int, sd diffSide, num int) {
	path := rv.files[file].path
	s := &rv.sel
	if s.on && s.file == file && s.side == sd && num >= s.lo && num <= s.hi {
		s.used = [4]int{s.file, int(s.side), s.lo, s.hi}
		s.on = false
		rv.addCommentRange(path, sd, s.lo, s.hi)
		return
	}
	rv.addComment(path, sd, num)
}
