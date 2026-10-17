package app

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// comment is a review comment being written on a line, or a range of lines
// of one side, anchored on its last line.
type comment struct {
	path  string
	side  diffSide
	start int // the first line of the range
	line  int // the last line, where it shows
	text  string
	// focus gives the text the focus as it is built.
	focus      bool
	wasFocused bool
	// saved shows the comment as written, to edit or delete, rather than the
	// field it is written in. Comments stay here until the review is
	// submitted: they go to GitHub with it.
	saved bool
}

func (c *comment) pending() bool { return strings.TrimSpace(c.text) != "" }

// label is "New line 12", "Old lines 3-5".
func (c *comment) label() string {
	s := "New"
	if c.side == sideOld {
		s = "Old"
	}
	if c.start != 0 && c.start != c.line {
		return fmt.Sprintf("%s lines %d-%d", s, min(c.start, c.line), max(c.start, c.line))
	}
	return fmt.Sprintf("%s line %d", s, c.line)
}

// addComment starts a comment on a line of a side of a file, or focuses the
// empty draft already there.
func (rv *review) addComment(path string, sd diffSide, line int) {
	rv.addCommentRange(path, sd, line, line)
}

// addCommentRange starts a comment on the lines first to last of a side of a
// file, or focuses the empty draft already there.
func (rv *review) addCommentRange(path string, sd diffSide, first, last int) {
	first, last = min(first, last), max(first, last)
	for _, c := range rv.comments {
		if c.path == path && c.side == sd && c.line == last && min(c.start, c.line) == first && !c.pending() {
			c.focus = true
			return
		}
	}
	// An empty draft elsewhere moves here rather than stay behind.
	rv.comments = slices.DeleteFunc(rv.comments, func(c *comment) bool { return !c.pending() })
	rv.comments = append(rv.comments, &comment{path: path, side: sd, start: first, line: last, focus: true})
	rv.dirty = true
}

// lineSel is the lines of a file being chosen by dragging over them, to
// comment on them together: from where the pointer went down to where it is,
// by the numbers of the lines on one side. The rows are drawn by the review
// surface, which does not tell where they are: each records its place as it is
// painted, from which the pointer finds its row.
type lineSel struct {
	on       bool
	dragging bool
	file     int
	side     diffSide
	anchor   int
	head     int
	// scrollTo is the row to bring into view as the selection grows.
	scrollTo int
	// rects are the rows painted.
	rects map[int]rowRect
}

// rowRect is the place of a row in the window.
type rowRect struct{ x, y, w, h float32 }

func (s *lineSel) range_() (lo, hi int) { return min(s.anchor, s.head), max(s.anchor, s.head) }

// chosen reports whether line n of a side of a file is among those chosen.
func (rv *review) chosen(file int, sd diffSide, n int) bool {
	if s := &rv.sel; s.on && s.file == file && s.side == sd {
		lo, hi := s.range_()
		return n >= lo && n <= hi
	}
	path := rv.files[file].path
	for _, c := range rv.comments {
		if c.path == path && c.side == sd && c.start != 0 && c.start != c.line && n >= min(c.start, c.line) && n <= max(c.start, c.line) {
			return true
		}
	}
	return false
}

// rowAt returns the row painted at a y of the window: the nearest when none
// is there, unless exact, which then says -1.
func (s *lineSel) rowAt(y float32, exact bool) int {
	best, dist := -1, float32(math.MaxFloat32)
	for i, rc := range s.rects {
		if y >= rc.y && y < rc.y+rc.h {
			return i
		}
		d := min(float32(math.Abs(float64(y-rc.y))), float32(math.Abs(float64(y-rc.y-rc.h))))
		if d < dist {
			best, dist = i, d
		}
	}
	if exact {
		return -1
	}
	return best
}

// lineOf is the number of the line a row has on a side, when it has one
// there: a row of side by side lines has one on each, a unified row on the one
// its line is of.
func (rv *review) lineOf(i int, sd diffSide) (int, bool) {
	if i < 0 || i >= len(rv.rows) || rv.rows[i].kind != rowLine {
		return 0, false
	}
	r := rv.rows[i]
	f := rv.files[r.file]
	if rv.split && !f.oneSided() {
		s := sideOf(f, true, r, sd)
		return s.num, s.present && s.num > 0
	}
	s := sideOf(f, false, r, sideNew)
	rowSide := sideNew
	if s.kind == '-' {
		rowSide = sideOld
	}
	return s.num, s.present && s.num > 0 && rowSide == sd
}

// rowInput takes the pointer going down, over and up on line row i, whose
// events are in its own box: down starts a choice of lines (Shift extends it),
// dragging grows it, and letting go starts a comment on them.
func (rv *review) rowInput(i int, ev ui.InputEvent) bool {
	s := &rv.sel
	rc, ok := s.rects[i]
	if !ok {
		return false
	}
	x, y := rc.x+ev.X, rc.y+ev.Y
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button != 0 || !rv.canComment {
			return false
		}
		row := s.rowAt(y, true)
		if row < 0 || rv.rows[row].kind != rowLine {
			return false
		}
		f := rv.files[rv.rows[row].file]
		sd := sideNew
		if rv.split && !f.oneSided() {
			rc := s.rects[row]
			if x < rc.x+rc.w/2 {
				sd = sideOld
			}
		} else if sl := sideOf(f, false, rv.rows[row], sideNew); sl.kind == '-' {
			sd = sideOld
		}
		n, ok := rv.lineOf(row, sd)
		if !ok {
			return false
		}
		file := rv.rows[row].file
		if ev.Mods&ui.Shift != 0 && s.on && s.file == file && s.side == sd {
			s.head = n
		} else {
			s.on, s.file, s.side, s.anchor, s.head = true, file, sd, n, n
		}
		s.dragging = true
		return true
	case ui.InputPointerMove:
		if !s.dragging || ev.Button != 0 {
			return false
		}
		if row := s.rowAt(y, false); row >= 0 && rv.rows[row].file == s.file {
			if n, ok := rv.lineOf(row, s.side); ok && n != s.head {
				s.head = n
				s.scrollTo = row
			}
		}
		return true
	case ui.InputPointerUp:
		if !s.dragging {
			return false
		}
		s.dragging, s.on, s.scrollTo = false, false, -1
		lo, hi := s.range_()
		rv.addCommentRange(rv.files[s.file].path, s.side, lo, hi)
		return true
	}
	return false
}

// draftCount counts the comments written, which the review submitted takes
// to GitHub.
func (rv *review) draftCount() int {
	n := 0
	for _, c := range rv.comments {
		if c.pending() {
			n++
		}
	}
	return n
}

func (rv *review) deleteComment(c *comment) {
	rv.comments = slices.DeleteFunc(rv.comments, func(o *comment) bool { return o == c })
	rv.dirty = true
}

// commentSurface is the surface of a review whose lines can be commented on:
// the lines are chosen by dragging over them, to comment on them together. It
// keeps where the rows are, which their pointer handlers need.
func (tab *repoTab) commentSurface(c *ui.Context, rv *review) {
	if !rv.canComment {
		tab.reviewSurface(c, rv)
		return
	}
	s := &rv.sel
	if s.rects == nil {
		s.rects = map[int]rowRect{}
	}
	clear(s.rects)
	frame := ui.Column(c).Fill().MinWidth(0).MinHeight(0)
	frame.Children(func() {
		if s.scrollTo >= 0 {
			rv.list.ScrollIntoView(s.scrollTo)
			s.scrollTo = -1
		}
		tab.reviewSurface(c, rv)
	})
}

// commentsIn counts the comments on a file: those of its threads, and those
// written here that the review will send.
func (rv *review) commentsIn(f *reviewFile) int {
	if !rv.pr {
		return 0
	}
	n := 0
	for _, th := range rv.threads {
		if th.Path == f.path {
			n += len(th.Comments)
		}
	}
	for _, c := range rv.comments {
		if c.path == f.path && c.pending() {
			n++
		}
	}
	return n
}

// totalComments counts the comments on every file.
func (rv *review) totalComments() int {
	n := 0
	for _, f := range rv.files {
		n += rv.commentsIn(f)
	}
	return n
}

// commentRows are the rows of the threads and of the comments written, in
// the order of the surface: file by file, line by line.
func (rv *review) commentRows() []int {
	var out []int
	for i, r := range rv.rows {
		if r.kind == rowThread || r.kind == rowComment {
			out = append(out, i)
		}
	}
	return out
}

// openComments shows the files that have comments, which may be folded, and
// builds the rows again if they were.
func (rv *review) openComments(only int) {
	for i, f := range rv.files {
		if (only < 0 || only == i) && f.collapsed && rv.commentsIn(f) > 0 {
			f.collapsed, rv.dirty = false, true
		}
	}
	if rv.dirty {
		rv.build()
	}
}

// showRow scrolls the surface to a row, and makes its file the current one.
func (rv *review) showRow(i int) {
	rv.list.ScrollTo(i, ui.Center)
	rv.navRow = i
	rv.current = rv.rows[i].file
	rv.revealedAt = time.Now()
}

// goToComment moves to the next comment, or the previous one for d < 0, from
// the one last moved to if it is in view, else from the top of the surface,
// and round to the first or the last at the end.
func (rv *review) goToComment(d int) {
	rv.openComments(-1)
	rows := rv.commentRows()
	if len(rows) == 0 {
		return
	}
	first, last := rv.list.Visible()
	cur := rv.navRow
	inView := cur >= first && cur <= last && slices.Contains(rows, cur)
	pick := -1
	if d >= 0 {
		for _, r := range rows {
			if (inView && r > cur) || (!inView && r >= first) {
				pick = r
				break
			}
		}
		if pick < 0 {
			pick = rows[0]
		}
	} else {
		for _, r := range slices.Backward(rows) {
			if (inView && r < cur) || (!inView && r < first) {
				pick = r
				break
			}
		}
		if pick < 0 {
			pick = rows[len(rows)-1]
		}
	}
	rv.showRow(pick)
}

// goToFileComment moves to the first comment of a file.
func (rv *review) goToFileComment(file int) {
	rv.openComments(file)
	for i, r := range rv.rows {
		if r.file == file && (r.kind == rowThread || r.kind == rowComment) {
			rv.showRow(i)
			return
		}
	}
}
