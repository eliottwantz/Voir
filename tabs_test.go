package main

import (
	"fmt"
	"github.com/eliott/myfork/internal/git"
	"os"
	"path/filepath"
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
	if len(tab.pview.hunkRows) != 2 {
		t.Fatalf("hunks = %d", len(tab.pview.hunkRows))
	}
	return a, tt
}

func hoverRow(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("no row %q in %q", label, tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	tt.Frame()
}

func TestHunkBoxShowsOverEveryLineOfTheHunk(t *testing.T) {
	for _, layout := range []string{"split", "unified"} {
		t.Run(layout, func(t *testing.T) {
			_, tt := longDiff(t, layout)
			// A context line of the second hunk, then of the first.
			hoverRow(t, tt, "  line 58")
			if !tt.HasText("Stage Hunk") {
				t.Fatalf("no buttons over a context line: %q", tt.Texts())
			}
			snapshot(t, tt, "hunk-"+layout)
			hoverRow(t, tt, "  line 8")
			if !tt.HasText("Stage Hunk") {
				t.Fatalf("no buttons over the first hunk: %q", tt.Texts())
			}
			// Off the diff, they go.
			tt.Move(5, 5)
			tt.Frame()
			tt.Frame()
			if tt.HasText("Stage Hunk") {
				t.Fatal("the buttons stayed with the pointer away")
			}
		})
	}
}

func TestDraggingSelectsLinesToStage(t *testing.T) {
	a, r := testApp(t)
	a.store.data.Settings.DiffLayout = "unified"
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tt.Click("Uncommitted changes")
	tt.Frame()
	if tab.work.path != "main.go" {
		t.Fatalf("work = %+v", tab.work)
	}
	centre := func(label string) (float32, float32) {
		t.Helper()
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("no row %q in %q", label, tt.Texts())
		}
		return r.X + r.W/2, r.Y + r.H/2
	}
	// Drag from the deleted line over its replacement and the line added
	// after it.
	x, y0 := centre("- \tprintln(\"hello, world\")")
	_, y2 := centre("+ \tprintln(\"bye\")")
	tt.Press(x, y0)
	tt.Move(x, (y0+y2)/2)
	tt.Move(x, y2)
	tt.Release(x, y2)
	tt.Frame()
	tt.Frame()
	if got := len(tab.pview.selected()); got != 3 {
		t.Fatalf("selected %d lines, want 3", got)
	}
	snapshot(t, tt, "lines-selected")
	if err := tt.Click("Stage Lines"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	tt.Frame()
	staged, _ := r.StagedDiff("main.go", git.DiffOptions{})
	if !strings.Contains(staged, `+	println("bye")`) || !strings.Contains(staged, `+	println("hello, world!")`) {
		t.Fatalf("staged:\n%s", staged)
	}
}

func TestClickingOneLineStagesOnlyIt(t *testing.T) {
	a, r := testApp(t)
	a.store.data.Settings.DiffLayout = "unified"
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Click("Uncommitted changes")
	tt.Frame()
	row, ok := tt.Find("+ \tprintln(\"bye\")")
	if !ok {
		t.Fatalf("no row: %q", tt.Texts())
	}
	tt.Press(row.X+row.W/2, row.Y+row.H/2)
	tt.Release(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	tt.Frame()
	if err := tt.Click("Stage Lines"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	tt.Frame()
	staged, _ := r.StagedDiff("main.go", git.DiffOptions{})
	if !strings.Contains(staged, `+	println("bye")`) || strings.Contains(staged, "hello, world!") {
		t.Fatalf("staged:\n%s", staged)
	}
	rest, _ := r.WorkDiff("main.go", git.DiffOptions{})
	if !strings.Contains(rest, `+	println("hello, world!")`) || strings.Contains(rest, `+	println("bye")`) {
		t.Fatalf("unstaged:\n%s", rest)
	}
}

func TestLinesOfAnUntrackedFileCanBeStaged(t *testing.T) {
	a, r := testApp(t)
	a.store.data.Settings.DiffLayout = "unified"
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Click("Uncommitted changes")
	tt.Frame()
	if err := tt.Click("notes.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	row, ok := tt.Find("+ todo")
	if !ok {
		t.Fatalf("no row: %q", tt.Texts())
	}
	tt.Press(row.X+row.W/2, row.Y+row.H/2)
	tt.Release(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	tt.Frame()
	if err := tt.Click("Stage Lines"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	tt.Frame()
	staged, _ := r.StagedDiff("notes.txt", git.DiffOptions{})
	if !strings.Contains(staged, "+todo") {
		t.Fatalf("staged:\n%s", staged)
	}
}

func TestWordChange(t *testing.T) {
	a, b := wordChange(`println("hello, world")`, `println("hello, world!")`)
	if a[1] > a[0] {
		t.Errorf("deleted side %v", a)
	}
	if b != [2]int{21, 22} {
		t.Errorf("added side %v", b)
	}
	if a, b := wordChange("abc", "xyz"); a != [2]int{} || b != [2]int{} {
		t.Errorf("nothing in common: %v %v", a, b)
	}
}
