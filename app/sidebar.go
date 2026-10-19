package app

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

// open returns the open/closed state of a sidebar section.
func (tab *repoTab) open(key string) *bool {
	b, ok := tab.sideOpen[key]
	if !ok {
		// Tags, and each remote in Remotes, start collapsed.
		on := key != "tags" && !strings.HasPrefix(key, "remote:")
		b = &on
		tab.sideOpen[key] = b
	}
	return b
}

// sidebarView is the repository: the two views on top, then its branches,
// remotes, tags, stashes, submodules and work trees as a source list.
func (tab *repoTab) sidebarView(c *ui.Context) {
	var locals, remotes, tags []git.Ref
	for _, r := range tab.refs {
		switch r.Kind {
		case git.LocalBranch:
			locals = append(locals, r)
		case git.RemoteBranch:
			remotes = append(remotes, r)
		case git.Tag:
			tags = append(tags, r)
		}
	}
	slices.SortFunc(tags, func(a, b git.Ref) int { return strings.Compare(b.Name, a.Name) })
	byRemote := map[string][]git.Ref{}
	for _, r := range remotes {
		byRemote[r.Remote()] = append(byRemote[r.Remote()], r)
	}

	prev := tab.side
	tab.sideHit, tab.sideCmd = "", false
	var sb ui.Element
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
		tab.viewTabs(c)
		sb = ui.Sidebar(c, &tab.side, func() {
			ui.SidebarSection(c, "Worktrees", tab.open("trees"), func() {
				for _, w := range tab.trees {
					label := w.Path
					if i := strings.LastIndexByte(label, '/'); i >= 0 {
						label = label[i+1:]
					}
					if w.Branch != "" {
						label += " (" + w.Branch + ")"
					}
					tab.worktreeItem(c, w, label)
				}
			})
			ui.SidebarSection(c, "Branches", tab.open("local"), func() {
				for _, b := range locals {
					tab.branchItem(c, b)
				}
			})
			ui.SidebarSection(c, "Remotes", tab.open("remote"), func() {
				for _, rem := range tab.remotes {
					key := "remote:" + rem.Name
					// A section is not indented under its parent: pad it.
					ui.Column(c).Key(key).Padding(0, 0, 0, 12).Children(func() {
						ui.SidebarSection(c, rem.Name, tab.open(key), func() {
							for _, b := range byRemote[rem.Name] {
								tab.remoteItem(c, b)
							}
						})
					})
				}
			})
			ui.SidebarSection(c, "Tags", tab.open("tags"), func() {
				for _, tg := range tags {
					tab.tagItem(c, tg)
				}
			})
			ui.SidebarSection(c, "Stashes", tab.open("stashes"), func() {
				for _, s := range tab.stashes {
					tab.stashItem(c, s)
				}
			})
			ui.SidebarSection(c, "Submodules", tab.open("subs"), func() {
				for _, s := range tab.subs {
					ui.SidebarItem(c, "sub:"+s.Path, iconSubmodule, s.Path).Tooltip(s.Path)
				}
			})
		}).Label("Repository")
		sb.Grow(1).MinHeight(0).MinWidth(0).Background(sidebarBg(c))
	})
	switch {
	case tab.sideHit != "" && tab.sideCmd:
		// Cmd-click compares the item with the commit chosen: the choice of the
		// sidebar stays where it was.
		tab.side = prev
		tab.compareRef(tab.sideHit)
	case sb.Changed() && tab.side != prev, tab.sideHit != "" && tab.comparing():
		tab.sidebarChosen(tab.side)
	}
}

// sideItem notes a click on an item of the sidebar, and marks the items
// compared.
func (tab *repoTab) sideItem(c *ui.Context, item ui.Element, id string) {
	if item.Clicked() {
		tab.sideHit, tab.sideCmd = id, item.ClickModifiers()&ui.Cmd != 0
	}
	if tab.cmpSide[id] {
		item.Background(c.Theme().Accent.Alpha(0.22))
	}
}

// refTarget is the commit a branch, tag or stash of the sidebar is at.
func (tab *repoTab) refTarget(id string) (string, bool) {
	switch {
	case strings.HasPrefix(id, "b:"), strings.HasPrefix(id, "r:"), strings.HasPrefix(id, "t:"):
		kind := map[byte]git.RefKind{'b': git.LocalBranch, 'r': git.RemoteBranch, 't': git.Tag}[id[0]]
		for _, r := range tab.refs {
			if r.Kind == kind && r.Name == id[2:] {
				return r.Target, true
			}
		}
	case strings.HasPrefix(id, "s:"):
		n, _ := strconv.Atoi(id[2:])
		if n < len(tab.stashes) {
			return tab.stashes[n].Hash, true
		}
	}
	return "", false
}

// viewTabs are the two views of a repository: its uncommitted changes, with
// the number of files to stage, and every commit of its branches.
func (tab *repoTab) viewTabs(c *ui.Context) {
	n := 0
	if tab.status != nil {
		n = len(tab.status.Entries)
	}
	ui.Column(c).Padding(8, 10, 6).Gap(2).Shrink(0).Children(func() {
		tab.viewTab(c, "Local Changes", iconFileDiff, tab.onWip, n, func() { tab.showChanges() })
		tab.viewTab(c, "All Commits", iconHistory, !tab.onWip, 0, func() { tab.showHistory() })
	})
	ui.Box(c).Height(1).Background(hairline).Shrink(0)
}

// viewTab is one of the two views: the current one is lit.
func (tab *repoTab) viewTab(c *ui.Context, name string, svg *ui.SVG, on bool, count int, choose func()) {
	t := c.Theme()
	row := ui.Row(c).Key("view:"+name).Height(28).Padding(0, 8).Gap(8).Radius(7).AlignItems(ui.Center).MinWidth(0)
	switch {
	case on:
		row.Background(t.Surface).Border(1, hairline)
	case row.Hovered():
		row.Background(hoverBg)
	}
	row.Role(ui.RoleButton).Label(name)
	if row.Clicked() && !on {
		choose()
	}
	row.Children(func() {
		col := t.TextMuted
		if on {
			col = t.Text
		}
		ui.Icon(c, svg).FontSize(15).TextColor(col).Shrink(0)
		ui.Text(c, name).FontSize(13).FontWeight(500).TextColor(col).SingleLine().Grow(1).MinWidth(0)
		if count > 0 {
			ui.Textf(c, "(%d)", count).Font(mono).FontSize(11.5).TextColor(t.TextMuted).Shrink(0)
		}
	})
}

func (tab *repoTab) sidebarChosen(id string) {
	if strings.HasPrefix(id, "s:") {
		tab.showStash(id)
		return
	}
	if hash, ok := tab.refTarget(id); ok {
		tab.jumpTo(hash)
	}
}

// showStash shows the changes of a stash. Only the latest stash is in the
// log, so the others (and all of them when hidden) are shown from their own
// commit rather than from a row of the list.
func (tab *repoTab) showStash(id string) {
	n, _ := strconv.Atoi(id[2:])
	if n < 0 || n >= len(tab.stashes) {
		return
	}
	s := tab.stashes[n]
	if tab.commitRow(s.Hash) >= 0 {
		tab.jumpTo(s.Hash)
		return
	}
	tab.leaveSpecialView()
	tab.onWip = false
	tab.cmpGen++
	tab.selHash = s.Hash
	tab.pick(s.Hash)
	parents := s.Parents
	if len(parents) > 1 {
		parents = parents[:1]
	}
	tab.setCommit(&git.Commit{Hash: s.Hash, Parents: parents, Author: "stash", CommitTime: s.Time, AuthorTime: s.Time, Subject: s.Message})
}

func (tab *repoTab) branchItem(c *ui.Context, b git.Ref) {
	t := c.Theme()
	label := b.Name
	item := ui.SidebarItem(c, "b:"+b.Name, iconBranch, label)
	tab.sideItem(c, item, "b:"+b.Name)
	if b.Head {
		item.FontWeight(700)
	}
	if item.DoubleClicked() && !b.Head {
		tab.checkout(b.Name)
	}
	item.Children(func() {
		if b.Head {
			ui.Icon(c, iconCheck).FontSize(12).TextColor(t.Accent)
		}
		if b.Ahead > 0 {
			ui.Textf(c, "↑%d", b.Ahead).FontSize(11).TextColor(t.Success)
		}
		if b.Behind > 0 {
			ui.Textf(c, "↓%d", b.Behind).FontSize(11).TextColor(t.Warning)
		}
	})
	item.ContextMenu(func(m *ui.Menu) { tab.branchMenu(m, b) })
}

func (tab *repoTab) branchMenu(m *ui.Menu, b git.Ref) {
	a := tab.app
	cur := tab.currentBranch()
	if m.Item("Checkout").Disabled(b.Head).Chosen() {
		tab.checkout(b.Name)
	}
	if m.Item("Create Branch from Here…").Chosen() {
		a.newBranchDialog(tab, b.Name)
	}
	m.Separator()
	if cur != nil {
		if m.Item(fmt.Sprintf("Merge “%s” into “%s”…", b.Name, cur.Name)).Disabled(b.Head).Chosen() {
			a.mergeDialog(tab, b.Name)
		}
		if m.Item(fmt.Sprintf("Rebase “%s” onto “%s”", cur.Name, b.Name)).Disabled(b.Head).Chosen() {
			tab.do("Rebase", func(r *git.Repo) error { return r.Rebase(b.Name) })
		}
		if m.Item(fmt.Sprintf("Interactive Rebase “%s” onto “%s”…", cur.Name, b.Name)).Disabled(b.Head).Chosen() {
			a.rebaseDialog(tab, b.Name, b.Name)
		}
	}
	m.Separator()
	if m.Item("Pull").Disabled(!b.Head || b.Upstream == "").Chosen() {
		tab.pullCurrent(false)
	}
	if m.Item("Push…").Chosen() {
		a.pushDialog(tab, b.Name)
	}
	if m.Item("Create Pull Request…").Disabled(tab.gh == nil || !b.Head && false).Chosen() {
		a.createPRDialog(tab, b.Name)
	}
	m.Separator()
	if m.Item("Rename…").Chosen() {
		a.renameBranchDialog(tab, b.Name)
	}
	if m.Item("Delete…").Disabled(b.Head).Chosen() {
		a.confirm("Delete branch “"+b.Name+"”?", "Commits only on this branch can be lost.", "Delete", func() error {
			return tab.repo.DeleteBranch(b.Name, true)
		}, nil)
	}
	m.Separator()
	if m.Item("Copy Branch Name").Chosen() {
		copyText(b.Name)
	}
}

func (tab *repoTab) remoteItem(c *ui.Context, b git.Ref) {
	_, short, _ := strings.Cut(b.Name, "/")
	item := ui.SidebarItem(c, "r:"+b.Name, iconBranch, short)
	tab.sideItem(c, item, "r:"+b.Name)
	if item.DoubleClicked() {
		tab.checkoutRemote(b.Name)
	}
	item.ContextMenu(func(m *ui.Menu) {
		if m.Item("Checkout…").Chosen() {
			tab.checkoutRemote(b.Name)
		}
		if m.Item("Create Branch from Here…").Chosen() {
			tab.app.newBranchDialog(tab, b.Name)
		}
		m.Separator()
		if m.Item("Merge into Current Branch…").Chosen() {
			tab.app.mergeDialog(tab, b.Name)
		}
		m.Separator()
		if m.Item("Delete…").Chosen() {
			rem, name, _ := strings.Cut(b.Name, "/")
			tab.app.confirm("Delete “"+b.Name+"” on the remote?", "The branch is deleted on "+rem+", for everyone.", "Delete", func() error {
				return tab.repo.DeleteRemoteBranch(context.Background(), rem, name)
			}, nil)
		}
		if m.Item("Copy Branch Name").Chosen() {
			copyText(b.Name)
		}
	})
}

func (tab *repoTab) tagItem(c *ui.Context, tg git.Ref) {
	item := ui.SidebarItem(c, "t:"+tg.Name, iconTag, tg.Name)
	tab.sideItem(c, item, "t:"+tg.Name)
	if item.DoubleClicked() {
		tab.checkout(tg.Name)
	}
	item.ContextMenu(func(m *ui.Menu) {
		if m.Item("Checkout").Chosen() {
			tab.checkout(tg.Name)
		}
		if m.Item("Create Branch from Here…").Chosen() {
			tab.app.newBranchDialog(tab, tg.Name)
		}
		m.Separator()
		if m.Item("Push to Remote…").Disabled(len(tab.remotes) == 0).Chosen() {
			tab.app.pushTagDialog(tab, tg.Name)
		}
		if m.Item("Delete…").Chosen() {
			tab.app.confirm("Delete tag “"+tg.Name+"”?", "The tag is only deleted locally.", "Delete", func() error {
				return tab.repo.DeleteTag(tg.Name)
			}, nil)
		}
		if m.Item("Copy Tag Name").Chosen() {
			copyText(tg.Name)
		}
	})
}

func (tab *repoTab) stashItem(c *ui.Context, s git.Stash) {
	label := s.Message
	if i := strings.Index(label, ": "); i >= 0 {
		label = label[i+2:]
	}
	item := ui.SidebarItem(c, "s:"+strconv.Itoa(s.Index), iconStash, label).Tooltip(s.Name + " — " + s.Message)
	tab.sideItem(c, item, "s:"+strconv.Itoa(s.Index))
	item.ContextMenu(func(m *ui.Menu) {
		if m.Item("Apply…").Chosen() {
			tab.do("Apply stash", func(r *git.Repo) error { return r.StashApply(s.Name) })
		}
		if m.Item("Pop").Chosen() {
			tab.do("Pop stash", func(r *git.Repo) error { return r.StashPop(s.Name) })
		}
		m.Separator()
		if m.Item("Delete…").Chosen() {
			tab.app.confirm("Delete this stash?", s.Message, "Delete", func() error { return tab.repo.StashDrop(s.Name) }, nil)
		}
	})
}

func (tab *repoTab) worktreeItem(c *ui.Context, w git.Worktree, label string) {
	item := ui.SidebarItem(c, "w:"+w.Path, iconWorktree, label).Tooltip(w.Path)
	if item.DoubleClicked() {
		tab.app.openRepoReporting(w.Path)
	}
	item.ContextMenu(func(m *ui.Menu) {
		if m.Item("Open in Tab").Chosen() {
			tab.app.openRepoReporting(w.Path)
		}
		if m.Item("Remove…").Disabled(w.Main).Chosen() {
			tab.app.confirm("Remove this worktree?", w.Path, "Remove", func() error { return tab.repo.RemoveWorktree(w.Path, false) }, nil)
		}
	})
}
