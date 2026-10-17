package main

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/eliott/myfork/internal/git"
)

// detailsView is the commit chosen: Summary, Changes and File Tree.
func (tab *repoTab) detailsView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		cm := tab.commit
		if cm == nil {
			ui.Text(c, "Select a commit to see its details").TextColor(t.TextMuted).Padding(20).TextAlign(ui.Center).FillWidth()
			return
		}
		ui.Row(c).Padding(5, 10).Gap(10).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
			ui.Segmented(c, &tab.detail, "Summary", "Changes", "File Tree").Label("Commit details")
			ui.Text(c, cm.Short()).Font("SF Mono, Menlo, monospace").FontSize(12).TextColor(t.TextMuted).Selectable()
			ui.Text(c, cm.Subject).FontSize(12.5).SingleLine().Grow(1).MinWidth(0)
			if len(tab.changes) > 0 {
				ui.Text(c, plural(len(tab.changes), "file", "files")).FontSize(12).TextColor(t.TextMuted)
			}
		})
		switch tab.detail {
		case 0:
			tab.summaryView(c, cm)
		case 2:
			tab.treeView(c, cm)
		default:
			tab.changesTab(c)
		}
	})
}

// summaryView is the commit's message, author, and neighbours.
func (tab *repoTab) summaryView(c *ui.Context, cm *git.Commit) {
	t := c.Theme()
	now := c.Now()
	label := func(s string) { ui.Text(c, s).FontSize(10.5).TextColor(t.TextMuted).Margin(8, 0, 2) }
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Padding(14, 18).Gap(2).FillWidth().Children(func() {
			ui.Text(c, cm.Subject).FontSize(16).Bold().Selectable()
			if cm.Body != "" {
				ui.Text(c, cm.Body).FontSize(13).Selectable().Margin(6, 0, 0)
			}
			label("AUTHOR")
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Avatar(c, cm.Author, nil)
				ui.Column(c).Children(func() {
					ui.Text(c, cm.Author+"  <"+cm.AuthorEmail+">").FontSize(13).Selectable()
					ui.Text(c, cm.AuthorTime.Format("Mon, 2 Jan 2006 15:04:05")+"  ·  "+relativeTime(now, cm.AuthorTime)).FontSize(12).TextColor(t.TextMuted)
				})
			})
			if cm.Committer != cm.Author || cm.CommitTime != cm.AuthorTime {
				label("COMMITTER")
				ui.Text(c, cm.Committer+"  <"+cm.CommitterEmail+">  ·  "+cm.CommitTime.Format("Mon, 2 Jan 2006 15:04:05")).FontSize(12.5).Selectable()
			}
			label("SHA")
			ui.Text(c, cm.Hash).Font("SF Mono, Menlo, monospace").FontSize(12).Selectable()
			if len(cm.Parents) > 0 {
				label("PARENTS")
				for _, p := range cm.Parents {
					hash := p
					link := ui.ButtonBase(c).Key(hash).Padding(1, 0).Justify(ui.Start).FillWidth().TextColor(t.Accent).Label("Parent " + hash[:7])
					if link.Clicked() {
						tab.jumpTo(hash)
					}
					link.Children(func() {
						ui.Text(c, hash[:7]).Font("SF Mono, Menlo, monospace").FontSize(12).Underline()
						ui.Text(c, " "+tab.subjectOf(hash)).FontSize(12).TextColor(t.TextMuted).SingleLine()
					})
				}
			}
			if refs := tab.refsAt[cm.Hash]; len(refs) > 0 {
				label("REFS")
				ui.Row(c).Gap(6).Wrap().Children(func() { tab.refChips(c, cm.Hash) })
			}
			if len(tab.changes) > 0 {
				label("CHANGED FILES")
				for _, ch := range tab.changes {
					dir, base := dirAndBase(ch.Path)
					ui.Row(c).Gap(8).Children(func() {
						statusBadge(c, ch.Status)
						ui.Text(c, dir).FontSize(12).TextColor(t.TextMuted)
						ui.Text(c, base).FontSize(12.5).Margin(0, 0, 0, -8)
					})
				}
			}
		})
	})
}

func (tab *repoTab) subjectOf(hash string) string {
	for i := range tab.commits {
		if tab.commits[i].Hash == hash {
			return tab.commits[i].Subject
		}
	}
	return ""
}

// statusColor is the color of a change's letter.
func statusColor(c *ui.Context, s byte) ui.Color {
	t := c.Theme()
	switch s {
	case 'A', '?':
		return t.Success
	case 'D', 'U':
		return t.Danger
	case 'R', 'C':
		return ui.Hex("#8b5cf6")
	}
	return t.Warning
}

// statusBadge is the colored letter before a file.
func statusBadge(c *ui.Context, s byte) {
	letter, meaning := statusLetter(s)
	col := statusColor(c, s)
	ui.Text(c, letter).Size(16, 16).TextAlign(ui.Center).FontSize(10.5).FontWeight(700).TextColor(col).Radius(4).Background(col.Alpha(0.16)).Tooltip(meaning).Shrink(0)
}

// changesTab is the files of the commit, and the patch of the one chosen.
func (tab *repoTab) changesTab(c *ui.Context) {
	t := c.Theme()
	ui.Split(c, &tab.filesW, func() {
		if len(tab.changes) == 0 {
			ui.Text(c, "No file changed").TextColor(t.TextMuted).Padding(16)
			return
		}
		list := ui.List(c, &tab.cList, len(tab.changes), func(i int) {
			ch := tab.changes[i]
			dir, base := dirAndBase(ch.Path)
			row := ui.Row(c).Gap(7).Padding(3, 8).AlignItems(ui.Center).Tooltip(ch.Path)
			row.ContextMenu(func(m *ui.Menu) { tab.fileMenu(m, ch.Path) })
			row.Children(func() {
				statusBadge(c, ch.Status)
				ui.Text(c, base).SingleLine().FontSize(12.5).Shrink(0).MaxWidth(240)
				ui.Text(c, dir).SingleLine().FontSize(11.5).Opacity(0.6).Grow(1).MinWidth(0)
			})
		}).Grow(1).Label("Changed files")
		if list.Changed() {
			tab.showCommitFile(tab.cSel)
		}
	}, func() {
		tab.diffArea(c, &tab.hist, nil)
	}).Grow(1).MinHeight(0)
}

// fileMenu is the menu of a file shown with a commit.
func (tab *repoTab) fileMenu(m *ui.Menu, p string) {
	cm := tab.commit
	if m.Item("File History…").Chosen() {
		tab.app.fileHistoryDialog(tab, p)
	}
	if m.Item("Blame…").Chosen() {
		tab.app.blameDialog(tab, p, "")
	}
	m.Separator()
	if m.Item("Copy Path").Chosen() {
		copyText(p)
	}
	if cm != nil && m.Item("Reset File to This Commit").Chosen() {
		hash := cm.Hash
		tab.app.confirm("Reset “"+p+"”?", "The file in the work tree takes its state at "+hash[:7]+".", "Reset", func() error {
			_, err := tab.repo.Do("checkout", hash, "--", p)
			return err
		}, nil)
	}
}

// treeView lists the files of the commit as they were.
func (tab *repoTab) treeView(c *ui.Context, cm *git.Commit) {
	t := c.Theme()
	if tab.tree == nil && !tab.treeLoading {
		tab.treeLoading = true
		hash, repo := cm.Hash, tab.repo
		tab.app.background(func() {
			files, _ := repo.Tree(hash)
			tab.app.update(func() {
				tab.treeLoading = false
				if tab.commit != nil && tab.commit.Hash == hash {
					tab.tree = files
					if files == nil {
						tab.tree = []string{}
					}
				}
			})
		})
	}
	ui.Split(c, &tab.filesW, func() {
		if tab.treeLoading {
			ui.Spinner(c).Margin(16)
			return
		}
		list := ui.List(c, &tab.treeList, len(tab.tree), func(i int) {
			dir, base := dirAndBase(tab.tree[i])
			row := ui.Row(c).Gap(7).Padding(3, 8).AlignItems(ui.Center).Tooltip(tab.tree[i])
			row.ContextMenu(func(m *ui.Menu) { tab.fileMenu(m, tab.tree[i]) })
			row.Children(func() {
				ui.Icon(c, iconFile).FontSize(13).TextColor(t.TextMuted)
				ui.Text(c, base).SingleLine().FontSize(12.5).Shrink(0).MaxWidth(240)
				ui.Text(c, dir).SingleLine().FontSize(11.5).Opacity(0.6).Grow(1).MinWidth(0)
			})
		}).Grow(1).Label("Files")
		if list.Changed() && tab.treeSel >= 0 && tab.treeSel < len(tab.tree) {
			tab.showTreeFile(tab.tree[tab.treeSel])
		}
	}, func() {
		tab.diffArea(c, &tab.hist, nil)
	}).Grow(1).MinHeight(0)
}

// diffArea shows a pane's patch, or why there is none. hunkActions, when
// set, adds buttons to each hunk's header.
func (tab *repoTab) diffArea(c *ui.Context, p *diffPane, hunkActions func(c *ui.Context, index int)) {
	t := c.Theme()
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
		switch {
		case p.key == "":
			ui.Text(c, "Select a file to see its changes").TextColor(t.TextMuted).Padding(20).TextAlign(ui.Center).FillWidth()
		case p.loading:
			ui.Row(c).Padding(20).Justify(ui.Center).FillWidth().Children(func() { ui.Spinner(c).Label("Loading") })
		case p.err != "":
			ui.Text(c, p.err).TextColor(t.Danger).Padding(20).Selectable()
		case p.binary:
			ui.Text(c, "Binary file: "+p.path).TextColor(t.TextMuted).Padding(20).TextAlign(ui.Center).FillWidth()
		case p.empty || p.state == nil:
			ui.Text(c, "No textual changes").TextColor(t.TextMuted).Padding(20).TextAlign(ui.Center).FillWidth()
		default:
			tab.fileHeader(c, p)
			if git.PatchHasLFSPointer(p.patch) {
				ui.Row(c).Gap(8).Padding(4, 12).Shrink(0).AlignItems(ui.Center).Background(t.Accent.Alpha(0.1)).Children(func() {
					ui.Icon(c, iconCloud).FontSize(13).TextColor(t.Accent)
					ui.Text(c, "This file is stored with Git LFS: the lines are its pointer, not its content.").FontSize(12)
				})
			}
			d := p.state
			if hunkActions == nil {
				editor.DiffView(c, d).Grow(1).FillWidth().MinHeight(0)
				return
			}
			tab.hunkDiff(c, p, hunkActions)
		}
	})
}

// fileHeader names the file above its diff, with the lines it changed.
func (tab *repoTab) fileHeader(c *ui.Context, p *diffPane) {
	t := c.Theme()
	added, deleted := p.state.Stats()
	dir, base := dirAndBase(p.path)
	ui.Row(c).Padding(4, 12).Gap(8).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
		ui.Text(c, dir).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
		ui.Text(c, base).FontSize(12.5).FontWeight(600).SingleLine().Shrink(0).Margin(0, 0, 0, -8)
		ui.Spacer(c)
		ui.Textf(c, "+%d", added).FontSize(12).TextColor(t.Success).Font("SF Mono, Menlo, monospace")
		ui.Textf(c, "−%d", deleted).FontSize(12).TextColor(t.Danger).Font("SF Mono, Menlo, monospace")
	})
}

// hunkDiff shows a diff whose hunks light up under the pointer: a box around
// the hunk, context lines included, with the buttons that act on it, as Fork
// does. MyGo's viewer reports no hover over unchanged lines, and no geometry,
// so the hunks are placed by what the frame draws: each hunk starts at its
// header, and ends where the next separator starts, or under the last line.
func (tab *repoTab) hunkDiff(c *ui.Context, p *diffPane, actions func(c *ui.Context, index int)) {
	t := c.Theme()
	d, g := p.state, &p.geo
	box := ui.Column(c).Grow(1).FillWidth().MinHeight(0)
	_, local, over := box.PointerPosition()
	g.over, g.local = over, local
	g.hov = -1
	if over {
		g.hov, _, _ = g.region(g.top+local, g.top, g.bottom)
	}
	g.sticky = g.hov >= 0
	d.RenderSeparator = func(c *ui.Context, sep ui.Element, s *editor.DiffSeparator) {
		if s.Lines > 0 {
			editor.DefaultSeparator(c, sep, s)
			sep.Draw(func(pt *ui.Painter, r ui.Rect) { g.add(-1, r) })
			return
		}
		idx := hunkIndexOf(p.hunks, s.Header)
		shown := idx >= 0 && idx == g.hov
		if shown {
			g.sticky = false
		}
		sep.Background(t.Accent.Alpha(0.08)).Draw(func(pt *ui.Painter, r ui.Rect) { g.add(idx, r) })
		sep.Children(func() {
			ui.Row(c).FillWidth().Fill().Padding(0, 8, 0, 12).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, s.Header).Font("SF Mono, Menlo, monospace").FontSize(11.5).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
				if shown {
					actions(c, idx)
				}
			})
		})
	}
	// A line under the last one, which tells where the hunks end.
	d.Annotations = nil
	if n := len(p.hunks); n > 0 {
		start, count := hunkNewRange(p.hunks[n-1])
		if line := min(start+count-1, d.New.LineCount()-1); line >= 0 {
			d.Annotations = []editor.DiffAnnotation{{Side: editor.DiffNew, Line: line, Height: 1, Key: "end"}}
		}
	}
	d.RenderAnnotation = func(c *ui.Context, a editor.DiffAnnotation) {
		ui.Box(c).Fill().Draw(func(pt *ui.Painter, r ui.Rect) { g.end, g.hasEnd = r.Y, true })
	}
	box.Draw(func(pt *ui.Painter, r ui.Rect) { g.reset(r) })
	box.DrawOver(func(pt *ui.Painter, r ui.Rect) {
		hov, top, bottom := -1, float32(0), float32(0)
		if g.over {
			hov, top, bottom = g.region(r.Y+g.local, r.Y, r.Y+r.H)
		}
		if hov >= 0 {
			pt.Stroke(ui.Rect{X: r.X + 1, Y: top, W: r.W - 2, H: bottom - top}, hunkBoxColor, 3, 1.5)
		}
		if hov != g.hov {
			// The hunks moved under the pointer, as the view scrolled.
			tab.app.update(func() {})
		}
	})
	box.Children(func() {
		editor.DiffView(c, d).Fill()
		if g.sticky {
			// The hunk under the pointer has its header out of view.
			ui.Row(c).Absolute().Right(18).Top(6).Gap(6).Padding(4).Radius(8).Background(t.Surface).Border(1, hairline).AlignItems(ui.Center).Children(func() { actions(c, g.hov) })
		}
	})
}

// hunkGeo is where a diff's hunks stand, as the frame drew them, in window
// coordinates.
type hunkGeo struct {
	seps        []sepGeo
	end         float32 // under the last line, when it is in view
	hasEnd      bool
	top, bottom float32 // the viewer's
	local       float32 // the pointer's y in the viewer
	over        bool    // the pointer is over the viewer
	hov         int     // the hunk under the pointer as the view was built
	sticky      bool    // its header is out of view
}

// sepGeo is a separator drawn: a hunk's header, or hidden lines (hunk -1).
type sepGeo struct {
	hunk        int
	top, bottom float32
}

func (g *hunkGeo) reset(r ui.Rect) {
	g.seps, g.hasEnd = g.seps[:0], false
	g.top, g.bottom = r.Y, r.Y+r.H
}

func (g *hunkGeo) add(hunk int, r ui.Rect) {
	g.seps = append(g.seps, sepGeo{hunk, r.Y, r.Y + r.H})
}

// region finds the hunk y is in, and where its lines are: from under its
// header, or the viewer's top, to the next separator, the last line, or the
// viewer's bottom. It is -1 for what is no hunk's.
func (g *hunkGeo) region(y, viewTop, viewBottom float32) (hunk int, top, bottom float32) {
	seps := g.seps
	if len(seps) == 0 {
		return -1, 0, 0
	}
	if y < seps[0].top {
		if seps[0].hunk > 0 {
			return seps[0].hunk - 1, viewTop, seps[0].top
		}
		return -1, 0, 0
	}
	for k, s := range seps {
		next := viewBottom
		switch {
		case k+1 < len(seps):
			next = seps[k+1].top
		case g.hasEnd:
			next = min(next, g.end)
		}
		if y >= s.top && y < next {
			if s.hunk < 0 {
				return -1, 0, 0
			}
			return s.hunk, s.bottom, next
		}
	}
	return -1, 0, 0
}

var hunkBoxColor = ui.RGB(230, 170, 20)

// hunkIndexOf is the hunk of git's patch a header of the viewer is of.
func hunkIndexOf(hunks []string, header string) int {
	key, _, _ := strings.Cut(header, " @@")
	for i, h := range hunks {
		first, _, _ := strings.Cut(h, "\n")
		if first == header || strings.HasPrefix(first, key+" @@") {
			return i
		}
	}
	return -1
}

// hunkNewRange reads the new lines a hunk covers, from its header
// "@@ -a,b +c,d @@": the first, from 0, and how many.
func hunkNewRange(hunk string) (start, count int) {
	head, _, _ := strings.Cut(hunk, "\n")
	_, rest, ok := strings.Cut(head, " +")
	if !ok {
		return 0, 0
	}
	spec, _, _ := strings.Cut(rest, " ")
	first, n, hasN := strings.Cut(spec, ",")
	start, _ = strconv.Atoi(first)
	count = 1
	if hasN {
		count, _ = strconv.Atoi(n)
	}
	if count > 0 {
		start--
	}
	return start, count
}
