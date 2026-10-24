package app

import (
	"fmt"
	"github.com/eliott/myfork/internal/git"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
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
	if got := a.tabWhere(a.tabs[1]); got != "" {
		t.Fatalf("worktree tab shows %q", got)
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

func TestDraggingATabMovesIt(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	wt := filepath.Join(t.TempDir(), "feature-tree")
	if err := r.AddWorktree(wt, "hotfix", true); err != nil {
		t.Fatal(err)
	}
	a.openRepoReporting(wt)
	a.refreshAllStatus()
	tt.Frame()
	tt.Frame()
	if len(a.tabs) != 2 || a.cur != 1 {
		t.Fatalf("tabs = %d, current %d", len(a.tabs), a.cur)
	}
	first, ok := tt.Find("Tab " + a.tabTitle(a.tabs[0]))
	if !ok {
		t.Fatalf("no first tab: %q", tt.Texts())
	}
	second, ok := tt.Find("Tab " + a.tabTitle(a.tabs[1]))
	if !ok {
		t.Fatalf("no second tab: %q", tt.Texts())
	}
	// The second tab, dragged onto the first, goes before it, and stays shown.
	tt.Press(second.X+second.W/2, second.Y+second.H/2)
	tt.Move(second.X-20, second.Y+second.H/2)
	tt.Move(first.X+2, first.Y+first.H/2)
	tt.Frame()
	tt.Release(first.X+2, first.Y+first.H/2)
	tt.Frame()
	tt.Frame()
	if a.tabs[0].repo.Root == r.Root || a.tab() != a.tabs[0] {
		t.Fatalf("tabs = %s, %s; current %d", a.tabs[0].repo.Root, a.tabs[1].repo.Root, a.cur)
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
	if err := tt.Click("Local Changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("long.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tab.wrv == nil || len(tab.wrv.files) != 1 {
		t.Fatalf("surface = %+v", tab.wrv)
	}
	return a, tt
}

// hoverDiff moves the pointer down the new side of the viewer of the diff
// until the label shows, which a hunk's buttons do under the pointer.
func hoverDiff(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	r, ok := tt.Find("New text")
	if !ok {
		t.Fatalf("no viewer in %q", tt.Texts())
	}
	for y := r.Y + 40; y < r.Y+r.H-10; y += 6 {
		tt.Move(r.X+r.W/2, y)
		tt.Frame()
		if tt.HasText(label) {
			return
		}
	}
	t.Fatalf("no %q under the pointer: %q", label, tt.Texts())
}

func TestLocalChangesShowTheDiffOfTheFileChosen(t *testing.T) {
	a, _ := testApp(t)
	a.store.data.Settings.DiffLayout = "unified"
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tt.Click("Local Changes")
	tt.Frame()
	// One file at a time: the diff is that of the file the lists chose.
	cards := func() []string {
		var got []string
		for _, f := range tab.wrv.files {
			got = append(got, fmt.Sprintf("%s:%v", f.path, f.staged))
		}
		return got
	}
	if want := []string{"main.go:false"}; !slices.Equal(cards(), want) {
		t.Fatalf("cards = %q, want %q", cards(), want)
	}
	if d := tab.wrv.files[0].ed; d == nil || !strings.Contains(d.New.String(), "bye") {
		t.Errorf("no line of main.go in its viewer: %+v", tab.wrv.files[0])
	}
	// Choosing it shows its own card, and nothing of the one before.
	if err := tt.Click("notes.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if want := []string{"notes.txt:false"}; !slices.Equal(cards(), want) {
		t.Fatalf("cards = %q, want %q", cards(), want)
	}
	if d := tab.wrv.files[0].ed; d == nil || !strings.Contains(d.New.String(), "todo") || strings.Contains(d.New.String(), "bye") {
		t.Errorf("the wrong file's lines show: %+v", tab.wrv.files[0])
	}
	// A hunk of the file chosen stages from its viewer.
	hoverDiff(t, tt, "Stage Hunk")
	if err := tt.Click("Stage Hunk"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	tt.Frame()
	a.runHeld()
	tt.Frame()
	if !slices.ContainsFunc(tab.staged, func(e git.StatusEntry) bool { return e.Path == "notes.txt" }) {
		t.Fatalf("staged = %+v", tab.staged)
	}
}

// chooseLines chooses lines of the new side of the viewer of the file shown
// in Local Changes, as dragging over their numbers does.
func chooseLines(t *testing.T, tt *ui.Tester, tab *repoTab, first, last int) {
	t.Helper()
	d := tab.wrv.files[0].ed
	if d == nil {
		t.Fatalf("no viewer: %+v", tab.wrv.files[0])
	}
	d.SetSelectedLines(editor.DiffLineRange{Side: editor.DiffNew, Start: first, End: last}, true)
	tt.Frame()
	tt.Frame()
}

func TestChoosingLinesStagesOnlyThem(t *testing.T) {
	for _, layout := range []string{"split", "unified"} {
		t.Run(layout, func(t *testing.T) {
			a, r := testApp(t)
			a.store.data.Settings.DiffLayout = layout
			tt := ui.NewTester(a.view, 1280, 800)
			tab := a.tab()
			tt.Click("Local Changes")
			tt.Frame()
			// The new text of main.go: its fifth line is the one added,
			// println("bye"), after the one changed.
			if d := tab.wrv.files[0].ed; d == nil || strings.Split(d.New.String(), "\n")[4] != "\tprintln(\"bye\")" {
				t.Fatalf("new text: %q", tab.wrv.files[0].ed.New.String())
			}
			chooseLines(t, tt, tab, 4, 4)
			snapshot(t, tt, "lines-chosen-"+layout)
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
		})
	}
}

func TestStagingTheFileAfterALineDoesNotPanic(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tt.Click("Local Changes")
	tt.Frame()
	chooseLines(t, tt, tab, 4, 4)
	if err := tt.Click("Stage Lines"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Stage main.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if rest, _ := r.WorkDiff("main.go", git.DiffOptions{}); strings.TrimSpace(rest) != "" {
		t.Fatalf("unstaged:\n%s", rest)
	}
}

func TestLinesOfAnUntrackedFileCanBeStaged(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tt.Click("Local Changes")
	tt.Frame()
	if err := tt.Click("notes.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	chooseLines(t, tt, tab, 0, 0)
	if err := tt.Click("Stage Lines"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	tt.Frame()
	staged, _ := r.StagedDiff("notes.txt", git.DiffOptions{})
	if !strings.Contains(staged, "+todo") {
		t.Fatalf("staged:\n%s", staged)
	}
}
