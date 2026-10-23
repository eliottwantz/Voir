package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/aigen"
	"github.com/eliott/myfork/internal/git"
	"github.com/eliott/myfork/internal/github"
)

// app is the window's state: the repositories open in tabs, and what covers
// them (a dialog, a message).
type app struct {
	win     *mygo.Window
	store   *store
	avatars *avatarCache

	tabs []*repoTab
	cur  int

	// welcome is the state of the page shown without a repository.
	welcome welcomePage

	dialog *dialog
	sheet  *sheet
	// cwins are the windows that show one commit each.
	cwins []*commitWindow
	// fwins are the windows that show the history or the blame of a file.
	fwins []*fileWindow
	// apps are the applications installed, which Open With offers, and opened
	// what tests were asked to open: without a window nothing opens.
	apps   []mygo.InstalledApp
	opened []string
	// pulls shows the pull requests page in place of the repository: the
	// list of every repository open or pinned, and the pull request chosen
	// beside it. prTab is the repository of that pull request.
	pulls bool
	prTab *repoTab
	// pinTabs are the pinned repositories that are not open, by root: they
	// are read for their pull requests only.
	pinTabs map[string]*repoTab
	// prSearch, prFilters and prSort narrow and order the list of pull
	// requests, which prList holds the state of.
	prSearch  string
	prFilters prFilters
	prSort    int
	prList    ui.ListState
	// prExpanded gives the pull request chosen the whole page, and prListW is
	// the width of the list beside it. prTree shows the files of its Code.
	prExpanded bool
	prListW    float32
	// prHalved is set once the list has been given 35% of the page for the
	// pull request chosen; it clears when none is chosen.
	prHalved bool
	prTree     bool
	// prFilterOpen shows the filters of the pull requests, and prFilterSub the
	// one whose choices are beside them; prAuthorQ searches the authors.
	prFilterOpen bool
	prFilterSub  string
	prAuthorQ    string
	// openWithMenu opens the menu of the toolbar's Open With button.
	openWithMenu bool
	// prMore opens the menu of the pull request's More button.
	prMore bool
	// prMergeMenu opens the ways to merge, beside the merge button.
	prMergeMenu bool
	// prStack opens the menu of the stack the pull request is in.
	prStack bool
	// copied is what was last copied, which tests read: without a window
	// there is no clipboard to ask.
	copied string
	// newGitHub signs in to a host; tests replace it.
	newGitHub func(ctx context.Context, host string) (*github.Client, error)
	// fetchPR makes the repository hold a pull request's commits, and says
	// where it branched; tests replace it.
	fetchPR func(ctx context.Context, r *git.Repo, remote string, n int, pr github.PR) (string, error)
	// mergePoll is how long to wait before asking again whether GitHub merged a
	// stack, after the attempt-th time; tests make it short.
	mergePoll  func(attempt int) time.Duration
	clients    map[string]*github.Client
	closed     []string
	prefsWin   *mygo.Window
	prefsTab   int
	themeDirty bool
	// messages wait for the view, which has the context toasts need.
	mu       sync.Mutex
	messages []message
	// guideDir is where review guides are kept; "" is the user's cache.
	guideDir string
	// modelList are the models each provider has, once asked.
	modelList map[aigen.Provider][]aigen.ModelChoice
	// sidebarShown hides the sidebar, as View ▸ Hide Sidebar.
	sidebarShown bool
	// panelCollapsed folds the commit panel under the graph down to its tabs.
	panelCollapsed bool
	// panelMaximized gives the commit panel the whole area, over the graph.
	panelMaximized bool
	// quick is Quick Launch: a field over the recent repositories.
	quick quickLaunch
	// hold keeps background work in tests until they run it.
	hold bool
	held []func()
}

func newApp(st *store) *app {
	a := &app{store: st, avatars: newAvatarCache(), sidebarShown: true, prListW: 440, newGitHub: github.NewClient, clients: map[string]*github.Client{}, pinTabs: map[string]*repoTab{}}
	a.fetchPR = a.fetchPRCommits
	a.mergePoll = func(attempt int) time.Duration { return min(time.Second<<attempt, 10*time.Second) }
	return a
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
	a.win.Update(func() {
		fn()
		a.invalidateWindows()
	})
}

// say shows a short message, once the view runs.
func (a *app) say(format string, args ...any) {
	a.post(message{text: fmt.Sprintf(format, args...)})
}

// message is what the toasts say, with the button some have: label, which
// runs action on the main thread.
type message struct {
	text, label string
	action      func()
}

// post shows a message, once the view runs.
func (a *app) post(m message) {
	a.mu.Lock()
	a.messages = append(a.messages, m)
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
		if m.action != nil {
			c.ToastAction(m.text, m.label, m.action)
		} else {
			c.Toast(m.text)
		}
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
	a.pulls = false
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

// closeTabs closes the tabs keep rejects, and keeps the one shown if it is
// among those kept.
func (a *app) closeTabs(keep func(i int) bool) {
	shown := a.tab()
	kept := a.tabs[:0:0]
	for i, t := range a.tabs {
		if keep(i) {
			kept = append(kept, t)
			continue
		}
		a.closed = append(a.closed, t.repo.Root)
	}
	if len(a.closed) > 10 {
		a.closed = a.closed[len(a.closed)-10:]
	}
	a.tabs = kept
	a.cur = max(slices.Index(a.tabs, shown), 0)
	a.saveTabs()
}

// moveTab moves tab i by d places.
func (a *app) moveTab(i, d int) {
	j := i + d
	if i < 0 || i >= len(a.tabs) || j < 0 || j >= len(a.tabs) {
		return
	}
	shown := a.tab()
	a.tabs[i], a.tabs[j] = a.tabs[j], a.tabs[i]
	a.cur = slices.Index(a.tabs, shown)
	a.saveTabs()
}

// refreshAllStatus reads the status of the tabs behind the one shown, which
// keeps their changed marks and branches current.
func (a *app) refreshAllStatus() {
	for i, t := range a.tabs {
		if i != a.cur {
			t.refreshStatus()
		}
	}
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

// refresh reloads the repository shown from disk, as the window comes forward
// and after an action; it does not reach the network.
func (a *app) refresh() {
	if t := a.tab(); t != nil {
		t.reload()
	}
	a.refreshAllStatus()
}

// refreshOnFocus is refresh as the window comes forward: something else (an
// editor, a terminal, another git client) may have changed the repositories
// meanwhile, so the diff of the file chosen is read again too.
func (a *app) refreshOnFocus() {
	for _, t := range a.tabs {
		t.workStale = true
	}
	a.refresh()
}

// refreshNow is ⌘R and the Refresh buttons: refresh, and also ask the remote
// and GitHub what changed.
func (a *app) refreshNow() {
	shown := a.tab()
	if shown != nil {
		shown.sync()
	}
	a.refreshAllStatus()
	if a.pulls {
		// The page lists the others' pull requests too.
		for _, t := range a.prRepos() {
			if t != shown {
				t.syncPulls()
			}
		}
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

// copy puts text on the clipboard, and keeps it for tests, which have none.
func (a *app) copy(s string) {
	a.copied = s
	if a.win != nil {
		copyText(s)
	}
}

// copyText puts text on the clipboard.
func copyText(s string) { mygo.Clipboard.WriteText(s) }

// showPane shows a tab of the commit view under the graph, leaving the pull
// requests page and Local Changes if either is shown.
func (a *app) showPane(p panelTab) {
	tab := a.tab()
	if tab == nil {
		return
	}
	a.pulls = false
	if tab.onWip {
		tab.showHistory()
	}
	tab.cv.setPane(p)
}

// showPulls goes to the pull requests page, which lists those of the
// repositories open on GitHub and of the pinned ones; with none, it offers to
// pin one.
func (a *app) showPulls() {
	a.pulls = true
	a.syncPinned()
	// Read what is shown again, so that nothing stale is.
	for _, t := range a.prRepos() {
		if t.gh != nil {
			t.syncPulls()
		}
	}
}

// activePR is the repository of the pull request shown on the page, nil when
// none is.
func (a *app) activePR() *repoTab {
	if a.prTab != nil && a.prTab.pr != nil {
		return a.prTab
	}
	return nil
}

// settlePR picks the pull request to show once the one shown was closed: the
// first of another repository that has one open.
func (a *app) settlePR() {
	if a.activePR() != nil {
		return
	}
	a.prTab = nil
	for _, t := range a.prRepos() {
		if t.pr != nil {
			a.prTab = t
			return
		}
	}
	a.prExpanded = false
}
