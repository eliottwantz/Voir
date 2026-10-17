package main

import (
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
			if hunkActions != nil {
				d.RenderSeparator = func(c *ui.Context, sep ui.Element, s *editor.DiffSeparator) {
					tab.hunkSeparator(c, sep, s, p, hunkActions)
				}
			}
			editor.DiffView(c, d).Grow(1).FillWidth().MinHeight(0)
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

// hunkSeparator is the header of a hunk, with the buttons that act on it.
func (tab *repoTab) hunkSeparator(c *ui.Context, sep ui.Element, s *editor.DiffSeparator, p *diffPane, actions func(c *ui.Context, index int)) {
	t := c.Theme()
	if s.Lines > 0 {
		editor.DefaultSeparator(c, sep, s)
		return
	}
	idx := -1
	for i, h := range p.hunks {
		if first, _, _ := strings.Cut(h, "\n"); first == s.Header || strings.HasPrefix(first, strings.SplitN(s.Header, " @@", 2)[0]) {
			idx = i
			break
		}
	}
	sep.Background(t.Accent.Alpha(0.08)).Children(func() {
		ui.Row(c).FillWidth().Fill().Padding(0, 8, 0, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, s.Header).Font("SF Mono, Menlo, monospace").FontSize(11.5).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
			if idx >= 0 {
				actions(c, idx)
			}
		})
	})
}
