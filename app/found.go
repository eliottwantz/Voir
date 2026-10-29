package app

import (
	"context"
	"errors"
	"slices"

	"github.com/eliott/voir/internal/github"
)

// The pull requests page is where the account's work on GitHub is gathered,
// not only that of the repositories open, pinned or named: a search finds the
// pull requests the account wrote, is asked to review, has reviewed or
// commented on, and each repository they are of that is not listed otherwise
// gets a tab of its own for them, which lists those and no others.

// foundHosts are the hosts to look on: github.com and those of the
// repositories listed.
func (a *app) foundHosts() []string {
	hosts := []string{"github.com"}
	for _, t := range a.prRepos() {
		if !slices.Contains(hosts, t.gh.Host) {
			hosts = append(hosts, t.gh.Host)
		}
	}
	return hosts
}

// findInvolved looks for the pull requests the account is involved in, off
// the main thread, unless that is being done or is done already.
func (a *app) findInvolved() {
	if a.foundLoading || a.foundLoaded {
		return
	}
	a.foundLoading, a.foundErr = true, ""
	state, hosts := a.prFilters.fetchState(), a.foundHosts()
	a.background(func() {
		ctx := context.Background()
		type result struct {
			viewer github.User
			found  []github.Found
		}
		results := map[string]result{}
		var firstErr error
		for _, host := range hosts {
			cl, err := a.client(ctx, host)
			var r result
			if err == nil {
				r.viewer, r.found, err = cl.Involved(ctx, host, state)
			}
			// Not being signed in is told by the repositories listed, and not
			// worth an error when there are none; nor is a host but GitHub's.
			if err != nil && firstErr == nil && host == "github.com" && !errors.Is(err, github.ErrNotSignedIn) {
				firstErr = err
			}
			results[host] = r
		}
		a.update(func() {
			a.foundLoading, a.foundLoaded = false, true
			if a.prFilters.fetchState() != state {
				// The filters moved on to another state meanwhile.
				a.foundLoaded = false
				return
			}
			if firstErr != nil {
				a.foundErr = errorText(firstErr)
			}
			var all []github.Found
			viewers := map[string]string{}
			for _, host := range hosts {
				all = append(all, results[host].found...)
				viewers[host] = results[host].viewer.Login
			}
			a.showFound(all, viewers)
		})
	})
}

// showFound gives each repository the pull requests found in it a tab that
// lists them, and takes those off the page that nothing is found in anymore.
func (a *app) showFound(found []github.Found, viewers map[string]string) {
	byRepo := map[github.Repo][]github.Found{}
	var order []github.Repo
	for _, f := range found {
		key := github.Repo{Host: f.Repo.Host, Owner: f.Repo.Owner, Name: f.Repo.Name}
		i := slices.IndexFunc(order, func(r github.Repo) bool { return sameRepo(r, key) })
		if i >= 0 {
			key = order[i]
		} else {
			order = append(order, key)
		}
		byRepo[key] = append(byRepo[key], f)
	}
	listed := map[*repoTab]bool{}
	for _, repo := range order {
		var tab *repoTab
		for _, t := range a.prRepos() {
			if sameRepo(*t.gh, repo) {
				tab = t
			}
		}
		switch {
		case tab == nil:
			tab = newTab(a, nil)
			tab.name = repo.String()
			r := repo
			tab.gh = &r
			a.remoteTabs = append(a.remoteTabs, tab)
		case tab.found, tab.only && tab.remote():
			// Those found take the place of the pull requests opened.
		default:
			// A repository that lists all its pull requests lists these too.
			continue
		}
		tab.found, tab.only, tab.prsLoaded, tab.prsErr = true, false, true, ""
		tab.stacksOff = true
		tab.prViewer = viewers[repo.Host]
		opened := slices.DeleteFunc(slices.Clone(tab.prs), func(p github.PR) bool {
			return !slices.ContainsFunc(tab.prTabs, func(pv *prView) bool { return pv.number == p.Number })
		})
		tab.prs, tab.prStatus, tab.prStacks = nil, map[int]github.PRStatus{}, nil
		for _, f := range byRepo[repo] {
			tab.prs = append(tab.prs, f.PR)
			tab.prStatus[f.PR.Number] = f.Status
		}
		for _, p := range opened {
			if !slices.ContainsFunc(tab.prs, func(q github.PR) bool { return q.Number == p.Number }) {
				tab.prs = append(tab.prs, p)
			}
		}
		listed[tab] = true
	}
	// A repository nothing is found in anymore leaves the page, unless a pull
	// request of it is open there.
	a.remoteTabs = slices.DeleteFunc(a.remoteTabs, func(t *repoTab) bool {
		gone := t.found && !listed[t] && len(t.prTabs) == 0
		if gone {
			delete(a.prFilters.repos, t.gh.String())
		}
		return gone
	})
	if a.prTab != nil && !slices.Contains(a.prRepos(), a.prTab) {
		a.prTab = nil
		a.settlePR()
	}
}
