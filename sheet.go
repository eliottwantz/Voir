package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

// sheet is a large window-sized panel over the repository: File History and
// Blame.
type sheet struct {
	open  bool
	title string
	body  func(c *ui.Context)
	// state is what the sheet shows, for tests to look into.
	state any
	// wide makes the sheet as large as the window allows.
	wide bool
}

func (a *app) sheetView(c *ui.Context) {
	s := a.sheet
	if s == nil {
		return
	}
	if !s.open {
		a.sheet = nil
		return
	}
	t := c.Theme()
	w, h := c.Size()
	ui.Modal(c, &s.open, func() {
		width, height := min(w-80, 1040), min(h-80, 700)
		if s.wide {
			width, height = w-60, h-60
		}
		ui.Column(c).Width(width).Height(height).Gap(8).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, s.title).FontSize(15).Bold().SingleLine().Grow(1).MinWidth(0)
				if ui.Button(c, "Close").Clicked() {
					s.open = false
				}
			})
			ui.Box(c).Height(1).Background(t.Border).Shrink(0)
			s.body(c)
		})
	})
}

// fileHistoryDialog lists the commits that changed a file, with their patch.
func (a *app) fileHistoryDialog(tab *repoTab, p string) {
	st := &fileHistory{tab: tab, path: p, sel: -1, loading: true}
	st.list.Selected = &st.sel
	st.list.Key = func(i int) any { return st.commits[i].Hash }
	a.sheet = &sheet{open: true, title: "History of " + p, body: st.view, state: st}
	repo := tab.repo
	a.background(func() {
		cs, err := repo.Log(git.LogOptions{Path: p, Follow: true, All: false, Limit: 500})
		a.update(func() {
			st.loading, st.commits, st.err = false, cs, err
			if len(cs) > 0 {
				st.sel = 0
				st.show()
			}
		})
	})
}

type fileHistory struct {
	tab     *repoTab
	path    string
	commits []git.Commit
	loading bool
	err     error
	list    ui.ListState
	sel     int
	pane    diffPane
}

func (st *fileHistory) show() {
	if st.sel < 0 || st.sel >= len(st.commits) {
		return
	}
	cm := st.commits[st.sel]
	repo := st.tab.repo
	o := st.tab.diffOptions()
	st.tab.loadDiff(&st.pane, "fh:"+cm.Hash, st.path, func() (string, error) {
		patch, err := repo.CommitDiff(&cm, st.path, o)
		if err == nil && patch == "" {
			// A rename: the file had another name there.
			patch, err = repo.DiffBetween(firstParentOrEmpty(&cm), cm.Hash, "", o)
		}
		return patch, err
	})
}

func firstParentOrEmpty(c *git.Commit) string {
	if len(c.Parents) > 0 {
		return c.Parents[0]
	}
	return git.EmptyTree
}

func (st *fileHistory) view(c *ui.Context) {
	t := c.Theme()
	now := c.Now()
	switch {
	case st.loading:
		ui.Spinner(c).Margin(20).Label("Loading")
		return
	case st.err != nil:
		ui.Text(c, errorText(st.err)).TextColor(t.Danger).Selectable()
		return
	case len(st.commits) == 0:
		ui.Text(c, "No commit changed this file").TextColor(t.TextMuted)
		return
	}
	ui.Row(c).Grow(1).MinHeight(0).Gap(8).Children(func() {
		list := ui.List(c, &st.list, len(st.commits), func(i int) {
			cm := st.commits[i]
			ui.Column(c).Padding(4, 8).Gap(1).Children(func() {
				ui.Text(c, cm.Subject).FontSize(12.5).SingleLine()
				ui.Text(c, fmt.Sprintf("%s · %s · %s", cm.Short(), cm.Author, relativeTime(now, cm.AuthorTime))).FontSize(11).Opacity(0.65).SingleLine()
			})
		}).Width(330).MinHeight(0).Label("Commits")
		if list.Changed() {
			st.show()
		}
		st.tab.diffArea(c, &st.pane, nil)
	})
}

// blameDialog shows who last changed each line of a file.
func (a *app) blameDialog(tab *repoTab, p, rev string) {
	st := &blameState{tab: tab, path: p, loading: true}
	a.sheet = &sheet{open: true, title: "Blame " + p, body: st.view, state: st}
	repo := tab.repo
	a.background(func() {
		lines, err := repo.Blame(rev, p)
		a.update(func() { st.loading, st.lines, st.err = false, lines, err })
	})
}

type blameState struct {
	tab     *repoTab
	path    string
	lines   []git.BlameLine
	loading bool
	err     error
	list    ui.ListState
}

func (st *blameState) view(c *ui.Context) {
	t := c.Theme()
	switch {
	case st.loading:
		ui.Spinner(c).Margin(20).Label("Loading")
		return
	case st.err != nil:
		ui.Text(c, errorText(st.err)).TextColor(t.Danger).Selectable()
		return
	}
	now := c.Now()
	mono := "SF Mono, Menlo, monospace"
	ui.List(c, &st.list, len(st.lines), func(i int) {
		l := st.lines[i]
		newBlock := i == 0 || st.lines[i-1].Hash != l.Hash
		row := ui.Row(c).Height(20).Gap(8).AlignItems(ui.Center)
		if newBlock && i > 0 {
			row.BorderWidth(1, 0, 0, 0).BorderColor(hairline)
		}
		row.Children(func() {
			if newBlock {
				ui.Text(c, l.Hash[:7]).Font(mono).FontSize(11).TextColor(t.Accent).Width(56)
				ui.Text(c, l.Author).FontSize(11).SingleLine().Width(110).Opacity(0.8)
				ui.Text(c, relativeTime(now, time.Unix(l.Time, 0))).FontSize(11).SingleLine().Width(90).Opacity(0.6)
			} else {
				ui.Box(c).Width(56 + 110 + 90 + 16)
			}
			ui.Textf(c, "%d", l.Line).Font(mono).FontSize(11).Width(34).TextAlign(ui.End).Opacity(0.5)
			ui.Text(c, l.Text).Font(mono).FontSize(12).SingleLine().Grow(1).MinWidth(0)
		}).Tooltip(l.Summary)
	}).Grow(1).MinHeight(0).Label("Blame")
}
