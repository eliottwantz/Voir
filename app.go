package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

// app is the window's state: the repositories open in tabs, and what covers
// them (a dialog, a message).
type app struct {
	win   *mygo.Window
	store *store

	tabs []*repoTab
	cur  int

	// welcome is the state of the page shown without a repository.
	welcome welcomePage

	dialog     *dialog
	sheet      *sheet
	closed     []string
	prefsOpen  bool
	prefsTab   int
	themeDirty bool
	// messages wait for the view, which has the context toasts need.
	mu       sync.Mutex
	messages []string
	// sidebarShown hides the sidebar, as View ▸ Hide Sidebar.
	sidebarShown bool
	// quick is Quick Launch (⌘⇧O): a field over the recent repositories.
	quick quickLaunch
	// hold keeps background work in tests until they run it.
	hold bool
	held []func()
}

func newApp(st *store) *app {
	return &app{store: st, sidebarShown: true}
}

// background runs fn on a goroutine of its own, or at once in tests, which
// have no window.
func (a *app) background(fn func()) {
	if a.win == nil {
		if a.hold {
			a.held = append(a.held, fn)
			return
		}
		fn()
		return
	}
	go fn()
}

// runHeld runs the background work tests held.
func (a *app) runHeld() {
	for len(a.held) > 0 {
		fn := a.held[0]
		a.held = a.held[1:]
		fn()
	}
}

// update runs fn on the main thread, then draws a frame.
func (a *app) update(fn func()) {
	if a.win == nil {
		fn()
		return
	}
	a.win.Update(fn)
}

// say shows a short message, once the view runs.
func (a *app) say(format string, args ...any) {
	a.mu.Lock()
	a.messages = append(a.messages, fmt.Sprintf(format, args...))
	a.mu.Unlock()
	if a.win != nil {
		a.win.Invalidate()
	}
}

func (a *app) flushMessages(c *ui.Context) {
	a.mu.Lock()
	msgs := a.messages
	a.messages = nil
	a.mu.Unlock()
	for _, m := range msgs {
		c.Toast(m)
	}
}

// fail tells the user an operation did not work.
func (a *app) fail(title string, err error) {
	a.showDialog(&dialog{title: title, message: errorText(err), ok: "OK", info: true})
}

func (a *app) tab() *repoTab {
	if a.cur < 0 || a.cur >= len(a.tabs) {
		return nil
	}
	return a.tabs[a.cur]
}

// openRepo opens a repository in a tab, or goes to the one showing it.
func (a *app) openRepo(dir string) error {
	repo, err := git.Open(dir)
	if err != nil {
		return err
	}
	for i, t := range a.tabs {
		if t.repo.Root == repo.Root {
			a.cur = i
			return nil
		}
	}
	t := newTab(a, repo)
	a.tabs = append(a.tabs, t)
	a.cur = len(a.tabs) - 1
	a.store.addRecent(repo.Root)
	a.saveTabs()
	t.reload()
	return nil
}

// openRepoReporting opens a repository and says why when it cannot.
func (a *app) openRepoReporting(dir string) {
	if err := a.openRepo(dir); err != nil {
		title := "Could not open the repository"
		if errors.Is(err, git.ErrNotRepository) {
			title = "Not a Git repository"
		}
		a.fail(title, err)
	}
}

func (a *app) closeTab(i int) {
	if i < 0 || i >= len(a.tabs) {
		return
	}
	a.closed = append(a.closed, a.tabs[i].repo.Root)
	if len(a.closed) > 10 {
		a.closed = a.closed[1:]
	}
	a.tabs = append(a.tabs[:i], a.tabs[i+1:]...)
	if a.cur >= len(a.tabs) {
		a.cur = len(a.tabs) - 1
	} else if a.cur > i {
		a.cur--
	}
	a.saveTabs()
}

func (a *app) saveTabs() {
	a.store.mu.Lock()
	a.store.data.Tabs = nil
	for _, t := range a.tabs {
		a.store.data.Tabs = append(a.store.data.Tabs, t.repo.Root)
	}
	a.store.data.Active = a.cur
	a.store.mu.Unlock()
	a.store.save()
}

// restoreTabs reopens the repositories of the last run.
func (a *app) restoreTabs() {
	a.store.mu.Lock()
	tabs, active := append([]string(nil), a.store.data.Tabs...), a.store.data.Active
	a.store.mu.Unlock()
	for _, dir := range tabs {
		if _, err := os.Stat(dir); err == nil {
			a.openRepo(dir)
		}
	}
	if active >= 0 && active < len(a.tabs) {
		a.cur = active
	}
}

// refreshAll reloads the repository shown, as the window comes forward.
func (a *app) refresh() {
	if t := a.tab(); t != nil {
		t.reload()
	}
}

// browseForRepo asks for a folder and opens it.
func (a *app) browseForRepo() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "Open a Git Repository", Directory: true})
		if err != nil || len(paths) == 0 {
			return
		}
		a.update(func() { a.openRepoReporting(paths[0]) })
	}()
}

// repoName is a repository's name for a tab: its folder.
func repoName(root string) string { return filepath.Base(root) }

// copyText puts text on the clipboard.
func copyText(s string) { mygo.Clipboard.WriteText(s) }
