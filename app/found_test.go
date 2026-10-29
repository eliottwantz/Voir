package app

import (
	"testing"
	"time"

	"github.com/eliott/voir/internal/github"
)

func TestFoundPullRequestsGetTabsOfTheirOwn(t *testing.T) {
	a, tt, _ := remoteApp(t)
	tt.Frame()

	found := func(repo string, n int, author string, reviewers ...string) github.Found {
		r, _ := github.ParseRemote("https://github.com/" + repo)
		p := github.PR{Number: n, Title: "Work " + repo, State: "open", User: github.User{Login: author}, UpdatedAt: time.Now()}
		for _, l := range reviewers {
			p.RequestedReviewers = append(p.RequestedReviewers, github.User{Login: l})
		}
		return github.Found{Repo: r, PR: p, Status: github.PRStatus{Review: "required"}}
	}
	a.showFound([]github.Found{found("acme/api", 5, "me"), found("acme/web", 8, "bob", "me"), found("acme/web", 9, "bob")}, map[string]string{"github.com": "me"})
	tt.Frame()

	if len(a.remoteTabs) != 2 {
		t.Fatalf("%d tabs", len(a.remoteTabs))
	}
	web := a.remoteTabs[1]
	if !web.found || !web.remote() || len(web.prs) != 2 || web.prViewer != "me" {
		t.Fatalf("web %+v", web)
	}
	rows, shown := a.prListRows(a.prRepos())
	if shown != 3 || rows[0].heading != "Authored" || rows[2].heading != "Review requested" {
		t.Errorf("rows %+v", rows)
	}
	for _, want := range []string{"Involved", "acme/api", "acme/web"} {
		if !tt.HasText(want) {
			t.Errorf("no %q: %q", want, tt.Texts())
		}
	}

	// What is found no more leaves the page; a repository now listed whole
	// is left as it is.
	a.showFound([]github.Found{found("acme/web", 8, "bob", "me")}, map[string]string{"github.com": "me"})
	if len(a.remoteTabs) != 1 || a.remoteTabs[0].gh.Name != "web" || len(a.remoteTabs[0].prs) != 1 {
		t.Fatalf("tabs %+v", a.remoteTabs)
	}
	a.watchRepo(a.remoteTabs[0])
	a.showFound([]github.Found{found("acme/web", 8, "bob", "me")}, map[string]string{"github.com": "me"})
	if a.remoteTabs[0].found {
		t.Error("a pinned repository stays found")
	}
}

func TestPullRequestsPageSearchesForInvolvement(t *testing.T) {
	f, srv := newFakeGitHub(t)
	f.involved = []map[string]any{{
		"number": 5, "title": "Fix the thing", "state": "OPEN", "updatedAt": "2026-10-10T09:00:00Z",
		"author":         map[string]any{"login": "ada"},
		"repository":     map[string]any{"nameWithOwner": "acme/api"},
		"reviewRequests": map[string]any{"nodes": []map[string]any{{"requestedReviewer": map[string]any{"login": "me"}}}},
	}}
	a, tt := remoteAppWith(newStore(), srv.URL)
	tt.Frame()
	tt.Frame()

	if !a.foundLoaded || a.foundLoading || a.foundErr != "" {
		t.Fatalf("loaded %v, loading %v, error %q", a.foundLoaded, a.foundLoading, a.foundErr)
	}
	if len(a.remoteTabs) != 1 || !a.remoteTabs[0].found || a.remoteTabs[0].gh.String() != "acme/api" {
		t.Fatalf("tabs %+v", a.remoteTabs)
	}
	for _, want := range []string{"Fix the thing", "Review requested", "acme/api"} {
		if !tt.HasText(want) {
			t.Errorf("no %q: %q", want, tt.Texts())
		}
	}
	// It is looked for once, not at every frame.
	tt.Frame()
	if n := f.searches; n != 3 {
		t.Errorf("%d searches", n)
	}
}
