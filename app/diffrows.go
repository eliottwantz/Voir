package app

import (
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
	"github.com/eliott/myfork/internal/github"
	"github.com/eliott/myfork/internal/highlight"
	"github.com/eliott/myfork/internal/worddiff"
)

// diffHunk is a hunk of a file's patch, with how many lines it holds on
// each side.
type diffHunk struct {
	git.PatchHunk
	oldLines, newLines int
	// orig is, for each of Lines, its index in the hunk as the patch has it,
	// with the markers of a missing newline, which the lines leave out.
	orig []int
}

type diffSide uint8

const (
	sideOld diffSide = iota
	sideNew
)

// gapShown is how many lines of a gap of unchanged lines show, from its top
// and its bottom.
type gapShown struct{ top, bottom int }

// spanKey identifies the code of a line on one side, in the light or the
// dark.
type spanKey struct {
	hunk, index int32
	num         int32
	side        diffSide
	dark        bool
}

// expandStep is how many lines a click on a gap's arrows shows, and
// inlineGap the most unchanged lines between hunks shown without asking.
const (
	expandStep = 100
	inlineGap  = 12
)

// oneSided reports whether the file is all new or all gone, with lines on
// one side only.
func (f *reviewFile) oneSided() bool { return f.status == 'A' || f.status == 'D' }

// prepare reads the patch's hunks and finds the words changed in the lines
// they replace, once.
func (f *reviewFile) prepare() {
	if f.parsed {
		return
	}
	f.parsed = true
	p, err := git.ParsePatchLines(f.patch)
	if err != nil {
		f.unreadable = true
		return
	}
	for _, h := range p.Hunks {
		dh := diffHunk{PatchHunk: h}
		dh.Lines = nil
		for li, l := range h.Lines {
			if l.Kind == '\\' {
				continue
			}
			dh.Lines = append(dh.Lines, l)
			dh.orig = append(dh.orig, li)
			if l.Kind != '+' {
				dh.oldLines++
			}
			if l.Kind != '-' {
				dh.newLines++
			}
		}
		f.hunks = append(f.hunks, dh)
	}
	f.words = map[[2]int][]worddiff.Range{}
	for hi := range f.hunks {
		lines := f.hunks[hi].Lines
		for _, p := range worddiff.Pairs(lines) {
			ra, rb := worddiff.WordDiff(lines[p.Del].Text, lines[p.Add].Text)
			if ra != nil || rb != nil {
				f.words[[2]int{hi, p.Del}] = ra
				f.words[[2]int{hi, p.Add}] = rb
			}
		}
	}
}

// lineAt returns the line of a hunk, nil for -1.
func (f *reviewFile) lineAt(hunk, i int32) *git.PatchLine {
	if hunk < 0 || i < 0 {
		return nil
	}
	return &f.hunks[hunk].Lines[i]
}

// gap is a run of unchanged lines between hunks, or before the first or
// after the last.
type gap struct {
	index              int
	oldStart, newStart int // the numbers of its first lines
	count              int
}

// lastLines returns the numbers of the last lines of a hunk on each side.
func lastLines(h *diffHunk) (int, int) {
	o := h.OldStart + h.oldLines - 1
	if h.oldLines == 0 {
		o = h.OldStart
	}
	n := h.NewStart + h.newLines - 1
	if h.newLines == 0 {
		n = h.NewStart
	}
	return o, n
}

// gapBefore returns the gap before hunk i; i == len(hunks) is the gap
// after the last, known once the contents are read.
func (f *reviewFile) gapBefore(i int) gap {
	prevOld, prevNew := 0, 0
	if i > 0 {
		prevOld, prevNew = lastLines(&f.hunks[i-1])
	}
	g := gap{index: i, oldStart: prevOld + 1, newStart: prevNew + 1}
	if i < len(f.hunks) {
		h := &f.hunks[i]
		first := h.OldStart
		if h.oldLines == 0 {
			first = h.OldStart + 1
		}
		g.count = first - g.oldStart
		return g
	}
	switch {
	case f.newLines != nil:
		g.count = len(f.newLines) - prevNew
	case f.oldLines != nil:
		g.count = len(f.oldLines) - prevOld
	}
	return g
}

// contextText returns the text of an unchanged line, by its numbers.
func (f *reviewFile) contextText(old, new int32) string {
	if new > 0 && int(new) <= len(f.newLines) {
		return f.newLines[new-1]
	}
	if old > 0 && int(old) <= len(f.oldLines) {
		return f.oldLines[old-1]
	}
	return ""
}

// canExpand reports whether gaps can show their lines.
func (f *reviewFile) canExpand() bool { return f.newLines != nil || f.oldLines != nil }

// fileMetrics are the widths a file's rows share: of its line numbers, and
// how far its lines go.
type fileMetrics struct {
	digits  int
	maxCols int
}

func (f *reviewFile) metrics() fileMetrics {
	if f.metricsDone {
		return f.metric
	}
	maxNum, maxCols := 0, 0
	for _, h := range f.hunks {
		maxNum = max(maxNum, h.OldStart+h.oldLines, h.NewStart+h.newLines)
		for _, l := range h.Lines {
			maxCols = max(maxCols, columns(l.Text))
		}
	}
	maxNum = max(maxNum, len(f.oldLines), len(f.newLines))
	if f.canExpand() {
		for _, l := range f.newLines {
			maxCols = max(maxCols, columns(l))
		}
		for _, l := range f.oldLines {
			maxCols = max(maxCols, columns(l))
		}
	}
	f.metric = fileMetrics{digits: max(len(strconv.Itoa(maxNum)), 2), maxCols: maxCols}
	f.metricsDone = true
	return f.metric
}

// appendFileRows adds the rows of a file's lines to a surface: its note
// when its lines do not show, else its hunks with the gaps between them,
// and the bottom of its card.
func (rv *review) appendFileRows(rows []reviewRow, fi int, f *reviewFile, group int) []reviewRow {
	idx := fi
	base := reviewRow{file: idx, group: group, a: -1, b: -1}
	add := func(r reviewRow) { rows = append(rows, r) }
	note := func(n int32) {
		r := base
		r.kind, r.count = rowFileNote, n
		add(r)
	}
	end := func() {
		rows = rv.appendUnplaced(rows, fi, f, group)
		r := base
		r.kind = rowEnd
		add(r)
	}
	switch {
	case f.binary:
		note(noteBinary)
		end()
		return rows
	case f.big && !f.open:
		note(noteBig)
		end()
		return rows
	case f.patch == "" || (f.added+f.deleted == 0 && f.status != 'R'):
		note(noteNoText)
		end()
		return rows
	}
	if git.PatchHasLFSPointer(f.patch) {
		note(noteLFS)
	}
	f.prepare()
	if f.unreadable {
		note(noteUnreadable)
		end()
		return rows
	}
	split := rv.split && !f.oneSided()
	line := func(h, a, b int32) {
		r := base
		r.kind, r.hunk, r.a, r.b = rowLine, h, a, b
		add(r)
		if a >= 0 {
			rows = rv.appendComments(rows, fi, f, group, &f.hunks[h].Lines[a])
		}
		if b >= 0 && b != a {
			rows = rv.appendComments(rows, fi, f, group, &f.hunks[h].Lines[b])
		}
	}
	context := func(old, new int32) {
		r := base
		r.kind, r.hunk, r.old, r.new = rowLine, -1, old, new
		add(r)
		l := git.PatchLine{Kind: ' ', Old: int(old), New: int(new)}
		rows = rv.appendComments(rows, fi, f, group, &l)
	}
	addGap := func(g gap) {
		if g.count <= 0 {
			return
		}
		shown := f.expanded[g.index]
		if !f.canExpand() {
			shown = gapShown{}
		} else if g.count <= inlineGap {
			shown = gapShown{top: g.count}
		}
		hidden := g.count - shown.top - shown.bottom
		if hidden <= 0 {
			shown = gapShown{top: g.count}
			hidden = 0
		}
		for k := range shown.top {
			context(int32(g.oldStart+k), int32(g.newStart+k))
		}
		if hidden > 0 {
			r := base
			r.kind, r.gap, r.old, r.new, r.count = rowGap, int32(g.index), int32(g.oldStart+shown.top), int32(g.newStart+shown.top), int32(hidden)
			add(r)
		}
		for k := g.count - shown.bottom; k < g.count; k++ {
			context(int32(g.oldStart+k), int32(g.newStart+k))
		}
	}
	for hi := range f.hunks {
		addGap(f.gapBefore(hi))
		lines := f.hunks[hi].Lines
		h := int32(hi)
		if !split {
			for li := range lines {
				line(h, int32(li), -1)
			}
			continue
		}
		for li := 0; li < len(lines); {
			if lines[li].Kind == ' ' {
				line(h, int32(li), int32(li))
				li++
				continue
			}
			start := li
			for li < len(lines) && lines[li].Kind == '-' {
				li++
			}
			dels := li - start
			addStart := li
			for li < len(lines) && lines[li].Kind == '+' {
				li++
			}
			adds := li - addStart
			for k := range max(dels, adds) {
				a, b := int32(-1), int32(-1)
				if k < dels {
					a = int32(start + k)
				}
				if k < adds {
					b = int32(addStart + k)
				}
				line(h, a, b)
			}
		}
	}
	if len(f.hunks) > 0 {
		addGap(f.gapBefore(len(f.hunks)))
	}
	end()
	return rows
}

// The notes of a file whose lines do not show.
const (
	noteBinary int32 = iota
	noteBig
	noteNoText
	noteLFS
	noteUnreadable
)

// rowKey identifies a row across rebuilds, so that the list keeps its place
// as rows come and go above it.
type rowKey struct {
	kind     rowKind
	file     int
	group    int
	old, new int32
	gap      int32
	comment  *comment
	thread   int64
}

// key returns the identity of a row.
func (rv *review) key(r *reviewRow) rowKey {
	k := rowKey{kind: r.kind, file: r.file, group: r.group, gap: r.gap, comment: r.comment}
	if r.thread != nil {
		k.thread = r.thread.RootID
	}
	switch r.kind {
	case rowCommit, rowPills, rowNote:
		k.file, k.group = 0, 0
	case rowGroup:
		k.file = 0
	case rowFileNote:
		k.gap = r.count
	case rowLine:
		f := rv.files[r.file]
		if r.hunk < 0 {
			k.old, k.new = r.old, r.new
			break
		}
		if l := f.lineAt(r.hunk, r.a); l != nil {
			k.old, k.new = int32(l.Old), int32(l.New)
		}
		if l := f.lineAt(r.hunk, r.b); l != nil {
			if k.old == 0 {
				k.old = int32(l.Old)
			}
			k.new = int32(l.New)
		}
		if k.old == 0 && k.new == 0 {
			k.gap = r.hunk<<16 | r.a
		}
	}
	return k
}

// tabWidth is the columns a tab takes.
const tabWidth = 4

// mark is a range of a line drawn with a background: a changed word.
type mark struct {
	worddiff.Range
	color ui.Color
}

// styleCode styles a line of code: the colors of its tokens, and the
// backgrounds of its marks, later marks over earlier ones. Tabs become
// spaces up to the next tab stop.
func styleCode(text string, segs []highlight.Seg, marks []mark, pal *diffPalette) []ui.Span {
	if text == "" {
		return []ui.Span{{Text: " "}}
	}
	// The places where the style may change.
	cuts := []int{0, len(text)}
	for _, s := range segs {
		cuts = append(cuts, int(s.Start), int(s.End))
	}
	for _, m := range marks {
		cuts = append(cuts, m.Start, m.End)
	}
	slices.Sort(cuts)
	cuts = slices.Compact(cuts)

	var spans []ui.Span
	col := 0
	si := 0
	for i := 0; i+1 < len(cuts); i++ {
		start, end := cuts[i], cuts[i+1]
		if start >= len(text) || start < 0 {
			break
		}
		end = min(end, len(text))
		if end <= start {
			continue
		}
		for si < len(segs) && int(segs[si].End) <= start {
			si++
		}
		color := pal.code
		if si < len(segs) && int(segs[si].Start) <= start {
			color = pal.syntax[segs[si].Class]
		}
		var bg ui.Color
		for _, m := range marks {
			if m.Start <= start && end <= m.End {
				bg = m.color
			}
		}
		part := text[start:end]
		if strings.IndexByte(part, '\t') >= 0 {
			part, col = expandTabs(part, col)
		} else {
			col += len(part)
		}
		if n := len(spans); n > 0 && spans[n-1].Color == color && spans[n-1].Background == bg {
			spans[n-1].Text += part
			continue
		}
		spans = append(spans, ui.Span{Text: part, Color: color, Background: bg})
	}
	return spans
}

// expandTabs replaces the tabs of s, which starts at column col, and
// returns the column after it.
func expandTabs(s string, col int) (string, int) {
	var b strings.Builder
	for _, r := range s {
		if r == '\t' {
			n := tabWidth - col%tabWidth
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		col++
	}
	return b.String(), col
}

// columns returns how many columns a line takes, tabs expanded.
func columns(s string) int {
	col := 0
	for _, r := range s {
		if r == '\t' {
			col += tabWidth - col%tabWidth
		} else {
			col++
		}
	}
	return col
}

// threadsOf returns the review threads of a file.
func (rv *review) threadsOf(path string) []*github.Thread {
	var out []*github.Thread
	for _, t := range rv.threads {
		if t.Path == path {
			out = append(out, t)
		}
	}
	return out
}

// appendUnplaced adds the threads of a file whose lines do not show:
// outdated, or in lines hidden.
func (rv *review) appendUnplaced(rows []reviewRow, fi int, f *reviewFile, group int) []reviewRow {
	for _, t := range rv.threadsOf(f.path) {
		if !rv.placed[t] {
			rv.placed[t] = true
			rows = append(rows, reviewRow{kind: rowThread, file: fi, group: group, a: -1, b: -1, thread: t})
		}
	}
	return rows
}

// appendComments adds the threads and the drafts on a line.
func (rv *review) appendComments(rows []reviewRow, fi int, f *reviewFile, group int, l *git.PatchLine) []reviewRow {
	if !rv.pr {
		return rows
	}
	on := func(sd diffSide, line int) bool {
		// Unchanged lines take comments on either side.
		return (sd == sideOld && l.Kind != '+' && line == l.Old) || (sd == sideNew && l.Kind != '-' && line == l.New)
	}
	for _, t := range rv.threadsOf(f.path) {
		if t.Outdated || t.Line == 0 || rv.placed[t] {
			continue
		}
		sd := sideNew
		if t.Side == github.Left {
			sd = sideOld
		}
		if on(sd, t.Line) {
			rv.placed[t] = true
			rows = append(rows, reviewRow{kind: rowThread, file: fi, group: group, a: -1, b: -1, thread: t})
		}
	}
	for _, cm := range rv.comments {
		if cm.path == f.path && on(cm.side, cm.line) {
			rows = append(rows, reviewRow{kind: rowComment, file: fi, group: group, a: -1, b: -1, comment: cm})
		}
	}
	return rows
}
