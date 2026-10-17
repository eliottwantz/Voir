package main

import (
	"fmt"
	"math"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/eliott/myfork/internal/git"
)

// The diff of Local Changes is drawn here, row by row, rather than by
// MyGo's diff viewer, which does not tell where its lines are: this one has
// to put a box around the hunk under the pointer, and let lines be selected
// by dragging over them, to stage or discard only those.

const patchRowH = 20

var selColor = ui.RGB(120, 100, 210)

// pcell is a line of a patch in a row.
type pcell struct {
	ok   bool
	l    git.PatchLine
	id   git.LineID
	hl   [2]int // the bytes that differ from the line it replaces
	toks []editor.Token
}

func (c *pcell) changed() bool { return c.ok && c.l.Changed() }

// prow is a row of the diff: a hunk's header, or a line. A split row holds
// the old line on the left and the new one on the right; a unified row, one
// line, on the left.
type prow struct {
	hunk        int
	header      string
	left, right pcell
}

func (r *prow) isHeader() bool { return r.header != "" }

// patchView is the diff of the file chosen in Local Changes, and the lines
// selected in it.
type patchView struct {
	patch  string
	path   string
	split  bool
	parsed *git.Patch
	rows   []prow
	// hunkRows is, for each hunk, its header's row and its last row.
	hunkRows [][2]int
	err      string

	// anchor and head are the rows selected, from where the pointer went
	// down to where it is, -1 for none.
	anchor, head int
	dragging     bool
	scrollTo     int

	list ui.ListState
	// hov is the hunk under the pointer, -1 for none.
	hov int
	// rowAnchor is a row drawn, and its top in the window, from which the
	// row at a point is found.
	rowAnchor int
	rowTop    float32
	viewTop   float32
}

// sync makes the rows of a patch, and drops the selection when the patch or
// the layout changed under it.
func (pv *patchView) sync(patch, file string, split bool) {
	if pv.patch == patch && pv.path == file && pv.split == split && pv.parsed != nil {
		return
	}
	pv.patch, pv.path, pv.split, pv.err = patch, file, split, ""
	pv.anchor, pv.head, pv.dragging, pv.scrollTo, pv.hov = -1, -1, false, -1, -1
	pv.parsed, pv.rows, pv.hunkRows = nil, nil, nil
	p, err := git.ParsePatchLines(patch)
	if err != nil {
		pv.err = err.Error()
		return
	}
	pv.parsed = p
	_, raw := git.SplitPatch(patch)
	var tok editor.Tokenizer
	if lang := editor.LanguageFor(path.Base(file)); lang != nil {
		tok = lang.Tokenizer
	}
	for hi, h := range p.Hunks {
		header, _, _ := strings.Cut(raw[hi], "\n")
		start := len(pv.rows)
		pv.rows = append(pv.rows, prow{hunk: hi, header: header})
		var oldSt, newSt uint64
		cell := func(li int) pcell {
			l := h.Lines[li]
			c := pcell{ok: true, l: l, id: git.LineID{Hunk: hi, Line: li}}
			if tok != nil {
				switch l.Kind {
				case '-':
					c.toks, oldSt = tok.Tokenize(l.Text, oldSt, nil)
				case '+':
					c.toks, newSt = tok.Tokenize(l.Text, newSt, nil)
				default:
					c.toks, newSt = tok.Tokenize(l.Text, newSt, nil)
					oldSt = newSt
				}
			}
			return c
		}
		for li := 0; li < len(h.Lines); {
			switch h.Lines[li].Kind {
			case '\\':
				li++
			case ' ':
				c := cell(li)
				pv.rows = append(pv.rows, prow{hunk: hi, left: c, right: c})
				li++
			default:
				// A change: the lines deleted, then those added.
				var dels, adds []pcell
				for ; li < len(h.Lines) && h.Lines[li].Kind == '-'; li++ {
					dels = append(dels, cell(li))
				}
				for ; li < len(h.Lines) && h.Lines[li].Kind == '+'; li++ {
					adds = append(adds, cell(li))
				}
				for i := 0; i < min(len(dels), len(adds)); i++ {
					dels[i].hl, adds[i].hl = wordChange(dels[i].l.Text, adds[i].l.Text)
				}
				if split {
					for i := 0; i < max(len(dels), len(adds)); i++ {
						var r prow
						r.hunk = hi
						if i < len(dels) {
							r.left = dels[i]
						}
						if i < len(adds) {
							r.right = adds[i]
						}
						pv.rows = append(pv.rows, r)
					}
				} else {
					for _, c := range append(dels, adds...) {
						pv.rows = append(pv.rows, prow{hunk: hi, left: c})
					}
				}
			}
		}
		pv.hunkRows = append(pv.hunkRows, [2]int{start, len(pv.rows) - 1})
	}
}

// wordChange finds what differs between a deleted line and the one that
// replaces it, as what is left between their common start and end.
func wordChange(a, b string) (ha, hb [2]int) {
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	for p > 0 && (!utf8.RuneStart(a[min(p, len(a)-1)]) && p < len(a) || !utf8.RuneStart(b[min(p, len(b)-1)]) && p < len(b)) {
		p--
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	for s > 0 && (len(a)-s < len(a) && !utf8.RuneStart(a[len(a)-s]) || len(b)-s < len(b) && !utf8.RuneStart(b[len(b)-s])) {
		s--
	}
	if p+s == 0 || p+s >= len(a) && p+s >= len(b) {
		return
	}
	return [2]int{p, len(a) - s}, [2]int{p, len(b) - s}
}

// selection returns the rows selected, first to last, and whether any are.
func (pv *patchView) selection() (lo, hi int, ok bool) {
	if pv.anchor < 0 || pv.head < 0 || len(pv.rows) == 0 {
		return 0, 0, false
	}
	lo, hi = min(pv.anchor, pv.head), max(pv.anchor, pv.head)
	return lo, min(hi, len(pv.rows)-1), true
}

// selected returns the changed lines of the rows selected.
func (pv *patchView) selected() map[git.LineID]bool {
	out := map[git.LineID]bool{}
	if lo, hi, ok := pv.selection(); ok {
		for _, r := range pv.rows[lo : hi+1] {
			for _, c := range []pcell{r.left, r.right} {
				if c.changed() {
					out[c.id] = true
				}
			}
		}
	}
	return out
}

// hunkLines returns the changed lines of hunk h.
func (pv *patchView) hunkLines(h int) map[git.LineID]bool {
	out := map[git.LineID]bool{}
	if h < 0 || h >= len(pv.hunkRows) {
		return out
	}
	for _, r := range pv.rows[pv.hunkRows[h][0] : pv.hunkRows[h][1]+1] {
		for _, c := range []pcell{r.left, r.right} {
			if c.changed() {
				out[c.id] = true
			}
		}
	}
	return out
}

// counts says how many lines the selection adds and deletes.
func (pv *patchView) counts(sel map[git.LineID]bool) (added, removed int) {
	for id := range sel {
		switch pv.parsed.Hunks[id.Hunk].Lines[id.Line].Kind {
		case '+':
			added++
		case '-':
			removed++
		}
	}
	return
}

// rowAt returns the row at a y of the window, clamped to the rows.
func (pv *patchView) rowAt(y float32) int {
	row := pv.rowAnchor + int(math.Floor(float64((y-pv.rowTop)/patchRowH)))
	return max(0, min(row, len(pv.rows)-1))
}

// input takes the pointer going down, over and up on the diff: down starts a
// selection of rows (Shift extends it), dragging grows it.
func (pv *patchView) input(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button != 0 || len(pv.rows) == 0 {
			return false
		}
		row := pv.rowAt(pv.viewTop + ev.Y)
		if pv.rows[row].isHeader() {
			return false
		}
		if ev.Mods&ui.Shift != 0 && pv.anchor >= 0 {
			pv.head = row
		} else {
			pv.anchor, pv.head = row, row
		}
		pv.dragging = true
		return true
	case ui.InputPointerMove:
		if !pv.dragging || ev.Button != 0 {
			return false
		}
		row := pv.rowAt(pv.viewTop + ev.Y)
		if row != pv.head {
			pv.head = row
			pv.scrollTo = row
		}
		return true
	case ui.InputPointerUp:
		if !pv.dragging {
			return false
		}
		pv.dragging = false
		// A click on an unchanged line, or again on the line alone,
		// takes the selection away.
		if pv.anchor == pv.head {
			if r := pv.rows[pv.head]; !r.left.changed() && !r.right.changed() {
				pv.anchor, pv.head = -1, -1
			}
		}
		return true
	}
	return false
}

// patchArea is the right side of Local Changes for a file: its diff, whose
// hunks and lines are staged, unstaged or discarded from the diff itself.
func (tab *repoTab) patchArea(c *ui.Context, e git.StatusEntry, staged bool) {
	t := c.Theme()
	p := &tab.work
	switch {
	case p.key == "":
		ui.Text(c, "Select a file to see its changes").TextColor(t.TextMuted).Padding(20).TextAlign(ui.Center).FillWidth()
		return
	case p.loading:
		ui.Row(c).Padding(20).Justify(ui.Center).FillWidth().Children(func() { ui.Spinner(c).Label("Loading") })
		return
	case p.err != "":
		ui.Text(c, p.err).TextColor(t.Danger).Padding(20).Selectable()
		return
	case p.binary:
		ui.Text(c, "Binary file: "+p.path).TextColor(t.TextMuted).Padding(20).TextAlign(ui.Center).FillWidth()
		return
	case p.empty:
		ui.Text(c, "No textual changes").TextColor(t.TextMuted).Padding(20).TextAlign(ui.Center).FillWidth()
		return
	}
	pv := &tab.pview
	pv.sync(p.patch, e.Path, tab.app.store.settings().DiffLayout != "unified")
	if pv.err != "" {
		ui.Text(c, pv.err).TextColor(t.Danger).Padding(20)
		return
	}
	if git.PatchHasLFSPointer(p.patch) {
		ui.Row(c).Gap(8).Padding(4, 12).Shrink(0).AlignItems(ui.Center).Background(t.Accent.Alpha(0.1)).Children(func() {
			ui.Icon(c, iconCloud).FontSize(13).TextColor(t.Accent)
			ui.Text(c, "This file is stored with Git LFS: the lines are its pointer, not its content.").FontSize(12)
		})
	}

	// What the box is around, and what its buttons act on.
	sel, haveSel := pv.selected(), false
	lo, hi := 0, -1
	if l, h, ok := pv.selection(); ok && len(sel) > 0 {
		lo, hi, haveSel = l, h, true
	} else if pv.hov >= 0 && pv.hov < len(pv.hunkRows) {
		lo, hi = pv.hunkRows[pv.hov][0]+1, pv.hunkRows[pv.hov][1]
		sel = pv.hunkLines(pv.hov)
	}
	first, _ := pv.list.Visible()
	buttonsAt := max(lo, first)
	if buttonsAt < len(pv.rows) && pv.rows[buttonsAt].isHeader() && buttonsAt+1 <= hi {
		buttonsAt++
	}
	actions := func() {
		tab.lineButtons(c, e, staged, sel, haveSel)
	}

	ct := editor.DefaultTheme(t)
	hov := -1
	frame := ui.Column(c).Fill().MinWidth(0).MinHeight(0)
	frame.Draw(func(pt *ui.Painter, rc ui.Rect) { pv.viewTop = rc.Y }).HandleInput(pv.input)
	frame.Children(func() {
		if pv.scrollTo >= 0 {
			pv.list.ScrollIntoView(pv.scrollTo)
			pv.scrollTo = -1
		}
		pv.list.Label = func(i int) string {
			if i >= 0 && i < len(pv.rows) {
				return pv.rows[i].label()
			}
			return ""
		}
		pv.list.Key = func(i int) any { return i }
		list := ui.List(c, &pv.list, len(pv.rows), func(i int) {
			if i >= len(pv.rows) {
				return
			}
			r := &pv.rows[i]
			row := ui.Row(c).Height(patchRowH).FillWidth().AlignItems(ui.Center).Label(r.label())
			if r.isHeader() {
				row.Background(t.Accent.Alpha(0.08))
			}
			inSel := haveSel && i >= lo && i <= hi
			if row.Hovered() {
				hov = r.hunk
			}
			tab.paintPatchRow(row, r, inSel, t)
			recordRow := i
			row.Draw(func(pt *ui.Painter, rc ui.Rect) { pv.rowAnchor, pv.rowTop = recordRow, rc.Y })
			boxed := hi >= lo && i >= lo && i <= hi
			top, bottom := i == lo, i == hi
			row.DrawOver(func(pt *ui.Painter, rc ui.Rect) {
				if boxed {
					strokeBoxRow(pt, rc, top, bottom)
				}
			})
			row.Children(func() {
				tab.patchCells(c, r, ct)
				if boxed && i == buttonsAt {
					ui.Row(c).Absolute().Right(16).Top(1).Gap(6).AlignItems(ui.Center).Children(actions)
				}
			})
		})
		list.Grow(1).MinHeight(0)
	})
	if hov != pv.hov && !pv.dragging {
		pv.hov = hov
		c.Invalidate()
	}
}

func (r *prow) label() string {
	if r.isHeader() {
		return r.header
	}
	switch {
	case r.left.ok && r.right.ok && r.left.id != r.right.id:
		return fmt.Sprintf("- %s + %s", r.left.l.Text, r.right.l.Text)
	case r.left.ok:
		return fmt.Sprintf("%c %s", r.left.l.Kind, r.left.l.Text)
	case r.right.ok:
		return fmt.Sprintf("%c %s", r.right.l.Kind, r.right.l.Text)
	}
	return ""
}

// strokeBoxRow draws the part of a box that falls on a row: its sides, and
// its top or bottom on the first and last rows.
func strokeBoxRow(pt *ui.Painter, r ui.Rect, top, bottom bool) {
	const w = 1.5
	pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: w, H: r.H}, hunkBoxColor, 0)
	pt.Fill(ui.Rect{X: r.X + r.W - w, Y: r.Y, W: w, H: r.H}, hunkBoxColor, 0)
	if top {
		pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: w}, hunkBoxColor, 0)
	}
	if bottom {
		pt.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - w, W: r.W, H: w}, hunkBoxColor, 0)
	}
}

var hunkBoxColor = ui.RGB(230, 170, 20)

// paintPatchRow tints a row by what its lines do.
func (tab *repoTab) paintPatchRow(row ui.Element, r *prow, selected bool, t *ui.Theme) {
	if r.isHeader() {
		return
	}
	tint := func(c pcell) ui.Color {
		switch {
		case c.ok && c.l.Kind == '+':
			return t.Success.Alpha(0.14)
		case c.ok && c.l.Kind == '-':
			return t.Danger.Alpha(0.14)
		}
		return ui.Color{}
	}
	if selected {
		row.Background(selColor.Alpha(0.3))
		return
	}
	if tab.pview.split {
		return // each side tints its own cell
	}
	if bg := tint(r.left); bg.A != 0 {
		row.Background(bg)
	}
}

// patchCells builds the numbers and the code of a row.
func (tab *repoTab) patchCells(c *ui.Context, r *prow, ct *editor.Theme) {
	t := c.Theme()
	if r.isHeader() {
		ui.Text(c, r.header).Font(mono).FontSize(11.5).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0).Padding(0, 12)
		return
	}
	num := func(n int) {
		s := ""
		if n > 0 {
			s = fmt.Sprint(n)
		}
		ui.Text(c, s).Font(mono).FontSize(11).Width(40).TextAlign(ui.End).TextColor(t.TextMuted).Opacity(0.7).Shrink(0).Padding(0, 4, 0, 0)
	}
	code := func(cl pcell, cellBg bool) {
		box := ui.Row(c).Grow(1).MinWidth(0).Height(patchRowH).AlignItems(ui.Center).Padding(0, 6)
		if cellBg {
			switch {
			case cl.ok && cl.l.Kind == '+':
				box.Background(t.Success.Alpha(0.14))
			case cl.ok && cl.l.Kind == '-':
				box.Background(t.Danger.Alpha(0.14))
			}
		}
		box.Children(func() {
			if !cl.ok {
				return
			}
			hl := t.Success.Alpha(0.32)
			if cl.l.Kind == '-' {
				hl = t.Danger.Alpha(0.32)
			}
			ui.RichText(c, codeSpans(cl.l.Text, cl.toks, cl.hl, ct, t, hl)...).Font(mono).FontSize(12).SingleLine().MinWidth(0)
		})
	}
	if tab.pview.split {
		num(r.left.l.Old)
		code(r.left, true)
		num(r.right.l.New)
		code(r.right, true)
		return
	}
	num(r.left.l.Old)
	num(r.left.l.New)
	code(r.left, false)
}

// codeSpans cuts a line into runs of one color, at its tokens and at the
// bytes that changed, which get a background.
func codeSpans(text string, toks []editor.Token, hl [2]int, ct *editor.Theme, t *ui.Theme, hlBg ui.Color) []ui.Span {
	text = strings.TrimRight(text, "\r")
	cuts := []int{0, len(text)}
	for _, tk := range toks {
		cuts = append(cuts, tk.End)
	}
	if hl[1] > hl[0] {
		cuts = append(cuts, hl[0], hl[1])
	}
	slices.Sort(cuts)
	cuts = slices.Compact(cuts)
	var spans []ui.Span
	ti := 0
	for i := 0; i+1 < len(cuts); i++ {
		a, b := cuts[i], cuts[i+1]
		if a < 0 || b > len(text) || a >= b {
			continue
		}
		for ti < len(toks) && toks[ti].End <= a {
			ti++
		}
		sp := ui.Span{Text: strings.ReplaceAll(text[a:b], "\t", "    ")}
		if ti < len(toks) && toks[ti].Kind != editor.TokenText {
			if col := ct.TokenColor(toks[ti].Kind); col.A != 0 {
				sp.Color = col
			}
		}
		if hl[1] > hl[0] && a >= hl[0] && b <= hl[1] {
			sp.Background = hlBg
		}
		spans = append(spans, sp)
	}
	if len(spans) == 0 {
		spans = append(spans, ui.Span{Text: " "})
	}
	return spans
}

// lineButtons are the buttons of the box: what they act on is the lines
// selected, or else the hunk under the pointer.
func (tab *repoTab) lineButtons(c *ui.Context, e git.StatusEntry, staged bool, sel map[git.LineID]bool, selection bool) {
	t := c.Theme()
	idle := tab.busy == ""
	patch := tab.work.patch
	file := e.Path
	untracked := e.Untracked
	noun := "Hunk"
	if selection {
		noun = "Lines"
	}
	mini := func(label string, primary bool) ui.Element {
		b := ui.ButtonBase(c).Height(18).Padding(0, 8).Radius(5).Center().Label(label).Disabled(!idle || len(sel) == 0).
			HandleInput(func(ui.InputEvent) bool { return false })
		if primary {
			b.Background(t.Accent).TextColor(t.AccentText)
		} else {
			b.Background(t.Surface).Border(1, hairline)
		}
		b.Children(func() { ui.Text(c, label).FontSize(11.5).SingleLine() })
		return b
	}
	cloned := cloneSel(sel)
	if staged {
		if mini("Unstage "+noun, true).Clicked() {
			tab.do("Unstage lines", func(r *git.Repo) error { return r.UnstageLines(patch, cloned) })
		}
		return
	}
	// A new file has no lines in a patch until git knows of it: it is added
	// with no content, then its lines are staged or discarded.
	prepare := func(r *git.Repo) (string, error) {
		if !untracked {
			return patch, nil
		}
		if err := r.IntentToAdd(file); err != nil {
			return "", err
		}
		return r.WorkDiff(file, git.DiffOptions{Context: 3})
	}
	if mini("Discard "+noun+"…", false).Clicked() {
		added, removed := tab.pview.counts(cloned)
		tab.app.confirm("Discard the "+strings.ToLower(noun)+"?", fmt.Sprintf("%s in “%s” will be lost.", plural(added+removed, "change", "changes"), file), "Discard", func() error {
			p, err := prepare(tab.repo)
			if err != nil {
				return err
			}
			return tab.repo.DiscardLines(p, cloned)
		}, nil)
	}
	if mini("Stage "+noun, true).Clicked() {
		tab.do("Stage lines", func(r *git.Repo) error {
			p, err := prepare(r)
			if err != nil {
				return err
			}
			return r.StageLines(p, cloned)
		})
	}
}

func cloneSel(m map[git.LineID]bool) map[git.LineID]bool {
	out := make(map[git.LineID]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
