package app

import (
	"fmt"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/eliott/myfork/internal/git"
	"github.com/eliott/myfork/internal/github"
)

// prRepos are the repositories whose pull requests the page lists: those open
// in tabs, then the pinned ones that are not, each GitHub repository once and
// only when it has a remote there.
func (a *app) prRepos() []*repoTab {
	var list []*repoTab
	seen := map[github.Repo]bool{}
	add := func(t *repoTab) {
		if t.gh == nil || seen[*t.gh] {
			return
		}
		seen[*t.gh] = true
		list = append(list, t)
	}
	for _, t := range a.tabs {
		add(t)
	}
	for _, root := range a.store.pinned() {
		if t := a.pinTabs[root]; t != nil {
			add(t)
		}
	}
	for _, t := range a.remoteTabs {
		add(t)
	}
	return list
}

// syncPinned makes a tab, for their pull requests only, of each pinned
// repository that has none yet, reading its remotes off the main thread.
func (a *app) syncPinned() {
	for _, root := range a.store.pinned() {
		if a.pinTabs[root] != nil {
			continue
		}
		a.background(func() {
			repo, err := git.Open(root)
			if err != nil {
				return
			}
			remotes := repo.Remotes()
			a.update(func() {
				if a.pinTabs[root] != nil || !a.store.isPinned(root) {
					return
				}
				t := newTab(a, repo)
				t.remotes = remotes
				t.updateGitHub()
				a.pinTabs[root] = t
			})
		})
	}
}

// mainCheckout is where the repository of root has its own checkout, and
// whether root is another work tree of it.
func mainCheckout(root string, trees []git.Worktree) (main string, linked bool) {
	for _, w := range trees {
		if w.Main {
			main = w.Path
			if r, err := filepath.EvalSymlinks(main); err == nil {
				main = r
			}
			return main, main != root
		}
	}
	return root, false
}

// pinRepo pins the repository holding dir, once it is known to be a
// repository's own checkout, not a work tree linked to it, with a remote on
// GitHub, and says why when it is not.
func (a *app) pinRepo(dir string) {
	a.background(func() {
		repo, err := git.Open(dir)
		var remotes []git.Remote
		var main string
		var linked bool
		if err == nil {
			remotes = repo.Remotes()
			main, linked = mainCheckout(repo.Root, repo.Worktrees())
		}
		a.update(func() {
			switch {
			case err != nil:
				a.fail("Could not pin the repository", err)
			case linked:
				a.fail("Could not pin the work tree", fmt.Errorf("%s is a work tree linked to %s. Pin the checkout of the repository itself.", abbreviateHome(repo.Root), abbreviateHome(main)))
			default:
				if _, _, ok := detectGitHub(remotes); !ok {
					a.fail("Could not pin the repository", fmt.Errorf("%s has no remote on GitHub, so it has no pull requests to list.", repo.Root))
					return
				}
				if a.store.pin(repo.Root) {
					a.say("Pinned %s", repoName(repo.Root))
				}
				a.syncPinned()
			}
		})
	})
}

// unpinRepo takes a repository out of the pinned ones; its pull requests are
// listed no more unless it is open.
func (a *app) unpinRepo(root string) {
	a.store.unpin(root)
	if t := a.pinTabs[root]; t != nil {
		if t.gh != nil {
			delete(a.prFilters.repos, t.gh.String())
		}
		delete(a.pinTabs, root)
		for _, o := range t.prTabs {
			o.gen++
		}
		if a.prTab == t {
			a.prTab = nil
			a.settlePR()
		}
	}
}

// browseForPin asks for a folder and pins the repository in it.
func (a *app) browseForPin() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "Pin a Git Repository", Directory: true})
		if err != nil || len(paths) == 0 {
			return
		}
		a.update(func() { a.pinRepo(paths[0]) })
	}()
}

// togglePin pins the repository of a tab, or unpins it.
func (a *app) togglePin(tab *repoTab) {
	if a.store.isPinned(tab.repo.Root) {
		a.unpinRepo(tab.repo.Root)
		return
	}
	a.pinRepo(tab.repo.Root)
}
