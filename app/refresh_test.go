package app

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/eliott/voir/internal/git"
	"github.com/eliott/voir/internal/github"
)

// gitIn runs git in dir, with the private configuration of the test.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// pushedTo makes a bare repository the demo one tracks, and a second clone
// that can push to it.
func pushedTo(t *testing.T, r *git.Repo) (other string) {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "origin.git")
	gitIn(t, t.TempDir(), "init", "-q", "--bare", bare)
	gitIn(t, r.Root, "remote", "add", "origin", bare)
	gitIn(t, r.Root, "push", "-q", "-u", "origin", "main")
	other = filepath.Join(t.TempDir(), "other")
	gitIn(t, t.TempDir(), "clone", "-q", "-b", "main", bare, other)
	gitIn(t, other, "config", "user.name", "Bob")
	gitIn(t, other, "config", "user.email", "bob@example.com")
	gitIn(t, other, "config", "commit.gpgsign", "false")
	return other
}

func TestRefreshFetchesTheRemote(t *testing.T) {
	a, r := testApp(t)
	other := pushedTo(t, r)
	gitIn(t, other, "commit", "-q", "--allow-empty", "-m", "Upstream work")
	gitIn(t, other, "push", "-q", "origin", "main")
	tab := a.tab()
	tab.reload()
	if tab.status.Behind != 0 {
		t.Fatalf("behind = %d before the fetch", tab.status.Behind)
	}
	// Coming back to the window reads the disk, and cannot know.
	a.refresh()
	if tab.status.Behind != 0 {
		t.Fatalf("behind = %d after a reload from disk", tab.status.Behind)
	}
	a.refreshNow()
	if tab.status.Behind != 1 {
		t.Fatalf("behind = %d, want 1 after ⌘R", tab.status.Behind)
	}
	if !slices.ContainsFunc(tab.commits, func(c git.Commit) bool { return c.Subject == "Upstream work" }) {
		t.Fatal("the fetched commit is not in the graph")
	}
	if tab.busy != "" {
		t.Fatalf("still busy with %q", tab.busy)
	}
}

func TestRefreshReadsLocalChanges(t *testing.T) {
	a, r := testApp(t)
	tab := a.tab()
	if err := os.WriteFile(filepath.Join(r.Root, "fresh.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshNow()
	if !slices.ContainsFunc(tab.status.Entries, func(e git.StatusEntry) bool { return e.Path == "fresh.txt" }) {
		t.Fatalf("fresh.txt is not among the changes: %+v", tab.status.Entries)
	}
}

// countingTransport notes each request, then sends it.
type countingTransport func(*http.Request)

func (f countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f(r)
	return http.DefaultTransport.RoundTrip(r)
}

func TestRefreshOnThePullRequestsPageReadsThemAgain(t *testing.T) {
	a, r, _ := prApp(t)
	// Count the reads of GitHub by path.
	var mu sync.Mutex
	reads := map[string]int{}
	inner := a.newGitHub
	a.newGitHub = func(ctx context.Context, host string) (*github.Client, error) {
		cl, err := inner(ctx, host)
		if err == nil {
			cl.HTTP = &http.Client{Transport: countingTransport(func(r *http.Request) {
				mu.Lock()
				reads[r.URL.Path]++
				mu.Unlock()
			})}
		}
		return cl, err
	}
	count := func(path string) int {
		mu.Lock()
		defer mu.Unlock()
		return reads[path]
	}
	// The fetch fails at once, rather than reaching GitHub.
	gitIn(t, r.Root, "config", "protocol.https.allow", "never")
	tab := a.tab()
	tab.reload()
	a.showPulls()
	tab.loadPRs()
	tab.openPR(3)
	if !tab.prsLoaded || tab.pr == nil || !tab.pr.loaded {
		t.Fatalf("the page did not load: loaded=%v pr=%v err=%q", tab.prsLoaded, tab.pr, tab.prsErr)
	}
	list, one := count("/repos/o/r/pulls"), count("/repos/o/r/pulls/3")
	if list == 0 || one == 0 {
		t.Fatalf("reads = %v", reads)
	}
	a.refreshNow()
	if got := count("/repos/o/r/pulls"); got <= list {
		t.Fatalf("the list was not read again: %v", reads)
	}
	if got := count("/repos/o/r/pulls/3"); got <= one {
		t.Fatalf("the pull request shown was not read again: %v", reads)
	}
}
