package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
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

func TestSidebarShortcuts(t *testing.T) {
	a, _, _ := prApp(t) // a repository on GitHub, so the pull requests are there
	tt := ui.NewTester(a.view, 1280, 800)
	menu := a.menuTemplate()
	for id, want := range map[string]struct {
		accel string
		view  int
	}{
		"side-files":    {"CmdOrCtrl+1", sideFiles},
		"side-history":  {"CmdOrCtrl+2", sideHistory},
		"side-branches": {"CmdOrCtrl+3", sideBranches},
		"side-pulls":    {"CmdOrCtrl+4", sidePulls},
	} {
		it := find(t, menu, id)
		if it.Accelerator != want.accel {
			t.Errorf("%s has %q, want %q", id, it.Accelerator, want.accel)
		}
		a.sideTab = -1
		it.Click(it, nil)
		tt.Frame()
		if a.sideTab != want.view {
			t.Errorf("%s: the sidebar shows view %d, want %d", id, a.sideTab, want.view)
		}
	}

	// ⌘B hides the sidebar and shows it again; a view chosen while it is
	// hidden shows it.
	toggle := find(t, menu, "toggle-sidebar")
	if toggle.Accelerator != "CmdOrCtrl+B" {
		t.Errorf("toggle has %q", toggle.Accelerator)
	}
	toggle.Click(toggle, nil)
	tt.Frame()
	if a.sidebarShown || tt.HasText("Filter history") {
		t.Error("the sidebar is still shown")
	}
	toggle.Click(toggle, nil)
	tt.Frame()
	if !a.sidebarShown {
		t.Error("the sidebar is still hidden")
	}
	toggle.Click(toggle, nil)
	hist := find(t, menu, "side-history")
	hist.Click(hist, nil)
	tt.Frame()
	if !a.sidebarShown || !tt.HasText("Filter history") {
		t.Error("choosing a view did not show the sidebar")
	}
}

func TestPullRequestsShortcutNeedsGitHub(t *testing.T) {
	a, _ := testApp(t) // no remote
	a.sideTab = sideHistory
	it := find(t, a.menuTemplate(), "side-pulls")
	it.Click(it, nil)
	if a.sideTab != sideHistory {
		t.Errorf("the sidebar went to %d", a.sideTab)
	}
	if len(a.messages) == 0 || !strings.Contains(a.messages[len(a.messages)-1], "no remote on GitHub") {
		t.Errorf("messages = %q", a.messages)
	}
}
