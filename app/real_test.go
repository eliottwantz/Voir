package app

import (
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

// TestRealRepository opens a repository of the user's (MYFORK_REPO), only to
// read it, and reviews its largest recent commits: how long they take, and
// a picture.
func TestRealRepository(t *testing.T) {
	dir := os.Getenv("MYFORK_REPO")
	if dir == "" {
		t.Skip("set MYFORK_REPO to a repository to read")
	}
	r, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := r.Log(git.LogOptions{Limit: 60})
	if err != nil || len(cs) == 0 {
		t.Fatalf("log: %v", err)
	}
	// The commit with the most files among the last 60.
	best, bestFiles := 0, 0
	for i := range cs {
		start := time.Now()
		patch, err := r.CommitPatch(&cs[i], git.DiffOptions{})
		if err != nil {
			t.Fatal(err)
		}
		parts := git.SplitPatchFiles(patch)
		if len(parts) > bestFiles {
			best, bestFiles = i, len(parts)
		}
		if d := time.Since(start); d > 300*time.Millisecond {
			t.Logf("%s: %d files, %d KB patch, %v", cs[i].Short(), len(parts), len(patch)>>10, d)
		}
	}
	t.Logf("largest: %s %q with %d files", cs[best].Short(), cs[best].Subject, bestFiles)

	a := newApp(&store{data: stored{Settings: defaultSettings(), SidebarW: 260, ListH: 330, FilesW: 280}})
	if err := a.openRepo(r.Root); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.view, 1500, 950)
	tt.SetDark(true)
	tab := a.tab()
	start := time.Now()
	tab.selectRow(tab.commitRow(cs[best].Hash))
	tt.Frame()
	t.Logf("select + first frame: %v", time.Since(start))
	if tab.rv == nil || len(tab.rv.files) != bestFiles {
		t.Fatalf("review has %d files, want %d", len(tab.rv.files), bestFiles)
	}
	// Scroll through the surface, as a reader does.
	start = time.Now()
	for i := 0; i < 12; i++ {
		tt.Scroll(800, 500, 0, -600)
	}
	t.Logf("12 scrolls: %v", time.Since(start))
	snapshot(t, tt, "real-dark")
	tab.rv.reveal(len(tab.rv.files) - 1)
	tt.Frame()
	snapshot(t, tt, "real-last")
}
