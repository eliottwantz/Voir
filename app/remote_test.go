package app

import (
	"context"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/github"
)

// remoteApp is an app with no repository open, that GitHub answers for.
func remoteApp(t *testing.T) (*app, *ui.Tester) {
	t.Helper()
	_, srv := newFakeGitHub(t)
	a := newApp(newStore())
	a.newGitHub = func(context.Context, string) (*github.Client, error) {
		return &github.Client{BaseURL: srv.URL, Token: "tok"}, nil
	}
	a.pulls = true
	return a, ui.NewTester(a.view, 1280, 900)
}

func TestPullRequestOfARepositoryNotHere(t *testing.T) {
	a, tt := remoteApp(t)
	tt.Frame()
	if err := tt.Click("Open a pull request by repository and number"); err != nil {
		t.Fatal(err)
	}
	tt.Type("o/r #3")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	tt.Frame()

	if len(a.tabs) != 0 || len(a.remoteTabs) != 1 {
		t.Fatalf("tabs %d, remote %d", len(a.tabs), len(a.remoteTabs))
	}
	tab := a.remoteTabs[0]
	if !tab.remote() || tab.gh.String() != "o/r" || tab.pr == nil || tab.pr.number != 3 || !tab.pr.loaded {
		t.Fatalf("remote %v, gh %v, pr %+v", tab.remote(), tab.gh, tab.pr)
	}
	for _, want := range []string{"Add feature", "Looks good to me"} {
		if !tt.HasText(want) {
			t.Errorf("no %q: %q", want, tt.Texts())
		}
	}
	if a.prOpen != "" || a.prOpenErr != "" {
		t.Errorf("field %q, error %q", a.prOpen, a.prOpenErr)
	}

	// The files come from GitHub's patches, and the commits' too.
	if err := tt.Click("Code"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	rv := tab.pr.rv
	if rv == nil || len(rv.files) != 2 || rv.local {
		t.Fatalf("review %+v", rv)
	}
	tab.showPRCommit(tab.pr, "1111111aaaa")
	tt.Frame()
	if c := tab.pr.commitRV; c == nil || c.err != "" || len(c.files) != 1 || c.files[0].path != "feature.go" {
		t.Fatalf("commit review %+v", c)
	}

	// Without a checkout there is nothing to check out or to write a guide from.
	tab.checkoutPR(3)
	tab.pr.tab = 1
	tt.Frame()
	if !tt.HasText("No review guide for this repository") {
		t.Errorf("guide: %q", tt.Texts())
	}

	// Naming the pull request again opens the tab it has.
	a.openPRRef("https://github.com/O/R/pull/3/files")
	if len(a.remoteTabs) != 1 || len(tab.prTabs) != 1 {
		t.Fatalf("remote %d, tabs %d", len(a.remoteTabs), len(tab.prTabs))
	}

	a.removeRemote(tab)
	if len(a.remoteTabs) != 0 || a.prTab != nil {
		t.Fatalf("remote %d, prTab %v", len(a.remoteTabs), a.prTab)
	}
	tt.Frame()
}

func TestPullRequestOfTheRepositoryOpenIsNotListedTwice(t *testing.T) {
	a, _, _ := prApp(t)
	tab := a.tab()
	a.openPRRef("O/R#3")
	if len(a.remoteTabs) != 0 || tab.pr == nil || tab.pr.number != 3 {
		t.Fatalf("remote %d, pr %+v", len(a.remoteTabs), tab.pr)
	}
}

func TestOpenPRRefSaysWhatItCannotRead(t *testing.T) {
	a, tt := remoteApp(t)
	a.openPRRef("what is this")
	tt.Frame()
	if len(a.remoteTabs) != 0 || !strings.Contains(a.prOpenErr, "is not owner/repo") || !tt.HasText(a.prOpenErr) {
		t.Fatalf("remote %d, error %q, %q", len(a.remoteTabs), a.prOpenErr, tt.Texts())
	}
}

func TestOpenARepositoryWithoutANumberListsItsPullRequests(t *testing.T) {
	a, tt := remoteApp(t)
	a.openPRRef("o/r")
	tt.Frame()
	tt.Frame()
	if len(a.remoteTabs) != 1 || a.activePR() != nil || !tt.HasText("Add feature") {
		t.Fatalf("remote %d, %q", len(a.remoteTabs), tt.Texts())
	}
}
