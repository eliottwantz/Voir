package main

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestWorktreesOpenInTheirOwnTabs(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()

	// A single repository still shows its tab.
	if !tt.HasText(filepath.Base(r.Root)) {
		t.Fatalf("no tab: %q", tt.Texts())
	}

	wt := filepath.Join(t.TempDir(), "feature-tree")
	if err := r.AddWorktree(wt, "hotfix", true); err != nil {
		t.Fatal(err)
	}
	a.tab().reload()
	tt.Frame()
	a.openRepoReporting(wt)
	tt.Frame()
	if len(a.tabs) != 2 || a.cur != 1 {
		t.Fatalf("tabs = %d, current %d", len(a.tabs), a.cur)
	}
	// Opening the main worktree again goes to its tab.
	a.openRepoReporting(r.Root)
	if len(a.tabs) != 2 || a.cur != 0 {
		t.Fatalf("tabs = %d, current %d", len(a.tabs), a.cur)
	}
	a.refreshAllStatus()
	tt.Frame()
	if !a.tabs[1].linked() || a.tabs[0].linked() {
		t.Fatalf("linked: main %v, worktree %v", a.tabs[0].linked(), a.tabs[1].linked())
	}
	if got := a.tabWhere(a.tabs[1]); got != "hotfix" {
		t.Fatalf("worktree tab shows %q", got)
	}
	if !tt.HasText("hotfix") {
		t.Fatalf("branch missing from the strip: %q", tt.Texts())
	}
	snapshot(t, tt, "tabs")

	a.moveTab(1, -1)
	if a.tabs[0].repo.Root == r.Root || a.tab().repo.Root != r.Root {
		t.Fatalf("move: %s first, shown %s", a.tabs[0].repo.Root, a.tab().repo.Root)
	}
	a.closeTabs(func(i int) bool { return i == 0 })
	if len(a.tabs) != 1 || len(a.closed) != 1 {
		t.Fatalf("tabs = %d, closed = %d", len(a.tabs), len(a.closed))
	}
}
