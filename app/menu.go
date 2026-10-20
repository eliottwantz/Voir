package app

import (
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

// onMain returns a menu item's action, run on the main thread.
func (a *app) onMain(fn func()) func(*mygo.MenuItem, *mygo.Window) {
	return func(*mygo.MenuItem, *mygo.Window) { a.update(fn) }
}

// closePRTabOrWindow is ⌘W: on the pull requests page it closes the pull
// request shown, and once none is left (or anywhere else) it closes the window.
func (a *app) closePRTabOrWindow(_ *mygo.MenuItem, w *mygo.Window) {
	a.update(func() {
		if tab := a.activePR(); tab != nil && a.pulls {
			tab.closePR(tab.pr)
			return
		}
		w.Close()
	})
}

// buildMenu makes the menu bar: Fork's menus, as the main menu of the
// original app lays them out (File, Edit, Repository, View, Window, Help).
func (a *app) buildMenu() *mygo.Menu { return mygo.NewMenu(a.menuTemplate()) }

// menuTemplate is the items of the menu bar.
func (a *app) menuTemplate() []*mygo.MenuItem {
	s := a.store.settings()
	return []*mygo.MenuItem{
		{Role: mygo.RoleAppMenu, Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			mygo.Separator(),
			{Label: "Preferences…", Accelerator: "CmdOrCtrl+,", Click: a.onMain(a.openPrefs)},
			mygo.Separator(),
			{Label: "Install `myfork` CLI…", Click: a.onMain(a.installCLI)},
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
			{Label: "New Tab", Accelerator: "CmdOrCtrl+T", Click: a.onMain(a.newTab)},
			mygo.Separator(),
			{Label: "Open…", Accelerator: "CmdOrCtrl+O", Click: a.onMain(a.browseForRepo)},
			{Label: "Open Recent", Submenu: a.recentItems()},
			{Label: "Clone…", Accelerator: "CmdOrCtrl+Shift+N", Click: a.onMain(a.cloneDialog)},
			{Label: "Create New Local Repository…", Click: a.onMain(a.initDialog)},
			{Label: "Quick Launch…", Click: a.onMain(func() { a.quick.open = true })},
			mygo.Separator(),
			{ID: "close-window", Label: "Close Window", Accelerator: "CmdOrCtrl+W", Click: a.closePRTabOrWindow},
			{ID: "close-tab", Label: "Close Tab", Accelerator: "CmdOrCtrl+Shift+W", Click: a.onMain(func() { a.closeTab(a.cur) })},
			{Label: "Restore Closed Tab", Accelerator: "CmdOrCtrl+Shift+T", Click: a.onMain(a.restoreClosedTab)},
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
			{Label: "Find…", Accelerator: "CmdOrCtrl+F", Click: a.inTab(func(t *repoTab) { t.focusSearch() })},
		}},
		{Label: "Repository", Submenu: []*mygo.MenuItem{
			{Label: "Fetch…", Accelerator: "CmdOrCtrl+Shift+F", Click: a.inTab(a.fetchDialog)},
			{Label: "Quick Fetch", Click: a.inTab(func(t *repoTab) { t.fetch("") })},
			{Label: "Pull…", Accelerator: "CmdOrCtrl+Shift+L", Click: a.inTab(a.pullDialog)},
			{Label: "Quick Pull", Click: a.inTab(func(t *repoTab) { t.pullCurrent(false) })},
			{Label: "Push…", Accelerator: "CmdOrCtrl+Shift+P", Click: a.inTab(func(t *repoTab) { a.pushDialog(t, "") })},
			{Label: "Quick Push", Click: a.inTab(func(t *repoTab) { t.pushCurrent() })},
			mygo.Separator(),
			{Label: "Save Stash…", Click: a.inTab(a.stashDialog)},
			{Label: "Quick Stash", Click: a.inTab(a.quickStash)},
			mygo.Separator(),
			{Label: "Checkout Branch…", Accelerator: "CmdOrCtrl+Shift+B", Click: a.inTab(a.checkoutBranchDialog)},
			{Label: "New Branch…", Accelerator: "CmdOrCtrl+Alt+B", Click: a.inTab(func(t *repoTab) { a.newBranchDialog(t, "") })},
			{Label: "New Tag…", Click: a.inTab(func(t *repoTab) { a.newTagDialog(t, "") })},
			{Label: "New Worktree…", Click: a.inTab(a.worktreeDialog)},
			mygo.Separator(),
			{Label: "Create Pull Request…", Click: a.inTab(func(t *repoTab) { a.createPRDialog(t, "") })},
			{Label: "Open Pull Request…", Click: a.inTab(a.openPRDialog)},
			mygo.Separator(),
			{Label: "Git LFS…", Click: a.inTab(a.lfsSheet)},
			{Label: "Add Remote…", Click: a.inTab(a.addRemoteDialog)},
			{Label: "Apply Patch…", Click: a.inTab(a.applyPatchFile)},
			mygo.Separator(),
			{Label: "Show in Finder", Click: a.inTab(func(t *repoTab) { revealInFinder(t.repo.Root) })},
			{Label: "Open in Terminal", Click: a.inTab(func(t *repoTab) { openInTerminal(t.repo.Root) })},
			mygo.Separator(),
			{Label: "Settings for This Repository…", Click: a.inTab(a.repoSettingsDialog)},
		}},
		{Label: "View", Submenu: []*mygo.MenuItem{
			// The sidebar of the repository: what the window shows.
			{ID: "view-changes", Label: "Local Changes", Accelerator: "CmdOrCtrl+1", Click: a.inTab(func(t *repoTab) { t.showChanges() })},
			{ID: "view-commits", Label: "All Commits", Accelerator: "CmdOrCtrl+2", Click: a.inTab(func(t *repoTab) { t.showHistory() })},
			{ID: "side-pulls", Label: "Pull Requests", Click: a.onMain(a.showPulls)},
			{ID: "toggle-sidebar", Label: "Toggle Sidebar", Accelerator: "CmdOrCtrl+B", Click: a.onMain(func() { a.sidebarShown = !a.sidebarShown })},
			mygo.Separator(),
			// The three tabs of the commit chosen, under the graph or in a
			// window of its own.
			{ID: "pane-commit", Label: "Summary", Accelerator: "Ctrl+1", Click: a.paneTab(paneCommit)},
			{ID: "pane-changes", Label: "Changes", Accelerator: "Ctrl+2", Click: a.paneTab(paneChanges)},
			{ID: "pane-tree", Label: "File Tree", Accelerator: "Ctrl+3", Click: a.paneTab(paneTree)},
			{ID: "toggle-panel", Label: "Toggle Commit Panel", Accelerator: "CmdOrCtrl+J", Click: a.onMain(func() { a.panelCollapsed = !a.panelCollapsed })},
			mygo.Separator(),
			{ID: "open-commit-window", Label: "Open Commit in New Window", Accelerator: "CmdOrCtrl+Shift+O", Click: a.inTab(func(t *repoTab) { a.openCommitWindow(t) })},
			mygo.Separator(),
			{Label: "Show HEAD", Accelerator: "CmdOrCtrl+Shift+H", Click: a.inTab(func(t *repoTab) {
				if t.status != nil && t.status.Head != "" {
					t.jumpTo(t.status.Head)
				}
			})},
			{Label: "Hide Tags", Type: mygo.MenuItemCheckbox, Checked: s.HideTags, Click: a.toggle(func(s *Settings) { s.HideTags = !s.HideTags })},
			{Label: "Hide Stashes in Commit List", Type: mygo.MenuItemCheckbox, Checked: s.HideStashes, Click: a.toggle(func(s *Settings) { s.HideStashes = !s.HideStashes })},
			{Label: "Collapse All Merges (First Parent only)", Type: mygo.MenuItemCheckbox, Checked: s.FirstParent, Click: a.toggle(func(s *Settings) { s.FirstParent = !s.FirstParent })},
			{Label: "Filter Current Branch", Click: a.inTab(func(t *repoTab) {
				if t.side == "history" || t.side == "" {
					if b := t.currentBranch(); b != nil {
						t.side = "only:" + b.Name
					}
				} else {
					t.side = "history"
				}
				t.reload()
			})},
			mygo.Separator(),
			{Label: "Diff", Submenu: []*mygo.MenuItem{
				{Label: "Side by Side", Type: mygo.MenuItemRadio, Checked: s.DiffLayout != "unified", Click: a.toggle(func(s *Settings) { s.DiffLayout = "split" })},
				{Label: "Unified", Type: mygo.MenuItemRadio, Checked: s.DiffLayout == "unified", Click: a.toggle(func(s *Settings) { s.DiffLayout = "unified" })},
				mygo.Separator(),
				{Label: "Ignore Whitespace", Type: mygo.MenuItemCheckbox, Checked: s.IgnoreWhitespace, Click: a.toggle(func(s *Settings) { s.IgnoreWhitespace = !s.IgnoreWhitespace })},
				{Label: "Wrap Lines", Type: mygo.MenuItemCheckbox, Checked: s.WrapLines, Click: a.toggle(func(s *Settings) { s.WrapLines = !s.WrapLines })},
			}},
			mygo.Separator(),
			{Label: "Refresh", Accelerator: "CmdOrCtrl+R", Click: a.onMain(a.refreshNow)},
			mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen},
		}},
		{Role: mygo.RoleWindowMenu},
		{Role: mygo.RoleHelp},
	}
}

// paneTab is the menu item that shows one of the three tabs of the commit
// chosen (⌃1, ⌃2 and ⌃3): in the commit's own window when the key comes from
// it, in the panel under the graph otherwise. One shortcut means one thing in
// both windows.
func (a *app) paneTab(p panelTab) func(*mygo.MenuItem, *mygo.Window) {
	return func(_ *mygo.MenuItem, win *mygo.Window) {
		a.update(func() {
			if cw := a.commitWindowOf(win); cw != nil {
				cw.cv.setPane(p)
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
