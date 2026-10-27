package app

import (
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

// view builds the window: the sidebar down its left, and beside it the graph
// of commits with the commit chosen under it.
func (a *app) view(c *ui.Context) {
	a.applyThemeChoice(c)
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	a.flushMessages(c)
	tab := a.tab()
	switch {
	case a.pulls:
		a.pullsPage(c)
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
		if tab.onWip {
			// Local Changes are not part of the graph: they fill the area.
			tab.panel(c)
			return
		}
		if a.panelCollapsed {
			ui.Column(c).Fill().MinHeight(0).Children(func() {
				ui.Column(c).Fill().MinHeight(0).Children(func() { tab.commitList(c) })
				tab.cv.tabsRow(c)
			})
			return
		}
		if a.panelMaximized {
			tab.panel(c)
			return
		}
		ui.SplitVertical(c, &a.store.data.ListH, func() { tab.commitList(c) }, func() { tab.panel(c) }).Fill().MinHeight(0)
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
	ui.Row(c).Height(max(bar.Height, titleBarHeight)).Padding(0, bar.Right+12, 0, left).Gap(8).Shrink(0).DragWindow().AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
		if !a.sidebarShown {
			if iconButton(c, iconSidebar, "Show sidebar"+a.hint("toggle-sidebar")).Clicked() {
				a.sidebarShown = true
			}
			if tab.gh != nil {
				a.pullsButton(c)
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
		ui.Spacer(c)
		if tab.busy != "" && !tab.remoteBusy() {
			spinner(c, tab.busy)
		} else if tab.loading && !tab.loaded {
			spinner(c, "Loading")
		}
		if iconButton(c, iconSearch, "Search commits"+a.hint("find")).Clicked() {
			tab.focusSearch()
		}
		sep := func() { ui.Box(c).Width(1).Height(20).Background(hairline).Margin(0, 4).Shrink(0) }
		sep()
		tab.remoteButton(c, iconFetch, "Fetch", func() { tab.fetch("") })
		pull := tab.remoteButton(c, iconPull, "Pull", func() { tab.pullCurrent(false) })
		pull.Menu(func(m *ui.Menu) {
			if m.Item("Pull…").Chosen() {
				a.pullDialog(tab)
			}
			if m.Item("Pull with Rebase").Chosen() {
				tab.pullCurrent(true)
			}
		})
		push := tab.remoteButton(c, iconPush, "Push", func() { tab.pushCurrent(false) })
		push.Menu(func(m *ui.Menu) {
			if m.Item("Push").Chosen() {
				tab.pushCurrent(false)
			}
			if m.Item("Force Push (with lease)").Chosen() {
				tab.pushCurrent(true)
			}
		})
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
		}
		sep()
		a.openRepoButton(c, tab)
		if workingIconButton(c, iconRefresh, "Refresh"+a.hint("refresh"), tab.loading && tab.busy == "").Clicked() {
			a.refreshNow()
		}
		tab.layoutControl(c)
	})
}

// remoteButton is the button of a command that talks to the remote: a click
// runs it, and while it runs the button shows a loading indicator and a click
// stops it.
func (tab *repoTab) remoteButton(c *ui.Context, icon *ui.SVG, name string, run func()) ui.Element {
	if tab.busy != name {
		b := iconButton(c, icon, name).Disabled(tab.busy != "")
		if b.Clicked() {
			run()
		}
		return b
	}
	b := workingIconButton(c, icon, "Stop "+name, true).Disabled(tab.cancel == nil)
	if b.Clicked() {
		tab.stop()
	}
	return b
}

// remoteBusy tells that the command running has its own button in the bar.
func (tab *repoTab) remoteBusy() bool {
	return tab.busy == "Fetch" || tab.busy == "Pull" || tab.busy == "Push"
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
	if tab := a.tab(); tab != nil && !tab.linked() {
		pinned := a.store.isPinned(tab.repo.Root)
		label := "Pin Repository"
		if pinned {
			label = "Unpin Repository"
		}
		if m.Item(label).Disabled(!pinned && tab.gh == nil).Chosen() {
			a.togglePin(tab)
		}
	}
	if m.Item("Pin Other Repository…").Chosen() {
		a.browseForPin()
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
		closing, at := -1, 0
		var dropped *repoTab
		for i, tab := range a.tabs {
			active := i == a.cur
			chip := ui.Row(c.Key(tab.repo.Root)).Height(26).Padding(0, 6, 0, 10).Gap(6).Radius(7).MinWidth(90).MaxWidth(320).Shrink(1).AlignItems(ui.Center)
			if active {
				chip.Background(t.Surface).Border(1, hairline)
			} else if chip.Hovered() {
				chip.Background(hoverBg)
			}
			chip.Role(ui.RoleButton).Label("Tab " + a.tabTitle(tab)).Tooltip(tab.repo.Root)
			chip.Drag(tabDrag{tab})
			if d, ok := ui.DragOver[tabDrag](chip); ok && d.tab != tab {
				// The tab lands where this one is: after it when it comes
				// from the left, else before it.
				accent := t.Accent
				if slices.Index(a.tabs, d.tab) < i {
					chip.BorderColor(accent).BorderWidth(0, 2, 0, 0)
				} else {
					chip.BorderColor(accent).BorderWidth(0, 0, 0, 2)
				}
			}
			if d, ok := ui.Drop[tabDrag](chip); ok {
				dropped, at = d.tab, i
			}
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
				pinned := a.store.isPinned(tab.repo.Root)
				title := tab.name
				if tab.hasChanges() {
					title += " *"
				}
				lbl := ui.Text(c, title).FontSize(12.5).SingleLine().Shrink(0)
				if !active {
					lbl.TextColor(t.TextMuted)
				}
				if where := a.tabWhere(tab); where != "" {
					ui.Text(c, where).FontSize(11.5).SingleLine().MinWidth(0).Shrink(1).TextColor(t.TextMuted)
				}
				ui.Spacer(c)
				if pinned {
					ui.Icon(c, iconPinFilled).FontSize(11).TextColor(t.TextMuted)
				}
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
		if dropped != nil {
			a.dragTab(dropped, at)
		}
		if ui.ButtonBase(c).Size(26, 26).Radius(7).Center().TextColor(t.TextMuted).Label("New tab").Tooltip("New tab" + a.hint("new-tab")).Children(func() {
			ui.Icon(c, iconPlus).FontSize(14)
		}).Clicked() {
			a.newTab()
		}
	})
}

// tabDrag is a tab of the strip being dragged to another place.
type tabDrag struct{ tab *repoTab }

// dragTab puts tab at place to, the one shown staying shown.
func (a *app) dragTab(tab *repoTab, to int) {
	from := slices.Index(a.tabs, tab)
	if from < 0 || from == to || to < 0 || to >= len(a.tabs) {
		return
	}
	shown := a.tab()
	a.tabs = slices.Delete(a.tabs, from, from+1)
	a.tabs = slices.Insert(a.tabs, to, tab)
	a.cur = slices.Index(a.tabs, shown)
	a.saveTabs()
}

// tabWhere tells tabs of the same folder name apart by the name of the
// folder holding them, as in "Library (kiba)".
func (a *app) tabWhere(tab *repoTab) string {
	for _, o := range a.tabs {
		if o != tab && o.name == tab.name {
			return filepath.Base(filepath.Dir(tab.repo.Root))
		}
	}
	return ""
}

// tabTitle is the accessible name of a tab.
func (a *app) tabTitle(tab *repoTab) string {
	if w := a.tabWhere(tab); w != "" {
		return tab.name + " (" + w + ")"
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
	if !tab.linked() {
		pinned := a.store.isPinned(tab.repo.Root)
		label := "Pin Repository"
		if pinned {
			label = "Unpin Repository"
		}
		if m.Item(label).Disabled(!pinned && tab.gh == nil).Chosen() {
			a.togglePin(tab)
		}
		m.Separator()
	}
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

// focusSearch shows the graph's filter, which takes the keys.
func (tab *repoTab) focusSearch() {
	tab.app.pulls = false
	tab.showHistory()
	tab.searchOpen, tab.searchFocus = true, true
}

// searchStatus is how many commits the search found, for its bar.
func (tab *repoTab) searchStatus() string {
	q := strings.TrimSpace(tab.filter)
	switch {
	case q == "":
		return ""
	case tab.searched != q:
		return "Searching…"
	case tab.rowCount() == 0:
		return "No results"
	case tab.sel >= 0 && tab.sel < tab.rowCount():
		return fmt.Sprintf("%d of %d", tab.sel+1, tab.rowCount())
	}
	return plural(tab.rowCount(), "result", "results")
}

// stepSearch chooses the commit after or before the one chosen among those the
// search found, round the ends, and shows it.
func (tab *repoTab) stepSearch(d int) {
	n := tab.rowCount()
	if n == 0 {
		return
	}
	i := tab.sel + d
	if tab.sel < 0 && d < 0 {
		i = n - 1
	}
	i = (i%n + n) % n
	tab.selectRow(i)
	tab.list.ScrollTo(i, ui.Center)
	tab.scrollSel = true
}

// closeSearch hides the graph's filter and shows the whole log again.
func (tab *repoTab) closeSearch() {
	tab.searchOpen, tab.searchFocus = false, false
	if tab.filter != "" {
		tab.search("")
		tab.sel, tab.selHash = -1, ""
	}
}

// ---- the sidebar ----

func (a *app) sidebar(c *ui.Context, tab *repoTab) {
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Background(sidebarBg(c)).Children(func() {
		sidebarBar(c, func() {
			if iconButton(c, iconSidebar, "Hide sidebar"+a.hint("toggle-sidebar")).Clicked() {
				a.sidebarShown = false
			}
			if tab.gh != nil {
				a.pullsButton(c)
			}
		})
		tab.sidebarView(c)
	})
}

// sidebarBar is the bar along the top of a sidebar, in line with the window's
// buttons: the icons that toggle it sit at its right on macOS, at its left
// elsewhere.
func sidebarBar(c *ui.Context, icons func()) {
	bar := c.TitleBar()
	mac := runtime.GOOS == "darwin"
	left, right := float32(10), float32(10)
	if mac {
		left = max(bar.Left+6, 10)
	} else {
		right = max(bar.Right+6, 10)
	}
	ui.Row(c).Height(max(bar.Height, titleBarHeight)).Padding(0, right, 0, left).Gap(6).Shrink(0).DragWindow().AlignItems(ui.Center).Children(func() {
		if mac {
			ui.Spacer(c)
		}
		icons()
		if !mac {
			ui.Spacer(c)
		}
	})
}

// pullsButton is the icon that opens the pull requests page, beside the one
// that toggles the sidebar.
func (a *app) pullsButton(c *ui.Context) {
	if iconButton(c, iconPR, "Pull Requests").Clicked() {
		a.showPulls()
	}
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

// reviewFiles is the files of a review as a tree: choosing one scrolls the
// surface to its card.
func (tab *repoTab) reviewFiles(c *ui.Context, rv *review) {
	files := make([]sideFile, len(rv.files))
	for i, f := range rv.files {
		files[i] = sideFile{path: f.path, status: f.status, added: f.added, deleted: f.deleted, binary: f.binary, viewed: f.viewed, group: f.group, comments: rv.commentsIn(f)}
	}
	tab.filesTree(c, &rv.side, &rv.filter, rv.closedDirs, files, rv.current, true, rv.reveal, func(f sideFile) fileRef {
		return fileRef{path: f.path, status: f.status, remote: true}
	})
}

var _ = git.EmptyTree

// openRepoButton opens the repository's folder in an editor or another
// application, from a menu.
func (a *app) openRepoButton(c *ui.Context, tab *repoTab) {
	btn := iconButton(c, iconOpen, "Open With…")
	if btn.Clicked() {
		a.openWithMenu = !a.openWithMenu
	}
	var rows []menuRow
	for _, ap := range a.openWithApps() {
		rows = append(rows, menuRow{icon: iconOpen, label: ap.Name, run: func() { a.openPathWith(ap.Path, tab.repo.Root) }})
	}
	rows = append(rows, menuRow{icon: iconFolder, label: "Other…", run: func() { a.chooseOpenWith(tab.repo.Root) }})
	popMenu(c, btn, &a.openWithMenu, rows)
}
