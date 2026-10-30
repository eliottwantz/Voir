package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/voir/internal/git"
	"github.com/eliott/voir/internal/github"
)

// commitFile writes a file of the demo repository and commits it, and
// returns the commit.
func commitFile(t *testing.T, r *git.Repo, name, text, msg string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.Root, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Stage(name); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(git.CommitOptions{Message: msg}); err != nil {
		t.Fatal(err)
	}
	head, _ := r.Resolve("HEAD")
	return head
}

// cleanTree drops the demo repository's changes not committed.
func cleanTree(t *testing.T, r *git.Repo) {
	t.Helper()
	for _, args := range [][]string{{"reset", "-q", "--hard"}, {"clean", "-fdq"}} {
		if _, err := r.Do(args...); err != nil {
			t.Fatal(err)
		}
	}
}

func numbered(n int, change map[int]string) string {
	var s strings.Builder
	for i := 1; i <= n; i++ {
		if v, ok := change[i]; ok {
			s.WriteString(v + "\n")
		} else {
			fmt.Fprintf(&s, "line %d\n", i)
		}
	}
	return s.String()
}

func prCommit(sha, msg string) github.Commit {
	var cm github.Commit
	cm.SHA, cm.Commit.Message = sha, msg
	return cm
}

func TestChangesSinceTheLastReview(t *testing.T) {
	a, r, f := prApp(t)
	cleanTree(t, r)
	base, _ := r.Resolve("HEAD")
	reviewed := commitFile(t, r, "notes.md", numbered(20, nil), "Add notes")
	head := commitFile(t, r, "later.go", "package main\n\nfunc later() {}\n", "Add later")
	f.head, f.myReview = head, reviewed
	a.fetchPR = func(context.Context, *git.Repo, string, int, github.PR) (string, error) { return base, nil }
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	rv := openPRFiles(t, a, tt)
	tt.Frame()
	if len(rv.files) != 2 {
		t.Fatalf("the whole pull request has %d files", len(rv.files))
	}
	pv := tab.pr
	pv.commits = []github.Commit{prCommit(reviewed, "Add notes"), prCommit(head, "Add later")}
	tt.Frame()
	if !tt.HasText("1 new commit") {
		t.Fatalf("no way to the changes since the review: %q", tt.Texts())
	}
	snapshot(t, tt, "pr-since-review-button")
	if err := tt.Click("Since your last review: 1 new commit"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	crv := pv.commitRV
	if pv.commitSel != sinceReview || crv == nil || crv.loading || crv.err != "" {
		t.Fatalf("since review: sel %q review %+v", pv.commitSel, crv)
	}
	if len(crv.files) != 1 || crv.files[0].path != "later.go" || crv.base != reviewed {
		t.Fatalf("files since the review: %+v", crv.files)
	}
	if !tt.HasText("Changes since your last review") || !tt.HasText(reviewed[:7]+" → "+head[:7]) {
		t.Errorf("no banner: %q", tt.Texts())
	}
	if tt.HasText("1 new commit") {
		t.Error("the button stays while its changes show")
	}
	snapshot(t, tt, "pr-since-review")

	// Viewing every file of them is reviewing the head: there is nothing new.
	tab.setViewed(crv, crv.files[0], true)
	if h, ok := a.store.reviewed(*tab.gh, 3); !ok || h.SHA != head {
		t.Fatalf("remembered %+v %v", h, ok)
	}
	if _, _, ok := tab.changedSinceReview(pv); ok {
		t.Error("changes since the review after viewing them all")
	}
	tab.showPRCommit(pv, "")
	tt.Frame()
	if pv.since != nil || tt.HasText("1 new commit") {
		t.Errorf("since %+v: %q", pv.since, tt.Texts())
	}
}

func TestChangesSinceTheLastReviewAfterAForcePush(t *testing.T) {
	a, r, f := prApp(t)
	cleanTree(t, r)
	base, _ := r.Resolve("HEAD")
	commitFile(t, r, "notes.md", numbered(20, nil), "Add notes")
	reviewed := commitFile(t, r, "list.md", numbered(30, nil), "Add list")
	if _, err := r.Do("reset", "-q", "--hard", base); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r, "notes.md", numbered(20, nil), "Add notes")
	head := commitFile(t, r, "list.md", numbered(30, map[int]string{15: "fifteen"}), "Add list")
	f.head = head
	a.fetchPR = func(context.Context, *git.Repo, string, int, github.PR) (string, error) { return base, nil }
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	openPRFiles(t, a, tt)
	pv := tab.pr
	// Reviewed here, not on GitHub.
	a.store.setReviewed(*tab.gh, 3, reviewedHead{SHA: reviewed})
	tt.Frame()
	if !tt.HasText("force-pushed") {
		t.Fatalf("no way to the changes since the review: %q", tt.Texts())
	}
	tab.showSinceReview(pv)
	tt.Frame()
	sv, crv := pv.since, pv.commitRV
	if crv == nil || crv.err != "" || sv == nil || !sv.forced || len(sv.pairs) != 2 {
		t.Fatalf("since %+v review %+v", sv, crv)
	}
	if sv.pairs[0].Status != '=' || sv.pairs[1].Status != '!' || !strings.Contains(sv.pairs[1].Diff, "+fifteen") {
		t.Errorf("pairs %+v", sv.pairs)
	}
	for _, want := range []string{"Force-pushed since your last review", "2 commits", "Unchanged", "Changed", "++fifteen"} {
		if !tt.HasText(want) {
			t.Errorf("no %q: %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "pr-since-review-forced")
	// A commit's diff folds away.
	sv.closed[1] = true
	tt.Frame()
	if tt.HasText("++fifteen") || !tt.HasText("Add list") {
		t.Error("the diff stays open")
	}
}

func TestChangesSinceTheLastReviewOfARepositoryNotHere(t *testing.T) {
	a, tt, f := remoteApp(t)
	f.myReview = "2222222"
	a.openPRRef("o/r#3")
	tt.Frame()
	if len(a.remoteTabs) != 1 || a.remoteTabs[0].pr == nil {
		t.Fatal("not opened")
	}
	tab := a.remoteTabs[0]
	pv := tab.pr
	if _, how, ok := tab.changedSinceReview(pv); !ok || how != "0 new commits" {
		t.Errorf("since the review: %q %v", how, ok)
	}
	f.compare = github.Comparison{Status: "ahead", Files: []github.File{{Filename: "main.go", Status: "modified", Additions: 1, Deletions: 1, Patch: "@@ -1 +1 @@\n-a\n+b"}}}
	tab.showSinceReview(pv)
	tt.Frame()
	if crv := pv.commitRV; crv == nil || crv.err != "" || len(crv.files) != 1 {
		t.Fatalf("review %+v", crv)
	}
	f.compare = github.Comparison{Status: "diverged"}
	tab.showSinceReview(pv)
	tt.Frame()
	if crv := pv.commitRV; crv == nil || !strings.Contains(crv.err, "force-pushed") {
		t.Fatalf("review %+v", crv)
	}
}
