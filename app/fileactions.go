package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/eliott/voir/internal/git"
)

// fileRef is a file the context menus and the shortcuts of the files act on.
type fileRef struct {
	path string
	// rev is the commit the file is listed at, "" for the work tree; parent
	// is the commit it is compared with.
	rev, parent string
	// status is the letter of the change, 0 when it is not known.
	status byte
	// remote is set for the files of a pull request, whose commits are not
	// necessarily in the repository.
	remote bool
	// inTree is set in the File Tree, which is where Show in File Tree goes.
	inTree bool
	// showInTree shows the file in the File Tree of its commit; nil for none.
	showInTree func()
}

func (f fileRef) deleted() bool { return f.status == 'D' }

// abs is the path of a file of the repository on disk.
func (tab *repoTab) abs(p string) string {
	return filepath.Join(tab.repo.Root, filepath.FromSlash(p))
}

// openPath opens a file with the application the system has for its type.
func (a *app) openPath(p string) {
	if a.win == nil {
		a.opened = append(a.opened, p)
		return
	}
	go mygo.Shell.OpenPath(p)
}

// openPathWith opens a file with an application, given by its path.
func (a *app) openPathWith(appPath, p string) {
	if a.win == nil {
		a.opened = append(a.opened, filepath.Base(appPath)+" "+p)
		return
	}
	go func() {
		if runtime.GOOS == "darwin" {
			exec.Command("open", "-a", appPath, p).Run()
			return
		}
		mygo.Shell.OpenPath(p)
	}()
}

// openFile opens a file of the work tree in its application.
func (tab *repoTab) openFile(p string) {
	full := tab.abs(p)
	if _, err := os.Stat(full); err != nil {
		tab.app.say("%s is not in the working directory", p)
		return
	}
	tab.app.openPath(full)
}

// editorNames are the applications Open With offers when they are installed,
// before the others: the editors and the viewers files are opened in.
var editorNames = []string{
	"Visual Studio Code", "Cursor", "Zed", "Sublime Text", "Xcode", "Nova", "BBEdit",
	"TextEdit", "IntelliJ IDEA", "GoLand", "WebStorm", "PyCharm", "Android Studio",
	"Preview", "Pixelmator Pro", "Typora", "Obsidian",
}

// loadApps reads which applications are installed, for Open With.
func (a *app) loadApps() {
	go func() {
		apps, err := mygo.Shell.InstalledApps()
		if err != nil {
			return
		}
		a.update(func() { a.apps = apps })
	}()
}

// openWithApps are the applications of editorNames that are installed.
func (a *app) openWithApps() []mygo.InstalledApp {
	var out []mygo.InstalledApp
	for _, name := range editorNames {
		if i := slices.IndexFunc(a.apps, func(x mygo.InstalledApp) bool { return x.Name == name }); i >= 0 {
			out = append(out, a.apps[i])
		}
	}
	return out
}

// chooseOpenWith asks for an application and opens the file with it.
func (a *app) chooseOpenWith(p string) {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "Open With", DefaultPath: "/Applications", Filters: []mygo.FileFilter{{Name: "Applications", Extensions: []string{"app"}}}, ButtonLabel: "Open"})
		if err != nil || len(paths) == 0 {
			return
		}
		a.openPathWith(paths[0], p)
	}()
}

// copyPath puts a file's path in the repository on the clipboard, or its
// path on disk.
func (tab *repoTab) copyPath(p string, full bool) {
	if full {
		p = tab.abs(p)
	}
	tab.app.copy(p)
	tab.app.say("Copied %s", p)
}

// openItems are the entries that open a file: Open, Open With and Show in
// Finder.
func (tab *repoTab) openItems(m *ui.Menu, f fileRef) {
	a := tab.app
	gone := f.deleted()
	if a.keyed(m.Item("Open").Disabled(gone), "open-selected-file").Chosen() {
		tab.openFile(f.path)
	}
	m.Submenu("Open With", func(m *ui.Menu) {
		for _, ap := range a.openWithApps() {
			if m.Item(ap.Name).Disabled(gone).Chosen() {
				full := tab.abs(f.path)
				if _, err := os.Stat(full); err != nil {
					a.say("%s is not in the working directory", f.path)
				} else {
					a.openPathWith(ap.Path, full)
				}
			}
		}
		if m.Item("Other…").Disabled(gone).Chosen() {
			a.chooseOpenWith(tab.abs(f.path))
		}
	}).Disabled(gone)
	if m.Item("Show in Finder").Disabled(gone).Chosen() {
		revealInFinder(tab.abs(f.path))
	}
}

// historyItems are Blame/Timeline… and History….
func (tab *repoTab) historyItems(m *ui.Menu, f fileRef, untracked bool) {
	a := tab.app
	if m.Item("Blame/Timeline…").Disabled(f.deleted() || f.remote || untracked).Chosen() {
		a.blameWindow(tab, f.path, f.rev)
	}
	if m.Item("History…").Disabled(f.remote || untracked).Chosen() {
		a.fileHistoryWindow(tab, f.path)
	}
}

// copyItems are Copy Path and Copy Full Path.
func (tab *repoTab) copyItems(m *ui.Menu, f fileRef) {
	if tab.app.keyed(m.Item("Copy Path"), "copy-path").Chosen() {
		tab.copyPath(f.path, false)
	}
	if tab.app.keyed(m.Item("Copy Full Path"), "copy-full-path").Chosen() {
		tab.copyPath(f.path, true)
	}
}

// fileMenu is the menu of a file of a commit or a pull request.
func (tab *repoTab) fileMenu(m *ui.Menu, f fileRef) {
	a := tab.app
	tab.openItems(m, f)
	if f.rev != "" && !f.remote {
		m.Separator()
		m.Submenu("Reset File to", func(m *ui.Menu) {
			short := func(h string) string { return h[:min(7, len(h))] }
			if m.Item("This Commit (" + short(f.rev) + ")").Disabled(f.deleted()).Chosen() {
				tab.resetFile(f.path, f.rev)
			}
			if f.parent != "" && f.parent != git.EmptyTree && m.Item("Parent ("+short(f.parent)+")").Disabled(f.status == 'A').Chosen() {
				tab.resetFile(f.path, f.parent)
			}
		})
	}
	m.Separator()
	tab.historyItems(m, f, false)
	if f.rev != "" && !f.remote {
		m.Separator()
		if m.Item("Save as…").Disabled(f.deleted()).Chosen() {
			a.saveFileAs(tab, f.path, f.rev)
		}
		if f.showInTree != nil && !f.inTree && m.Item("Show in File Tree").Chosen() {
			f.showInTree()
		}
	}
	m.Separator()
	tab.copyItems(m, f)
}

// resetFile puts a file of the work tree back to what it was in a commit,
// after asking.
func (tab *repoTab) resetFile(p, rev string) {
	short := rev[:min(7, len(rev))]
	tab.app.confirmDiscard("Reset "+p+"?", fmt.Sprintf("The file goes back to what it was in %s. Changes to it in the working directory will be lost.", short), "Reset", func() error {
		return tab.repo.RestoreFile(rev, p)
	}, func() { tab.workStale = true })
}

// saveFileAs saves what a file was in a commit to a place chosen.
func (a *app) saveFileAs(tab *repoTab, p, rev string) {
	repo := tab.repo
	save := func(dest string) {
		data, err := repo.ReadFile(rev, p)
		if err == nil {
			err = os.WriteFile(dest, data, 0o644)
		}
		a.update(func() {
			if err != nil {
				a.fail("Save failed", err)
				return
			}
			a.say("Saved %s", filepath.Base(dest))
		})
	}
	if a.win == nil {
		return
	}
	go func() {
		dest, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Title: "Save as", DefaultPath: filepath.Base(p)})
		if err != nil || dest == "" {
			return
		}
		save(dest)
	}()
}

// workMenu is the menu of files of Local Changes: the files chosen, when the
// one clicked is among them, else that one.
func (tab *repoTab) workMenu(m *ui.Menu, e git.StatusEntry, staged bool) {
	a := tab.app
	paths := []string{e.Path}
	multi, entries := &tab.uMulti, tab.unstaged
	if staged {
		multi, entries = &tab.sMulti, tab.staged
	}
	if multi.Len() > 1 && multi.Has(e.Path) {
		paths = tab.chosenPaths(entries, multi)
	}
	f := fileRef{path: e.Path, status: e.Work}
	if staged {
		f.status = e.Index
	}
	if e.Untracked {
		f.status = 'A'
	}
	tab.openItems(m, f)
	m.Separator()
	tab.historyItems(m, f, e.Untracked)
	m.Separator()
	if staged {
		if a.keyed(m.Item("Unstage"), "stage-toggle").Chosen() {
			tab.unstagePaths(paths...)
		}
	} else {
		if a.keyed(m.Item("Stage"), "stage-toggle").Chosen() {
			tab.stagePaths(paths...)
		}
		if m.Item("Discard Changes…").Chosen() {
			tab.discardPaths(entries, paths)
		}
	}
	if e.Conflict && m.Item("Resolve Conflict…").Chosen() {
		a.mergeEditor(tab, e.Path)
	}
	m.Separator()
	if staged {
		if m.Item("Unstage All").Disabled(tab.busy != "").Chosen() {
			tab.unstagePaths()
		}
	} else if a.keyed(m.Item("Stage All"), "stage-all-toggle").Disabled(tab.busy != "").Chosen() {
		tab.stagePaths()
	}
	m.Separator()
	if m.Item("Ignore…").Disabled(staged).Chosen() {
		a.ignoreDialog(tab, e.Path)
	}
	m.Separator()
	label := "Stash " + plural(len(paths), "File", "Files") + "…"
	if m.Item(label).Chosen() {
		a.stashFilesDialog(tab, paths)
	}
	if m.Item("Save as Patch…").Chosen() {
		a.saveWorkPatch(tab, paths)
	}
	m.Separator()
	tab.copyItems(m, f)
}

// stashFilesDialog saves the changes to some files to a new stash.
func (a *app) stashFilesDialog(tab *repoTab, paths []string) {
	untracked := false
	if tab.status != nil {
		for _, e := range tab.status.Entries {
			untracked = untracked || e.Untracked && slices.Contains(paths, e.Path)
		}
	}
	a.showDialog(&dialog{
		title:   "Stash " + plural(len(paths), "file", "files"),
		message: "Save the changes to " + plural(len(paths), "file", "files") + " to a new stash",
		ok:      "Save Stash",
		fields:  []*field{text("msg", "Message:", "Stash message (optional)", "")},
		run: func(d *dialog) error {
			return tab.repo.StashPushPaths(d.str("msg"), untracked, paths)
		},
	})
}

// saveWorkPatch saves the changes to some files of Local Changes as a patch.
func (a *app) saveWorkPatch(tab *repoTab, paths []string) {
	untracked := map[string]bool{}
	if tab.status != nil {
		for _, e := range tab.status.Entries {
			if e.Untracked {
				untracked[e.Path] = true
			}
		}
	}
	name := "changes.patch"
	if len(paths) == 1 {
		name = filepath.Base(paths[0]) + ".patch"
	}
	repo := tab.repo
	if a.win == nil {
		return
	}
	go func() {
		dest, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Title: "Save as Patch", DefaultPath: name})
		if err != nil || dest == "" {
			return
		}
		patch, err := repo.WorkPatch(paths, untracked)
		if err == nil {
			err = os.WriteFile(dest, []byte(patch), 0o644)
		}
		a.update(func() {
			if err != nil {
				a.fail("Save as patch failed", err)
				return
			}
			a.say("Patch saved")
		})
	}()
}

// selectedFile is the file chosen in what the window shows, with what it is
// listed at: the file of the pull request or the commit under the cursor, or
// of Local Changes. v is the commit's own window when the key comes from it.
func (a *app) selectedFile(v *commitView) (*repoTab, fileRef, bool) {
	tab := a.tab()
	if v != nil {
		tab = v.tab
	}
	if tab == nil {
		return nil, fileRef{}, false
	}
	if v == nil {
		if pt := a.activePR(); pt != nil && a.pulls {
			pv := pt.pr
			rv := pv.rv
			if pv.commitSel != "" {
				rv = pv.commitRV
			}
			if rv != nil && rv.current >= 0 && rv.current < len(rv.files) {
				f := rv.files[rv.current]
				return pt, fileRef{path: f.path, status: f.status, remote: true}, true
			}
			return nil, fileRef{}, false
		}
		if tab.onWip {
			if tab.workPath == "" {
				return nil, fileRef{}, false
			}
			f := fileRef{path: tab.workPath}
			for _, e := range tab.status.Entries {
				if e.Path == tab.workPath {
					f.status = e.Work
					if tab.workStaged {
						f.status = e.Index
					}
					if e.Untracked {
						f.status = 'A'
					}
				}
			}
			return tab, f, true
		}
		v = tab.cv
	}
	if v.commit == nil {
		return nil, fileRef{}, false
	}
	p := v.file
	if v.pane == paneTree {
		p = v.tree.path
	}
	if p == "" {
		return nil, fileRef{}, false
	}
	f := v.fileRef(p)
	return tab, f, true
}

// fileRef is a file of the commit shown.
func (v *commitView) fileRef(p string) fileRef {
	f := fileRef{path: p, rev: v.commit.Hash, inTree: v.pane == paneTree}
	switch {
	case v.base != nil:
		f.parent = v.base.Hash
	case len(v.commit.Parents) > 0:
		f.parent = v.commit.Parents[0]
	}
	if v.rv != nil {
		if i := slices.IndexFunc(v.rv.files, func(g *reviewFile) bool { return g.path == p }); i >= 0 {
			f.status = v.rv.files[i].status
		}
	}
	f.showInTree = func() { v.showInTree(p) }
	return f
}

// showInTree shows a file in the File Tree of the commit.
func (v *commitView) showInTree(p string) {
	v.setPane(paneTree)
	v.openTreeFile(p)
	v.tree.closed = map[string]bool{}
	v.tree.rowsOK = false
}

// openSelectedFile is Open Selected File (⇧⌘O).
func (a *app) openSelectedFile(v *commitView) {
	tab, f, ok := a.selectedFile(v)
	if !ok {
		a.say("Choose a file to open it")
		return
	}
	if f.deleted() {
		a.say("%s was deleted", f.path)
		return
	}
	tab.openFile(f.path)
}

// fileKeys handles ⌘C and ⇧⌘C on the file shown, for a list of files that
// has the focus: the path in the repository, and the path on disk. The Edit
// menu owns ⌘C, which it sends as the command Copy: the list takes that too.
func (tab *repoTab) fileKeys(l ui.Element, p string) {
	if p == "" {
		return
	}
	tab.copyCommand(l, p)
	if tab.app.pressed(l, "copy-full-path") {
		tab.copyPath(p, true)
	} else if tab.app.pressed(l, "copy-path") {
		tab.copyPath(p, false)
	}
}

// copyCommand has an element that takes the focus copy the path of a file for
// the Copy of the Edit menu.
func (tab *repoTab) copyCommand(el ui.Element, p string) {
	el.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputCommand || ev.Text != "copy" {
			return false
		}
		tab.copyPath(p, false)
		return true
	})
}
