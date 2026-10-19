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
	a := newApp(newStore())
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

func TestTheWindowShowsTheGraphAndTheCommit(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	if !tab.loaded || tab.err != nil {
		t.Fatalf("not loaded: %v", tab.err)
	}
	// The sidebar is the repository's branches and the two views; the main
	// area is the graph of commits with the commit chosen in three tabs
	// under it.
	for _, want := range []string{"Branches", "main", "feature/login", "Stashes", "Filter commits",
		"Local Changes", "All Commits", "Merge branch 'feature/login'", "Greet the world", "Add login",
		"Initial commit", "Ada Lovelace", "login.go"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	// The uncommitted changes are not a commit of the graph.
	if tt.HasText("Uncommitted changes") {
		t.Errorf("the graph shows the uncommitted changes: %q", tt.Texts())
	}
	for _, want := range []string{"Summary", "Changes", "File Tree"} {
		if !tt.HasText(want) {
			t.Errorf("no %q tab in %q", want, tt.Texts())
		}
	}
	if len(tab.commits) != 5 || len(tab.rows) != 5 { // four commits, and the stash
		t.Fatalf("commits = %d, graph rows = %d", len(tab.commits), len(tab.rows))
	}
	if tab.layout.MaxCols < 2 {
		t.Errorf("graph uses %d columns", tab.layout.MaxCols)
	}
	snapshot(t, tt, "window")

	// The tags, in the sidebar, once its section is opened.
	if err := tt.Click("Tags"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"Tags", "v0.1.0"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "sidebar")
}

func TestTheCommitShowsInThePanel(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	if err := tt.Click("Greet the world"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	cv := tab.cv
	if cv.commit == nil || cv.commit.Subject != "Greet the world" {
		t.Fatalf("commit = %+v", cv.commit)
	}
	rv := cv.rv
	if rv == nil || rv.loading || len(rv.files) != 1 || rv.files[0].path != "main.go" || rv.files[0].added != 1 || rv.files[0].deleted != 1 {
		t.Fatalf("review = %+v", rv)
	}
	for _, want := range []string{"Greet the world", "Ada Lovelace", "main.go"} {
		if !strings.Contains(strings.Join(tt.Texts(), "|"), want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	// The diff shows straight: no header with a Viewed mark, and the file is
	// not folded.
	if strings.Contains(strings.Join(tt.Texts(), "|"), "Viewed") {
		t.Errorf("the commit's diff has a header: %q", tt.Texts())
	}
	if rv.files[0].collapsed {
		t.Errorf("the commit's file is folded: %+v", rv.files[0])
	}
	snapshot(t, tt, "commit")
}

func TestCommitTabsShowTheFilesAndDiffs(t *testing.T) {
	r := conflictFreeMixedRepo(t)
	a := appFor(t, r)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tt.Frame()
	if tab.cv.rv == nil || len(tab.cv.rv.files) != 4 {
		t.Fatalf("review = %+v", tab.cv.rv)
	}
	// The Changes tab: the commit's files as a tree, each with its diff.
	for _, want := range []string{"Filter files", "src", "app.go", "README.md", "app_test.go", "mise.toml"} {
		if !tt.HasText(want) {
			t.Errorf("the changes lack %q: %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "changes-tab")
	// Choosing a file shows its diff.
	if err := tt.Click("app_test.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tab.cv.file != "src/app_test.go" {
		t.Fatalf("chosen %q", tab.cv.file)
	}
	// The Summary tab: the commit's message and the files it changed.
	if err := tt.Click("Summary"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"Changed files", "README.md", "src/app.go", "src/app_test.go", "mise.toml"} {
		if !tt.HasText(want) {
			t.Errorf("the summary lacks %q: %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "commit-summary")
}

func TestTheGraphChoosesTheCommitInThePanel(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	// The graph, and the commit chosen under it.
	for _, want := range []string{"Graph", "Description", "Author", "Greet the world", "Add login", "Summary", "Changes", "File Tree"} {
		if !tt.HasText(want) {
			t.Errorf("the window lacks %q: %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "graph")
	if err := tt.Click("Add login"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tab.cv.commit == nil || tab.cv.commit.Subject != "Add login" {
		t.Fatalf("commit = %+v", tab.cv.commit)
	}
	if tab.cv.rv == nil || len(tab.cv.rv.files) != 1 || tab.cv.rv.files[0].path != "login.go" {
		t.Fatalf("review = %+v", tab.cv.rv)
	}
}

func TestUncommittedChangesOpenInThePanel(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	if tab.onWip {
		t.Fatal("Local Changes are shown before they are chosen")
	}
	// The sidebar's Local Changes view shows the files to stage, with the
	// diff of the one chosen beside them, and no graph.
	if err := tt.Click("Local Changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tab.onWip {
		t.Fatal("Local Changes are not shown")
	}
	for _, want := range []string{"Local Changes", "Unstaged", "Staged", "Stage All", "main.go", "staged.txt"} {
		if !tt.HasText(want) {
			t.Errorf("the changes lack %q: %q", want, tt.Texts())
		}
	}
	if tt.HasText("Filter commits") {
		t.Errorf("the graph is shown beside Local Changes: %q", tt.Texts())
	}
	if tab.wrv == nil || len(tab.wrv.files) != 1 || tab.wrv.files[0].path != tab.workPath {
		t.Errorf("the surface shows %+v, not %q alone", tab.wrv, tab.workPath)
	}
	snapshot(t, tt, "changes-view")

	// Choosing another file shows its diff in place of the one before.
	if err := tt.Click("notes.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tab.workPath != "notes.txt" || tab.wrv == nil || len(tab.wrv.files) != 1 || tab.wrv.files[0].path != "notes.txt" {
		t.Errorf("after choosing notes.txt: work = %q, surface = %+v", tab.workPath, tab.wrv)
	}
	if !tt.HasText("+ todo") {
		t.Errorf("the diff of notes.txt is not shown: %q", tt.Texts())
	}
	if tt.HasText("bye") {
		t.Errorf("the diff of main.go is still shown with it: %q", tt.Texts())
	}

	// All Commits takes the panel back to the commit's three tabs.
	if err := tt.Click("All Commits"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tab.onWip || tab.commit == nil {
		t.Errorf("the changes are still shown (%v) after choosing a commit", tab.onWip)
	}
}

func TestStageAndCommitFromLocalChanges(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	if err := tt.Click("Local Changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tab.onWip {
		t.Fatal("not in Local Changes")
	}
	if len(tab.unstaged) != 2 || len(tab.staged) != 1 {
		t.Fatalf("unstaged = %+v, staged = %+v", tab.unstaged, tab.staged)
	}
	if tab.wrv == nil || len(tab.wrv.files) != 1 || tab.wrv.files[0].path != tab.workPath {
		t.Fatalf("the diff of %q is not the one shown: %+v", tab.workPath, tab.wrv)
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
	// Shift+Cmd+S unstages everything with the focus in the message box,
	// and stages it again.
	if err := tt.Click("Commit message"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyS)
	tt.Frame()
	if len(tab.staged) != 0 || len(tab.unstaged) != 3 {
		t.Fatalf("after Shift+Cmd+S: unstaged = %d, staged = %d", len(tab.unstaged), len(tab.staged))
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyS)
	tt.Frame()
	if len(tab.unstaged) != 0 || len(tab.staged) != 3 {
		t.Fatalf("after Shift+Cmd+S again: unstaged = %d, staged = %d", len(tab.unstaged), len(tab.staged))
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
	if tab.onWip || tab.commit == nil || tab.commit.Subject != "Say bye" {
		t.Errorf("on the changes = %v, commit = %+v after committing", tab.onWip, tab.commit)
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
	tt := ui.NewTester(a.view, 1280, 1100)
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
	if !tab.uClosed["d:pkg/util"] {
		t.Error("the folder is still open")
	}
	if err := tt.Click("pkg/util"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tab.uClosed["d:pkg/util"] {
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

// The two lists of Local Changes hold one choice between them: choosing a file
// in one takes the mark off the file chosen in the other, and a file that
// moves to the other list takes the choice with it.
func TestOneListOfLocalChangesHoldsTheChoice(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	if err := tt.Click("Local Changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tab.uMulti.Has("main.go") || tab.sMulti.Len() != 0 || tab.workPath != "main.go" || tab.workStaged {
		t.Fatalf("chosen main.go: unstaged %d, staged %d, work %q staged %v", tab.uMulti.Len(), tab.sMulti.Len(), tab.workPath, tab.workStaged)
	}
	// A file of the staged list is chosen: the one of the other list is let go.
	if err := tt.Click("staged.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tab.sMulti.Has("staged.txt") || tab.uMulti.Len() != 0 || tab.workPath != "staged.txt" || !tab.workStaged {
		t.Fatalf("chosen staged.txt: unstaged %d, staged %d, work %q staged %v", tab.uMulti.Len(), tab.sMulti.Len(), tab.workPath, tab.workStaged)
	}
	// Staging the file chosen takes the choice with it to the staged list.
	if err := tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Stage main.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.runHeld()
	tt.Frame()
	if !tab.sMulti.Has("main.go") || tab.uMulti.Len() != 0 || tab.workPath != "main.go" || !tab.workStaged {
		t.Fatalf("staged main.go: unstaged %d, staged %d, work %q staged %v", tab.uMulti.Len(), tab.sMulti.Len(), tab.workPath, tab.workStaged)
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
	if err := tt.Click("Filter commits"); err != nil {
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
	if tab.wrv == nil || len(tab.wrv.files) != 1 || tab.wrv.files[0].path != "main.go" || len(tab.wrv.files[0].hunks) != 1 {
		t.Fatalf("surface = %+v", tab.wrv)
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

func TestChoosingAnotherFileScrollsToItsTop(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	if err := tt.Click("Greet the world"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	rv := a.tab().cv.rv
	rv.list.ScrollTo(len(rv.rows)-1, ui.End)
	tt.Frame()
	rv.showOnly(rv.only + 1)
	rv.showOnly(0)
	tt.Frame()
	if first, _ := rv.list.Visible(); first > 1 {
		t.Fatalf("the surface shows from row %d", first)
	}
}

func TestChoosingAnotherFileFoldsTheLinesExpanded(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	if err := tt.Click("Greet the world"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	rv := a.tab().cv.rv
	f := rv.files[0]
	f.expanded = map[int]gapShown{0: {top: 3}}
	rv.showOnly(-1)
	if len(f.expanded) != 0 {
		t.Fatalf("still expanded: %v", f.expanded)
	}
}

func TestChoosingAStashShowsItsChanges(t *testing.T) {
	a, _ := testApp(t)
	tab := a.tab()
	if len(tab.stashes) == 0 {
		t.Fatal("no stash in the demo repository")
	}
	// A hidden stash is not in the list, and is shown from its own commit.
	tab.commits, tab.rows = nil, nil
	tab.sideOpen["stashes"] = new(bool)
	tab.showStash("s:0")
	if tab.commit == nil || tab.commit.Hash != tab.stashes[0].Hash {
		t.Fatalf("commit = %+v, want the stash %s", tab.commit, tab.stashes[0].Hash)
	}
	if tab.onWip {
		t.Error("still on the local changes")
	}
}

func TestTheChosenCommitSurvivesAReloadBeyondTheFirstPage(t *testing.T) {
	a, r := testApp(t)
	for i := 0; i < logPage; i++ {
		if _, err := r.Do("commit", "--allow-empty", "-q", "-m", "filler"); err != nil {
			t.Fatal(err)
		}
	}
	tab := a.tab()
	tab.reload()
	tab.loadMore()
	if len(tab.commits) <= logPage {
		t.Fatalf("commits = %d, want more than one page", len(tab.commits))
	}
	last := len(tab.commits) - 1
	want := tab.commits[last].Hash
	tab.selectRow(last)
	// The window coming forward reloads the repository.
	a.refresh()
	if tab.selHash != want || tab.commit == nil || tab.commit.Hash != want {
		t.Fatalf("selection = %q (commit %v), want %q", tab.selHash, tab.commit, want)
	}
	if tab.sel != tab.commitRow(want) {
		t.Fatalf("sel = %d, want row %d", tab.sel, tab.commitRow(want))
	}
}
