package app

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/eliott/myfork/internal/git"
	"github.com/eliott/myfork/internal/github"
)

// The files of a change are shown by MyGo's diff viewer, editor.DiffView: a
// commit's and Local Changes' one at a time, scrolling by itself, a pull
// request's in a list, each as high as its lines. A file read whole (the
// commit's, the pull request's once its commits are here) is diffed by the
// viewer, which folds the unchanged lines and expands them; the others show
// their patch.

// diffSide is a side of a diff, as comments name it.
type diffSide uint8

const (
	sideOld diffSide = iota
	sideNew
)

func (s diffSide) editor() editor.DiffSide { return editor.DiffSide(s) }

// maxWhole is the size of file beyond which it is not read whole.
const maxWhole = 1 << 20

// annotationHeight is the height a comment or a thread is given, until it
// has been drawn and measured.
const annotationHeight = 150

// patchLines maps the lines of a viewer made of a patch to the file's and to
// the patch's: the text of each side holds the lines of the hunks only.
type patchLines struct {
	side [2][]patchLine // by side, then by line of the side's text
	at   [2]map[int]int // by side, the line of the text of a line of the file
}

// patchLine is a line of the text of a side.
type patchLine struct {
	file    int // the line's number in the file, from 1
	id      git.LineID
	changed bool
}

func newPatchLines(patch string) *patchLines {
	pl := &patchLines{at: [2]map[int]int{{}, {}}}
	p, err := git.ParsePatchLines(patch)
	if err != nil {
		return pl
	}
	for hi, h := range p.Hunks {
		for li, l := range h.Lines {
			if l.Kind == '\\' {
				continue
			}
			id := git.LineID{Hunk: hi, Line: li}
			if l.Kind != '+' {
				pl.at[sideOld][l.Old] = len(pl.side[sideOld])
				pl.side[sideOld] = append(pl.side[sideOld], patchLine{file: l.Old, id: id, changed: l.Kind == '-'})
			}
			if l.Kind != '-' {
				pl.at[sideNew][l.New] = len(pl.side[sideNew])
				pl.side[sideNew] = append(pl.side[sideNew], patchLine{file: l.New, id: id, changed: l.Kind == '+'})
			}
		}
	}
	return pl
}

// fileLine is the number, from 1, in the file of a line, from 0, of the text
// of a side, 0 for none.
func (f *reviewFile) fileLine(side editor.DiffSide, line int) int {
	if f.lines == nil {
		return line + 1
	}
	ls := f.lines.side[side]
	if line < 0 || line >= len(ls) {
		return 0
	}
	return ls[line].file
}

// textLine is the line, from 0, of the text of a side that a line of the
// file, from 1, is.
func (f *reviewFile) textLine(side diffSide, fileLine int) (int, bool) {
	if f.lines == nil {
		return fileLine - 1, fileLine > 0
	}
	i, ok := f.lines.at[side][fileLine]
	return i, ok
}

// changedIDs are the lines of the patch that add or delete among the lines
// from to to, exclusive, of a side's text.
func (f *reviewFile) changedIDs(out map[git.LineID]bool, side editor.DiffSide, from, to int) {
	if f.lines == nil {
		return
	}
	ls := f.lines.side[side]
	for i := max(from, 0); i < min(to, len(ls)); i++ {
		if ls[i].changed {
			out[ls[i].id] = true
		}
	}
}

// readWhole reports whether a file is shown from its two whole texts, as the
// commits' and the pull requests' that are here are.
func (tab *repoTab) readWhole(rv *review, f *reviewFile) bool {
	return !rv.work && !(rv.pr && !rv.local) && !f.binary && !f.big && f.patch != "" &&
		f.status != 'A' && f.status != 'D' && !tab.app.store.settings().IgnoreWhitespace
}

// viewerOf is the viewer of a file, made once, from its patch or its texts;
// nil while the texts are read.
func (tab *repoTab) viewerOf(rv *review, f *reviewFile) *editor.DiffState {
	whole := tab.readWhole(rv, f)
	key := fmt.Sprint(rv.hash, "|", len(f.patch), "|", whole)
	if f.edKey == key {
		return f.ed
	}
	fromPatch := func() {
		f.ed, f.edKey, f.lines = nil, key, nil
		files, err := editor.ParsePatch(f.patch)
		if err != nil || len(files) == 0 || files[0].Binary {
			return
		}
		f.ed = &editor.DiffState{}
		f.ed.SetPatch(files[0])
		f.lines = newPatchLines(f.patch)
	}
	if !whole {
		fromPatch()
		return f.ed
	}
	if f.edReading {
		return nil
	}
	f.edReading = true
	hash, base, repo, p, oldP := rv.hash, rv.base, tab.repo, f.path, f.oldPath
	tab.app.background(func() {
		var oldData, newData []byte
		var errO, errN error
		both(func() { oldData, errO = repo.ReadFile(base, oldP) }, func() { newData, errN = repo.ReadFile(hash, p) })
		ok := errO == nil && errN == nil && len(oldData) <= maxWhole && len(newData) <= maxWhole &&
			bytes.IndexByte(newData, 0) < 0 && bytes.IndexByte(oldData, 0) < 0
		tab.app.update(func() {
			f.edReading = false
			if rv.hash != hash {
				return
			}
			if !ok {
				fromPatch()
				return
			}
			d := &editor.DiffState{Old: editor.NewBuffer(string(oldData)), New: editor.NewBuffer(string(newData))}
			if l := editor.LanguageFor(p); l != nil {
				d.Old.SetLanguage(l)
				d.New.SetLanguage(l)
			}
			f.ed, f.edKey, f.lines = d, key, nil
		})
	})
	return nil
}

// both runs two functions at once, and returns when both have.
func both(a, b func()) {
	done := make(chan struct{})
	go func() { a(); close(done) }()
	b()
	<-done
}

// The notes of a file whose lines do not show.
const (
	noteNone int32 = iota
	noteBinary
	noteBig
	noteNoText
	noteRenamed
	noteLFS
	noteUnreadable
)

// noteOf says why a file's lines do not show, or what to know of them.
func noteOf(f *reviewFile) int32 {
	switch {
	case f.binary:
		return noteBinary
	case f.big && !f.open:
		return noteBig
	case f.patch == "" || (f.added+f.deleted == 0 && f.status != 'R'):
		return noteNoText
	case f.added+f.deleted == 0 && !strings.Contains(f.patch, "\n@@"):
		return noteRenamed
	case git.PatchHasLFSPointer(f.patch):
		return noteLFS
	}
	return noteNone
}

// fileNote says why a file's lines do not show.
func (tab *repoTab) fileNote(c *ui.Context, f *reviewFile, kind int32) {
	t := c.Theme()
	pal := diffPaletteFor(t)
	note := func(text string, col ui.Color) {
		ui.Row(c).FillWidth().Background(pal.codeBg).Padding(18, 16).Justify(ui.Center).Children(func() {
			ui.Text(c, text).FontSize(12.5).TextColor(col)
		})
	}
	switch kind {
	case noteBinary:
		note("Binary file not shown", t.TextMuted)
	case noteNoText:
		note("No textual changes", t.TextMuted)
	case noteRenamed:
		note("File renamed without changes", t.TextMuted)
	case noteUnreadable:
		note("The patch could not be read", t.Danger)
	case noteBig:
		ui.Row(c).FillWidth().Background(pal.codeBg).Gap(10).Padding(16).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("Large diff: %s changed lines", plural(f.added+f.deleted, "line", "lines"))).TextColor(t.TextMuted)
			if ui.Button(c, "Load Diff").Key("load" + f.path).Clicked() {
				f.open = true
			}
		})
	case noteLFS:
		ui.Row(c).FillWidth().Gap(8).Padding(5, 12).AlignItems(ui.Center).Background(t.Accent.Alpha(0.1)).Children(func() {
			ui.Icon(c, iconCloud).FontSize(13).TextColor(t.Accent)
			ui.Text(c, "This file is stored with Git LFS: the lines are its pointer, not its content.").FontSize(12)
		})
	}
}

// cardRow is a row of the card of a comment or a thread, between the sides
// of the viewer it is in.
func cardRow(c *ui.Context, pal *diffPalette) ui.Element {
	return ui.Row(c).FillWidth().Background(pal.codeBg)
}

// reviewSurface is the files of a change: the one chosen alone in a commit
// and in Local Changes, else every file of a pull request, in a list.
func (tab *repoTab) reviewSurface(c *ui.Context, rv *review) {
	t := c.Theme()
	pal := diffPaletteFor(t)
	ui.Column(c).Grow(1).MinHeight(0).Background(pal.appBg).Children(func() {
		if rv.work || rv.only >= 0 {
			i := max(rv.only, 0)
			if i >= len(rv.files) {
				ui.Text(c, "No file changed").TextColor(t.TextMuted).Padding(24).TextAlign(ui.Center).FillWidth()
				return
			}
			tab.fileBody(c, rv, rv.files[i], false)
			return
		}
		if len(rv.files) == 0 {
			ui.Text(c, "No file changed").TextColor(t.TextMuted).Padding(24).TextAlign(ui.Center).FillWidth()
			return
		}
		// The groups are pills above the files, not among them, so that they
		// stay where they are while the files scroll and any group is a click
		// away.
		if rv.grouped {
			ui.Column(c).Padding(0, 16, 8).Shrink(0).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() { tab.groupPills(c, rv) })
		}
		rv.layoutItems()
		ui.List(c, &rv.list, len(rv.items), func(i int) {
			if i >= len(rv.items) {
				return
			}
			it := rv.items[i]
			switch it.kind {
			case itemGroup:
				tab.groupHeading(c, rv, it.group)
			case itemHeader:
				tab.fileHeader2(c, rv, it.file)
			default:
				tab.fileBody(c, rv, rv.files[it.file], true)
			}
		}).Grow(1).MinHeight(0).Background(pal.appBg).Padding(0, 16, 24).Label("Changes")
	})
	// The tree of files follows what is at the top of the surface.
	if rv.pr && len(rv.items) > 0 && time.Since(rv.revealedAt) > 1200*time.Millisecond {
		first, _ := rv.list.Visible()
		for first >= 0 && first < len(rv.items) && rv.items[first].kind == itemGroup {
			first++
		}
		if first >= 0 && first < len(rv.items) {
			rv.current = rv.items[first].file
		}
	}
}

// fileBody is the lines of a file: its viewer, or why it has none, and the
// threads that are not on a line shown.
func (tab *repoTab) fileBody(c *ui.Context, rv *review, f *reviewFile, fit bool) {
	t := c.Theme()
	pal := diffPaletteFor(t)
	col := ui.Column(c).FillWidth().Background(pal.codeBg)
	if fit {
		col.BorderWidth(0, 1, 1, 1).BorderColor(pal.cardBorder).Radius(0, 0, cardRadius, cardRadius).Clip()
	} else {
		col.Grow(1).MinHeight(0)
	}
	col.Children(func() {
		// A conflicted file of Local Changes keeps its buttons to settle it.
		if rv.work && f.entry.Conflict {
			ui.Row(c).FillWidth().Gap(8).Padding(8, 12).AlignItems(ui.Center).Background(pal.headerBg).Shrink(0).Children(func() {
				ui.Text(c, f.path).FontSize(12.5).SingleLine().MinWidth(0).Grow(1)
				tab.workHeaderActions(c, f)
			})
		}
		note := noteOf(f)
		if note != noteNone {
			tab.fileNote(c, f, note)
			if note != noteLFS {
				tab.looseComments(c, rv, f, nil)
				return
			}
		}
		d := tab.viewerOf(rv, f)
		if d == nil {
			if f.edReading {
				ui.Row(c).Padding(20).Justify(ui.Center).FillWidth().Children(func() { spinner(c, "Loading") })
			} else {
				tab.fileNote(c, f, noteUnreadable)
			}
			return
		}
		placed := tab.configureViewer(c, rv, f, d, fit)
		if rv.work {
			tab.selectionBar(c, f, d)
		}
		view := editor.DiffView(c, d).FillWidth()
		if !fit {
			view.Grow(1).MinHeight(0)
		}
		tab.looseComments(c, rv, f, placed)
	})
}

// configureViewer sets a viewer to the settings, the comments of the file and
// what Local Changes does with its lines, and returns what it places.
func (tab *repoTab) configureViewer(c *ui.Context, rv *review, f *reviewFile, d *editor.DiffState, fit bool) map[any]bool {
	s := tab.app.store.settings()
	d.Layout = editor.DiffSplit
	if s.DiffLayout == "unified" {
		d.Layout = editor.DiffUnified
	}
	d.Font = ui.Font{Family: tab.codeFont(), Size: tab.codeSize()}
	d.LineHeight = tab.lineHeight() / tab.codeSize()
	d.Wrap = s.WrapLines
	d.FitContent = fit
	d.ContextLines = max(s.ContextLines, 3)
	placed := map[any]bool{}
	switch {
	case rv.pr:
		tab.annotate(rv, f, d, placed)
		if rv.canComment {
			d.LineSelection = true
			d.OnGutterButton = func(r editor.DiffLineRange) {
				first, last := f.fileLine(r.Side, r.Start), f.fileLine(r.Side, r.End)
				if first > 0 && last > 0 {
					rv.addCommentRange(f.path, diffSide(r.Side), first, last)
				}
			}
		}
	case rv.work:
		d.HunkActions = true
		d.LineSelection = true
		d.RenderHunkActions = func(c *ui.Context, bar ui.Element, a *editor.DiffHunkActions) {
			ids := map[git.LineID]bool{}
			f.changedIDs(ids, editor.DiffOld, a.Hunk.OldStart, a.Hunk.OldEnd)
			f.changedIDs(ids, editor.DiffNew, a.Hunk.NewStart, a.Hunk.NewEnd)
			bar.Gap(4).Children(func() { tab.lineButtons(c, f, ids, "Hunk") })
		}
	}
	return placed
}

// annotate places the threads and the drafts of a file under their lines, and
// notes which it placed. Those on lines the viewer does not show are left for
// looseComments.
func (tab *repoTab) annotate(rv *review, f *reviewFile, d *editor.DiffState, placed map[any]bool) {
	d.Annotations = d.Annotations[:0]
	shown := func(side editor.DiffSide, line int) bool {
		if f.lines != nil {
			return true
		}
		ctx := d.ContextLines
		for _, h := range d.Hunks() {
			from, to := h.OldStart, h.OldEnd
			if side == editor.DiffNew {
				from, to = h.NewStart, h.NewEnd
			}
			if line >= from-ctx && line < to+ctx {
				return true
			}
		}
		return false
	}
	place := func(key any, sd diffSide, fileLine int) {
		line, ok := f.textLine(sd, fileLine)
		if !ok || !shown(sd.editor(), line) {
			return
		}
		h, known := rv.annH[key]
		if !known {
			h = annotationHeight
		}
		d.Annotations = append(d.Annotations, editor.DiffAnnotation{Side: sd.editor(), Line: line, Height: h, Key: key})
		placed[key] = true
	}
	for _, th := range rv.threadsOf(f.path) {
		if th.Outdated || th.Line == 0 {
			continue
		}
		sd := sideNew
		if th.Side == github.Left {
			sd = sideOld
		}
		place(th.RootID, sd, th.Line)
	}
	for _, cm := range rv.comments {
		if cm.path == f.path {
			place(cm, cm.side, cm.line)
		}
	}
	d.RenderAnnotation = func(c *ui.Context, a editor.DiffAnnotation) { tab.annotation(c, rv, a.Key) }
}

// annotation is a thread or a draft under its line. Its height is read once
// it is drawn, and the viewer leaves that room for it in the next frame.
func (tab *repoTab) annotation(c *ui.Context, rv *review, key any) {
	col := ui.Column(c).FillWidth().Shrink(0)
	col.Draw(func(p *ui.Painter, r ui.Rect) {
		if h := r.H; h > 0 && math.Abs(float64(h-rv.annH[key])) > 0.5 {
			rv.annH[key] = h
			p.AnimationFrame()
		}
	})
	col.Children(func() {
		switch k := key.(type) {
		case *comment:
			tab.commentRow(c, rv, k)
		case int64:
			if i := slices.IndexFunc(rv.threads, func(th *github.Thread) bool { return th.RootID == k }); i >= 0 {
				tab.threadRow(c, rv, rv.threads[i])
			}
		}
	})
}

// looseComments are the threads and the drafts on a file that its viewer does
// not place: outdated, or on lines it does not show.
func (tab *repoTab) looseComments(c *ui.Context, rv *review, f *reviewFile, placed map[any]bool) {
	if !rv.pr {
		return
	}
	for _, th := range rv.threadsOf(f.path) {
		if !placed[th.RootID] {
			tab.threadRow(c, rv, th)
		}
	}
	for _, cm := range rv.comments {
		if cm.path == f.path && !placed[cm] {
			tab.commentRow(c, rv, cm)
		}
	}
}

// threadsOf are the review threads on a file.
func (rv *review) threadsOf(path string) []*github.Thread {
	var out []*github.Thread
	for _, t := range rv.threads {
		if t.Path == path {
			out = append(out, t)
		}
	}
	return out
}

// selectionBar is what Local Changes does with the lines chosen in a viewer.
func (tab *repoTab) selectionBar(c *ui.Context, f *reviewFile, d *editor.DiffState) {
	r, ok := d.SelectedLines()
	if !ok {
		return
	}
	t := c.Theme()
	ids := map[git.LineID]bool{}
	f.changedIDs(ids, r.Side, r.Start, r.End+1)
	ui.Row(c).FillWidth().Padding(4, 12).Gap(8).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
		ui.Text(c, plural(len(ids), "changed line", "changed lines")+" chosen").FontSize(12).TextColor(t.TextMuted)
		ui.Spacer(c)
		tab.lineButtons(c, f, ids, "Lines")
	})
}

// lineButtons are the buttons that stage, unstage or discard lines of the
// patch of a file: noun says whether they are a hunk's or the lines chosen.
func (tab *repoTab) lineButtons(c *ui.Context, f *reviewFile, sel map[git.LineID]bool, noun string) {
	t := c.Theme()
	idle := tab.busy == ""
	patch, staged, file, untracked := f.patch, f.staged, f.path, f.entry.Untracked
	mini := func(label string, primary bool) ui.Element {
		b := ui.ButtonBase(c).Height(18).Padding(0, 8).Radius(5).Center().Label(label).Disabled(!idle || len(sel) == 0)
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

// openFind opens the find bar of the viewer of the file shown, or of the one
// at the top of a pull request's.
func (rv *review) openFind() {
	i := rv.only
	if rv.work {
		i = 0
	}
	if i < 0 {
		i = rv.current
	}
	if i >= 0 && i < len(rv.files) {
		if d := rv.files[i].ed; d != nil {
			d.Run(editor.Find)
		}
	}
}

func (rv *review) toggleFind() { rv.openFind() }
