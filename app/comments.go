package app

import (
	"fmt"
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

// selectSurface is the surface whose text is selected as any text is, by
// dragging over it, to copy it: the code of each line records its paragraph
// as it is built, and the code cell the pointer went down on records the
// side the selection is read on. In a pull request, the + of a line
// comments on the lines selected, and in Local Changes the box stages them.
func (tab *repoTab) selectSurface(c *ui.Context, rv *review) {
	s := &rv.sel
	s.paragrap, s.split = map[uint64]lineRef{}, rv.split
	frame := ui.Column(c).Fill().MinWidth(0).MinHeight(0).Selectable()
	// Copying takes the lines the selection is read on, the side of the cell
	// the selection started on.
	frame.CopyData(s.copied)
	frame.Children(func() { tab.reviewSurface(c, rv) })
	s.read(frame.SelectedTexts())
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
