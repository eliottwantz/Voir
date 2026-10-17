package app

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

// The views of the sidebar.
const (
	sideFiles = iota
	sideHistory
	sideBranches
)

// view builds the window: the sidebar down its left, and the change beside it.
func (a *app) view(c *ui.Context) {
	a.applyThemeChoice(c)
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	a.flushMessages(c)
	tab := a.tab()
	if tab != nil {
		a.syncSide(tab)
	}
	switch {
	case a.pulls && tab != nil:
		a.pullsPage(c, tab)
	case tab == nil:
		ui.Column(c).Fill().Children(func() {
			bar := c.TitleBar()
			ui.Box(c).Height(max(bar.Height, titleBarHeight)).FillWidth().DragWindow().Shrink(0)
			a.welcomeView(c)
		})
	case a.sidebarShown:
		ui.Split(c, &a.store.data.SidebarW, func() { a.sidebar(c, tab) }, func() { a.mainColumn(c, tab) }).Fill()
	default:
		a.mainColumn(c, tab)
	}
	a.shortcuts(c)
	a.sheetView(c)
	a.dialogView(c)
	a.quickLaunchView(c)
	a.prefsView(c)
}

// sidebarBg is the sidebar's background: none where the window's material
// shows through, else a shade of the page.
func sidebarBg(c *ui.Context) ui.Color {
	if c.Vibrancy() {
		return ui.Transparent
	}
	t := c.Theme()
	return t.Background.Mix(t.Text, 0.035)
}

// mainColumn is everything right of the sidebar: the header, a strip of
// tabs when several repositories are open, and the view.
func (a *app) mainColumn(c *ui.Context, tab *repoTab) {
	t := c.Theme()
	ui.Column(c).Fill().MinWidth(0).Background(t.Background).Children(func() {
		tab.header(c)
		a.tabStrip(c)
		tab.banner(c)
		if tab.err != nil && tab.status == nil {
			emptyPanel(c, "Unable to read the repository", errorText(tab.err), nil)
			return
		}
		switch a.sideTab {
		case sideBranches:
			tab.commitList(c)
		default:
			// History and Files show the same change: of the commit, or of the
			// uncommitted changes.
			if tab.mode == modeChanges {
				tab.changesView(c)
			} else {
				tab.reviewView(c)
			}
		}
	})
	if tab.jump != "" {
		h := tab.jump
		tab.jump = ""
		tab.jumpTo(h)
	}
}

// header is the bar along the top of the change: the repository and the
// commit shown, and the commands.
func (tab *repoTab) header(c *ui.Context) {
	t := c.Theme()
	a := tab.app
	bar := c.TitleBar()
	left := float32(16)
	if !a.sidebarShown {
		left = bar.Left + 8
	}
	idle := tab.busy == ""
	ui.Row(c).Height(max(bar.Height, titleBarHeight)).Padding(0, bar.Right+12, 0, left).Gap(8).Shrink(0).DragWindow().AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
		if !a.sidebarShown {
			if iconButton(c, iconSidebar, "Show sidebar (⌘B)").Clicked() {
				a.sidebarShown = true
			}
		}
		// The repository: its name and where it is, which opens the others.
		repo := ui.ButtonBase(c).Padding(3, 6).Radius(7).Shrink(1).MinWidth(0).Justify(ui.Start).Label("Repository " + tab.name).Tooltip(tab.repo.Root)
		if repo.Hovered() {
			repo.Background(hoverBg)
		}
		repo.Menu(func(m *ui.Menu) { a.repoMenu(m) })
		repo.Children(func() {
			ui.Column(c).Gap(1).MinWidth(0).Children(func() {
				ui.Text(c, tab.name).FontSize(13).Bold().SingleLine()
				ui.Text(c, abbreviateHome(filepath.Dir(tab.repo.Root))).FontSize(11).TextColor(t.TextMuted).SingleLine()
			})
		})
		tab.headChip(c)
		if a.showsCommit(tab) {
			ui.Row(c).Gap(5).Padding(3, 9).Radius(12).Background(subtleBg).Border(1, hairline).AlignItems(ui.Center).Shrink(0).Tooltip(tab.commit.Hash).Children(func() {
				ui.Icon(c, iconCommit).FontSize(12).TextColor(refColor)
				ui.Text(c, tab.commit.Short()).Font(mono).FontSize(12).FontWeight(600).TextColor(refColor)
			})
		}
		ui.Spacer(c)
		if tab.busy != "" {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Spinner(c).Label(tab.busy)
				ui.Text(c, tab.busy+"…").FontSize(12).TextColor(t.TextMuted)
				if tab.cancel != nil && ui.Button(c, "Stop").Clicked() {
					tab.stop()
				}
			})
		} else if tab.loading && !tab.loaded {
			ui.Spinner(c).Label("Loading")
		}
		if iconButton(c, iconSearch, "Search commits (⌘F)").Clicked() {
			tab.focusSearch()
		}
		sep := func() { ui.Box(c).Width(1).Height(20).Background(hairline).Margin(0, 4).Shrink(0) }
		sep()
		if iconButton(c, iconFetch, "Fetch").Disabled(!idle).Clicked() {
			tab.fetch("")
		}
		pull := iconButton(c, iconPull, "Pull").Disabled(!idle)
		if pull.Clicked() {
			tab.pullCurrent(false)
		}
		pull.Menu(func(m *ui.Menu) {
			if m.Item("Pull…").Chosen() {
				a.pullDialog(tab)
			}
			if m.Item("Pull with Rebase").Chosen() {
				tab.pullCurrent(true)
			}
		})
		if iconButton(c, iconPush, "Push").Disabled(!idle).Clicked() {
			tab.pushCurrent()
		}
		sep()
		if iconButton(c, iconBranch, "Branch").Clicked() {
			a.newBranchDialog(tab, "")
		}
		if iconButton(c, iconStash, "Stash").Disabled(!tab.hasChanges()).Clicked() {
			a.stashDialog(tab)
		}
		if iconButton(c, iconTag, "Tag").Clicked() {
			a.newTagDialog(tab, "")
		}
		if tab.gh != nil {
			if iconButton(c, iconPRCreate, "Create Pull Request…").Clicked() {
				a.createPRDialog(tab, "")
			}
			if iconButton(c, iconPR, "Pull Requests (⌘4)").Clicked() {
				a.showPulls()
			}
		}
		sep()
		if iconButton(c, iconRefresh, "Refresh").Clicked() {
			tab.reload()
		}
		tab.layoutControl(c)
	})
}

// headChip shows the branch checked out, and how far it is from its remote.
func (tab *repoTab) headChip(c *ui.Context) {
	t := c.Theme()
	if tab.status == nil {
		return
	}
	ui.Row(c).Gap(6).Padding(3, 9).Radius(12).Background(subtleBg).Border(1, hairline).AlignItems(ui.Center).Shrink(0).Tooltip("Branch " + tab.headBranchName()).Children(func() {
		ui.Icon(c, iconBranch).FontSize(12).TextColor(t.TextMuted)
		ui.Text(c, tab.headBranchName()).FontSize(12).FontWeight(600).SingleLine().MaxWidth(200)
		if tab.status.Ahead > 0 {
			ui.Textf(c, "↑%d", tab.status.Ahead).FontSize(11).TextColor(t.Success)
		}
		if tab.status.Behind > 0 {
			ui.Textf(c, "↓%d", tab.status.Behind).FontSize(11).TextColor(t.Warning)
		}
	})
}

// layoutControl switches the diffs between side by side and unified.
func (tab *repoTab) layoutControl(c *ui.Context) {
	t := c.Theme()
	s := tab.app.store.settings()
	choice := 0
	if s.DiffLayout == "unified" {
		choice = 1
	}
	seg := ui.SegmentedBase(c, &choice, 2)
	seg.Track.Padding(2).Gap(2).Radius(8).Background(hoverBg).Label("Diff layout").Shrink(0).Margin(0, 0, 0, 4).Children(func() {
		for i, it := range []struct {
			icon *ui.SVG
			name string
		}{{iconSplit, "Side by side"}, {iconUnified, "Unified"}} {
			sg := seg.Segment(i).Size(30, 24).Radius(6).Center().Label(it.name).Tooltip(it.name).TextColor(t.TextMuted)
			if i == choice {
				sg.Background(t.Background).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.14)).TextColor(t.Text)
			}
			sg.Children(func() { ui.Icon(c, it.icon).FontSize(15) })
		}
	})
	if seg.Track.Changed() {
		layout := map[int]string{0: "split", 1: "unified"}[choice]
		if layout != s.DiffLayout {
			tab.app.store.update(func(s *Settings) { s.DiffLayout = layout })
			for _, tb := range tab.app.tabs {
				tb.resetDiffs()
			}
		}
	}
}

// repoMenu opens another repository: those in tabs, and the recent ones.
// Worktrees are tabs of their own, but not repositories to choose from.
func (a *app) repoMenu(m *ui.Menu) {
	for i, tb := range a.tabs {
		i := i
		if tb.linked() {
			continue
		}
		if m.Item(tb.name).Checked(i == a.cur).Chosen() {
			a.cur = i
			a.saveTabs()
		}
	}
	m.Separator()
	if m.Item("Open Repository…").Chosen() {
		a.browseForRepo()
	}
	if m.Item("Quick Launch…").Chosen() {
		a.quick.open = true
	}
	if m.Item("Clone…").Chosen() {
		a.cloneDialog()
	}
	if m.Item("Create New Local Repository…").Chosen() {
		a.initDialog()
	}
	m.Separator()
	m.Submenu("Open Recent", func(m *ui.Menu) {
		for _, root := range a.store.recent() {
			root := root
			if m.Item(abbreviateHome(root)).Chosen() {
				a.openRepoReporting(root)
			}
		}
	})
}

// tabStrip lists the repositories and worktrees open.
func (a *app) tabStrip(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Height(34).Padding(0, 10).Gap(4).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
		closing := -1
		for i, tab := range a.tabs {
			active := i == a.cur
			chip := ui.Row(c).Key(tab.repo.Root).Height(26).Padding(0, 6, 0, 10).Gap(6).Radius(7).MinWidth(90).MaxWidth(200).Shrink(1).AlignItems(ui.Center)
			if active {
				chip.Background(t.Surface).Border(1, hairline)
			} else if chip.Hovered() {
				chip.Background(hoverBg)
			}
			chip.Role(ui.RoleButton).Label(a.tabTitle(tab)).Tooltip(tab.repo.Root)
			if chip.Clicked() && a.cur != i {
				a.cur = i
				a.saveTabs()
			}
			chip.ContextMenu(func(m *ui.Menu) { a.tabMenu(m, i) })
			chip.Children(func() {
				glyph := iconFork
				if tab.linked() {
					glyph = iconWorktree
				}
				ui.Icon(c, glyph).FontSize(13).TextColor(t.TextMuted)
				title := tab.name
				if tab.hasChanges() {
					title += " *"
				}
				lbl := ui.Text(c, title).FontSize(12.5).SingleLine().MinWidth(0).Shrink(1)
				if !active {
					lbl.TextColor(t.TextMuted)
				}
				if where := a.tabWhere(tab); where != "" {
					ui.Text(c, where).FontSize(11.5).SingleLine().MinWidth(0).Shrink(2).TextColor(t.TextMuted)
				}
				ui.Spacer(c)
				if ui.ButtonBase(c).Size(18, 18).Radius(5).Center().TextColor(t.TextMuted).Label("Close tab").Tooltip("Close tab").Children(func() {
					ui.Icon(c, iconClose).FontSize(11)
				}).Clicked() {
					closing = i
				}
			})
		}
		if closing >= 0 {
			a.closeTab(closing)
		}
		if ui.ButtonBase(c).Size(26, 26).Radius(7).Center().TextColor(t.TextMuted).Label("New tab").Tooltip("New tab (⌘T)").Children(func() {
			ui.Icon(c, iconPlus).FontSize(14)
		}).Clicked() {
			a.newTab()
		}
	})
}

// tabWhere tells tabs of the same repository apart: a worktree by its
// branch, a repository named like another by its folder.
func (a *app) tabWhere(tab *repoTab) string {
	same := false
	for _, o := range a.tabs {
		if o != tab && o.name == tab.name {
			same = true
		}
	}
	switch {
	case tab.linked() || same:
		if tab.status != nil && tab.status.Branch != "" {
			return tab.status.Branch
		}
		if tab.linked() {
			return "detached"
		}
		return abbreviateHome(filepath.Dir(tab.repo.Root))
	}
	return ""
}

// tabTitle is the accessible name of a tab.
func (a *app) tabTitle(tab *repoTab) string {
	if w := a.tabWhere(tab); w != "" {
		return tab.name + " " + w
	}
	return tab.name
}

// tabMenu is the context menu of tab i.
func (a *app) tabMenu(m *ui.Menu, i int) {
	tab := a.tabs[i]
	if m.Item("Close Tab").Chosen() {
		a.closeTab(i)
		return
	}
	if m.Item("Close Other Tabs").Disabled(len(a.tabs) < 2).Chosen() {
		a.closeTabs(func(j int) bool { return j == i })
		a.cur = 0
		a.saveTabs()
		return
	}
	if m.Item("Close Tabs to the Right").Disabled(i >= len(a.tabs)-1).Chosen() {
		a.closeTabs(func(j int) bool { return j <= i })
		return
	}
	m.Separator()
	if m.Item("Move Left").Disabled(i == 0).Chosen() {
		a.moveTab(i, -1)
	}
	if m.Item("Move Right").Disabled(i >= len(a.tabs)-1).Chosen() {
		a.moveTab(i, 1)
	}
	if len(tab.trees) > 1 {
		m.Separator()
		m.Submenu("Worktrees", func(m *ui.Menu) {
			for _, w := range tab.trees {
				label := filepath.Base(w.Path)
				if w.Branch != "" {
					label += " (" + w.Branch + ")"
				}
				if m.Item(label).Chosen() {
					a.openRepoReporting(w.Path)
				}
			}
		})
	}
	m.Separator()
	if m.Item("Show in Finder").Chosen() {
		revealInFinder(tab.repo.Root)
	}
	if m.Item("Open in Terminal").Chosen() {
		openInTerminal(tab.repo.Root)
	}
	if m.Item("Copy Path").Chosen() {
		copyText(tab.repo.Root)
	}
}

// focusSearch goes to the history's filter, which takes the keys.
func (tab *repoTab) focusSearch() {
	tab.app.showSide(sideHistory)
	tab.searchFocus = true
}

// ---- the sidebar ----

func (a *app) sidebar(c *ui.Context, tab *repoTab) {
	t := c.Theme()
	bar := c.TitleBar()
	mac := runtime.GOOS == "darwin"
	left, right := float32(10), float32(10)
	if mac {
		left = max(bar.Left+6, 10)
	} else {
		right = max(bar.Right+6, 10)
	}
	type item struct {
		icon *ui.SVG
		name string
		tab  int
	}
	items := []item{
		{iconTree, "Files (⌘1)", sideFiles},
		{iconHistory, "History (⌘2)", sideHistory},
		{iconBranch, "Branches (⌘3)", sideBranches},
	}
	idx := slices.IndexFunc(items, func(it item) bool { return it.tab == a.sideTab })
	if idx < 0 {
		idx = 1
		a.sideTab = sideHistory
	}
	ui.Column(c).Fill().MinWidth(0).Background(sidebarBg(c)).Children(func() {
		ui.Row(c).Height(max(bar.Height, titleBarHeight)).Padding(0, right, 0, left).Gap(6).Shrink(0).DragWindow().AlignItems(ui.Center).Children(func() {
			if mac {
				ui.Spacer(c)
			}
			seg := ui.SegmentedBase(c, &idx, len(items))
			seg.Track.Padding(2).Gap(2).Radius(8).Background(hoverBg).Label("Sidebar").Shrink(0).Children(func() {
				for i, it := range items {
					// The files are those of a commit, or of a pull request.
					off := it.tab == sideFiles && !tab.hasFiles()
					sg := seg.Segment(i).Size(30, 24).Radius(6).Center().Label(it.name).Tooltip(it.name).TextColor(t.TextMuted).Disabled(off)
					if off {
						sg.Opacity(0.4)
					}
					if i == idx {
						sg.Background(t.Background).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.14)).TextColor(t.Text)
					}
					sg.Children(func() { ui.Icon(c, it.icon).FontSize(15) })
				}
			})
			if seg.Track.Changed() {
				a.showSide(items[idx].tab)
			}
			if iconButton(c, iconSidebar, "Hide sidebar (⌘B)").Clicked() {
				a.sidebarShown = false
			}
			if !mac {
				ui.Spacer(c)
			}
		})
		switch a.sideTab {
		case sideFiles:
			tab.filesSidebar(c)
		case sideBranches:
			tab.sidebarView(c)
		default:
			tab.historySidebar(c)
		}
	})
}

// searchBox is a field that filters a list, as the search fields of macOS: a
// magnifying glass, the text, and a button that clears it. It returns the
// input.
func searchBox(c *ui.Context, q *string, placeholder string) ui.Element {
	t := c.Theme()
	var in ui.Element
	ui.Row(c).Padding(0, 10, 8).Shrink(0).Children(func() {
		ui.Row(c).Height(28).Padding(0, 6, 0, 8).Gap(6).Radius(7).Background(hoverBg).Grow(1).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconSearch).FontSize(13).TextColor(t.TextMuted)
			in = ui.TextInputBase(c, q).Placeholder(placeholder).Label(placeholder).FontSize(13).Grow(1).MinWidth(0)
			if *q != "" {
				if ui.ButtonBase(c).Size(18, 18).Radius(9).Center().TextColor(t.TextMuted).Label("Clear").Tooltip("Clear").Children(func() {
					ui.Icon(c, iconClose).FontSize(11)
				}).Clicked() {
					*q = ""
				}
			}
		})
	})
	return in
}

// filterField is the field above a list of the sidebar.
func filterField(c *ui.Context, q *string, placeholder string) ui.Element {
	return searchBox(c, q, placeholder)
}

// historySidebar is the commits, compactly: hash, subject, author and age.
func (tab *repoTab) historySidebar(c *ui.Context) {
	t := c.Theme()
	now := c.Now()
	q := tab.filter
	in := searchBox(c, &q, "Filter history")
	if tab.searchFocus {
		in.Focus()
		tab.searchFocus = false
	}
	if q != tab.filter {
		tab.search(q)
		if q != "" {
			tab.leaveSpecialView()
		}
		tab.sel, tab.selHash = -1, ""
	}
	n := tab.rowCount()
	if n == 0 {
		msg := "No commits yet"
		if tab.filter != "" {
			msg = "No commit matches “" + strings.TrimSpace(tab.filter) + "”"
			if tab.searched != strings.TrimSpace(tab.filter) {
				msg = "Searching…"
			}
		}
		ui.Text(c, msg).FontSize(12).TextColor(t.TextMuted).Padding(16).TextAlign(ui.Center).FillWidth()
		return
	}
	tab.sideList.Selected = &tab.sel
	tab.sideList.Key = func(i int) any { return tab.rowKey(i) }
	tab.sideList.Label = func(i int) string {
		if cm := tab.rowCommit(i); cm != nil {
			return cm.Subject
		}
		return "Uncommitted changes"
	}
	list := ui.List(c, &tab.sideList, n, func(i int) {
		cm := tab.rowCommit(i)
		// The list paints the row chosen in the accent color.
		onAccent := tab.sel == i
		hash, sub, muted := refColor, t.Text, t.TextMuted
		if onAccent {
			hash, sub, muted = t.AccentText, t.AccentText, t.AccentText.Alpha(0.75)
		}
		row := ui.Row(c).Gap(10).Padding(6, 10).AlignItems(ui.Start).Radius(7).Margin(0, 6)
		if cm == nil {
			n := 0
			if tab.status != nil {
				n = len(tab.status.Entries)
			}
			row.Label("Uncommitted changes")
			// A click on the row chosen already is no new choice of the list,
			// yet it goes to the files.
			if row.Clicked() {
				tab.selectRow(i)
			}
			row.Children(func() {
				ui.Text(c, "local").Font(mono).FontSize(11.5).TextColor(hash).Width(54).Shrink(0)
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, "Uncommitted changes").FontSize(13).TextColor(sub).SingleLine()
					ui.Text(c, plural(n, "change", "changes")).FontSize(11).TextColor(muted).SingleLine()
				})
			})
			return
		}
		row.ContextMenu(func(m *ui.Menu) { tab.commitMenu(m, cm) })
		row.Children(func() {
			ui.Text(c, cm.Short()).Font(mono).FontSize(11.5).TextColor(hash).Width(54).Shrink(0)
			ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, cm.Subject).FontSize(13).TextColor(sub).SingleLine()
				ui.Text(c, cm.Author+" · "+relativeTime(now, cm.AuthorTime)).FontSize(11).TextColor(muted).SingleLine()
			})
		})
	}).Grow(1).MinHeight(0).Label("Commits")
	if list.Changed() {
		tab.selectRow(tab.sel)
	}
	if tab.scrollSel {
		tab.scrollSel = false
		if tab.sel >= 0 {
			tab.sideList.ScrollTo(tab.sel, ui.Center)
		}
	}
	if _, last := tab.sideList.Visible(); last >= n-40 && tab.hasMore {
		tab.loadMore()
	}
}

// filesSidebar is the files of the change shown: of the commit; for the
// uncommitted changes, those to stage, and the message to commit them with.
func (tab *repoTab) filesSidebar(c *ui.Context) {
	t := c.Theme()
	empty := func(msg string) {
		ui.Text(c, msg).FontSize(12).TextColor(t.TextMuted).Padding(16).TextAlign(ui.Center).FillWidth()
	}
	if tab.mode == modeChanges {
		tab.changesSidebar(c)
		return
	}
	rv := tab.rv
	if rv == nil || len(rv.files) == 0 {
		if rv != nil && rv.loading {
			empty("Loading…")
		} else {
			empty("No changed files")
		}
		return
	}
	tab.reviewFiles(c, rv)
}

// reviewFiles is the files of a review as a tree: choosing one scrolls the
// surface to its card.
func (tab *repoTab) reviewFiles(c *ui.Context, rv *review) {
	files := make([]sideFile, len(rv.files))
	for i, f := range rv.files {
		files[i] = sideFile{path: f.path, status: f.status, added: f.added, deleted: f.deleted, binary: f.binary, viewed: f.viewed, group: f.group, comments: rv.commentsIn(f)}
	}
	tab.filesTree(c, &rv.side, &rv.filter, rv.closedDirs, files, rv.current, rv.reveal)
}

var _ = git.EmptyTree
