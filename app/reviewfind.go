package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/worddiff"
)

// A search in the lines of a review (a commit's changes, a pull request's
// code, Local Changes) marks where its text is in the lines shown, and goes
// from one to the next.

var (
	findColor    = ui.RGBA(255, 200, 0, 0.42)
	findCurColor = ui.RGBA(255, 140, 0, 0.78)
)

// findHit is a place the search text is in: a text of a row of the surface,
// by its bytes.
type findHit struct {
	row        int
	side       diffSide
	start, end int
}

// reviewFind is the state of a review's search.
type reviewFind struct {
	open  bool
	focus bool
	query string
	// cur is the hit the search is at.
	cur  int
	hits []findHit
	// byRow has, for a row, the hits in it as indices of hits.
	byRow map[int][]int
	// forQuery and forVer are what hits were found for: the text, and the
	// version of the rows.
	forQuery string
	forVer   int
	forOK    bool
}

// openFind shows the search, which takes the keys.
func (rv *review) openFind() {
	if rv == nil {
		return
	}
	rv.find.open, rv.find.focus = true, true
}

// toggleFind shows the search, or hides it when it shows.
func (rv *review) toggleFind() {
	if rv.find.open {
		rv.closeFind()
		return
	}
	rv.openFind()
}

// closeFind hides the search, and with it the marks of what it found.
func (rv *review) closeFind() {
	rv.find.open, rv.find.focus = false, false
}

// findRanges returns where a text has q, as bytes, whatever the case.
func findRanges(text, q string) [][2]int {
	if q == "" {
		return nil
	}
	lt, lq := strings.ToLower(text), strings.ToLower(q)
	// A text whose lowercase is not as long has bytes that are not its own:
	// it is read as it is.
	if len(lt) != len(text) || len(lq) != len(q) {
		lt, lq = text, q
	}
	var out [][2]int
	for at := 0; at < len(lt); {
		i := strings.Index(lt[at:], lq)
		if i < 0 {
			break
		}
		out = append(out, [2]int{at + i, at + i + len(lq)})
		at += i + len(lq)
	}
	return out
}

// refreshFind finds the hits again when the text or the rows changed.
func (rv *review) refreshFind() {
	fs := &rv.find
	if !fs.open || fs.query == "" {
		fs.hits, fs.byRow, fs.forOK = nil, nil, false
		fs.cur = 0
		return
	}
	if fs.forOK && fs.forQuery == fs.query && fs.forVer == rv.ver {
		return
	}
	fresh := fs.forQuery != fs.query || !fs.forOK
	fs.hits, fs.byRow = fs.hits[:0], map[int][]int{}
	for i, r := range rv.rows {
		if r.kind != rowLine {
			continue
		}
		f := rv.files[r.file]
		add := func(s lineSide, sd diffSide) {
			for _, rg := range findRanges(s.text, fs.query) {
				fs.byRow[i] = append(fs.byRow[i], len(fs.hits))
				fs.hits = append(fs.hits, findHit{row: i, side: sd, start: rg[0], end: rg[1]})
			}
		}
		if rv.split && !f.oneSided() {
			// A line both sides have is in the search once.
			if left := sideOf(f, true, r, sideOld); left.kind == '-' {
				add(left, sideOld)
			}
			add(sideOf(f, true, r, sideNew), sideNew)
			continue
		}
		s := sideOf(f, false, r, sideNew)
		if s.kind == '-' {
			add(s, sideOld)
		} else {
			add(s, sideNew)
		}
	}
	fs.forQuery, fs.forVer, fs.forOK = fs.query, rv.ver, true
	if fresh || fs.cur >= len(fs.hits) {
		fs.cur = 0
	}
}

// findStatus is how far the search is, for the bar.
func (rv *review) findStatus() string {
	fs := &rv.find
	switch {
	case fs.query == "":
		return ""
	case len(fs.hits) == 0:
		return "No results"
	}
	return fmt.Sprintf("%d of %d", fs.cur+1, len(fs.hits))
}

// findStep goes to the hit after or before the current one, round the ends,
// and shows it.
func (rv *review) findStep(d int) {
	fs := &rv.find
	if n := len(fs.hits); n > 0 {
		fs.cur = ((fs.cur+d)%n + n) % n
		rv.showHit()
	}
}

// showHit scrolls the surface to the current hit.
func (rv *review) showHit() {
	fs := &rv.find
	if fs.cur >= 0 && fs.cur < len(fs.hits) {
		rv.list.ScrollTo(fs.hits[fs.cur].row, ui.Center)
	}
}

// findMarks are the marks of the hits in a side of a row.
func (rv *review) findMarks(row int, sd diffSide) []mark {
	fs := &rv.find
	if !fs.open {
		return nil
	}
	var out []mark
	for _, hi := range fs.byRow[row] {
		h := fs.hits[hi]
		if h.side != sd {
			continue
		}
		color := findColor
		if hi == fs.cur {
			color = findCurColor
		}
		out = append(out, mark{Range: worddiff.Range{Start: h.start, End: h.end}, color: color})
	}
	return out
}

// findBar is the search's bar above the lines, with what it asks for done.
func (rv *review) findBar(c *ui.Context) {
	fs := &rv.find
	q := fs.query
	act := findBar(c, &q, "Find in changes", rv.findStatus(), len(fs.hits) > 0, &fs.focus)
	if q != fs.query {
		fs.query = q
		rv.refreshFind()
		rv.showHit()
	}
	switch act {
	case findPrev:
		rv.findStep(-1)
	case findNext:
		rv.findStep(1)
	case findDone:
		rv.closeFind()
	}
}
