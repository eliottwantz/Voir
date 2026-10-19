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
	// ⌘1 and ⌘2 choose the two views of the sidebar: the uncommitted changes
	// and every commit.
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

	// ⌃1, ⌃2 and ⌃3 choose the three tabs of the commit under the graph.
	for id, want := range map[string]struct {
		accel string
		pane  panelTab
	}{
		"pane-commit":  {"Ctrl+1", paneCommit},
		"pane-changes": {"Ctrl+2", paneChanges},
		"pane-tree":    {"Ctrl+3", paneTree},
	} {
		it := find(t, menu, id)
		if it.Accelerator != want.accel {
			t.Errorf("%s has %q, want %q", id, it.Accelerator, want.accel)
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
	if pulls.Accelerator != "" {
		t.Errorf("side-pulls has %q, want no shortcut", pulls.Accelerator)
	}
	pulls.Click(pulls, nil)
	tt.Frame()
	if !a.pulls || !tt.HasText("Pull Requests") {
		t.Errorf("the pull requests page is not shown: %q", tt.Texts())
	}
	summary := find(t, menu, "pane-commit")
	summary.Click(summary, nil)
	tt.Frame()
	if a.pulls || a.tab().cv.pane != paneCommit {
		t.Errorf("the summary did not leave the pull requests page: pulls %v, pane %d", a.pulls, a.tab().cv.pane)
	}

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

	// The commit opens in a window of its own (⇧⌘O): the same three tabs,
	// with no button to open it in a window again.
	open := find(t, menu, "open-commit-window")
	if open.Accelerator != "CmdOrCtrl+Shift+O" {
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
	if wt.HasText("Open in a new window (⇧⌘O)") {
		t.Errorf("the window offers to open itself: %q", wt.Texts())
	}
	// The same commit again comes forward, rather than opening twice.
	open.Click(open, nil)
	if len(a.cwins) != 1 {
		t.Errorf("commit windows = %d after opening the same commit twice", len(a.cwins))
	}

	// ⌃1, ⌃2 and ⌃3 show the same three tabs in the window the key came
	// from: the commit's own window here.
	cw.win = &mygo.Window{} // the window the keys come from, which a test has none of
	// In order: the last leaves the window at the File Tree, which the check
	// after the loop relies on.
	for _, want := range []struct {
		id    string
		accel string
		pane  panelTab
	}{
		{"pane-commit", "Ctrl+1", paneCommit},
		{"pane-changes", "Ctrl+2", paneChanges},
		{"pane-tree", "Ctrl+3", paneTree},
	} {
		id := want.id
		it := find(t, menu, id)
		if it.Accelerator != want.accel {
			t.Errorf("%s has %q, want %q", id, it.Accelerator, want.accel)
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
	if closeWindow.Label != "Close Window" || closeWindow.Accelerator != "CmdOrCtrl+W" {
		t.Errorf("Close Window is %q on %q", closeWindow.Label, closeWindow.Accelerator)
	}
	// ⌘⇧W closes the tab shown.
	closeTab := menu.ItemByID("close-tab")
	if closeTab.Accelerator != "CmdOrCtrl+Shift+W" {
		t.Errorf("Close Tab has %q, want CmdOrCtrl+Shift+W", closeTab.Accelerator)
	}
	// ⌘⇧O opens the commit in a window of its own, not Quick Launch.
	if got := findItem(t, a, "Quick Launch…").Accelerator; got != "" {
		t.Errorf("Quick Launch has %q", got)
	}
	if got := find(t, a.menuTemplate(), "open-commit-window").Accelerator; got != "CmdOrCtrl+Shift+O" {
		t.Errorf("Open Commit in New Window has %q", got)
	}
}

func TestPullRequestsShortcutNeedsGitHub(t *testing.T) {
	a, _ := testApp(t) // no remote
	a.tab().cv.pane = paneCommit
	it := find(t, a.menuTemplate(), "side-pulls")
	it.Click(it, nil)
	if a.pulls || a.tab().cv.pane != paneCommit {
		t.Errorf("the window went to the pull requests (%v) or to pane %d", a.pulls, a.tab().cv.pane)
	}
	if len(a.messages) == 0 || !strings.Contains(a.messages[len(a.messages)-1], "no remote on GitHub") {
		t.Errorf("messages = %q", a.messages)
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
