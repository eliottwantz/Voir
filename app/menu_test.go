package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

// walk calls fn for every item of a menu, submenus included.
func walk(items []*mygo.MenuItem, fn func(*mygo.MenuItem)) {
	for _, it := range items {
		fn(it)
		walk(it.Submenu, fn)
	}
}

// find returns the item with an id.
func find(t *testing.T, items []*mygo.MenuItem, id string) *mygo.MenuItem {
	t.Helper()
	var found *mygo.MenuItem
	walk(items, func(it *mygo.MenuItem) {
		if it.ID == id {
			found = it
		}
	})
	if found == nil {
		t.Fatalf("no menu item %q", id)
	}
	return found
}

func TestNoTwoMenuItemsShareAShortcut(t *testing.T) {
	a, _ := testApp(t)
	seen := map[string]string{}
	walk(a.menuTemplate(), func(it *mygo.MenuItem) {
		if it.Accelerator == "" {
			return
		}
		key := strings.ToLower(it.Accelerator)
		if other, dup := seen[key]; dup {
			t.Errorf("%s is on %q and on %q", it.Accelerator, other, it.Label)
		}
		seen[key] = it.Label
	})
}

func TestViewShortcuts(t *testing.T) {
	a, _, _ := prApp(t) // a repository on GitHub, so the pull requests are there
	tt := ui.NewTester(a.view, 1280, 800)
	menu := a.menuTemplate()
	// ⌘1 and ⌘2 choose two views of the sidebar: the uncommitted changes and
	// every commit; ⌘3 is the pull requests page.
	for id, want := range map[string]struct {
		accel string
		wip   bool
	}{
		"view-changes": {"CmdOrCtrl+1", true},
		"view-commits": {"CmdOrCtrl+2", false},
	} {
		it := find(t, menu, id)
		if it.Accelerator != want.accel {
			t.Errorf("%s has %q, want %q", id, it.Accelerator, want.accel)
		}
		it.Click(it, nil)
		tt.Frame()
		if a.tab().onWip != want.wip {
			t.Errorf("%s: Local Changes shown is %v, want %v", id, a.tab().onWip, want.wip)
		}
	}

	// ⌥1, ⌥2 and ⌥3 choose the three tabs of the commit under the graph.
	for id, want := range map[string]struct {
		accel string
		pane  panelTab
	}{
		"pane-commit":  {"Alt+1", paneCommit},
		"pane-changes": {"Alt+2", paneChanges},
		"pane-tree":    {"Alt+3", paneTree},
	} {
		it := find(t, menu, id)
		// Option alone is read from the keyboard, not carried by the menu.
		if it.Accelerator != "" || a.accel(id) != want.accel {
			t.Errorf("%s has %q, with key %q, want %q", id, it.Accelerator, a.accel(id), want.accel)
		}
		a.tab().cv.pane = -1
		it.Click(it, nil)
		tt.Frame()
		if a.tab().cv.pane != want.pane {
			t.Errorf("%s: the pane is %d, want %d", id, a.tab().cv.pane, want.pane)
		}
	}

	// The pull requests have a page of their own, which the tabs leave.
	pulls := find(t, menu, "side-pulls")
	if pulls.Accelerator != "CmdOrCtrl+3" {
		t.Errorf("side-pulls has %q, want CmdOrCtrl+3", pulls.Accelerator)
	}
	pulls.Click(pulls, nil)
	tt.Frame()
	if !a.pulls || !tt.HasText("Pull Requests") {
		t.Errorf("the pull requests page is not shown: %q", tt.Texts())
	}
	// The keys of the repository's panes do not reach it behind the page.
	a.tab().cv.pane = paneChanges
	summary := find(t, menu, "pane-commit")
	summary.Click(summary, nil)
	tt.Frame()
	if !a.pulls || a.tab().cv.pane != paneChanges {
		t.Errorf("the summary acted on the pull requests page: pulls %v, pane %d", a.pulls, a.tab().cv.pane)
	}
	commits := find(t, menu, "view-commits")
	commits.Click(commits, nil)
	tt.Frame()

	// ⌘B hides the sidebar and shows it again.
	toggle := find(t, menu, "toggle-sidebar")
	if toggle.Accelerator != "CmdOrCtrl+B" {
		t.Errorf("toggle has %q", toggle.Accelerator)
	}
	toggle.Click(toggle, nil)
	tt.Frame()
	if a.sidebarShown || tt.HasText("Branches") {
		t.Error("the sidebar is still shown")
	}
	toggle.Click(toggle, nil)
	tt.Frame()
	if !a.sidebarShown {
		t.Error("the sidebar is still hidden")
	}

	// On the pull requests page ⌘B hides its own sidebar, not the repository's.
	pulls.Click(pulls, nil)
	tt.Frame()
	toggle.Click(toggle, nil)
	tt.Frame()
	if !a.prSideHidden || !a.sidebarShown || tt.HasText("All pull requests") {
		t.Errorf("the pull requests sidebar is still shown: hidden %v, repository sidebar %v", a.prSideHidden, a.sidebarShown)
	}
	toggle.Click(toggle, nil)
	tt.Frame()
	if a.prSideHidden || !tt.HasText("All pull requests") {
		t.Error("the pull requests sidebar is still hidden")
	}
	a.pulls = false
	tt.Frame()

	// ⌘J folds the commit panel under the graph, and opens it again.
	panel := find(t, menu, "toggle-panel")
	if panel.Accelerator != "CmdOrCtrl+J" {
		t.Errorf("toggle-panel has %q, want CmdOrCtrl+J", panel.Accelerator)
	}
	panel.Click(panel, nil)
	tt.Frame()
	if !a.panelCollapsed || !tt.HasText("Expand the commit panel (⌘J)") {
		t.Errorf("the panel is not collapsed: collapsed %v", a.panelCollapsed)
	}
	panel.Click(panel, nil)
	tt.Frame()
	if a.panelCollapsed || !tt.HasText("Collapse the commit panel (⌘J)") {
		t.Errorf("the panel is still collapsed: collapsed %v", a.panelCollapsed)
	}

	// The commit opens in a window of its own, with no key (⇧⌘O opens the file
	// chosen): the same three tabs, with no button to open it in a window
	// again.
	open := find(t, menu, "open-commit-window")
	if open.Accelerator != "" {
		t.Errorf("open-commit-window has %q", open.Accelerator)
	}
	open.Click(open, nil)
	if len(a.cwins) != 1 {
		t.Fatalf("commit windows = %d", len(a.cwins))
	}
	cw := a.cwins[0]
	if cw.cv.commit == nil || !cw.cv.windowed || cw.cv.tab != a.tab() {
		t.Fatalf("the window shows %+v", cw.cv.commit)
	}
	wt := ui.NewTester(cw.view, 1120, 760)
	wt.Frame()
	for _, want := range []string{"Summary", "Changes", "File Tree"} {
		if !wt.HasText(want) {
			t.Errorf("no %q in the commit's window: %q", want, wt.Texts())
		}
	}
	if wt.HasText("Open in a new window") {
		t.Errorf("the window offers to open itself: %q", wt.Texts())
	}
	// The same commit again comes forward, rather than opening twice.
	open.Click(open, nil)
	if len(a.cwins) != 1 {
		t.Errorf("commit windows = %d after opening the same commit twice", len(a.cwins))
	}

	// ⌥1, ⌥2 and ⌥3 show the same three tabs in the window the key came
	// from: the commit's own window here.
	cw.win = &mygo.Window{} // the window the keys come from, which a test has none of
	// In order: the last leaves the window at the File Tree, which the check
	// after the loop relies on.
	for _, want := range []struct {
		id    string
		accel string
		pane  panelTab
	}{
		{"pane-commit", "Alt+1", paneCommit},
		{"pane-changes", "Alt+2", paneChanges},
		{"pane-tree", "Alt+3", paneTree},
	} {
		id := want.id
		it := find(t, menu, id)
		if it.Accelerator != "" || a.accel(id) != want.accel {
			t.Errorf("%s has %q, with key %q, want %q", id, it.Accelerator, a.accel(id), want.accel)
		}
		cw.cv.pane = -1
		it.Click(it, cw.win)
		wt.Frame()
		if cw.cv.pane != want.pane {
			t.Errorf("%s left the commit's window at pane %d, want %d", id, cw.cv.pane, want.pane)
		}
	}
	// The key from the main window acts on the main window, not on the
	// commit's window, whatever the other window shows.
	a.tab().cv.pane = -1
	it := find(t, menu, "pane-changes")
	it.Click(it, nil)
	if a.tab().cv.pane != paneChanges || cw.cv.pane != paneTree {
		t.Errorf("the key from the main window left the panes at %d and %d", a.tab().cv.pane, cw.cv.pane)
	}
}

func TestFileShortcuts(t *testing.T) {
	a, _ := testApp(t)
	// The roles fill in the label and accelerator of the items they make,
	// which the raw template does not carry.
	menu := a.buildMenu()
	closeWindow := menu.ItemByID("close-window")
	if closeWindow == nil {
		t.Fatal("no Close Window item")
	}
	if closeWindow.Label != "Close Window" || closeWindow.Accelerator != "CmdOrCtrl+Shift+W" {
		t.Errorf("Close Window is %q on %q", closeWindow.Label, closeWindow.Accelerator)
	}
	// ⌘W closes the tab shown.
	closeTab := menu.ItemByID("close-tab")
	if closeTab.Accelerator != "CmdOrCtrl+W" {
		t.Errorf("Close Tab has %q, want CmdOrCtrl+W", closeTab.Accelerator)
	}
	// ⌘⇧O opens the file chosen, not Quick Launch nor the commit's window.
	if got := findItem(t, a, "Quick Launch…").Accelerator; got != "" {
		t.Errorf("Quick Launch has %q", got)
	}
	if got := find(t, a.menuTemplate(), "open-commit-window").Accelerator; got != "" {
		t.Errorf("Open Commit in New Window has %q", got)
	}
	if got := find(t, a.menuTemplate(), "open-selected-file").Accelerator; got != "CmdOrCtrl+Shift+O" {
		t.Errorf("Open Selected File has %q", got)
	}
}

func TestPullRequestsPageKeepsRepositoryShortcutsOff(t *testing.T) {
	a, _ := testApp(t)
	tab := a.tab()
	tab.showChanges()
	a.showPulls()
	// ⌘W with no pull request shown closes nothing: not the repository's tab.
	it := find(t, a.menuTemplate(), "close-tab")
	it.Click(it, a.win)
	if len(a.tabs) != 1 || !a.pulls {
		t.Fatalf("⌘W on the pull requests page left %d tabs, pulls %v", len(a.tabs), a.pulls)
	}
	// The keys of the repository do not reach it behind the page.
	for _, id := range []string{"pane-changes", "show-head", "next-commit"} {
		it := find(t, a.menuTemplate(), id)
		it.Click(it, nil)
		if !a.pulls || !tab.onWip {
			t.Errorf("%s acted on the repository from the pull requests page", id)
		}
	}
	a.closed = append(a.closed, t.TempDir())
	it = find(t, a.menuTemplate(), "restore-tab")
	it.Click(it, nil)
	if len(a.closed) != 1 || !a.pulls {
		t.Errorf("Restore Tab acted on the pull requests page")
	}
}

func TestPullRequestsPageWithoutGitHubHasNothingToList(t *testing.T) {
	a, _ := testApp(t) // no remote
	tt := ui.NewTester(a.view, 1280, 800)
	it := find(t, a.menuTemplate(), "side-pulls")
	it.Click(it, nil)
	tt.Frame()
	if !a.pulls || !tt.HasText("No repository to list") {
		t.Errorf("pulls %v: %q", a.pulls, tt.Texts())
	}
}

func TestToggleStagedKeepsTheChoice(t *testing.T) {
	a, _ := testApp(t)
	tab := a.tab()
	tab.showChanges()
	has := func(es []git.StatusEntry, path string) bool {
		return slices.ContainsFunc(es, func(e git.StatusEntry) bool { return e.Path == path })
	}
	tab.uMulti.Add("main.go")
	tab.uMulti.Add("notes.txt")
	tab.toggleStaged([]string{"main.go", "notes.txt"}, true)
	if !has(tab.staged, "main.go") || !has(tab.staged, "notes.txt") {
		t.Fatalf("staged = %v", tab.staged)
	}
	if !tab.sMulti.Has("main.go") || !tab.sMulti.Has("notes.txt") || tab.uMulti.Len() != 0 || !tab.focusStaged {
		t.Errorf("the choice did not follow: staged %d, unstaged %d, focus %v", tab.sMulti.Len(), tab.uMulti.Len(), tab.focusStaged)
	}
	// Shift+Cmd+S with everything staged unstages everything.
	tab.toggleStaged(nil, len(tab.unstaged) > 0)
	if len(tab.staged) != 0 || !tab.focusUnstaged {
		t.Errorf("staged = %v, focus unstaged = %v", tab.staged, tab.focusUnstaged)
	}
}

func TestStageShortcutStaysInTheList(t *testing.T) {
	a, _ := testApp(t)
	tab := a.tab()
	tab.showChanges()
	if len(tab.unstaged) < 2 {
		t.Skipf("unstaged = %v", tab.unstaged)
	}
	first := inTreeOrder(tab.unstaged)
	tab.moveAndStay(tab.unstaged, []string{first[0].Path}, false)
	if tab.workStaged || tab.workPath != first[1].Path || !tab.focusUnstaged || !tab.uMulti.Has(first[1].Path) {
		t.Errorf("staged %v, workStaged %v, workPath %q, want %q", tab.staged, tab.workStaged, tab.workPath, first[1].Path)
	}
}
