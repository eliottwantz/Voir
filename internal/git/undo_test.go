package git

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func read(t *testing.T, r *Repo, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.Root, name))
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

// around takes the snapshots before and after fn.
func around(t *testing.T, r *Repo, work bool, fn func()) (before, after Snapshot) {
	t.Helper()
	before, err := r.Snapshot(work)
	if err != nil {
		t.Fatal(err)
	}
	fn()
	after, err = r.Snapshot(work)
	if err != nil {
		t.Fatal(err)
	}
	return before, after
}

func TestUndoCommitKeepsItsChangesStaged(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "one\n")
	commit(t, r, "first")
	first, _ := r.Resolve("HEAD")
	write(t, r, "a.txt", "two\n")
	before, after := around(t, r, false, func() { commit(t, r, "second") })
	if err := r.Restore(before, after, true, "undo"); err != nil {
		t.Fatal(err)
	}
	if h, _ := r.Resolve("HEAD"); h != first {
		t.Fatalf("HEAD = %s, want %s", h, first)
	}
	if r.Branch() != "main" || read(t, r, "a.txt") != "two\n" {
		t.Fatalf("branch %q, file %q", r.Branch(), read(t, r, "a.txt"))
	}
	st, _ := r.Status()
	if len(st.Entries) != 1 || !st.Entries[0].Staged() {
		t.Fatalf("status = %+v, want a.txt staged", st.Entries)
	}
	// Redo makes the commit come back, and it is the same one.
	second := after.HeadHash
	if err := r.Restore(after, before, true, "redo"); err != nil {
		t.Fatal(err)
	}
	if h, _ := r.Resolve("HEAD"); h != second {
		t.Fatalf("HEAD after redo = %s, want %s", h, second)
	}
}

func TestUndoTheFirstCommit(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "one\n")
	r.Stage()
	before, after := around(t, r, false, func() { r.Commit(CommitOptions{Message: "first"}) })
	if err := r.Restore(before, after, true, "undo"); err != nil {
		t.Fatal(err)
	}
	if r.HasHead() {
		t.Fatal("the branch is still born")
	}
	if err := r.Restore(after, before, true, "redo"); err != nil {
		t.Fatal(err)
	}
	if h, _ := r.Resolve("HEAD"); h != after.HeadHash {
		t.Fatalf("HEAD = %s", h)
	}
}

func TestUndoCheckoutAndBranches(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "one\n")
	commit(t, r, "first")
	// A new branch, checked out and committed on.
	before, after := around(t, r, false, func() {
		if err := r.CreateBranch("topic", "HEAD", true, KeepChanges); err != nil {
			t.Fatal(err)
		}
		write(t, r, "b.txt", "b\n")
		commit(t, r, "on topic")
	})
	// A change not committed stays across the undo.
	write(t, r, "a.txt", "local\n")
	if err := r.Restore(before, after, false, "undo"); err != nil {
		t.Fatal(err)
	}
	if r.Branch() != "main" {
		t.Fatalf("branch = %q", r.Branch())
	}
	if _, err := r.Resolve("topic"); err == nil {
		t.Fatal("topic still exists")
	}
	if read(t, r, "b.txt") != "<missing>" || read(t, r, "a.txt") != "local\n" {
		t.Fatalf("b.txt %q, a.txt %q", read(t, r, "b.txt"), read(t, r, "a.txt"))
	}
	if err := r.Restore(after, before, false, "redo"); err != nil {
		t.Fatal(err)
	}
	if r.Branch() != "topic" || read(t, r, "b.txt") != "b\n" || read(t, r, "a.txt") != "local\n" {
		t.Fatalf("after redo: branch %q, b.txt %q, a.txt %q", r.Branch(), read(t, r, "b.txt"), read(t, r, "a.txt"))
	}
}

func TestUndoDeleteBranchAndRename(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "one\n")
	commit(t, r, "first")
	r.CreateBranch("old", "HEAD", false, KeepChanges)
	before, after := around(t, r, false, func() { r.DeleteBranch("old", true) })
	if err := r.Restore(before, after, false, "undo"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Resolve("old"); err != nil {
		t.Fatal("old is not back")
	}
	// Renaming the branch checked out moves HEAD to the new name.
	before, after = around(t, r, false, func() {
		if err := r.RenameBranch("main", "trunk"); err != nil {
			t.Fatal(err)
		}
	})
	if err := r.Restore(before, after, false, "undo"); err != nil {
		t.Fatal(err)
	}
	if r.Branch() != "main" {
		t.Fatalf("branch = %q", r.Branch())
	}
	if _, err := r.Resolve("trunk"); err == nil {
		t.Fatal("trunk still exists")
	}
}

func TestUndoHardResetBringsTheFilesBack(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "one\n")
	commit(t, r, "first")
	write(t, r, "a.txt", "two\n")
	commit(t, r, "second")
	// Work of every kind: modified, staged, untracked, and ignored.
	write(t, r, ".gitignore", "*.log\n")
	r.Stage(".gitignore")
	write(t, r, "a.txt", "three\n")
	write(t, r, "new.txt", "new\n")
	write(t, r, "debug.log", "ignored\n")
	before, after := around(t, r, true, func() {
		if err := r.Reset("HEAD~1", ResetHard); err != nil {
			t.Fatal(err)
		}
		if err := r.DeleteUntracked("new.txt"); err != nil {
			t.Fatal(err)
		}
	})
	if read(t, r, "a.txt") != "one\n" || read(t, r, "new.txt") != "<missing>" {
		t.Fatal("the reset did not happen")
	}
	if err := r.Restore(before, after, false, "undo"); err != nil {
		t.Fatal(err)
	}
	if h, _ := r.Resolve("HEAD"); h != before.HeadHash {
		t.Fatalf("HEAD = %s", h)
	}
	for name, want := range map[string]string{"a.txt": "three\n", "new.txt": "new\n", ".gitignore": "*.log\n", "debug.log": "ignored\n"} {
		if got := read(t, r, name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	st, _ := r.Status()
	kinds := map[string]string{}
	for _, e := range st.Entries {
		switch {
		case e.Untracked:
			kinds[e.Path] = "untracked"
		case e.Staged():
			kinds[e.Path] = "staged"
		default:
			kinds[e.Path] = "unstaged"
		}
	}
	if kinds["a.txt"] != "unstaged" || kinds["new.txt"] != "untracked" || kinds[".gitignore"] != "staged" || len(kinds) != 3 {
		t.Fatalf("status = %v", kinds)
	}
	// Redo resets again.
	if err := r.Restore(after, before, false, "redo"); err != nil {
		t.Fatal(err)
	}
	if read(t, r, "a.txt") != "one\n" || read(t, r, "new.txt") != "<missing>" || read(t, r, "debug.log") != "ignored\n" {
		t.Fatal("redo did not reset")
	}
}

func TestUndoRefusesWhenTheRepositoryMovedOn(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "one\n")
	commit(t, r, "first")
	write(t, r, "a.txt", "two\n")
	before, after := around(t, r, false, func() { commit(t, r, "second") })
	write(t, r, "a.txt", "three\n")
	commit(t, r, "third, from a terminal")
	if err := r.Restore(before, after, true, "undo"); !errors.Is(err, ErrMovedOn) {
		t.Fatalf("err = %v, want ErrMovedOn", err)
	}
	// A discard whose files were edited since is not put back over them.
	before, after = around(t, r, true, func() {
		write(t, r, "a.txt", "four\n")
	})
	write(t, r, "a.txt", "five\n")
	if err := r.Restore(before, after, false, "undo"); !errors.Is(err, ErrMovedOn) {
		t.Fatalf("err = %v, want ErrMovedOn", err)
	}
	if read(t, r, "a.txt") != "five\n" {
		t.Fatal("the file was overwritten")
	}
}

func TestReflog(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "one\n")
	commit(t, r, "first")
	r.CreateBranch("topic", "HEAD", true, KeepChanges)
	write(t, r, "a.txt", "two\n")
	commit(t, r, "second")
	es, err := r.Reflog(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 3 {
		t.Fatalf("%d entries: %+v", len(es), es)
	}
	if es[0].Selector != "HEAD@{0}" || es[0].Action != "commit" || es[0].Message != "second" || es[0].Commit.Subject != "second" {
		t.Fatalf("entry 0 = %+v", es[0])
	}
	if es[1].Action != "checkout" || es[1].Message != "moving from main to topic" || es[1].Selector != "HEAD@{1}" {
		t.Fatalf("entry 1 = %+v", es[1])
	}
	if es[2].Action != "commit (initial)" || es[0].Time.IsZero() {
		t.Fatalf("entry 2 = %+v", es[2])
	}
	if es, _ := r.Reflog(1); len(es) != 1 {
		t.Fatalf("limit 1: %d entries", len(es))
	}
}
