package main

import (
	"fmt"
	"slices"
	"strings"
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
	// posting is set while the comment goes to GitHub, postErr when it
	// could not.
	posting bool
	postErr string
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
	for _, c := range rv.comments {
		if c.path == path && c.side == sd && c.line == line && !c.pending() {
			c.focus = true
			return
		}
	}
	// An empty draft elsewhere moves here rather than stay behind.
	rv.comments = slices.DeleteFunc(rv.comments, func(c *comment) bool { return !c.pending() && !c.posting })
	rv.comments = append(rv.comments, &comment{path: path, side: sd, start: line, line: line, focus: true})
	rv.dirty = true
}

func (rv *review) deleteComment(c *comment) {
	rv.comments = slices.DeleteFunc(rv.comments, func(o *comment) bool { return o == c })
	rv.dirty = true
}
