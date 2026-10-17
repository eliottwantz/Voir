package app

import (
	"image/png"
	"os"
	"path/filepath"
	"slices"
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
	// History, in the sidebar, and the change of the commit at HEAD beside it.
	for _, want := range []string{"Filter history", "Uncommitted changes", "Merge branch 'feature/login'", "Greet the world", "Add login", "Initial commit", "Ada Lovelace", "login.go", "Viewed"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	if len(tab.commits) != 5 || len(tab.rows) != 5 { // four commits, and the stash
		t.Fatalf("commits = %d, graph rows = %d", len(tab.commits), len(tab.rows))
	}
	if tab.layout.MaxCols < 2 {
		t.Errorf("graph uses %d columns", tab.layout.MaxCols)
	}
	snapshot(t, tt, "history")

	// Branches, in the sidebar's other view.
	a.sideTab = sideBranches
	tt.Frame()
	if err := tt.Click("Tags"); err != nil { // the section opens
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"Branches", "main", "feature/login", "Tags", "v0.1.0", "Stashes"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	// Local Changes have a view of their own.
	for _, not := range []string{"Workspace", "All Commits"} {
		if tt.HasText(not) {
			t.Errorf("the branches show %q", not)
		}
	}
	snapshot(t, tt, "branches")
}

func TestTheSurfaceShowsEveryFileOfACommit(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	if err := tt.Click("Greet the world"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tab.commit == nil || tab.commit.Subject != "Greet the world" {
		t.Fatalf("commit = %+v", tab.commit)
	}
	rv := tab.rv
	if rv == nil || rv.loading || len(rv.files) != 1 || rv.files[0].path != "main.go" || rv.files[0].added != 1 || rv.files[0].deleted != 1 {
		t.Fatalf("review = %+v", rv)
	}
	for _, want := range []string{"Greet the world", "Ada Lovelace committed", "main.go", "Viewed"} {
		if !strings.Contains(strings.Join(tt.Texts(), "|"), want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	// The lines are in the card, and the file is read whole, so that the
	// lines between the changes can show.
	if f := rv.files[0]; !f.loaded || f.oldLines == nil || len(f.hunks) == 0 {
		t.Fatalf("file = %+v", f)
	}
	for _, want := range []string{"var greeting"} {
		if !strings.Contains(strings.Join(tt.Texts(), "|"), want) {
			t.Logf("no %q in %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "commit")

	// Marking a file viewed collapses it, and counts it.
	if err := tt.Click("Viewed main.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !rv.files[0].viewed || !rv.files[0].collapsed {
		t.Fatalf("not viewed: %+v", rv.files[0])
	}
	if !tab.viewed[viewedKey(tab.commit.Hash, "main.go")] {
		t.Error("the viewed mark is not kept")
	}
	snapshot(t, tt, "viewed")
}

func TestFilesTabListsTheChangedFiles(t *testing.T) {
	r := conflictFreeMixedRepo(t)
	a := appFor(t, r)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tt.Frame()
	if tab.rv == nil || len(tab.rv.files) != 4 || !tab.rv.grouped {
		t.Fatalf("review = %+v", tab.rv)
	}
	// Docs, code, tests, config: each a group, in that order.
	var got []string
	for _, g := range tab.rv.groups {
		got = append(got, kinds[g.kind].key)
	}
	if strings.Join(got, ",") != "docs,code,tests,config" {
		t.Fatalf("groups = %v", got)
	}
	for _, want := range []string{"Docs", "Code", "Tests", "Config", "0/1", "Documentation updates."} {
		if !strings.Contains(strings.Join(tt.Texts(), "|"), want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "groups")
	a.sideTab = sideFiles
	tt.Frame()
	for _, want := range []string{"Filter files", "src", "app.go", "README.md", "app_test.go"} {
		if !tt.HasText(want) {
			t.Errorf("files tab lacks %q: %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "files")
	// Choosing a file goes to its card.
	if err := tt.Click("app_test.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tab.rv.current < 0 || tab.rv.files[tab.rv.current].path != "src/app_test.go" {
		t.Fatalf("current = %d", tab.rv.current)
	}
}

func TestBranchesViewIsTheGraphAlone(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	a.showSide(sideBranches)
	tt.Frame()
	for _, want := range []string{"Graph", "Description", "Author", "Greet the world", "Add login"} {
		if !tt.HasText(want) {
			t.Errorf("graph lacks %q: %q", want, tt.Texts())
		}
	}
	// Nothing of the commit's details, nor of the workspace, is there.
	for _, not := range []string{"Summary", "Changed files", "AUTHOR", "Workspace", "All Commits"} {
		if tt.HasText(not) {
			t.Errorf("the branches view shows %q", not)
		}
	}
	snapshot(t, tt, "graph")
	if err := tt.Click("Greet the world"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tab.commit == nil || tab.commit.Subject != "Greet the world" {
		t.Fatalf("commit = %+v", tab.commit)
	}
	if a.sideTab != sideBranches {
		t.Errorf("choosing a commit left the graph for view %d", a.sideTab)
	}
	if tt.HasText("Viewed") {
		t.Error("the graph shows the commit's files")
	}

	// The same commit is in History, with its summary and its diff.
	a.showSide(sideHistory)
	tt.Frame()
	for _, want := range []string{"Greet the world", "Ada Lovelace", "main.go", "Viewed"} {
		if !tt.HasText(want) {
			t.Errorf("history lacks %q: %q", want, tt.Texts())
		}
	}
}

func TestUncommittedChangesOpenInTheFiles(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	if a.sideTab != sideHistory || tab.mode != modeHistory {
		t.Fatalf("view = %d, mode = %d", a.sideTab, tab.mode)
	}
	// Local Changes are not a view of the sidebar.
	if tt.HasText("Local Changes (⌘4)") {
		t.Error("Local Changes have a view of their own")
	}
	// Choosing them in the history goes at once to the files, which are the
	// files to stage, and the message to commit them with; the patch is beside.
	if err := tt.Click("Uncommitted changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.sideTab != sideFiles || tab.mode != modeChanges || !tab.onWip {
		t.Fatalf("view = %d, mode = %d, on the changes = %v", a.sideTab, tab.mode, tab.onWip)
	}
	for _, want := range []string{"Unstaged", "Staged", "Stage All", "main.go", "staged.txt"} {
		if !tt.HasText(want) {
			t.Errorf("the files lack %q: %q", want, tt.Texts())
		}
	}
	if tab.work.state == nil && tab.workPath == "" {
		t.Errorf("no file chosen: %+v", tab.work)
	}
	snapshot(t, tt, "changes-view")

	// The history keeps them chosen.
	a.showSide(sideHistory)
	tt.Frame()
	if tab.mode != modeChanges || tab.sel != 0 {
		t.Errorf("mode = %d, row = %d in the history", tab.mode, tab.sel)
	}
	if !tt.HasText("Stage All") && !tt.HasText("Filter history") {
		t.Errorf("history: %q", tt.Texts())
	}

	// Choosing them again, though they are chosen, goes to the files.
	if err := tt.Click("Uncommitted changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.sideTab != sideFiles || tab.mode != modeChanges {
		t.Errorf("clicking them again: view = %d, mode = %d", a.sideTab, tab.mode)
	}
	a.showSide(sideHistory)
	tt.Frame()

	// A commit takes the window back to the commit's change.
	tab.selectRow(1)
	tt.Frame()
	if tab.mode != modeHistory || tab.onWip || tab.commit == nil {
		t.Errorf("mode = %d, on the changes = %v after choosing a commit", tab.mode, tab.onWip)
	}

	// From the graph too.
	a.showSide(sideBranches)
	tt.Frame()
	if err := tt.Click("Uncommitted changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.sideTab != sideFiles || tab.mode != modeChanges {
		t.Errorf("from the graph: view = %d, mode = %d", a.sideTab, tab.mode)
	}
}

func TestFilesNeedACommitOrAPullRequest(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	if !tab.hasFiles() {
		t.Fatal("no files for the commit at HEAD")
	}
	a.showSide(sideFiles)
	if a.sideTab != sideFiles {
		t.Fatalf("view = %d, want the files", a.sideTab)
	}

	// With no commit chosen, the files are out of reach.
	tab.commit, tab.selHash, tab.rv = nil, "", nil
	tt.Frame()
	if a.sideTab == sideFiles {
		t.Error("the files stay shown without a commit")
	}
	a.sideTab = sideBranches
	a.showSide(sideFiles)
	if a.sideTab != sideBranches {
		t.Errorf("went to view %d without a commit", a.sideTab)
	}
	if len(a.messages) == 0 || !strings.Contains(a.messages[len(a.messages)-1], "commit or a pull request") {
		t.Errorf("messages = %q", a.messages)
	}
}

func TestStageAndCommitFromLocalChanges(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	if err := tt.Click("Uncommitted changes"); err != nil {
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
	// With nothing left to stage, the files are those of the commit made.
	if tab.mode != modeHistory || tab.commit == nil || tab.commit.Subject != "Say bye" {
		t.Errorf("mode = %d, commit = %+v after committing", tab.mode, tab.commit)
	}
	snapshot(t, tt, "committed")
}

func TestLocalChangesAreTreesOfFolders(t *testing.T) {
	a, r := testApp(t)
	for name, text := range map[string]string{"pkg/util/a.go": "package util\n", "pkg/util/b.go": "package util\n", "docs/guide.md": "# Guide\n"} {
		p := filepath.Join(r.Root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tab := a.tab()
	tab.reload()
	a.runHeld()
	tt := ui.NewTester(a.view, 1280, 800)
	tab.showChanges()
	tt.Frame()
	// Untracked files are always there, in their folders, which hold only a
	// folder joined into one.
	for _, want := range []string{"docs", "guide.md", "pkg/util", "a.go", "b.go", "main.go", "Unstaged", "Staged"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	// The sidebar says which file it is, and what to do with it: the patch does not.
	for _, not := range []string{"Show untracked files", "Stage File", "Discard…", "Unstage File"} {
		if tt.HasText(not) {
			t.Errorf("the changes show %q", not)
		}
	}
	snapshot(t, tt, "changes-tree")

	// A folder closes, and opens.
	if err := tt.Click("pkg/util"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("a.go") {
		t.Error("the folder is still open")
	}
	if err := tt.Click("pkg/util"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("a.go") {
		t.Error("the folder did not open again")
	}

	// A file is chosen: its patch shows.
	if err := tt.Click("b.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tab.workPath != "pkg/util/b.go" || tab.workStaged {
		t.Fatalf("chosen %q, staged %v", tab.workPath, tab.workStaged)
	}

	// A folder is staged whole.
	if err := tt.Click("Stage pkg/util"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.runHeld()
	tt.Frame()
	var staged []string
	for _, e := range tab.staged {
		staged = append(staged, e.Path)
	}
	if !slices.Contains(staged, "pkg/util/a.go") || !slices.Contains(staged, "pkg/util/b.go") {
		t.Fatalf("staged = %q", staged)
	}
	// The choice followed its file to the staged list.
	if tab.workPath != "pkg/util/b.go" || !tab.workStaged {
		t.Errorf("chosen %q, staged %v", tab.workPath, tab.workStaged)
	}
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
	if err := tt.Click("Filter history"); err != nil {
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
	tt.Click("Uncommitted changes")
	tt.Frame()
	// main.go is the first unstaged file, with one hunk.
	if tab.work.path != "main.go" || len(tab.work.hunks) != 1 {
		t.Fatalf("work = %+v", tab.work)
	}
	// The hunk's buttons show with the pointer over it.
	if tt.HasText("Stage Hunk") {
		t.Fatalf("the buttons show without the pointer: %q", tt.Texts())
	}
	hoverRow(t, tt, "  package main")
	snapshot(t, tt, "hunk-hover")
	if err := tt.Click("Stage Hunk"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
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

// conflictFreeMixedRepo makes a repository whose last commit changes a
// document, a source file, a test and a config file.
func conflictFreeMixedRepo(t *testing.T) *git.Repo {
	t.Helper()
	r := linearRepo(t, 1)
	write := func(name, text string) {
		p := filepath.Join(r.Root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(text), 0o644)
	}
	write("README.md", "# Mixed\n")
	write("src/app.go", "package src\n\nfunc App() {}\n")
	write("src/app_test.go", "package src\n\nfunc TestApp() {}\n")
	write("mise.toml", "[tools]\n")
	r.Stage()
	if err := r.Commit(git.CommitOptions{Message: "Mixed changes"}); err != nil {
		t.Fatal(err)
	}
	return r
}
