package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

// otherRepo is a second repository, whose remote is o/other on GitHub.
func otherRepo(t *testing.T) *git.Repo {
	t.Helper()
	r := demoRepo(t)
	if err := r.AddRemote("origin", "https://github.com/o/other.git"); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPinnedRepositoryListsItsPullRequestsWithoutBeingOpen(t *testing.T) {
	a, _, _ := prApp(t)
	other := otherRepo(t)
	a.pinRepo(other.Root)
	if !a.store.isPinned(other.Root) || len(a.tabs) != 1 {
		t.Fatalf("pinned %v, %d tabs open", a.store.pinned(), len(a.tabs))
	}
	tt := ui.NewTester(a.view, 1280, 800)
	a.showPulls()
	tt.Frame()
	for _, want := range []string{"#3", "Add feature", "#7", "Other work", "o/other", "o/r"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in the list: %q", want, tt.Texts())
		}
	}
	// The repository is not a dropdown of the page: it is a filter.
	if tt.HasText("Repository demo") {
		t.Errorf("the page has a repository selector: %q", tt.Texts())
	}
	snapshot(t, tt, "pr-list-repos")

	// A pull request of the pinned repository opens beside the list, though the
	// repository is not.
	if err := tt.Click("#7 Other work"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.prTab == nil || a.prTab == a.tab() || a.prTab.pr == nil || a.prTab.pr.number != 7 || len(a.tabs) != 1 {
		t.Fatalf("shown %+v of %v", a.prTab, a.activePR())
	}
	// Closing it leaves the one of the open repository, if there is one.
	a.tab().openPR(3)
	tt.Frame()
	if a.prTab != a.tab() {
		t.Fatalf("the page shows the pull request of another repository than the one opened last")
	}
	a.tab().closePR(a.tab().pr)
	tt.Frame()
	if a.activePR() == nil || a.activePR().pr.number != 7 {
		t.Fatalf("after closing #3, the page shows %+v", a.activePR())
	}
}

func TestReposFilterChoosesTheRepositories(t *testing.T) {
	a, _, _ := prApp(t)
	other := otherRepo(t)
	a.pinRepo(other.Root)
	tt := ui.NewTester(a.view, 1280, 800)
	a.showPulls()
	tt.Frame()
	if err := tt.Click("Filters"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Repos: All"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	snapshot(t, tt, "pr-repos-menu")
	if err := tt.Click("Repo o/other"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.prFilters.repos["o/other"] || a.prFilters.active() != 1 {
		t.Fatalf("filters %+v", a.prFilters)
	}
	if tt.HasText("Add feature") || !tt.HasText("Other work") {
		t.Fatalf("only o/other should be listed: %q", tt.Texts())
	}
	// Both, then none: all again.
	if err := tt.Click("Repo o/r"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Add feature") || !tt.HasText("Other work") || a.prFilters.active() != 1 {
		t.Fatalf("both repositories: %+v %q", a.prFilters, tt.Texts())
	}
	// A pinned repository can be unpinned from the menu, which leaves the
	// filter and the list.
	if err := tt.Click("Unpin o/other"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.store.isPinned(other.Root) || a.prFilters.repos["o/other"] {
		t.Fatalf("still pinned: %v, filters %+v", a.store.pinned(), a.prFilters)
	}
	if tt.HasText("Other work") || !tt.HasText("Add feature") {
		t.Fatalf("after unpinning: %q", tt.Texts())
	}
}

func TestPullRequestsPageWithoutAnOpenRepository(t *testing.T) {
	a, r, _ := prApp(t)
	a.pinRepo(r.Root)
	a.closeTab(0)
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	if a.tab() != nil || !tt.HasText("Welcome to MyFork") {
		t.Fatalf("no welcome page: %q", tt.Texts())
	}
	// The welcome page lists the pinned repositories, and goes to the page.
	for _, want := range []string{"Pinned", "demo", "Pin Repository…", "Pull Requests"} {
		if !tt.HasText(want) {
			t.Errorf("no %q on the welcome page: %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "welcome-pinned")
	if err := tt.Click("Pull Requests"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.pulls || !tt.HasText("#3") || !tt.HasText("Add feature") {
		t.Fatalf("no pull requests of the pinned repository: %q", tt.Texts())
	}
	if err := tt.Click("#3 Add feature"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.prTab == nil || a.prTab.pr == nil || !a.prTab.pr.loaded || len(a.tabs) != 0 {
		t.Fatalf("pull request %+v with %d tabs", a.prTab, len(a.tabs))
	}
	// Back is the welcome page again.
	if err := tt.Click("Back"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.pulls || !tt.HasText("Welcome to MyFork") {
		t.Fatalf("back: pulls %v, %q", a.pulls, tt.Texts())
	}
}

func TestPullRequestsPageOffersToPinWhenThereIsNothingToList(t *testing.T) {
	a := newApp(newStore())
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	if err := tt.Click("Pull Requests"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.pulls || !tt.HasText("No repository to list") || !tt.HasText("Pin Repository…") {
		t.Fatalf("empty page: %q", tt.Texts())
	}
}

func TestOnlyTheRepositoryItselfCanBePinned(t *testing.T) {
	a, r, _ := prApp(t)

	// A work tree linked to it is refused, and says where the checkout is.
	tree := filepath.Join(t.TempDir(), "wt")
	if err := r.AddWorktree(tree, "wt", true); err != nil {
		t.Fatal(err)
	}
	a.pinRepo(tree)
	if len(a.store.pinned()) != 0 || a.dialog == nil || !strings.Contains(a.dialog.message, "linked") || !strings.Contains(a.dialog.message, filepath.Base(r.Root)) {
		t.Fatalf("pinned %v, dialog %+v", a.store.pinned(), a.dialog)
	}
	a.dialog = nil

	// A folder that is not a repository.
	a.pinRepo(t.TempDir())
	if len(a.store.pinned()) != 0 || a.dialog == nil {
		t.Fatalf("pinned %v, dialog %+v", a.store.pinned(), a.dialog)
	}
	a.dialog = nil

	// A repository with no remote on GitHub has no pull requests.
	bare := demoRepo(t)
	a.pinRepo(bare.Root)
	if len(a.store.pinned()) != 0 || a.dialog == nil || !strings.Contains(a.dialog.message, "no remote on GitHub") {
		t.Fatalf("pinned %v, dialog %+v", a.store.pinned(), a.dialog)
	}
	a.dialog = nil

	// A folder inside the repository pins the repository, once.
	sub := filepath.Join(r.Root, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	a.pinRepo(sub)
	a.pinRepo(r.Root)
	if got := a.store.pinned(); len(got) != 1 || got[0] != r.Root {
		t.Fatalf("pinned %v, want %s once", got, r.Root)
	}
}

func TestRepositoryMenuPinsTheOpenRepository(t *testing.T) {
	a, r, _ := prApp(t)
	a.togglePin(a.tab())
	if !a.store.isPinned(r.Root) {
		t.Fatalf("not pinned: %v", a.store.pinned())
	}
	a.togglePin(a.tab())
	if a.store.isPinned(r.Root) {
		t.Fatalf("not unpinned: %v", a.store.pinned())
	}
}

func TestPinsAreKept(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	s := openStore()
	s.pin(dir)
	s.pin(dir)
	if got := openStore().pinned(); len(got) != 1 || got[0] != dir {
		t.Fatalf("pinned %v", got)
	}
	s.unpin(dir)
	if got := openStore().pinned(); len(got) != 0 {
		t.Fatalf("pinned %v after unpinning", got)
	}
}
