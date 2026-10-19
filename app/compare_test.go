package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestCmdClickComparesTwoCommits(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	if err := tt.Click("Greet the world"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tab.comparing() {
		t.Fatal("one commit chosen is compared")
	}
	if err := tt.ClickWith(ui.Cmd, "Initial commit"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	cv := tab.cv
	if !tab.comparing() || cv.base == nil || cv.base.Subject != "Initial commit" || cv.commit.Subject != "Greet the world" {
		t.Fatalf("base = %+v, commit = %+v", cv.base, cv.commit)
	}
	if tab.picked.Len() != 2 {
		t.Fatalf("%d commits chosen in the graph", tab.picked.Len())
	}
	if rv := cv.rv; rv == nil || rv.loading || !rv.cmp || len(rv.files) != 1 || rv.files[0].path != "main.go" {
		t.Fatalf("review = %+v", rv)
	}
	for _, want := range []string{"From", "To", "Initial commit", "Greet the world"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "compare")

	// A third replaces the second, the first chosen stays.
	if err := tt.ClickWith(ui.Cmd, "Add login"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	pair := []string{cv.pair[0].Subject, cv.pair[1].Subject}
	if cv.base == nil || !slices.Contains(pair, "Greet the world") || !slices.Contains(pair, "Add login") || tab.picked.Len() != 2 {
		t.Fatalf("pair = %v (%d chosen)", pair, tab.picked.Len())
	}
	oldest, newest := cv.base.Hash, cv.commit.Hash

	// The button swaps them: the newest is compared with the oldest.
	if err := tt.Click("Swap the commits compared"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if cv.base.Hash != newest || cv.commit.Hash != oldest || !cv.swapped {
		t.Fatalf("base = %q, commit = %q", cv.base.Subject, cv.commit.Subject)
	}
	if rv := cv.rv; rv == nil || rv.loading || rv.base != newest || rv.hash != oldest || len(rv.files) == 0 {
		t.Fatalf("review = %+v", rv)
	}

	// Cmd-click on one of the two lets go of it: the other shows alone.
	if err := tt.ClickWith(ui.Cmd, "Add login"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if tab.comparing() || tab.commit == nil || tab.commit.Subject != "Greet the world" || tab.picked.Len() != 1 {
		t.Fatalf("comparing = %v, commit = %+v", tab.comparing(), tab.commit)
	}
}

func TestCmdClickOnABranchComparesIt(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	if err := tt.Click("main"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.ClickWith(ui.Cmd, "feature/login"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if !tab.comparing() {
		t.Fatal("not comparing")
	}
	if tab.side != "b:main" || !tab.cmpSide["b:feature/login"] {
		t.Fatalf("side = %q, compared = %v", tab.side, tab.cmpSide)
	}
	var subjects []string
	for _, c := range tab.cv.pair {
		subjects = append(subjects, c.Subject)
	}
	if !slices.Contains(subjects, "Add login") || !slices.Contains(subjects, "Merge branch 'feature/login'") {
		t.Fatalf("pair = %v", subjects)
	}
	snapshot(t, tt, "compare-branches")

	// A plain click on a commit goes back to one.
	if err := tt.Click("Initial commit"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tab.comparing() || len(tab.cmpSide) != 0 {
		t.Fatalf("comparing = %v, compared = %v", tab.comparing(), tab.cmpSide)
	}
}
