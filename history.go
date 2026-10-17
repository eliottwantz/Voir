package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
	"github.com/eliott/myfork/internal/graph"
)

const (
	rowHeight    = 26
	laneWidth    = 14
	maxGraphCols = 10
)

var laneColors = []ui.Color{
	ui.Hex("#3b82f6"), ui.Hex("#22c55e"), ui.Hex("#f59e0b"), ui.Hex("#ec4899"),
	ui.Hex("#8b5cf6"), ui.Hex("#06b6d4"), ui.Hex("#ef4444"), ui.Hex("#84cc16"),
}

func laneColor(i int) ui.Color { return laneColors[i%len(laneColors)] }

// graphWidth is the width of the graph column.
func (tab *repoTab) graphWidth() float32 {
	cols := min(max(tab.layout.MaxCols, 1), maxGraphCols)
	return float32(cols)*laneWidth + 14
}

const (
	authorW = 150
	dateW   = 118
	shaW    = 64
)

// commitList is the commit graph, as a table: graph, description, author,
// date, sha. It is what the Branches view shows.
func (tab *repoTab) commitList(c *ui.Context) {
	t := c.Theme()
	now := c.Now()
	gw := tab.graphWidth()
	searching := tab.filter != ""

	ui.Column(c).Fill().MinHeight(0).Children(func() {
		ui.Row(c).Height(24).Padding(0, 12, 0, 0).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(hairline).TextColor(t.TextMuted).FontSize(11.5).Children(func() {
			title := "Graph"
			if searching {
				title = ""
			}
			ui.Text(c, title).Width(gw).Shrink(0).Padding(0, 0, 0, 8)
			ui.Text(c, "Description").Grow(1).MinWidth(0).Padding(0, 0, 0, 6)
			ui.Text(c, "Author").Width(authorW)
			ui.Text(c, "Date").Width(dateW)
			ui.Text(c, "Commit").Width(shaW)
		})
		n := tab.rowCount()
		if n == 0 {
			msg := "No commits yet"
			if searching {
				msg = "No commit matches “" + strings.TrimSpace(tab.filter) + "”"
				if tab.searched != strings.TrimSpace(tab.filter) {
					msg = "Searching…"
				}
			}
			ui.Text(c, msg).TextColor(t.TextMuted).Padding(20).TextAlign(ui.Center).FillWidth()
			return
		}
		tab.list.Label = func(i int) string {
			if cm := tab.rowCommit(i); cm != nil {
				return cm.Subject
			}
			return "Uncommitted changes"
		}
		list := ui.List(c, &tab.list, n, func(i int) { tab.commitRowView(c, i, now, gw) }).Grow(1).MinHeight(0).Label("Commits")
		if list.Changed() {
			tab.selectRow(tab.sel)
		}
		if list.Submitted() {
			if cm := tab.rowCommit(tab.sel); cm != nil {
				tab.app.checkoutCommitDialog(tab, cm)
			}
		}
		// Older commits load as the end comes in view.
		if _, last := tab.list.Visible(); last >= n-40 && tab.hasMore {
			tab.loadMore()
		}
		if tab.jump != "" {
			h := tab.jump
			tab.jump = ""
			tab.jumpTo(h)
		}
	})
}

// commitRowView builds one row of the commit list.
func (tab *repoTab) commitRowView(c *ui.Context, i int, now time.Time, gw float32) {
	t := c.Theme()
	cm := tab.rowCommit(i)
	row := ui.Row(c).Height(rowHeight).Padding(0, 12, 0, 0).AlignItems(ui.Center)
	if cm == nil {
		tab.wipRow(c, row, gw)
		return
	}
	// The graph is drawn only when the log is shown whole.
	var gr *graph.Row
	if tab.filter == "" {
		idx := i
		if tab.hasWip() {
			idx--
		}
		if idx >= 0 && idx < len(tab.rows) {
			gr = &tab.rows[idx]
		}
	}
	isHead := tab.status != nil && tab.status.Head == cm.Hash
	row.ContextMenu(func(m *ui.Menu) { tab.commitMenu(m, cm) })
	row.Children(func() {
		tab.graphCell(c, gr, gw, isHead, tab.hasWip() && i == 1 && tab.status != nil && isHead)
		ui.Row(c).Grow(1).MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
			tab.refChips(c, cm.Hash)
			ui.Text(c, cm.Subject).SingleLine().Grow(1).MinWidth(0).FontSize(13)
		})
		ui.Text(c, cm.Author).Width(authorW).SingleLine().FontSize(12).Opacity(0.8)
		ui.Text(c, dateColumn(now, cm.AuthorTime)).Width(dateW).SingleLine().FontSize(12).Opacity(0.8)
		ui.Text(c, cm.Short()).Width(shaW).SingleLine().Font("SF Mono, Menlo, monospace").FontSize(11.5).Opacity(0.7)
	})
	_ = t
}

// wipRow is the first row: the uncommitted changes.
func (tab *repoTab) wipRow(c *ui.Context, row ui.Element, gw float32) {
	t := c.Theme()
	n := 0
	if tab.status != nil {
		n = len(tab.status.Entries)
	}
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item("Show Local Changes").Chosen() {
			tab.showChanges()
		}
	})
	row.Children(func() {
		ui.Box(c).Size(gw, rowHeight).Draw(func(p *ui.Painter, r ui.Rect) {
			cx := r.X + 7 + laneWidth/2
			cy := r.Y + r.H/2
			p.Line(cx, cy, cx, r.Y+r.H, 1.5, laneColor(0).Alpha(0.6))
			p.StrokeDashed(ui.Rect{X: cx - 4, Y: cy - 4, W: 8, H: 8}, laneColor(0), 4, 1.5)
		})
		ui.Text(c, "Uncommitted changes").FontSize(13).Italic().Grow(1)
		ui.Row(c).Gap(6).Children(func() {
			ui.Text(c, plural(n, "change", "changes")).FontSize(12).TextColor(t.TextMuted)
		})
	})
}

// graphCell draws a commit's dot and the lines to its neighbours.
func (tab *repoTab) graphCell(c *ui.Context, gr *graph.Row, gw float32, isHead, _ bool) {
	t := c.Theme()
	ui.Box(c).Size(gw, rowHeight).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		if gr == nil {
			return
		}
		x := func(col int) float32 { return r.X + 7 + (float32(min(col, maxGraphCols-1))+0.5)*laneWidth }
		top, mid, bot := r.Y, r.Y+r.H/2, r.Y+r.H
		cx := x(gr.Col)
		for _, s := range gr.Segs {
			col := laneColor(s.Color)
			var path ui.Path
			switch {
			case s.From >= 0 && s.To == graph.Node:
				path.MoveTo(x(s.From), top)
				if x(s.From) == cx {
					path.LineTo(cx, mid)
				} else {
					path.CubeTo(x(s.From), top+r.H*0.35, cx, mid-r.H*0.2, cx, mid)
				}
			case s.From == graph.Node && s.To >= 0:
				path.MoveTo(cx, mid)
				if x(s.To) == cx {
					path.LineTo(cx, bot)
				} else {
					path.CubeTo(cx, mid+r.H*0.2, x(s.To), bot-r.H*0.35, x(s.To), bot)
				}
			default:
				path.MoveTo(x(s.From), top).LineTo(x(s.To), bot)
			}
			p.StrokePath(&path, 1.6, col)
		}
		dot := ui.Rect{X: cx - 4.5, Y: mid - 4.5, W: 9, H: 9}
		col := laneColor(gr.Color)
		if isHead {
			p.Fill(ui.Rect{X: cx - 6.5, Y: mid - 6.5, W: 13, H: 13}, t.Background, 6.5)
			p.Stroke(ui.Rect{X: cx - 6, Y: mid - 6, W: 12, H: 12}, col, 6, 2)
		}
		p.Fill(dot, col, 4.5)
	})
}

// refChips are the labels of the branches and tags at a commit.
func (tab *repoTab) refChips(c *ui.Context, hash string) {
	refs := tab.refsAt[hash]
	if len(refs) == 0 {
		return
	}
	// A local branch and the remote branch of the same name show as one.
	type chip struct {
		label string
		kind  git.RefKind
		head  bool
		both  bool
	}
	var chips []chip
	local := map[string]int{}
	for _, r := range refs {
		if r.Kind == git.LocalBranch {
			local[r.Name] = len(chips)
			chips = append(chips, chip{label: r.Name, kind: r.Kind, head: r.Head})
		}
	}
	for _, r := range refs {
		switch r.Kind {
		case git.RemoteBranch:
			_, short, _ := strings.Cut(r.Name, "/")
			if i, ok := local[short]; ok && tab.app.store.settings().CompactLabels {
				chips[i].both = true
				continue
			}
			chips = append(chips, chip{label: r.Name, kind: r.Kind})
		case git.Tag:
			chips = append(chips, chip{label: r.Name, kind: r.Kind})
		case git.StashRef:
			chips = append(chips, chip{label: "stash", kind: r.Kind})
		}
	}
	for k, ch := range chips {
		if k == 4 {
			ui.Textf(c, "+%d", len(chips)-4).FontSize(11).Opacity(0.7)
			break
		}
		var col ui.Color
		var svg *ui.SVG
		switch ch.kind {
		case git.LocalBranch:
			col, svg = ui.Hex("#22c55e"), iconBranch
		case git.RemoteBranch:
			col, svg = ui.Hex("#3b82f6"), iconCloud
		case git.Tag:
			col, svg = ui.Hex("#f59e0b"), iconTag
		default:
			col, svg = ui.Hex("#8b5cf6"), iconStash
		}
		ui.Row(c).Gap(3).Padding(1, 6).Radius(9).Shrink(0).AlignItems(ui.Center).Background(col.Alpha(0.16)).Border(1, col.Alpha(0.45)).Children(func() {
			ui.Icon(c, svg).FontSize(10.5).TextColor(col)
			w := ui.Text(c, ch.label).FontSize(11).SingleLine().MaxWidth(180)
			if ch.head {
				w.FontWeight(700)
			}
		})
	}
}

// commitMenu is the menu of a commit in the list.
func (tab *repoTab) commitMenu(m *ui.Menu, cm *git.Commit) {
	a := tab.app
	cur := tab.currentBranch()
	if m.Item("Checkout…").Chosen() {
		a.checkoutCommitDialog(tab, cm)
	}
	m.Separator()
	if m.Item("Create Branch…").Chosen() {
		a.newBranchDialog(tab, cm.Hash)
	}
	if m.Item("Create Tag…").Chosen() {
		a.newTagDialog(tab, cm.Hash)
	}
	m.Separator()
	if m.Item("Cherry-pick").Disabled(cur == nil).Chosen() {
		tab.do("Cherry-pick", func(r *git.Repo) error { return r.CherryPick(cm.Hash) })
	}
	if m.Item("Revert Commit").Disabled(cur == nil).Chosen() {
		tab.do("Revert", func(r *git.Repo) error { return r.Revert(cm.Hash) })
	}
	if m.Item("Reset Current Branch to Here…").Disabled(cur == nil).Chosen() {
		a.resetDialog(tab, cm)
	}
	if m.Item("Merge into Current Branch…").Disabled(cur == nil).Chosen() {
		a.mergeDialog(tab, cm.Hash)
	}
	if m.Item("Interactive Rebase from Here…").Disabled(cur == nil).Chosen() {
		a.rebaseDialog(tab, cm.Hash, cm.Short())
	}
	m.Separator()
	if m.Item("Copy SHA").Chosen() {
		copyText(cm.Hash)
	}
	if m.Item("Copy Subject").Chosen() {
		copyText(cm.Subject)
	}
	if m.Item("Save as Patch…").Chosen() {
		a.savePatch(tab, cm)
	}
}

var _ = fmt.Sprint
