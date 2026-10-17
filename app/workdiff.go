package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

// The diff of Local Changes is the surface of a commit's, with a card for each
// file, those not staged then those staged. Over its lines goes a box, around
// the rows the text selected reaches or else the hunk under the pointer, with
// the buttons to stage or discard what is in it.

var selColor = ui.RGB(120, 100, 210)

var hunkBoxColor = ui.RGB(230, 170, 20)

// workDiff is what is chosen and under the pointer in the diff of Local
// Changes, by the index of rows of the surface.
type workDiff struct {
	// spans is, for each file and each of its hunks, its first and last row,
	// -1 when not shown.
	spans [][][2]int
	// hov is the hunk under the pointer and hovFile its file, -1 for none,
	// and hovNow and hovNowFile the ones the frame being built found.
	hov, hovFile       int
	hovNow, hovNowFile int

	// What the frame being built draws: the box, around the rows chosen or
	// the hunk, the changed lines it acts on, and the row with its buttons.
	boxLo, boxHi int
	boxFile      int
	sel          map[git.LineID]bool
	selection    bool
	buttonsAt    int
}

func (w *workDiff) reset() {
	w.hov, w.hovFile, w.hovNow, w.hovNowFile = -1, -1, -1, -1
	w.boxLo, w.boxHi, w.boxFile, w.buttonsAt = -1, -1, -1, -1
}

// index finds where the hunks are in the rows built, and drops what was
// chosen, which was by the rows of before.
func (w *workDiff) index(rv *review) {
	w.reset()
	w.spans = nil
	w.spans = make([][][2]int, len(rv.files))
	for fi, f := range rv.files {
		w.spans[fi] = make([][2]int, len(f.hunks))
		for i := range w.spans[fi] {
			w.spans[fi][i] = [2]int{-1, -1}
		}
	}
	for i, r := range rv.rows {
		if r.kind == rowLine && r.hunk >= 0 && int(r.hunk) < len(w.spans[r.file]) {
			sp := &w.spans[r.file][r.hunk]
			if sp[0] < 0 {
				sp[0] = i
			}
			sp[1] = i
		}
	}
}

// changedLines adds the lines that add or delete in a row to a set.
func (rv *review) changedLines(out map[git.LineID]bool, r reviewRow) {
	if r.kind != rowLine || r.hunk < 0 {
		return
	}
	f := rv.files[r.file]
	for _, idx := range []int32{r.a, r.b} {
		if l := f.lineAt(r.hunk, idx); l != nil && l.Changed() {
			out[git.LineID{Hunk: int(r.hunk), Line: f.hunks[r.hunk].orig[idx]}] = true
		}
	}
}

// linesOf returns the changed lines of a file in rows lo to hi.
func (rv *review) linesOf(file, lo, hi int) map[git.LineID]bool {
	out := map[git.LineID]bool{}
	for _, r := range rv.rows[lo : hi+1] {
		if r.file == file {
			rv.changedLines(out, r)
		}
	}
	return out
}

// prepare works out, before the rows are built, what the box is around and
// what its buttons act on: the lines chosen, or else the hunk under the
// pointer.
func (w *workDiff) prepare(rv *review) {
	w.hovNow, w.hovNowFile = -1, -1
	w.boxLo, w.boxHi, w.boxFile, w.buttonsAt, w.sel, w.selection = -1, -1, -1, -1, nil, false
	if s := &rv.sel; s.rowsOn && s.rowHi < len(rv.rows) {
		if sel := rv.linesOf(s.file, s.rowLo, s.rowHi); len(sel) > 0 {
			w.boxLo, w.boxHi, w.boxFile, w.sel, w.selection = s.rowLo, s.rowHi, s.file, sel, true
		}
	}
	if w.boxLo < 0 && w.hovFile >= 0 && w.hovFile < len(w.spans) && w.hov >= 0 && w.hov < len(w.spans[w.hovFile]) && w.spans[w.hovFile][w.hov][0] >= 0 {
		sp := w.spans[w.hovFile][w.hov]
		w.boxLo, w.boxHi, w.boxFile = sp[0], sp[1], w.hovFile
		w.sel = rv.linesOf(w.boxFile, w.boxLo, w.boxHi)
	}
	if w.boxLo < 0 {
		return
	}
	// The buttons stay in view with the first row of the box that is.
	first, _ := rv.list.Visible()
	w.buttonsAt = w.boxLo
	for i := max(w.boxLo, first); i <= w.boxHi; i++ {
		if r := rv.rows[i]; r.kind == rowLine && r.hunk >= 0 && r.file == w.boxFile {
			w.buttonsAt = i
			break
		}
	}
}

// rowLabel is what a line row says to assistive technology: its lines, each
// with the sign of what it does.
func rowLabel(f *reviewFile, r reviewRow) string {
	a, b := f.lineAt(r.hunk, r.a), f.lineAt(r.hunk, r.b)
	switch {
	case a != nil && b != nil && r.a != r.b:
		return fmt.Sprintf("- %s + %s", a.Text, b.Text)
	case a != nil:
		return fmt.Sprintf("%c %s", a.Kind, a.Text)
	case b != nil:
		return fmt.Sprintf("%c %s", b.Kind, b.Text)
	}
	return ""
}

// workLineRow gives a line row of the diff of Local Changes what lets it be
// chosen: its place, its label, the box around it and the tint of the choice.
func (tab *repoTab) workLineRow(rv *review, f *reviewFile, r reviewRow, i int, e ui.Element) {
	w := &rv.w
	if r.hunk < 0 {
		return
	}
	e.Label(rowLabel(f, r))
	if e.Hovered() {
		w.hovNow, w.hovNowFile = int(r.hunk), r.file
	}
	boxed := w.boxLo >= 0 && i >= w.boxLo && i <= w.boxHi && r.file == w.boxFile
	top, bottom := i == w.boxLo, i == w.boxHi
	e.DrawOver(func(pt *ui.Painter, rc ui.Rect) {
		if boxed {
			strokeBoxRow(pt, rc, top, bottom)
		}
	})
}

// workActions adds the buttons of the box to the row that has them.
func (tab *repoTab) workActions(c *ui.Context, rv *review, i int) {
	w := &rv.w
	if w.boxLo < 0 || i != w.buttonsAt {
		return
	}
	f := rv.files[rv.rows[i].file]
	ui.Row(c).Absolute().Right(16).Top(1).Gap(6).AlignItems(ui.Center).Children(func() {
		tab.lineButtons(c, f, w.sel, w.selection)
	})
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

// workSurface is the diff of the files of Local Changes, whose hunks and
// lines are staged, unstaged or discarded from the diff itself.
func (tab *repoTab) workSurface(c *ui.Context) {
	rv := tab.wrv
	if rv == nil || (rv.loading && len(rv.files) == 0) {
		ui.Row(c).Padding(20).Justify(ui.Center).FillWidth().Children(func() { ui.Spinner(c).Label("Loading") })
		return
	}
	if split := tab.app.store.settings().DiffLayout != "unified"; split != rv.split {
		rv.split, rv.dirty = split, true
	}
	if rv.dirty {
		rv.build()
	}
	w := &rv.w
	w.prepare(rv)
	tab.selectSurface(c, rv)
	if w.hovNow != w.hov || w.hovNowFile != w.hovFile {
		w.hov, w.hovFile = w.hovNow, w.hovNowFile
		c.Invalidate()
	}
}

// lineButtons are the buttons of the box: what they act on is the lines
// chosen, or else the hunk under the pointer.
func (tab *repoTab) lineButtons(c *ui.Context, f *reviewFile, sel map[git.LineID]bool, selection bool) {
	t := c.Theme()
	idle := tab.busy == ""
	patch, staged := f.patch, f.staged
	file := f.path
	untracked := f.entry.Untracked
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
		added, removed := countLines(patch, cloned)
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

// countLines says how many of the lines of a patch chosen add and delete.
func countLines(patch string, sel map[git.LineID]bool) (added, removed int) {
	p, err := git.ParsePatchLines(patch)
	if err != nil {
		return 0, 0
	}
	for id := range sel {
		switch p.Hunks[id.Hunk].Lines[id.Line].Kind {
		case '+':
			added++
		case '-':
			removed++
		}
	}
	return
}

func cloneSel(m map[git.LineID]bool) map[git.LineID]bool {
	out := make(map[git.LineID]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
