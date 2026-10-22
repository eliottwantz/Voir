package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFindRangesIgnoreCase(t *testing.T) {
	got := findRanges("Foo foo FOO", "foo")
	if len(got) != 3 || got[0] != [2]int{0, 3} || got[2] != [2]int{8, 11} {
		t.Errorf("ranges = %v", got)
	}
	if findRanges("abc", "") != nil {
		t.Error("an empty search finds something")
	}
}

func TestSearchStepsThroughCommitsAndCloses(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tab.focusSearch()
	tt.Frame()
	for _, want := range []string{"Search SHA, author or message", "Done"} {
		if !tt.HasText(want) {
			t.Fatalf("the search lacks %q: %q", want, tt.Texts())
		}
	}
	tt.Type("login")
	tt.Frame()
	if tab.rowCount() != 2 {
		t.Fatalf("rows = %d", tab.rowCount())
	}
	if err := tt.Click("Next result (↩)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tab.sel != 0 {
		t.Errorf("after Next: sel = %d", tab.sel)
	}
	tt.Click("Next result (↩)")
	tt.Frame()
	if tab.sel != 1 {
		t.Errorf("after Next twice: sel = %d", tab.sel)
	}
	tt.Click("Previous result (⇧↩)")
	tt.Frame()
	if tab.sel != 0 {
		t.Errorf("after Previous: sel = %d", tab.sel)
	}
	tt.Click("Previous result (⇧↩)")
	tt.Frame()
	if tab.sel != 1 {
		t.Errorf("Previous does not go round: sel = %d", tab.sel)
	}
	if err := tt.Click("Done"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tab.searchOpen || tab.filter != "" {
		t.Errorf("after Done: open %v, filter %q", tab.searchOpen, tab.filter)
	}
}

func TestFindInTheChangesOfACommit(t *testing.T) {
	r := conflictFreeMixedRepo(t)
	a := appFor(t, r)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tt.Frame()
	rv := tab.cv.rv
	if rv == nil {
		t.Fatal("no review")
	}
	// ⌘F with the pointer over the commit's changes searches them.
	tab.cv.hover = true
	a.findItem(nil, nil)
	tt.Frame()
	if !rv.find.open {
		t.Fatalf("the search is not open: graph %v", tab.searchOpen)
	}
	if !tt.HasText("Find in changes") {
		t.Fatalf("no bar: %q", tt.Texts())
	}
	tt.Type("a")
	tt.Frame()
	if len(rv.find.hits) < 2 {
		t.Fatalf("hits = %+v", rv.find.hits)
	}
	if rv.find.cur != 0 || !strings.HasPrefix(rv.findStatus(), "1 of ") {
		t.Errorf("cur = %d, status %q", rv.find.cur, rv.findStatus())
	}
	if err := tt.Click("Next result (↩)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if rv.find.cur != 1 {
		t.Errorf("after Next: cur = %d", rv.find.cur)
	}
	tt.Click("Previous result (⇧↩)")
	tt.Click("Previous result (⇧↩)")
	tt.Frame()
	if rv.find.cur != len(rv.find.hits)-1 {
		t.Errorf("Previous does not go round: cur = %d of %d", rv.find.cur, len(rv.find.hits))
	}
	rv.find.query = "zzzzzz"
	tt.Frame()
	if len(rv.find.hits) != 0 || rv.findStatus() != "No results" {
		t.Errorf("hits = %d, status %q", len(rv.find.hits), rv.findStatus())
	}
	if err := tt.Click("Done"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if rv.find.open || tt.HasText("Find in changes") {
		t.Error("the search stays after Done")
	}
}
