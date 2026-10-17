package main

import (
	"fmt"
	"github.com/eliott/myfork/internal/git"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func TestHunkRanges(t *testing.T) {
	hunks := []string{"@@ -1,5 +1,6 @@\n a\n", "@@ -20,3 +21,0 @@\n-x\n", "@@ -40 +40 @@ func f() {\n-y\n+z\n"}
	for _, c := range []struct {
		h            string
		start, count int
	}{{hunks[0], 0, 6}, {hunks[1], 21, 0}, {hunks[2], 39, 1}} {
		if s, n := hunkNewRange(c.h); s != c.start || n != c.count {
			t.Errorf("%q: %d,%d, want %d,%d", c.h, s, n, c.start, c.count)
		}
	}
	for header, want := range map[string]int{"@@ -1,5 +1,6 @@": 0, "@@ -20,3 +21,0 @@": 1, "@@ -40 +40 @@ func f() {": 2, "@@ -9 +9 @@": -1} {
		if got := hunkIndexOf(hunks, header); got != want {
			t.Errorf("%q is hunk %d, want %d", header, got, want)
		}
	}
}

func TestHunkRegions(t *testing.T) {
	// Two hunks, with hidden lines after the second, in a viewer 0-500.
	g := hunkGeo{seps: []sepGeo{{0, 10, 40}, {1, 200, 230}, {-1, 350, 380}}, end: 450, hasEnd: true}
	for _, c := range []struct {
		y          float32
		hunk       int
		top, bound float32
	}{
		{5, -1, 0, 0},      // above the first header
		{10, 0, 40, 200},   // on a header: the box starts under it
		{120, 0, 40, 200},  // a context line
		{210, 1, 230, 350}, // the next header
		{360, -1, 0, 0},    // hidden lines belong to no hunk
		{460, -1, 0, 0},    // under the last line
	} {
		h, top, bottom := g.region(c.y, 0, 500)
		if h != c.hunk || h >= 0 && (top != c.top || bottom != c.bound) {
			t.Errorf("y %v: hunk %d (%v-%v), want %d (%v-%v)", c.y, h, top, bottom, c.hunk, c.top, c.bound)
		}
	}
	// Scrolled so that the first header is gone, the lines above belong to
	// the hunk before it; the last hunk runs to the viewer's bottom without
	// an end.
	g = hunkGeo{seps: []sepGeo{{3, 100, 130}}}
	if h, top, bottom := g.region(50, 0, 500); h != 2 || top != 0 || bottom != 100 {
		t.Errorf("above: %d %v-%v", h, top, bottom)
	}
	if h, top, bottom := g.region(400, 0, 500); h != 3 || top != 130 || bottom != 500 {
		t.Errorf("last: %d %v-%v", h, top, bottom)
	}
}

// longDiff makes a file of 80 lines with two hunks, and shows it in Local
// Changes.
func longDiff(t *testing.T, layout string) (*app, *ui.Tester) {
	t.Helper()
	a, r := testApp(t)
	var lines []string
	for i := 1; i <= 80; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	path := filepath.Join(r.Root, "long.txt")
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	if err := r.Stage("long.txt"); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(git.CommitOptions{Message: "Add long"}); err != nil {
		t.Fatal(err)
	}
	lines[9], lines[60] = "changed ten", "changed sixty-one"
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	a.store.data.Settings.DiffLayout = layout
	tab := a.tab()
	tab.reload()
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	if err := tt.Click("Uncommitted changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("long.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(tab.work.hunks) != 2 {
		t.Fatalf("hunks = %d", len(tab.work.hunks))
	}
	return a, tt
}

// hunksUnder scans down the diff with the pointer, and lists the hunks it
// was over, in order, with the buttons showing as they are.
func hunksUnder(t *testing.T, a *app, tt *ui.Tester) []int {
	t.Helper()
	r, _ := tt.Find("New text")
	x := r.X + r.W - 100
	var seq []int
	for y := r.Y + 2; y < r.Y+r.H-2; y += 3 {
		tt.Move(x, y)
		tt.Frame()
		tt.Frame()
		hov := a.tab().work.geo.hov
		if got := tt.HasText("Stage Hunk"); got != (hov >= 0) {
			t.Fatalf("y %v: buttons %v, hunk %d", y, got, hov)
		}
		if len(seq) == 0 || seq[len(seq)-1] != hov {
			seq = append(seq, hov)
		}
	}
	return seq
}

func TestHunkBoxShowsOverEveryLineOfTheHunk(t *testing.T) {
	for _, layout := range []string{"split", "unified"} {
		t.Run(layout, func(t *testing.T) {
			a, tt := longDiff(t, layout)
			if got := hunksUnder(t, a, tt); !slices.Equal(got, []int{0, 1, -1}) {
				t.Fatalf("hunks under the pointer, top to bottom: %v", got)
			}
			snapshot(t, tt, "hunk-"+layout)
		})
	}
}

func TestHunkBoxFollowsAScrolledDiff(t *testing.T) {
	a, tt := longDiff(t, "unified")
	tt.SetSize(1280, 380)
	tt.Frame()
	r, _ := tt.Find("New text")
	x, y := r.X+r.W-100, r.Y+r.H/2
	tt.Scroll(x, y, 0, 60)
	tt.Frame()
	tt.Move(x, y)
	tt.Frame()
	tt.Frame()
	snapshot(t, tt, "hunk-scrolled")
	if hov := a.tab().work.geo.hov; hov < 0 || !tt.HasText("Stage Hunk") {
		t.Fatalf("hunk %d under the pointer, buttons %v: %q", hov, tt.HasText("Stage Hunk"), tt.Texts())
	}
}
