package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
	"github.com/eliott/myfork/internal/merge"
)

// conflictRepo leaves a merge stopped on a file changed in two places on both
// sides, and on a file one side deleted.
func conflictRepo(t *testing.T) *git.Repo {
	t.Helper()
	r := linearRepo(t, 0)
	write := func(name, text string) { os.WriteFile(filepath.Join(r.Root, name), []byte(text), 0o644) }
	commit := func(msg string) {
		r.Stage()
		if err := r.Commit(git.CommitOptions{Message: msg}); err != nil {
			t.Fatal(err)
		}
	}
	write("poem.txt", "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n")
	write("gone.txt", "to delete\n")
	commit("base")
	r.CreateBranch("other", "HEAD", true, git.KeepChanges)
	write("poem.txt", "one\nTWO (other)\nthree\nfour\nfive\nsix\nseven\neight\nnine\nTEN (other)\n")
	write("gone.txt", "changed on other\n")
	commit("other changes")
	r.Checkout("main", git.KeepChanges)
	write("poem.txt", "one\nTWO (main)\nthree\nfour\nfive\nsix\nseven\neight\nnine\nTEN (main)\n")
	os.Remove(filepath.Join(r.Root, "gone.txt"))
	commit("main changes")
	if err := r.Merge("other", git.MergeOptions{}); err == nil {
		t.Fatal("the merge did not conflict")
	}
	return r
}

func TestResolveAConflictInTheMergeEditor(t *testing.T) {
	r := conflictRepo(t)
	a := appFor(t, r)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	if tab.opState != "merge" {
		t.Fatalf("state = %q", tab.opState)
	}
	if !tt.HasText("A merge is in progress — 2 conflicts to resolve") {
		t.Errorf("no banner in %q", tt.Texts())
	}
	tt.Click("Local Changes")
	tt.Frame()
	if err := tt.Click("poem.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Resolve…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	st, ok := a.sheet.state.(*mergeState)
	if !ok || st.mode != mergeText {
		t.Fatalf("sheet = %+v", a.sheet)
	}
	if merge.Conflicts(st.chunks) != 2 || st.unresolved() != 2 {
		t.Fatalf("chunks = %+v", st.chunks)
	}
	if st.canSave() {
		t.Error("can save with markers in the result")
	}
	snapshot(t, tt, "merge")

	// The first conflict by clicking, the second by taking both.
	if err := tt.Click("Use Local"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if st.unresolved() != 1 || !strings.Contains(st.text(), "TWO (main)") || strings.Contains(st.text(), "TWO (other)") {
		t.Fatalf("after the first choice: unresolved %d\n%s", st.unresolved(), st.text())
	}
	st.choose(3, merge.TakeBoth)
	tt.Frame()
	if !st.canSave() {
		t.Fatalf("cannot save:\n%s", st.text())
	}
	snapshot(t, tt, "merge-resolved")
	if err := tt.Click("Save and Stage"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	got, _ := os.ReadFile(filepath.Join(r.Root, "poem.txt"))
	want := "one\nTWO (main)\nthree\nfour\nfive\nsix\nseven\neight\nnine\nTEN (main)\nTEN (other)\n"
	if string(got) != want {
		t.Fatalf("file = %q\nwant   %q", got, want)
	}
	st2, _ := r.Status()
	for _, e := range st2.Entries {
		if e.Path == "poem.txt" && e.Conflict {
			t.Errorf("poem.txt is still in conflict")
		}
	}
	if a.sheet != nil {
		t.Error("the sheet is still open")
	}
}

func TestMergeEditorWhenOneSideDeletedTheFile(t *testing.T) {
	r := conflictRepo(t)
	a := appFor(t, r)
	tt := ui.NewTester(a.view, 1280, 800)
	a.mergeEditor(a.tab(), "gone.txt")
	tt.Frame()
	st := a.sheet.state.(*mergeState)
	if st.mode != mergeGone || st.gone != "local" {
		t.Fatalf("mode %d gone %q", st.mode, st.gone)
	}
	if !tt.HasText("gone.txt was deleted on the local side and changed on the incoming side.") {
		t.Fatalf("texts %q", tt.Texts())
	}
	if err := tt.Click("Delete the File"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, err := os.Stat(filepath.Join(r.Root, "gone.txt")); err == nil {
		t.Error("gone.txt still exists")
	}
	if got, _ := r.Read("ls-files", "-u", "gone.txt"); len(got) != 0 {
		t.Errorf("gone.txt still in conflict: %s", got)
	}
}
