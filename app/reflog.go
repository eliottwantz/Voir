package app

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/voir/internal/git"
)

// reflogLimit is how many moves of HEAD the Reflog view reads.
const reflogLimit = 1000

// reflogEntryW is the width of the column of the selectors (HEAD@{12}), and
// reflogActionW the one of what moved HEAD.
const (
	reflogEntryW  = 92
	reflogActionW = 132
)

// showReflog shows the Reflog view: every move of HEAD, the latest first, over
// the commit chosen. It holds the commits no branch has any more, as those a
// reset or a rebase left behind.
func (t *repoTab) showReflog() {
	t.app.pulls = false
	if t.onReflog {
		return
	}
	if t.onWip {
		t.onWip = false
		t.wipReturn = ""
	}
	t.onReflog = true
	t.reflogSel = -1
	t.loadReflog()
}

// leaveReflog goes back to the graph, on the commit it showed before.
func (t *repoTab) leaveReflog() {
	if !t.onReflog {
		return
	}
	t.onReflog = false
	t.sel = -1
	if i := t.commitRow(t.selHash); i >= 0 {
		t.loadRow(i)
		t.scrollSel = true
		return
	}
	t.selHash = ""
	t.setCommit(nil)
	t.restoreSelection()
}

// loadReflog reads the reflog, off the main thread.
func (t *repoTab) loadReflog() {
	t.reflogGen++
	gen := t.reflogGen
	repo := t.repo
	t.reflogLoading = true
	t.app.background(func() {
		es, err := repo.Reflog(reflogLimit)
		t.app.update(func() {
			if gen != t.reflogGen {
				return
			}
			t.applyReflog(es, err)
		})
	})
}

// applyReflog shows the reflog read, keeping the entry chosen: new entries
// come on top, so it is found again by what it is rather than where.
func (t *repoTab) applyReflog(es []git.ReflogEntry, err error) {
	var was *git.ReflogEntry
	if t.reflogSel >= 0 && t.reflogSel < len(t.reflog) {
		e := t.reflog[t.reflogSel]
		was = &e
	}
	t.reflog, t.reflogLoading, t.reflogErr = es, false, ""
	if err != nil {
		t.reflogErr = errorText(err)
	}
	t.reflogSel = -1
	if was != nil {
		for i, e := range es {
			if e.Commit.Hash == was.Commit.Hash && e.Time.Equal(was.Time) && e.Action == was.Action && e.Message == was.Message {
				t.reflogSel = i
				break
			}
		}
	}
}

// chooseReflog shows the commit of an entry under the list.
func (t *repoTab) chooseReflog(i int) {
	if i < 0 || i >= len(t.reflog) {
		return
	}
	t.reflogSel = i
	cm := t.reflog[i].Commit
	if t.selHash == cm.Hash && t.commit != nil && !t.comparing() {
		return
	}
	t.cmpGen++
	t.selHash = cm.Hash
	t.pick(cm.Hash)
	t.setCommit(&cm)
}

// reflogList is the list of the Reflog view.
func (tab *repoTab) reflogList(c *ui.Context) {
	t := c.Theme()
	now := c.Now()
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		ui.Row(c).Height(24).Padding(0, 12).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(hairline).TextColor(t.TextMuted).FontSize(11.5).Children(func() {
			ui.Text(c, "Entry").Width(reflogEntryW)
			ui.Text(c, "Action").Width(reflogActionW)
			ui.Text(c, "Description").Grow(1).MinWidth(0)
			ui.Text(c, "Date").Width(dateW)
			ui.Text(c, "Commit").Width(shaW)
		})
		switch {
		case tab.reflogErr != "":
			ui.Text(c, tab.reflogErr).TextColor(t.Danger).Padding(20).TextAlign(ui.Center).FillWidth()
			return
		case len(tab.reflog) == 0 && tab.reflogLoading:
			ui.Text(c, "Loading…").TextColor(t.TextMuted).Padding(20).TextAlign(ui.Center).FillWidth()
			return
		case len(tab.reflog) == 0:
			ui.Text(c, "HEAD has not moved yet").TextColor(t.TextMuted).Padding(20).TextAlign(ui.Center).FillWidth()
			return
		}
		tab.reflogState.Selected = &tab.reflogSel
		tab.reflogState.Label = func(i int) string {
			if i < len(tab.reflog) {
				e := tab.reflog[i]
				return e.Selector + " " + e.Action + ": " + e.Message
			}
			return ""
		}
		list := ui.List(c.Key("reflog"), &tab.reflogState, len(tab.reflog)).Grow(1).MinHeight(0).Label("Reflog").Rows(func(lr ui.ListRow) {
			tab.reflogRow(lr.Context, lr.Index, now, lr.Selected())
		})
		if list.Changed() {
			tab.chooseReflog(tab.reflogSel)
		}
		if list.Submitted() && tab.reflogSel >= 0 && tab.reflogSel < len(tab.reflog) {
			tab.showInGraph(tab.reflog[tab.reflogSel].Commit.Hash)
		}
	})
}

// reflogRow is one move of HEAD; on the row chosen, its action takes the
// row's own color, which the highlight would drown.
func (tab *repoTab) reflogRow(c *ui.Context, i int, now time.Time, chosen bool) {
	if i >= len(tab.reflog) {
		return
	}
	e := tab.reflog[i]
	t := c.Theme()
	row := ui.Row(c).Height(rowHeight).Padding(0, 12).AlignItems(ui.Center)
	row.ContextMenu(func(m *ui.Menu) { tab.reflogMenu(m, e) })
	row.Children(func() {
		ui.Text(c, e.Selector).Width(reflogEntryW).SingleLine().Font(mono).FontSize(11.5).Opacity(0.7)
		ui.Row(c).Width(reflogActionW).Children(func() {
			pill := ui.Text(c, e.Action).SingleLine().FontSize(11.5).Padding(1, 7).Radius(5).Shrink(1).MinWidth(0)
			if col := reflogActionColor(t, e.Action); chosen {
				pill.Background(t.AccentText.Alpha(0.2))
			} else {
				pill.TextColor(col).Background(col.Alpha(0.14))
			}
		})
		ui.Row(c).Grow(1).MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
			tab.refChips(c, e.Commit.Hash, true)
			ui.Text(c, e.Message).SingleLine().Grow(1).MinWidth(0).FontSize(13)
		})
		ui.Text(c, dateColumn(now, e.Time)).Width(dateW).SingleLine().FontSize(12).Opacity(0.8).Tooltip(e.Time.Format("Mon 2 Jan 2006, 15:04:05"))
		ui.Text(c, e.Commit.Short()).Width(shaW).SingleLine().Font(mono).FontSize(11.5).Opacity(0.7)
	})
}

// reflogActionColor tells the kinds of moves apart: those that rewrite or
// throw away commits stand out.
func reflogActionColor(t *ui.Theme, action string) ui.Color {
	switch kind, _, _ := strings.Cut(action, " "); kind {
	case "commit":
		return t.Success
	case "reset", "rebase", "voir":
		return t.Warning
	case "merge", "pull", "cherry-pick", "revert":
		return t.Accent
	}
	return t.TextMuted
}

// reflogMenu is the context menu of an entry: what the graph offers on a
// commit, after the way back to it in the graph.
func (tab *repoTab) reflogMenu(m *ui.Menu, e git.ReflogEntry) {
	cm := e.Commit
	if m.Item("Show in All Commits").Chosen() {
		tab.showInGraph(cm.Hash)
	}
	if m.Item("Copy " + e.Selector).Chosen() {
		copyText(e.Selector)
	}
	m.Separator()
	tab.commitMenu(m, &cm)
}

// showInGraph leaves the Reflog view for the commit in the graph, if a branch
// still has it.
func (tab *repoTab) showInGraph(hash string) {
	if tab.commitRow(hash) >= 0 {
		tab.jumpTo(hash)
		return
	}
	tab.loadUntil(hash, func() { tab.jumpTo(hash) })
}
