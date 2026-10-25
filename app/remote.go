package app

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/eliott/myfork/internal/git"
	"github.com/eliott/myfork/internal/github"
)

// remote reports whether the tab is a repository that is not on this machine,
// named on the pull requests page: it has pull requests and nothing else.
func (t *repoTab) remote() bool { return t.repo == nil }

// sameRepo reports whether two names are one repository: GitHub does not tell
// the case of a name apart.
func sameRepo(x, y github.Repo) bool {
	return strings.EqualFold(x.Host, y.Host) && strings.EqualFold(x.Owner, y.Owner) && strings.EqualFold(x.Name, y.Name)
}

// prRepoTab is the tab that lists the pull requests of a repository: one open
// or pinned if there is one, else a tab for the repository alone.
func (a *app) prRepoTab(repo github.Repo) *repoTab {
	for _, t := range a.prRepos() {
		if sameRepo(*t.gh, repo) {
			return t
		}
	}
	t := newTab(a, nil)
	t.name = repo.String()
	t.gh = &repo
	a.remoteTabs = append(a.remoteTabs, t)
	return t
}

// openPRRef opens what the field of the pull requests page names: a pull
// request of any repository GitHub shows the account, as owner/repo #123 or
// an address; with no number, the repository's pull requests are listed.
func (a *app) openPRRef(text string) {
	repo, n, err := github.ParseRef(text)
	if err != nil {
		a.prOpenErr = err.Error()
		return
	}
	a.prOpen, a.prOpenErr = "", ""
	a.pulls = true
	t := a.prRepoTab(repo)
	if n > 0 {
		t.openPR(n)
	} else {
		t.loadPRs()
	}
}

// removeRemote takes a repository that is not on this machine off the page.
func (a *app) removeRemote(t *repoTab) {
	i := slices.Index(a.remoteTabs, t)
	if i < 0 {
		return
	}
	a.remoteTabs = slices.Delete(a.remoteTabs, i, i+1)
	delete(a.prFilters.repos, t.gh.String())
	for _, pv := range t.prTabs {
		pv.gen++
	}
	if a.prTab == t {
		a.prTab = nil
		a.settlePR()
	}
}

// showRemoteCommit makes the Code tab show the changes of one commit of a
// pull request of a repository that is not here, as GitHub lists them.
func (t *repoTab) showRemoteCommit(pv *prView, rv *review) {
	repo, sha, a := *t.gh, rv.hash, t.app
	a.background(func() {
		ctx := context.Background()
		var files []github.File
		cl, err := a.client(ctx, repo.Host)
		if err == nil {
			files, err = cl.CommitFiles(ctx, repo, sha)
		}
		a.update(func() {
			if pv.commitRV != rv {
				return
			}
			rv.loading = false
			if err != nil {
				rv.err = errorText(err)
				return
			}
			t.layoutReview(rv, partsOf(files))
		})
	})
}

// partsOf are the files of a change as GitHub lists them, as the parts that
// are laid out. GitHub sends no patch for a binary file, nor for one too big.
func partsOf(files []github.File) []git.FilePart {
	parts := make([]git.FilePart, len(files))
	for i, f := range files {
		status := map[string]byte{"added": 'A', "removed": 'D', "renamed": 'R', "copied": 'C'}[f.Status]
		if status == 0 {
			status = 'M'
		}
		parts[i] = git.FilePart{
			Path: f.Filename, OldPath: cmp.Or(f.PreviousFilename, f.Filename), Status: status,
			Binary: f.Patch == "" && f.Additions+f.Deletions == 0,
			Patch:  f.FilePatch(), Added: f.Additions, Deleted: f.Deletions,
		}
	}
	return parts
}
