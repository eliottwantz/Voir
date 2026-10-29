package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/voir/internal/git"
)

// subject is the subject of HEAD.
func subject(t *testing.T, r *git.Repo) string {
	t.Helper()
	cs, err := r.Log(git.LogOptions{Limit: 1})
	if err != nil || len(cs) == 0 {
		t.Fatalf("log: %v", err)
	}
	return cs[0].Subject
}

func TestCmdZUndoesACommitAndShiftCmdZRedoesIt(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	if err := tt.Click("Local Changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Commit message"); err != nil {
		t.Fatal(err)
	}
	tt.Type("Stage this")
	tt.Frame()
	// ⌘Z in the message box undoes the typing, not a commit.
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	if tab.msg == "Stage this" {
		t.Fatalf("⌘Z in the message box did not undo the typing")
	}
	tt.Type("Say bye")
	tt.Frame()
	if err := tt.Click("Commit 1 file"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if subject(t, r) != "Say bye" || len(tab.undos) != 1 || tab.undos[0].title != "Commit" || !tab.undos[0].soft {
		t.Fatalf("head %q, undos %+v", subject(t, r), tab.undos)
	}
	// Out of the message box, ⌘Z is the repository's.
	if err := tt.Click("All Commits"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	if got := subject(t, r); got != "Merge branch 'feature/login'" {
		t.Fatalf("after ⌘Z, HEAD is %q", got)
	}
	if !tt.HasText("Undid “Commit”") {
		t.Errorf("no toast: %q", tt.Texts())
	}
	// The commit's file is staged again.
	st, _ := r.Status()
	staged := false
	for _, e := range st.Entries {
		if e.Path == "staged.txt" && e.Staged() {
			staged = true
		}
	}
	if !staged || len(tab.undos) != 0 || len(tab.redos) != 1 {
		t.Fatalf("staged %v, undos %d, redos %d", staged, len(tab.undos), len(tab.redos))
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	tt.Frame()
	if got := subject(t, r); got != "Say bye" {
		t.Fatalf("after ⇧⌘Z, HEAD is %q", got)
	}
	if len(tab.undos) != 1 || len(tab.redos) != 0 {
		t.Fatalf("undos %d, redos %d", len(tab.undos), len(tab.redos))
	}
}

func TestUndoBringsBackDiscardedFiles(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tab.showChanges()
	tt.Frame()
	var es []git.StatusEntry
	for _, e := range tab.unstaged {
		es = append(es, e)
	}
	tab.discardEntries(es)
	tt.Frame()
	if a.dialog == nil || !strings.Contains(a.dialog.message, "Undo (⌘Z) brings them back") {
		t.Fatalf("dialog = %+v", a.dialog)
	}
	a.submitDialog(a.dialog)
	tt.Frame()
	if _, err := os.Stat(filepath.Join(r.Root, "notes.txt")); err == nil {
		t.Fatal("notes.txt was not discarded")
	}
	if len(tab.undos) != 1 || !tab.undos[0].after.HasWork() {
		t.Fatalf("undos = %+v", tab.undos)
	}
	tab.undo()
	tt.Frame()
	b, _ := os.ReadFile(filepath.Join(r.Root, "notes.txt"))
	m, _ := os.ReadFile(filepath.Join(r.Root, "main.go"))
	if string(b) != "todo\n" || !strings.Contains(string(m), "bye") {
		t.Fatalf("notes.txt %q, main.go %q", b, m)
	}
	// The staged file stays staged, the others unstaged.
	if len(tab.staged) != 1 || len(tab.unstaged) != 2 {
		t.Fatalf("staged %+v, unstaged %+v", tab.staged, tab.unstaged)
	}
}

func TestUndoADeletedBranch(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	// The button of the bar has nothing to undo yet.
	if _, ok := tt.Find("Nothing to undo"); !ok {
		t.Fatal("no Undo button in the bar")
	}
	tab.do("Delete branch", func(r *git.Repo) error { return r.DeleteBranch("feature/login", true) })
	tt.Frame()
	if _, err := r.Resolve("feature/login"); err == nil {
		t.Fatal("not deleted")
	}
	// Stage and unstage are not operations to undo.
	tab.stagePaths("notes.txt")
	tt.Frame()
	if len(tab.undos) != 1 {
		t.Fatalf("undos = %+v", tab.undos)
	}
	// The button names what it undoes, in every view.
	tab.showReflog()
	tt.Frame()
	if err := tt.Click("Undo “Delete branch”"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, err := r.Resolve("feature/login"); err != nil {
		t.Fatal("the branch is not back")
	}
	if !tt.HasText("feature/login") {
		t.Errorf("the sidebar does not show it: %q", tt.Texts())
	}
}

func TestUndoGivesUpWhenTheRepositoryMovedOn(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tab.do("Create tag", func(r *git.Repo) error { return r.CreateTag("v1", "HEAD", "") })
	tt.Frame()
	// The tag moves in a terminal.
	if _, err := r.Do("tag", "-f", "v1", "HEAD~1"); err != nil {
		t.Fatal(err)
	}
	tab.undo()
	tt.Frame()
	if len(tab.undos) != 0 {
		t.Fatalf("the step is still there: %+v", tab.undos)
	}
	if !tt.HasText("Can’t undo “Create tag”: the repository changed since") {
		t.Errorf("no message: %q", tt.Texts())
	}
	if _, err := r.Resolve("v1"); err != nil {
		t.Fatal("the tag was deleted")
	}
}

func TestTheReflogView(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	// A reset leaves a commit on no branch, which the reflog still has.
	gone, _ := r.Resolve("HEAD")
	if err := r.Reset("HEAD~1", git.ResetHard); err != nil {
		t.Fatal(err)
	}
	tab.reload()
	tt.Frame()
	if err := tt.Click("Reflog"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tab.onReflog || len(tab.reflog) == 0 {
		t.Fatalf("onReflog %v, %d entries", tab.onReflog, len(tab.reflog))
	}
	for _, want := range []string{"HEAD@{0}", "reset", "moving to HEAD~1", "HEAD@{1}", "Merge branch 'feature/login'", "Entry", "Action"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	// Choosing the merge's entry shows the commit no branch has, under the list.
	i := -1
	for k, e := range tab.reflog {
		if e.Commit.Hash == gone {
			i = k
			break
		}
	}
	if i < 0 {
		t.Fatalf("no entry for %s: %+v", gone, tab.reflog)
	}
	tab.chooseReflog(i)
	tt.Frame()
	if tab.commit == nil || tab.commit.Hash != gone {
		t.Fatalf("commit shown = %+v", tab.commit)
	}
	// A reload keeps it, though it is not in the graph.
	tab.reload()
	tt.Frame()
	if tab.commit == nil || tab.commit.Hash != gone || tab.reflogSel != i {
		t.Fatalf("after reload: commit %+v, sel %d", tab.commit, tab.reflogSel)
	}
	snapshot(t, tt, "reflog")
	// All Commits goes back to the graph.
	if err := tt.Click("All Commits"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tab.onReflog || !tt.HasText("Description") || tt.HasText("Entry") {
		t.Fatalf("still on the reflog: %v %q", tab.onReflog, tt.Texts())
	}
	// View ▸ Reflog (⌘3) shows it again.
	item := find(t, a.menuTemplate(), "view-reflog")
	if item.Accelerator != "CmdOrCtrl+3" {
		t.Fatalf("accelerator %q", item.Accelerator)
	}
	item.Click(item, nil)
	tt.Frame()
	if !tab.onReflog {
		t.Fatal("the menu did not show the reflog")
	}
	// Undo and Redo leave ⌘Z to the text fields: the menu does not carry it.
	if it := find(t, a.menuTemplate(), "undo"); it.Accelerator != "" || it.Label != "Undo" {
		t.Fatalf("undo item = %+v", it)
	}
}
