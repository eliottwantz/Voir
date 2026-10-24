package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

// goFile is a Go file of n functions, whose names the edits change.
func goFile(n int, edited ...int) string {
	var b strings.Builder
	b.WriteString("package big\n")
	for i := 1; i <= n; i++ {
		name := fmt.Sprintf("f%d", i)
		if slicesContains(edited, i) {
			name += "Edited"
		}
		fmt.Fprintf(&b, "\n// %s returns %d.\nfunc %s() int { return %d }\n", name, i, name, i)
	}
	return b.String()
}

func TestTheHunksTakeTheTokensOfTheirSides(t *testing.T) {
	patch := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,4 +1,4 @@\n package a\n-func old() {}\n+func new() {}\n // tail\n"
	f := &reviewFile{path: "a.go", patch: git.SplitPatchFiles(patch)[0].Patch}
	f.prepare()
	out := tokenizeHunks(f.path, f.hunks)
	for i, want := range []string{"package", "func", "func", "// tail"} {
		segs := out[[2]int{0, i}]
		line := f.hunks[0].Lines[i].Text
		if len(segs) == 0 || line[segs[0].Start:segs[0].End] != want {
			t.Errorf("line %d %q: %+v, want %q first", i, line, segs, want)
		}
	}
}

// commitOfGoFile is a repository whose last commit edits functions of a Go
// file of n, the second and fifth unless told, and the review of that commit.
func commitOfGoFile(t *testing.T, n int, edited ...int) (*app, *ui.Tester, *reviewFile) {
	t.Helper()
	r := demoRepo(t)
	write := func(text string) {
		if err := os.WriteFile(filepath.Join(r.Root, "big.go"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := r.Stage(); err != nil {
			t.Fatal(err)
		}
	}
	write(goFile(n))
	if err := r.Commit(git.CommitOptions{Message: "Add big.go"}); err != nil {
		t.Fatal(err)
	}
	if len(edited) == 0 {
		edited = []int{2, 5}
	}
	write(goFile(n, edited...))
	if err := r.Commit(git.CommitOptions{Message: "Edit big.go"}); err != nil {
		t.Fatal(err)
	}
	a := appFor(t, r)
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	rv := a.tab().cv.rv
	if rv == nil || len(rv.files) != 1 {
		t.Fatalf("review = %+v", rv)
	}
	tt.Frame()
	return a, tt, rv.files[0]
}

func TestAFileReadWholeIsColoredAsFarAsItShows(t *testing.T) {
	a, tt, f := commitOfGoFile(t, 50)
	if !f.loaded || len(f.newLines) < 150 {
		t.Fatalf("loaded %v, %d new lines", f.loaded, len(f.newLines))
	}
	// The old file is colored as far as its last deleted line, which is all
	// its tokens show, and the new one as far as the lines after the last
	// hunk shown without asking.
	lastDeleted, lastNew := 0, 0
	for i, h := range f.hunks {
		_, n := lastLines(&f.hunks[i])
		lastNew = max(lastNew, n)
		for _, l := range h.Lines {
			if l.Kind == '-' {
				lastDeleted = max(lastDeleted, l.Old)
			}
		}
	}
	if lastDeleted == 0 || len(f.oldHL) != lastDeleted {
		t.Errorf("%d old tokens, the last deleted line is %d", len(f.oldHL), lastDeleted)
	}
	if len(f.newHL) != lastNew+inlineGap {
		t.Errorf("%d new tokens, the last hunk ends at %d", len(f.newHL), lastNew)
	}
	// A short file is not colored twice: its hunks wait for the whole.
	if f.hunkHL != nil {
		t.Errorf("the hunks of a short file were colored apart")
	}
	// The end of the file expanded, the rest is colored.
	f.expanded = map[int]gapShown{len(f.hunks): {top: len(f.newLines)}}
	a.tab().cv.rv.dirty = true
	tt.Frame()
	if len(f.newHL) != len(f.newLines) {
		t.Errorf("expanded, %d tokens of %d lines", len(f.newHL), len(f.newLines))
	}
}

func TestALongFileShowsTheTokensOfItsHunksFirst(t *testing.T) {
	n := quickLines / 2
	_, _, f := commitOfGoFile(t, n, 2, n-1)
	if !f.loaded || f.newHL == nil {
		t.Fatalf("not read whole: loaded %v", f.loaded)
	}
	if f.hunkHL == nil {
		t.Error("the hunks of a long file were not colored first")
	}
}

func TestTheColoringOfAFileNoLongerDrawnIsDropped(t *testing.T) {
	a, tt, f := commitOfGoFile(t, 5)
	tab := a.tab()
	rv := tab.cv.rv
	// As if the file had scrolled away before its turn came.
	f.loaded, f.oldHL, f.newHL = false, nil, nil
	a.hold = true
	tab.markShown(rv, f)
	f.seen.Store(time.Now().Add(-2 * staleAfter).UnixNano())
	a.runHeld()
	if f.loading || f.loaded || f.newHL != nil {
		t.Fatalf("a file not drawn was colored: loading %v loaded %v", f.loading, f.loaded)
	}
	// Drawn again, it is.
	tt.Frame()
	a.runHeld()
	if !f.loaded || f.newHL == nil {
		t.Errorf("the file drawn again was not colored: loaded %v", f.loaded)
	}
}

func TestTheTokenQueueTakesTheLastFirst(t *testing.T) {
	var q tokenQueue
	var order []int
	for i := range 3 {
		q.jobs = append(q.jobs, func() { order = append(order, i) })
	}
	q.running = 1
	q.run()
	if fmt.Sprint(order) != "[2 1 0]" || q.running != 0 {
		t.Errorf("ran %v, %d running", order, q.running)
	}
}
