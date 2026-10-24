package app

import (
	"fmt"
	"slices"
	"strings"
	"time"
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
	focusedAt  time.Time
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

// filesWithComments are the files that have comments, in the order of the
// surface.
func (rv *review) filesWithComments() []int {
	var out []int
	for i, f := range rv.files {
		if rv.commentsIn(f) > 0 {
			out = append(out, i)
		}
	}
	return out
}

// goToComment moves to the file of the next comment, or of the previous one
// for d < 0, from the one last moved to, and round to the first or the last at
// the end.
func (rv *review) goToComment(d int) {
	files := rv.filesWithComments()
	if len(files) == 0 {
		return
	}
	pick := files[0]
	if d >= 0 {
		for _, i := range files {
			if i > rv.navFile {
				pick = i
				break
			}
		}
	} else {
		pick = files[len(files)-1]
		for _, i := range slices.Backward(files) {
			if i < rv.navFile {
				pick = i
				break
			}
		}
	}
	rv.goToFileComment(pick)
}

// goToFileComment moves to the comments of a file, which is unfolded.
func (rv *review) goToFileComment(file int) {
	rv.navFile = file
	rv.reveal(file)
}
