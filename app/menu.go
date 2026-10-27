package app

import (
	"strconv"

	"github.com/egoist/mygo"
	"github.com/eliott/myfork/internal/git"
)

// inTab returns a menu item's action on the tab shown.
func (a *app) inTab(fn func(tab *repoTab)) func(*mygo.MenuItem, *mygo.Window) {
	return func(*mygo.MenuItem, *mygo.Window) {
		a.update(func() {
			if tab := a.tab(); tab != nil {
				fn(tab)
			}
		})
	}
}

// viewItem is a ⌘1, ⌘2 or ⌘3 item of the View menu: it shows Local Changes, the
// commits or the pull requests page.
func (a *app) viewItem(show func(*repoTab)) func(*mygo.MenuItem, *mygo.Window) {
	return func(*mygo.MenuItem, *mygo.Window) {
		a.update(func() {
			switch {
			case show == nil:
				a.showPulls()
			case a.tab() != nil:
				show(a.tab())
			}
		})
	}
}

// toggleSidebar is ⌘B: it hides or shows the sidebar of the pull requests page
// when that is shown, else the one of the repository.
func (a *app) toggleSidebar() {
	if a.pulls {
		a.prSideHidden = !a.prSideHidden
		return
	}
	a.sidebarShown = !a.sidebarShown
}

// openSelectedFileItem is Open Selected File (⇧⌘O): the file chosen in the
// window the key comes from, a commit's own window or the main one.
func (a *app) openSelectedFileItem(_ *mygo.MenuItem, win *mygo.Window) {
	a.update(func() {
		var v *commitView
		if cw := a.commitWindowOf(win); cw != nil {
			v = cw.cv
		}
		a.openSelectedFile(v)
	})
}

// findItem is Find (⌘F): it searches the code of the pull request shown, the
// window of the commit it comes from, or the changes of Local Changes or of the
// commit under the pointer, and the graph else.
func (a *app) findItem(_ *mygo.MenuItem, win *mygo.Window) {
	a.update(func() {
		if cw := a.commitWindowOf(win); cw != nil {
			cw.cv.openFind()
			return
		}
		tab := a.tab()
		if tab == nil {
			return
		}
		if pt := a.activePR(); a.pulls && pt != nil {
			if pt.pr.tab == prTabCode {
				pt.pr.codeReview().openFind()
			}
			return
		}
		switch {
		case tab.onWip && tab.wrv != nil:
			tab.wrv.openFind()
		case !tab.onWip && !a.panelCollapsed && tab.cv.hover && tab.cv.pane == paneChanges && tab.cv.commit != nil:
			tab.cv.openFind()
		default:
			tab.focusSearch()
		}
	})
}

// onMain returns a menu item's action, run on the main thread.
func (a *app) onMain(fn func()) func(*mygo.MenuItem, *mygo.Window) {
	return func(*mygo.MenuItem, *mygo.Window) { a.update(fn) }
}

// closeTabOrWindow is ⌘W: in a window of its own (Preferences, a commit) it
// closes that window, on the pull requests page it closes the pull request
// shown, and anywhere else it closes the repository tab shown, closing the
// window once no tab is left.
func (a *app) closeTabOrWindow(_ *mygo.MenuItem, w *mygo.Window) {
	a.update(func() {
		if w != a.win {
			w.Close()
			return
		}
		if tab := a.activePR(); tab != nil && a.pulls {
			tab.closePR(tab.pr)
			return
		}
		if len(a.tabs) == 0 {
			w.Close()
			return
		}
		a.closeTab(a.cur)
	})
}

// closeWindow is ⌘⇧W: it closes the window the menu was used in.
func (a *app) closeWindow(_ *mygo.MenuItem, w *mygo.Window) {
	a.update(w.Close)
}

// buildMenu makes the menu bar: Fork's menus, as the main menu of the
// original app lays them out (File, Edit, Repository, View, Window, Help).
func (a *app) buildMenu() *mygo.Menu { return mygo.NewMenu(a.menuTemplate()) }

// menuTemplate is the items of the menu bar. The accelerators are the keybinds:
// the user's, else the defaults of the actions.
func (a *app) menuTemplate() []*mygo.MenuItem {
	s := a.store.settings()
	return []*mygo.MenuItem{
		{Role: mygo.RoleAppMenu, Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			mygo.Separator(),
			a.mi("prefs", a.onMain(a.openPrefs)),
			mygo.Separator(),
			a.mi("install-cli", a.onMain(a.installCLI)),
			mygo.Separator(),
			{Role: mygo.RoleServices},
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Label: "File", Submenu: []*mygo.MenuItem{
			a.mi("new-tab", a.onMain(a.newTab)),
			mygo.Separator(),
			a.mi("open-repo", a.onMain(a.browseForRepo)),
			{Label: "Open Recent", Submenu: a.recentItems()},
			a.mi("clone", a.onMain(a.cloneDialog)),
			a.mi("init-repo", a.onMain(a.initDialog)),
			a.mi("quick-launch", a.onMain(func() { a.quick.open = true })),
			mygo.Separator(),
			a.mi("close-tab", a.closeTabOrWindow),
			a.mi("close-window", a.closeWindow),
			a.mi("restore-tab", a.onMain(a.restoreClosedTab)),
			mygo.Separator(),
			// The file chosen in the files of a commit, Local Changes or a pull
			// request, in the application its type opens with.
			a.mi("open-selected-file", a.openSelectedFileItem),
		}},
		{Label: "Edit", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleUndo},
			{Role: mygo.RoleRedo},
			mygo.Separator(),
			{Role: mygo.RoleCut},
			{Role: mygo.RoleCopy},
			{Role: mygo.RolePaste},
			// No Select All here: the menu would take Cmd+A for itself and send it to
			// the text fields only, and the file lists would never see it.
			mygo.Separator(),
			a.mi("find", a.findItem),
		}},
		{Label: "Repository", Submenu: []*mygo.MenuItem{
			a.mi("fetch", a.inTab(a.fetchDialog)),
			a.mi("quick-fetch", a.inTab(func(t *repoTab) { t.fetch("") })),
			a.mi("pull", a.inTab(a.pullDialog)),
			a.mi("quick-pull", a.inTab(func(t *repoTab) { t.pullCurrent(false) })),
			a.mi("quick-pull-rebase", a.inTab(func(t *repoTab) { t.pullCurrent(true) })),
			a.mi("push", a.inTab(func(t *repoTab) { a.pushDialog(t, "") })),
			a.mi("quick-push", a.inTab(func(t *repoTab) { t.pushCurrent(false) })),
			mygo.Separator(),
			a.mi("stash", a.inTab(a.stashDialog)),
			a.mi("quick-stash", a.inTab(a.quickStash)),
			mygo.Separator(),
			a.mi("checkout-branch", a.inTab(a.checkoutBranchDialog)),
			a.mi("new-branch", a.inTab(func(t *repoTab) { a.newBranchDialog(t, "") })),
			a.mi("new-tag", a.inTab(func(t *repoTab) { a.newTagDialog(t, "") })),
			a.mi("new-worktree", a.inTab(a.worktreeDialog)),
			mygo.Separator(),
			a.mi("create-pr", a.inTab(func(t *repoTab) { a.createPRDialog(t, "") })),
			a.mi("open-pr", a.inTab(a.openPRDialog)),
			mygo.Separator(),
			a.mi("lfs", a.inTab(a.lfsSheet)),
			a.mi("add-remote", a.inTab(a.addRemoteDialog)),
			a.mi("apply-patch", a.inTab(a.applyPatchFile)),
			mygo.Separator(),
			a.mi("reveal", a.inTab(func(t *repoTab) { revealInFinder(t.repo.Root) })),
			a.mi("terminal", a.inTab(func(t *repoTab) { openInTerminal(t.repo.Root) })),
			mygo.Separator(),
			a.mi("repo-settings", a.inTab(a.repoSettingsDialog)),
		}},
		{Label: "Changes", Submenu: []*mygo.MenuItem{
			a.mi("commit", a.inTab(func(t *repoTab) { a.commitFromMenu(t, false, false) })),
			a.mi("commit-push", a.inTab(func(t *repoTab) { a.commitFromMenu(t, true, false) })),
			a.mi("commit-pr", a.inTab(func(t *repoTab) { a.commitFromMenu(t, true, true) })),
			mygo.Separator(),
			a.mi("stage-all", a.inTab(func(t *repoTab) { a.stageFromMenu(t, true) })),
			a.mi("unstage-all", a.inTab(func(t *repoTab) { a.stageFromMenu(t, false) })),
			mygo.Separator(),
			a.mi("amend", a.inTab(a.amendFromMenu)),
			a.mi("ai-message", a.inTab(a.aiMessageFromMenu)),
		}},
		{Label: "Pull Request", Submenu: []*mygo.MenuItem{
			a.mi("pr-tab-summary", a.onMain(func() { a.pickPRTab(prTabSummary) })),
			a.mi("pr-tab-timeline", a.onMain(func() { a.pickPRTab(prTabTimeline) })),
			a.mi("pr-tab-code", a.onMain(func() { a.pickPRTab(prTabCode) })),
			a.mi("pr-tab-guide", a.onMain(func() { a.pickPRTab(prTabGuide) })),
			mygo.Separator(),
			a.mi("pr-next", a.onMain(func() { a.stepPR(1) })),
			a.mi("pr-prev", a.onMain(func() { a.stepPR(-1) })),
			mygo.Separator(),
			a.mi("pr-reload", a.onMain(func() { a.onPR(func(t *repoTab) { t.reloadPR() }) })),
			a.mi("pr-checkout", a.onMain(func() { a.onPR(func(t *repoTab) { t.checkoutPR(t.pr.number) }) })),
			a.mi("pr-web", a.onMain(func() { a.onPR(func(t *repoTab) { openInBrowser(t.pr.pr.HTMLURL) }) })),
			a.mi("pr-ready", a.onMain(func() { a.onPR(togglePRDraft) })),
			mygo.Separator(),
			a.mi("pr-tree", a.onMain(func() { a.prTree = !a.prTree })),
		}},
		{Label: "View", Submenu: []*mygo.MenuItem{
			// The sidebar of the repository: what the window shows.
			a.mi("view-changes", a.viewItem(func(t *repoTab) { t.showChanges() })),
			a.mi("view-commits", a.viewItem(func(t *repoTab) { t.showHistory() })),
			a.mi("side-pulls", a.viewItem(nil)),
			a.mi("toggle-sidebar", a.onMain(a.toggleSidebar)),
			mygo.Separator(),
			// The three tabs of the commit chosen, under the graph or in a
			// window of its own; on the pull requests page, the tabs of the
			// pull request shown.
			a.mi("pane-commit", a.paneTab(paneCommit, prTabSummary)),
			a.mi("pane-changes", a.paneTab(paneChanges, prTabTimeline)),
			a.mi("pane-tree", a.paneTab(paneTree, prTabCode)),
			a.mi("toggle-panel", a.onMain(func() { a.panelCollapsed = !a.panelCollapsed })),
			mygo.Separator(),
			a.mi("open-commit-window", a.inTab(func(t *repoTab) { a.openCommitWindow(t) })),
			a.mi("prev-commit", a.inTab(func(t *repoTab) { a.stepCommit(t, -1) })),
			a.mi("next-commit", a.inTab(func(t *repoTab) { a.stepCommit(t, 1) })),
			a.mi("copy-hash", a.inTab(a.copyCommitHash)),
			mygo.Separator(),
			a.mi("show-head", a.inTab(func(t *repoTab) {
				if t.status != nil && t.status.Head != "" {
					t.jumpTo(t.status.Head)
				}
			})),
			a.check("hide-tags", s.HideTags, a.toggle(func(s *Settings) { s.HideTags = !s.HideTags })),
			a.check("hide-stashes", s.HideStashes, a.toggle(func(s *Settings) { s.HideStashes = !s.HideStashes })),
			a.check("first-parent", s.FirstParent, a.toggle(func(s *Settings) { s.FirstParent = !s.FirstParent })),
			a.mi("filter-current-branch", a.inTab(func(t *repoTab) {
				if t.side == "history" || t.side == "" {
					if b := t.currentBranch(); b != nil {
						t.side = "only:" + b.Name
					}
				} else {
					t.side = "history"
				}
				t.reload()
			})),
			mygo.Separator(),
			{Label: "Diff", Submenu: []*mygo.MenuItem{
				a.radio("diff-split", s.DiffLayout != "unified", a.toggle(func(s *Settings) { s.DiffLayout = "split" })),
				a.radio("diff-unified", s.DiffLayout == "unified", a.toggle(func(s *Settings) { s.DiffLayout = "unified" })),
				a.mi("diff-toggle", a.toggle(func(s *Settings) {
					s.DiffLayout = map[bool]string{true: "split", false: "unified"}[s.DiffLayout == "unified"]
				})),
				mygo.Separator(),
				a.check("ignore-whitespace", s.IgnoreWhitespace, a.toggle(func(s *Settings) { s.IgnoreWhitespace = !s.IgnoreWhitespace })),
				a.check("wrap-lines", s.WrapLines, a.toggle(func(s *Settings) { s.WrapLines = !s.WrapLines })),
			}},
			{Label: "Code Font Size", Submenu: []*mygo.MenuItem{
				a.mi("code-bigger", a.toggle(func(s *Settings) { s.CodeFontSize = min(max(s.CodeFontSize, 6)+1, 40) })),
				a.mi("code-smaller", a.toggle(func(s *Settings) { s.CodeFontSize = max(min(s.CodeFontSize, 40)-1, 6) })),
				a.mi("code-reset", a.toggle(func(s *Settings) { s.CodeFontSize = defaultSettings().CodeFontSize })),
			}},
			mygo.Separator(),
			a.mi("refresh", a.onMain(a.refreshNow)),
			mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen},
		}},
		{Label: "Tab", Submenu: a.tabItems()},
		{Role: mygo.RoleWindowMenu},
		{Role: mygo.RoleHelp},
	}
}

// mi is a menu item for an action: it has the label of the action, and the
// binding the user gave it.
func (a *app) mi(id string, click func(*mygo.MenuItem, *mygo.Window)) *mygo.MenuItem {
	ac, ok := bindableByID(id)
	if !ok {
		panic("app: no action " + id)
	}
	it := &mygo.MenuItem{ID: id, Label: ac.menuLabel(), Click: click}
	// The keys read where they act, and those with Option alone (see routed),
	// are not the menu's: it would take them from the fields they are typed in,
	// or not take them at all.
	if ch, ok := parseAccel(a.menuAccel(id)); ok && !ac.key && !routed(ch) {
		it.Accelerator = a.menuAccel(id)
	}
	return it
}

// check is the menu item of an action that is on or off.
func (a *app) check(id string, on bool, click func(*mygo.MenuItem, *mygo.Window)) *mygo.MenuItem {
	it := a.mi(id, click)
	it.Type, it.Checked = mygo.MenuItemCheckbox, on
	return it
}

// radio is the menu item of one of several actions, of which one is on.
func (a *app) radio(id string, on bool, click func(*mygo.MenuItem, *mygo.Window)) *mygo.MenuItem {
	it := a.mi(id, click)
	it.Type, it.Checked = mygo.MenuItemRadio, on
	return it
}

// tabItems are the items of the Tab menu: next, previous and the tab with a
// number. Their keys are read from the keyboard where they are set (see
// shortcuts), so that the menu shows the keys without taking them.
func (a *app) tabItems() []*mygo.MenuItem {
	items := []*mygo.MenuItem{
		a.mi("next-tab", a.onMain(func() { a.stepTab(1) })),
		a.mi("prev-tab", a.onMain(func() { a.stepTab(-1) })),
		mygo.Separator(),
	}
	for n := 1; n <= 9; n++ {
		items = append(items, a.mi("go-tab-"+strconv.Itoa(n), a.onMain(func() { a.goTab(n - 1) })))
	}
	return append(items, a.mi("last-tab", a.onMain(func() { a.goTab(-1) })))
}

// paneTab is the menu item that shows one of the three tabs of the commit
// chosen (⌥1, ⌥2 and ⌥3): in the commit's own window when the key comes from
// it, in the panel under the graph otherwise. One shortcut means one thing in
// both windows. On the pull requests page, with a pull request shown, the same
// keys pick that pull request's tab prTab instead.
func (a *app) paneTab(p panelTab, prTab int) func(*mygo.MenuItem, *mygo.Window) {
	return func(_ *mygo.MenuItem, win *mygo.Window) {
		a.update(func() {
			if cw := a.commitWindowOf(win); cw != nil {
				cw.cv.setPane(p)
				return
			}
			if pt := a.activePR(); a.pulls && pt != nil {
				pt.pr.tab = prTab
				return
			}
			a.showPane(p)
		})
	}
}

// toggle changes a setting from a menu item, and applies it.
func (a *app) toggle(fn func(*Settings)) func(*mygo.MenuItem, *mygo.Window) {
	return func(*mygo.MenuItem, *mygo.Window) { a.update(func() { a.setSetting(fn) }) }
}

// setSetting changes a setting, and shows every diff anew.
func (a *app) setSetting(fn func(*Settings)) {
	a.store.update(fn)
	for _, t := range a.tabs {
		t.resetDiffs()
		t.reload()
	}
	a.rebuildMenu()
}

func (a *app) rebuildMenu() { mygo.App.SetMenu(a.buildMenu()) }

func (a *app) recentItems() []*mygo.MenuItem {
	var items []*mygo.MenuItem
	for _, root := range a.store.recent() {
		root := root
		items = append(items, &mygo.MenuItem{Label: abbreviateHome(root), ToolTip: root, Click: a.onMain(func() { a.openRepoReporting(root) })})
	}
	if len(items) > 0 {
		items = append(items, mygo.Separator())
	}
	return append(items, &mygo.MenuItem{Label: "Clear Menu", Disabled: len(items) == 0, Click: a.onMain(func() {
		a.store.clearRecent()
		a.rebuildMenu()
	})})
}

func (a *app) restoreClosedTab() {
	for len(a.closed) > 0 {
		root := a.closed[len(a.closed)-1]
		a.closed = a.closed[:len(a.closed)-1]
		if err := a.openRepo(root); err == nil {
			return
		}
	}
}

var _ = git.EmptyTree
