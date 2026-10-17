package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

// lineRow is a row of the line-selection view: a hunk's header, or a line.
type lineRow struct {
	hunk   int
	line   int // -1 for the header
	header string
	l      git.PatchLine
}

// lineState is the selection of lines of the file shown in Local Changes.
type lineState struct {
	on     bool
	patch  string // the patch the rows were made from
	parsed *git.Patch
	rows   []lineRow
	sel    map[git.LineID]bool
	anchor int // the row last clicked, for Shift
	list   ui.ListState
	err    string
}

// sync makes the rows of a pane's patch, and drops the selection when the
// patch changed under it.
func (ls *lineState) sync(patch string) {
	if ls.patch == patch && ls.parsed != nil {
		return
	}
	ls.patch, ls.sel, ls.anchor, ls.err = patch, map[git.LineID]bool{}, -1, ""
	ls.parsed, ls.rows = nil, ls.rows[:0]
	p, err := git.ParsePatchLines(patch)
	if err != nil {
		ls.err = err.Error()
		return
	}
	ls.parsed = p
	for hi, h := range p.Hunks {
		hdr := fmt.Sprintf("@@ -%d +%d @@", h.OldStart, h.NewStart)
		if h.Section != "" {
			hdr += " " + h.Section
		}
		ls.rows = append(ls.rows, lineRow{hunk: hi, line: -1, header: hdr})
		for li, l := range h.Lines {
			if l.Kind == '\\' {
				continue
			}
			ls.rows = append(ls.rows, lineRow{hunk: hi, line: li, l: l})
		}
	}
}

func (ls *lineState) changed(r lineRow) bool { return r.line >= 0 && r.l.Changed() }

func (ls *lineState) toggle(i int, shift bool) {
	r := ls.rows[i]
	if !ls.changed(r) {
		return
	}
	id := git.LineID{Hunk: r.hunk, Line: r.line}
	if shift && ls.anchor >= 0 && ls.anchor < len(ls.rows) {
		// Every changed line between the last click and this one takes the
		// state the clicked line is going to have.
		on := !ls.sel[id]
		lo, hi := min(ls.anchor, i), max(ls.anchor, i)
		for j := lo; j <= hi; j++ {
			if q := ls.rows[j]; ls.changed(q) {
				ls.set(git.LineID{Hunk: q.hunk, Line: q.line}, on)
			}
		}
	} else {
		ls.set(id, !ls.sel[id])
	}
	ls.anchor = i
}

func (ls *lineState) set(id git.LineID, on bool) {
	if on {
		ls.sel[id] = true
	} else {
		delete(ls.sel, id)
	}
}

func (ls *lineState) selectHunk(h int) {
	all := true
	for _, r := range ls.rows {
		if r.hunk == h && ls.changed(r) && !ls.sel[git.LineID{Hunk: h, Line: r.line}] {
			all = false
		}
	}
	for _, r := range ls.rows {
		if r.hunk == h && ls.changed(r) {
			ls.set(git.LineID{Hunk: h, Line: r.line}, !all)
		}
	}
}

func (ls *lineState) selectAll(on bool) {
	for _, id := range ls.parsed.ChangedLines() {
		ls.set(id, on)
	}
}

func (ls *lineState) counts() (added, removed int) {
	for id := range ls.sel {
		switch ls.parsed.Hunks[id.Hunk].Lines[id.Line].Kind {
		case '+':
			added++
		case '-':
			removed++
		}
	}
	return
}

// setLineMode switches Local Changes to choosing lines. A new file has no
// lines in a patch until git knows of it, so it is added with no content.
func (tab *repoTab) setLineMode(on bool, e git.StatusEntry) {
	tab.lines.on = on
	if on && e.Untracked {
		tab.do("Prepare lines", func(r *git.Repo) error { return r.IntentToAdd(e.Path) })
	}
}

// lineView shows the patch of the file chosen with a checkbox on every
// changed line, and stages, unstages or discards those checked.
func (tab *repoTab) lineView(c *ui.Context, p *diffPane, staged bool, e git.StatusEntry) {
	t := c.Theme()
	ls := &tab.lines
	switch {
	case p.key == "" || p.loading:
		ui.Row(c).Padding(20).Justify(ui.Center).FillWidth().Children(func() { ui.Spinner(c).Label("Loading") })
		return
	case p.binary || p.empty || p.err != "":
		ui.Text(c, "There are no lines to select in this file").TextColor(t.TextMuted).Padding(20).TextAlign(ui.Center).FillWidth()
		return
	}
	ls.sync(p.patch)
	if ls.err != "" {
		ui.Text(c, ls.err).TextColor(t.Danger).Padding(20)
		return
	}
	added, removed := ls.counts()
	n := added + removed
	idle := tab.busy == ""
	mono := "SF Mono, Menlo, monospace"
	patch := p.patch

	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
		ui.Row(c).Padding(4, 10).Gap(8).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
			label := "Click lines to select them (Shift for a range)"
			if n > 0 {
				label = fmt.Sprintf("%s selected: %d added, %d removed", plural(n, "line", "lines"), added, removed)
			}
			ui.Text(c, label).FontSize(12).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
			if ui.Button(c, "All").Tooltip("Select every changed line").Clicked() {
				ls.selectAll(true)
			}
			if ui.Button(c, "None").Disabled(n == 0).Clicked() {
				ls.selectAll(false)
			}
			if staged {
				if ui.PrimaryButton(c, "Unstage Selected").Disabled(n == 0 || !idle).Clicked() {
					sel := cloneSel(ls.sel)
					tab.do("Unstage lines", func(r *git.Repo) error { return r.UnstageLines(patch, sel) })
				}
				return
			}
			if ui.Button(c, "Discard Selected…").Disabled(n == 0 || !idle).Clicked() {
				sel := cloneSel(ls.sel)
				tab.app.confirm("Discard the selected lines?", fmt.Sprintf("%s in “%s” will be lost.", plural(n, "change", "changes"), e.Path), "Discard", func() error {
					return tab.repo.DiscardLines(patch, sel)
				}, nil)
			}
			if ui.PrimaryButton(c, "Stage Selected").Disabled(n == 0 || !idle).Clicked() {
				sel := cloneSel(ls.sel)
				tab.do("Stage lines", func(r *git.Repo) error { return r.StageLines(patch, sel) })
			}
		})
		ls.list.Label = func(i int) string {
			if i >= 0 && i < len(ls.rows) {
				if ls.rows[i].line < 0 {
					return ls.rows[i].header
				}
				return ls.rows[i].l.Text
			}
			return ""
		}
		ls.list.Key = func(i int) any { return i }
		ui.List(c, &ls.list, len(ls.rows), func(i int) {
			if i >= len(ls.rows) {
				return
			}
			r := ls.rows[i]
			if r.line < 0 {
				ui.Row(c).Padding(0, 8, 0, 12).Gap(8).Height(28).AlignItems(ui.Center).Background(t.Accent.Alpha(0.08)).Children(func() {
					ui.Text(c, r.header).Font(mono).FontSize(11.5).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
					if ui.Button(c, "Select Hunk").Key(fmt.Sprint("hunk", r.hunk)).Clicked() {
						ls.selectHunk(r.hunk)
					}
				})
				return
			}
			id := git.LineID{Hunk: r.hunk, Line: r.line}
			on := ls.sel[id]
			row := ui.ButtonBase(c).Height(20).Padding(0, 8).Gap(8).FillWidth().Justify(ui.Start).Radius(0).Label(fmt.Sprintf("%c %s", r.l.Kind, r.l.Text))
			switch r.l.Kind {
			case '+':
				row.Background(t.Success.Alpha(map[bool]float32{false: 0.12, true: 0.28}[on]))
			case '-':
				row.Background(t.Danger.Alpha(map[bool]float32{false: 0.12, true: 0.28}[on]))
			}
			if ls.changed(r) {
				if row.Clicked() {
					ls.toggle(i, row.ClickModifiers()&ui.Shift != 0)
				}
			} else {
				row.Disabled(true)
			}
			row.Children(func() {
				box := ui.Box(c).Size(14, 14).Radius(3).Shrink(0)
				if ls.changed(r) {
					box.Border(1, hairline.Alpha(0.8))
					if on {
						box.Background(t.Accent)
					}
				}
				box.Children(func() {
					if on {
						ui.Icon(c, iconCheck).FontSize(11).TextColor(t.AccentText).Center()
					}
				})
				num := func(n int) string {
					if n == 0 {
						return ""
					}
					return fmt.Sprint(n)
				}
				ui.Text(c, num(r.l.Old)).Font(mono).FontSize(11).Width(38).TextAlign(ui.End).Opacity(0.5).Shrink(0)
				ui.Text(c, num(r.l.New)).Font(mono).FontSize(11).Width(38).TextAlign(ui.End).Opacity(0.5).Shrink(0)
				ui.Text(c, string(r.l.Kind)).Font(mono).FontSize(12).Width(10).Shrink(0)
				ui.Text(c, strings.ReplaceAll(r.l.Text, "\t", "    ")).Font(mono).FontSize(12).SingleLine().Grow(1).MinWidth(0)
			})
		}).Grow(1).MinHeight(0)
	})
}

func cloneSel(m map[git.LineID]bool) map[git.LineID]bool {
	out := make(map[git.LineID]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
