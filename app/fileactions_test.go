package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// choose picks a commit of the graph by its subject.
func chooseCommit(t *testing.T, a *app, tt *ui.Tester, subject string) {
	t.Helper()
	if err := tt.Click(subject); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
}

func wantMenu(t *testing.T, tt *ui.Tester, want ...string) {
	t.Helper()
	got := tt.Menu()
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("no %q in the menu %q", w, got)
		}
	}
}

func TestLocalChangesFileMenu(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 900)
	tab := a.tab()
	tab.showChanges()
	tt.Frame()

	if err := tt.RightClick("main.go"); err != nil {
		t.Fatal(err)
	}
	wantMenu(t, tt, "Open", "Open With", "Show in Finder", "Blame/Timeline…", "History…", "Stage", "Discard Changes…",
		"Stage All", "Stash 1 File…", "Save as Patch…", "Copy Path", "Copy Full Path")
	for _, not := range []string{"External Diff", "File History…", "Blame…"} {
		if slices.Contains(tt.Menu(), not) {
			t.Errorf("the menu has %q", not)
		}
	}
	if err := tt.ChooseMenuItem("Copy Full Path"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	full := filepath.Join(r.Root, "main.go")
	if a.copied != full {
		t.Errorf("copied %q, want %q", a.copied, full)
	}
	if err := tt.RightClick("main.go"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Copy Path"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.copied != "main.go" {
		t.Errorf("copied %q", a.copied)
	}

	// Open goes to the application of the file's type.
	if err := tt.RightClick("main.go"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Open"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.opened) != 1 || a.opened[0] != full {
		t.Errorf("opened %q", a.opened)
	}

	// Stash 1 File… stashes that file only.
	if err := tt.RightClick("main.go"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Stash 1 File…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.dialog == nil || !a.dialog.open || !strings.Contains(a.dialog.title, "Stash 1 file") {
		t.Fatalf("dialog = %+v", a.dialog)
	}
	if err := a.dialog.run(a.dialog); err != nil {
		t.Fatal(err)
	}
	if out, _ := r.Read("stash", "list"); strings.Count(string(out), "\n") != 2 {
		t.Errorf("stashes: %s", out)
	}
	if out, _ := r.Read("status", "--porcelain"); strings.Contains(string(out), "main.go") || !strings.Contains(string(out), "notes.txt") {
		t.Errorf("status after stashing main.go:\n%s", out)
	}
}

func TestOpenSelectedFileIsShiftCmdO(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 900)
	tab := a.tab()
	item := find(t, a.menuTemplate(), "open-selected-file")
	if item.Accelerator != "CmdOrCtrl+Shift+O" {
		t.Fatalf("accelerator %q", item.Accelerator)
	}

	// In a commit's files: the file chosen in the Changes tab.
	chooseCommit(t, a, tt, "Greet the world")
	item.Click(item, nil)
	if len(a.opened) != 1 || a.opened[0] != filepath.Join(r.Root, "main.go") {
		t.Errorf("opened %q", a.opened)
	}
	// In the File Tree: the file shown there.
	a.opened = nil
	a.showPane(paneTree)
	tt.Frame()
	if err := tt.Click("README.md"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	item.Click(item, nil)
	if len(a.opened) != 1 || a.opened[0] != filepath.Join(r.Root, "README.md") {
		t.Errorf("opened %q from the file tree", a.opened)
	}

	// In Local Changes: the file chosen there.
	a.opened = nil
	tab.showChanges()
	tt.Frame()
	if err := tt.Click("notes.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	item.Click(item, nil)
	if len(a.opened) != 1 || a.opened[0] != filepath.Join(r.Root, "notes.txt") {
		t.Errorf("opened %q from Local Changes", a.opened)
	}

	// A file that is not in the working directory says so.
	a.opened = nil
	os.Remove(filepath.Join(r.Root, "notes.txt"))
	item.Click(item, nil)
	if len(a.opened) != 0 {
		t.Errorf("opened %q", a.opened)
	}
}

func TestCommitFileTreeMenuAndCopyKeys(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 900)
	chooseCommit(t, a, tt, "Greet the world")
	if err := tt.RightClick("main.go"); err != nil {
		t.Fatal(err)
	}
	wantMenu(t, tt, "Open", "Open With", "Show in Finder", "Reset File to", "Blame/Timeline…", "History…", "Save as…", "Show in File Tree", "Copy Path", "Copy Full Path")
	if slices.Contains(tt.Menu(), "External Diff") {
		t.Error("the menu has External Diff")
	}
	tt.CloseMenu()

	// ⌘C and ⇧⌘C copy the path of the file chosen, when the tree has the focus,
	// and so does Copy of the Edit menu.
	if err := tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyC)
	tt.Frame()
	if a.copied != "main.go" {
		t.Errorf("⌘C copied %q", a.copied)
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyC)
	tt.Frame()
	if want := filepath.Join(r.Root, "main.go"); a.copied != want {
		t.Errorf("⇧⌘C copied %q, want %q", a.copied, want)
	}
	a.copied = ""
	tt.Command("copy")
	tt.Frame()
	if a.copied != "main.go" {
		t.Errorf("Copy copied %q", a.copied)
	}

	// Show in File Tree shows it in the File Tree.
	if err := tt.RightClick("main.go"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Show in File Tree"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.tab().cv.pane != paneTree || a.tab().cv.tree.path != "main.go" {
		t.Errorf("pane %v, tree path %q", a.tab().cv.pane, a.tab().cv.tree.path)
	}
}

func TestResetFileToACommit(t *testing.T) {
	a, r := testApp(t)
	tt := ui.NewTester(a.view, 1280, 900)
	chooseCommit(t, a, tt, "Greet the world")
	cm := a.tab().commit
	if err := tt.RightClick("main.go"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Reset File to", "Parent ("+cm.Parents[0][:7]+")"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.dialog == nil || !a.dialog.open || !strings.Contains(a.dialog.title, "main.go") {
		t.Fatalf("dialog = %+v", a.dialog)
	}
	if err := a.dialog.run(a.dialog); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(r.Root, "main.go"))
	if strings.Contains(string(data), "world") || strings.Contains(string(data), "bye") {
		t.Errorf("main.go is %q", data)
	}
}

func TestSaveWorkPatchAndStashFilesInGit(t *testing.T) {
	r := demoRepo(t)
	patch, err := r.WorkPatch([]string{"main.go", "notes.txt"}, map[string]bool{"notes.txt": true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"+\tprintln(\"bye\")", "+++ b/notes.txt", "+todo"} {
		if !strings.Contains(patch, want) {
			t.Errorf("no %q in\n%s", want, patch)
		}
	}
	if strings.Contains(patch, "staged.txt") {
		t.Errorf("the patch has a file that was not asked for:\n%s", patch)
	}
}
