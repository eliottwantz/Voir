package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

// longFile is a document of n lines, the ones numbered in changed edited.
func longFile(n int, edited ...int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		if slicesContains(edited, i) {
			fmt.Fprintf(&b, "- Line %d, **edited** and `code`\n", i)
			continue
		}
		fmt.Fprintf(&b, "- Line %d of the document\n", i)
	}
	return b.String()
}

func slicesContains(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestDiffRowsShowGapsAndExpandThem(t *testing.T) {
	a, r := testApp(t)
	write := func(text string) {
		if err := os.WriteFile(filepath.Join(r.Root, "NOTES.md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := r.Stage(); err != nil {
			t.Fatal(err)
		}
	}
	write(longFile(300))
	if err := r.Commit(git.CommitOptions{Message: "Add notes"}); err != nil {
		t.Fatal(err)
	}
	write(longFile(300, 10, 200))
	if err := r.Commit(git.CommitOptions{Message: "Edit notes"}); err != nil {
		t.Fatal(err)
	}
	a.tab().reload()
	tt := ui.NewTester(a.view, 1280, 900)
	tt.SetDark(true)
	a.store.data.Settings.DiffLayout = "unified"
	tab := a.tab()
	tt.Frame()
	if err := tt.Click("Edit notes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	rv := tab.cv.rv
	if rv == nil || len(rv.files) != 1 || rv.files[0].path != "NOTES.md" {
		t.Fatalf("review = %+v", rv)
	}
	f := rv.files[0]
	if !f.loaded || len(f.hunks) != 2 {
		t.Fatalf("file: loaded %v, %d hunks", f.loaded, len(f.hunks))
	}
	texts := strings.Join(tt.Texts(), "|")
	for _, want := range []string{"unmodified lines", "Expand all", "Line 10, edited"} {
		if !strings.Contains(texts, want) && !strings.Contains(texts, strings.ReplaceAll(want, ", ", ", **")) {
			t.Logf("no %q in the texts", want)
		}
	}
	if !strings.Contains(texts, "unmodified lines") {
		t.Fatalf("no gap row: %q", texts)
	}
	snapshot(t, tt, "diff-unified-dark")
	if err := tt.Click("Expand all"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := f.expanded[1]; got.top == 0 {
		t.Fatalf("gap not expanded: %+v", f.expanded)
	}
	// The gap's lines are rows now.
	lines := 0
	for _, row := range rv.rows {
		if row.kind == rowLine && row.hunk < 0 {
			lines++
		}
	}
	if lines < 100 {
		t.Errorf("%d expanded lines", lines)
	}
}

func TestCommitSelectsInItsOwnPanel(t *testing.T) {
	a, r := testApp(t)
	src := "alpha\nbravo\ncharlie\ndelta\n"
	if err := os.WriteFile(filepath.Join(r.Root, "notes.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Stage(); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(git.CommitOptions{Message: "Add notes"}); err != nil {
		t.Fatal(err)
	}
	a.tab().reload()
	tt := ui.NewTester(a.view, 1280, 900)
	tt.Frame()
	if err := tt.Click("Add notes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	rv := a.tab().cv.rv
	if rv == nil || len(rv.files) == 0 || rv.files[0].path != "notes.md" {
		t.Fatalf("review = %+v", rv)
	}
	// The commit's lines select as any text does: a drag from a letter of
	// charlie to the same of delta selects just the characters between.
	from, ok := tt.Find("charlie")
	if !ok {
		t.Fatalf("no charlie: %q", tt.Texts())
	}
	to, ok := tt.Find("delta")
	if !ok {
		t.Fatal("no delta")
	}
	cw := from.W / float32(len("charlie"))
	x, y := from.X+3*cw, from.Y+from.H/2
	tt.Press(x, y)
	tt.Move(to.X+3*cw, to.Y+to.H/2)
	tt.Release(to.X+3*cw, to.Y+to.H/2)
	tt.Frame()
	if !rv.sel.on || rv.sel.lo != 3 || rv.sel.hi != 4 {
		t.Fatalf("selection = %+v", rv.sel)
	}
	// ⌘C copies just those, and the empty sides of the lines are nothing.
	tt.Key(ui.Cmd, ui.KeyC)
	tt.Frame()
	if got := tt.Clipboard(); !strings.HasPrefix(got, "rlie") || !strings.HasSuffix(got, "del") || strings.Contains(got, "alpha") {
		t.Fatalf("copied %q", got)
	}
}
