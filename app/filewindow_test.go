package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestHistoryWindowListsTheCommitsOfAFile(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 900)
	chooseCommit(t, a, tt, "Greet the world")
	if err := tt.RightClick("main.go"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("History…"); err != nil {
		t.Fatal(err)
	}
	if len(a.fwins) != 1 {
		t.Fatalf("file windows = %d", len(a.fwins))
	}
	fw := a.fwins[0]
	if fw.kind != historyWindow || fw.path != "main.go" || len(fw.commits) != 2 {
		t.Fatalf("window = %+v", fw)
	}
	wt := ui.NewTester(fw.view, 1180, 780)
	wt.Frame()
	for _, want := range []string{"History", "main.go", "main", "Greet the world", "Initial commit", "Ada Lovelace"} {
		if !wt.HasText(want) {
			t.Errorf("no %q in the History window: %q", want, wt.Texts())
		}
	}
	// The newest commit is chosen, with its patch.
	if !strings.Contains(fw.pane.patch, `+	println("hello, world")`) {
		t.Errorf("no patch of the newest commit: %q", fw.pane.patch)
	}
	snapshot(t, wt, "file-history")

	// Another commit shows its own patch.
	if err := wt.Click("Initial commit"); err != nil {
		t.Fatal(err)
	}
	wt.Frame()
	if fw.sel != 1 || !strings.Contains(fw.pane.patch, "+func main() {") {
		t.Errorf("sel %d: %q", fw.sel, fw.pane.patch)
	}

	// Opening the same file's History again brings the window forward.
	a.fileHistoryWindow(a.tab(), "main.go")
	if len(a.fwins) != 1 {
		t.Errorf("file windows = %d after opening twice", len(a.fwins))
	}
	// A window about the same file's Blame is another window.
	a.blameWindow(a.tab(), "main.go", "")
	if len(a.fwins) != 2 {
		t.Errorf("file windows = %d", len(a.fwins))
	}
}

func TestBlameWindowShowsTheTimelineAndAStepper(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 900)
	tab := a.tab()
	tab.showChanges()
	tt.Frame()
	if err := tt.RightClick("main.go"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Blame/Timeline…"); err != nil {
		t.Fatal(err)
	}
	if len(a.fwins) != 1 || a.fwins[0].kind != blameWindowKind {
		t.Fatalf("file windows = %+v", a.fwins)
	}
	fw := a.fwins[0]
	wt := ui.NewTester(fw.view, 1180, 780)
	wt.Frame()
	d := fw.blames[fw.blameKey]
	if d == nil || len(d.lines) == 0 || d.err != "" {
		t.Fatalf("blame = %+v", d)
	}
	for _, want := range []string{"Blame", "main.go", "Greet the world", "Initial commit", "package main", "Ada Lovelace"} {
		if !wt.HasText(want) {
			t.Errorf("no %q in the Blame window: %q", want, wt.Texts())
		}
	}
	// The commit shown is the newest that changed the file: its added line is
	// marked as added, and has no line before it.
	if fw.sel != 0 {
		t.Fatalf("commit %d chosen", fw.sel)
	}
	var added []string
	for _, l := range d.lines {
		if _, isNew := d.lmap.Old(l.Line); isNew {
			added = append(added, l.Text)
		}
	}
	if len(added) != 1 || !strings.Contains(added[0], "hello, world") {
		t.Errorf("lines added by the commit: %q", added)
	}
	snapshot(t, wt, "file-blame")

	// The buttons go to the commit before, and back.
	if err := wt.Click("Previous commit"); err != nil {
		t.Fatal(err)
	}
	wt.Frame()
	if fw.sel != 1 || fw.commits[fw.sel].Subject != "Initial commit" {
		t.Fatalf("after Previous commit: %d", fw.sel)
	}
	if !wt.HasText(`println("hello")`) {
		t.Errorf("no line of the older version: %q", wt.Texts())
	}
	if err := wt.Click("Next commit"); err != nil {
		t.Fatal(err)
	}
	wt.Frame()
	if fw.sel != 0 {
		t.Errorf("after Next commit: %d", fw.sel)
	}
	// A click on the timeline chooses a commit too.
	if err := wt.Click(fw.commits[1].Short() + " Initial commit"); err != nil {
		t.Fatal(err)
	}
	wt.Frame()
	if fw.sel != 1 {
		t.Errorf("after a click on the timeline: %d", fw.sel)
	}
}

func TestBlameWindowAtACommitThatDidNotChangeTheFile(t *testing.T) {
	a, _ := testApp(t)
	tab := a.tab()
	// Add login is the commit of a branch that left main.go alone: blame at it
	// is blame at the commit that last changed the file before it.
	var hash string
	for _, c := range tab.commits {
		if c.Subject == "Add login" {
			hash = c.Hash
		}
	}
	a.blameWindow(tab, "main.go", hash)
	fw := a.fwins[0]
	if fw.sel != len(fw.commits)-1 || fw.commits[fw.sel].Subject != "Initial commit" {
		t.Errorf("chose commit %d of %d", fw.sel, len(fw.commits))
	}
}
