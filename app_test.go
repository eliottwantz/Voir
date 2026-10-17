package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

// demoRepo makes a repository with a few branches, a merge, a tag, a stash
// and local changes.
func demoRepo(t *testing.T) *git.Repo {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	r, err := git.Init(filepath.Join(dir, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][2]string{{"user.name", "Ada Lovelace"}, {"user.email", "ada@example.com"}, {"commit.gpgsign", "false"}} {
		r.SetConfig(kv[0], kv[1])
	}
	r.Do("checkout", "-q", "-b", "main")
	write := func(name, text string) {
		p := filepath.Join(r.Root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(msg string) {
		if err := r.Stage(); err != nil {
			t.Fatal(err)
		}
		if err := r.Commit(git.CommitOptions{Message: msg}); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n")
	write("README.md", "# Demo\n")
	commit("Initial commit")
	r.CreateTag("v0.1.0", "HEAD", "")
	r.CreateBranch("feature/login", "HEAD", true, git.KeepChanges)
	write("login.go", "package main\n\nfunc login() {}\n")
	commit("Add login")
	r.Checkout("main", git.KeepChanges)
	write("main.go", "package main\n\nfunc main() {\n\tprintln(\"hello, world\")\n}\n")
	commit("Greet the world")
	if err := r.Merge("feature/login", git.MergeOptions{NoFastForward: true}); err != nil {
		t.Fatal(err)
	}
	write("README.md", "# Demo\n\nA demo.\n")
	r.StashPush("work in progress", false)
	// Local changes: one modified, one staged, one untracked.
	write("main.go", "package main\n\nfunc main() {\n\tprintln(\"hello, world!\")\n\tprintln(\"bye\")\n}\n")
	write("notes.txt", "todo\n")
	write("staged.txt", "staged\n")
	r.Stage("staged.txt")
	return r
}

func testApp(t *testing.T) (*app, *git.Repo) {
	t.Helper()
	r := demoRepo(t)
	a := newApp(&store{data: stored{Settings: defaultSettings(), SidebarW: 230, ListH: 330, FilesW: 280}})
	if err := a.openRepo(r.Root); err != nil {
		t.Fatal(err)
	}
	return a, r
}

func snapshot(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("MYFORK_SNAPSHOTS")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryShowsTheRepository(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	if !tab.loaded || tab.err != nil {
		t.Fatalf("not loaded: %v", tab.err)
	}
	if err := tt.Click("Tags"); err != nil { // the section opens
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"Local Changes", "All Commits", "Branches", "main", "feature/login", "Tags", "v0.1.0",
		"Stashes", "Greet the world", "Add login", "Initial commit", "Uncommitted changes", "Ada Lovelace"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	if len(tab.commits) != 5 || len(tab.rows) != 5 { // four commits, and the stash
		t.Fatalf("commits = %d, graph rows = %d", len(tab.commits), len(tab.rows))
	}
	// The merge makes a second lane.
	if tab.layout.MaxCols < 2 {
		t.Errorf("graph uses %d columns", tab.layout.MaxCols)
	}
	snapshot(t, tt, "history")
}

func TestSelectingACommitShowsItsDiff(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	if err := tt.Click("Greet the world"); err != nil {
		t.Fatal(err)
	}
	if tab.commit == nil || tab.commit.Subject != "Greet the world" {
		t.Fatalf("commit = %+v", tab.commit)
	}
	tt.Frame()
	if len(tab.changes) != 1 || tab.changes[0].Path != "main.go" {
		t.Fatalf("changes = %+v", tab.changes)
	}
	if tab.hist.state == nil {
		t.Fatalf("no diff: %+v", tab.hist)
	}
	if !strings.Contains(tab.hist.patch, `+	println("hello, world")`) {
		t.Errorf("patch = %q", tab.hist.patch)
	}
	snapshot(t, tt, "commit")
	if !tt.HasText("Changes") || tab.detail != 1 {
		t.Errorf("details open on tab %d", tab.detail)
	}
	// The other tabs of the details.
	tab.detail = 0
	tt.Frame()
	for _, want := range []string{"AUTHOR", "SHA", "PARENTS", "Ada Lovelace  <ada@example.com>"} {
		if !tt.HasText(want) {
			t.Errorf("summary lacks %q: %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "summary")
	tab.detail = 2
	tt.Frame()
	a.runHeld()
	tt.Frame()
	if !tt.HasText("main.go") || !tt.HasText("README.md") {
		t.Errorf("tree: %q", tt.Texts())
	}
	snapshot(t, tt, "tree")
}

func TestStageAndCommitFromLocalChanges(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	if err := tt.Click("Local Changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tab.mode != modeChanges {
		t.Fatal("not in Local Changes")
	}
	if len(tab.unstaged) != 2 || len(tab.staged) != 1 {
		t.Fatalf("unstaged = %+v, staged = %+v", tab.unstaged, tab.staged)
	}
	if tab.work.state == nil {
		t.Fatalf("no diff of the first file: %+v", tab.work)
	}
	snapshot(t, tt, "changes")

	// Stage everything, then write a message and commit.
	if err := tt.Click("Stage All"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(tab.unstaged) != 0 || len(tab.staged) != 3 {
		t.Fatalf("after Stage All: unstaged = %d, staged = %d", len(tab.unstaged), len(tab.staged))
	}
	if err := tt.Click("Commit message"); err != nil {
		t.Fatal(err)
	}
	tt.Type("Say bye")
	tt.Frame()
	if err := tt.Click("Commit 3 files"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	cs, _ := r.Log(git.LogOptions{Limit: 1})
	if len(cs) != 1 || cs[0].Subject != "Say bye" {
		t.Fatalf("head = %+v", cs)
	}
	if tab.hasChanges() {
		t.Errorf("still changes: %+v", tab.status.Entries)
	}
	snapshot(t, tt, "committed")
}

func TestBranchDialogCreatesAndChecksOut(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	if err := tt.Click("Branch"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.dialog == nil || !tt.HasText("Create branch") {
		t.Fatalf("no dialog: %q", tt.Texts())
	}
	snapshot(t, tt, "dialog")
	if err := tt.Click("Create Branch"); err == nil && a.dialog != nil && a.dialog.open {
		// The button is disabled until a name is typed.
		if got := r.Branch(); got != "main" {
			t.Fatalf("created a branch without a name: %s", got)
		}
	}
	tt.Type("topic")
	tt.Frame()
	// Stash-and-reapply the local changes across the checkout.
	a.dialog.field("lc").text = "Stash and reapply"
	if err := tt.Click("Create Branch"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := r.Branch(); got != "topic" {
		t.Fatalf("branch = %q, dialog error %q", got, dialogErr(a))
	}
	st, _ := r.Status()
	if len(st.Entries) != 3 {
		t.Errorf("local changes lost across the checkout: %+v", st.Entries)
	}
}

func TestSearchFiltersCommits(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	if err := tt.Click("Search commits"); err != nil {
		t.Fatal(err)
	}
	tt.Type("login")
	tt.Frame()
	var subjects []string
	for _, m := range tab.matches {
		subjects = append(subjects, m.Subject)
	}
	if len(subjects) != 2 || !tt.HasText("Add login") || !tt.HasText("Merge branch 'feature/login'") {
		t.Fatalf("matches = %q (filter %q)", subjects, tab.filter)
	}
	if tt.HasText("Initial commit") {
		t.Errorf("the list still shows other commits: %q", tt.Texts())
	}
}

func TestHunkStageFromTheDiff(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tt.Click("Local Changes")
	tt.Frame()
	// main.go is the first unstaged file, with one hunk.
	if tab.work.path != "main.go" || len(tab.work.hunks) != 1 {
		t.Fatalf("work = %+v", tab.work)
	}
	if err := tt.Click("Stage Hunk"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	staged, _ := r.StagedDiff("main.go", git.DiffOptions{})
	if !strings.Contains(staged, "bye") {
		t.Fatalf("hunk not staged:\n%s", staged)
	}
}

func dialogErr(a *app) string {
	if a.dialog == nil {
		return ""
	}
	return a.dialog.err
}
