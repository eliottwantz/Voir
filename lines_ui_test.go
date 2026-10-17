package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

func TestStageSelectedLinesFromTheView(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tt.Click("Uncommitted changes")
	tt.Frame()
	if tab.work.path != "main.go" {
		t.Fatalf("work = %+v", tab.work)
	}
	if err := tt.Click("Select Lines"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tab.lines.on || len(tab.lines.rows) == 0 {
		t.Fatalf("line mode not on: %+v", tab.lines)
	}
	snapshot(t, tt, "lines")
	// The patch replaces "hello, world" and adds "bye": pick the addition of
	// "bye" by clicking its row.
	if err := tt.Click("+ \tprintln(\"bye\")"); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	tt.Frame()
	if n := len(tab.lines.sel); n != 1 {
		t.Fatalf("selected %d lines", n)
	}
	if !tt.HasText("1 line selected: 1 added, 0 removed") {
		t.Errorf("no count in %q", tt.Texts())
	}
	snapshot(t, tt, "lines-selected")
	if err := tt.Click("Stage Selected"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	staged, _ := r.StagedDiff("main.go", git.DiffOptions{})
	if !strings.Contains(staged, `+	println("bye")`) || strings.Contains(staged, "hello, world!") {
		t.Fatalf("staged:\n%s", staged)
	}
	rest, _ := r.WorkDiff("main.go", git.DiffOptions{})
	if !strings.Contains(rest, `+	println("hello, world!")`) || strings.Contains(rest, "+\tprintln(\"bye\")") {
		t.Fatalf("unstaged:\n%s", rest)
	}
}

func TestLineModeOnAnUntrackedFile(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tt.Click("Uncommitted changes")
	tt.Frame()
	if err := tt.Click("notes.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Select Lines"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	st, _ := r.Status()
	for _, e := range st.Entries {
		if e.Path == "notes.txt" && e.Untracked {
			t.Fatalf("notes.txt is still untracked: %+v", e)
		}
	}
	if len(tab.lines.rows) == 0 {
		t.Fatalf("no lines to select: work %+v", tab.work)
	}
}
