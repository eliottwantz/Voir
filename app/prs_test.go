package app

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/eliott/myfork/internal/git"
	"github.com/eliott/myfork/internal/github"
)

// fakeGitHub answers the calls of one repository, o/r, with one open pull
// request (#3), and records the ones that change something.
type fakeGitHub struct {
	// head is the commit the pull request is of, and pending whether the
	// viewer has a review not yet submitted.
	head     string
	pending  bool
	resolved bool
	// reviewed lists the review #7, which asked for changes and opened the
	// thread on main.go, among the reviews.
	reviewed bool
	// copilot makes Copilot the one who opened the thread on main.go.
	copilot bool
	// stacked puts the pull request in a stack of it and #4; stackHead is
	// where the stack says #3 is (the head otherwise), stackDraft makes #4 a
	// draft, and stackLayer4 its state (open otherwise).
	stacked     bool
	stackHead   string
	stackDraft  bool
	stackLayer4 string
	// merges are the answers to a request to merge the stack and to asking
	// again, in turn; the last is repeated.
	merges []map[string]any
	// forkOf names the repository the branch of #4 is in, when it is not this
	// one, and readOnly takes away the right to push to the branches.
	forkOf   string
	readOnly bool
	// noStacks makes the host answer as one that has no stacks.
	noStacks bool
	// listSHA is the head commit the pull request of the list is of, when set.
	listSHA string
	// listed counts the asks for the list of pull requests.
	listed int
	// stackQueries counts the asks for where the pull requests are stacked.
	stackQueries int
	// stackOf puts the pull request asked about in the stack, when asked which.
	stackOf bool
	mu      sync.Mutex
	calls   []string
	posts   map[string]string
	// mutations are the GraphQL calls that change something.
	mutations []string
}

// mutated reports whether a GraphQL mutation named name was called.
func (f *fakeGitHub) mutated(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.mutations {
		if strings.Contains(m, name) {
			return true
		}
	}
	return false
}

func (f *fakeGitHub) record(r *http.Request) string {
	b, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	if f.posts == nil {
		f.posts = map[string]string{}
	}
	f.posts[r.Method+" "+r.URL.Path] = string(b)
	return string(b)
}

func (f *fakeGitHub) called(call string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, ok := f.posts[call]
	return body, ok
}

func newFakeGitHub(t *testing.T) (*fakeGitHub, *httptest.Server) {
	t.Helper()
	f := &fakeGitHub{head: "abc"}
	yes := true
	pr := github.PR{
		Number: 3, Title: "Add feature", Body: "Adds the **feature**.\n\n- one\n- two", State: "open",
		User: github.User{Login: "ada"}, Commits: 2, Additions: 5, Deletions: 1, ChangedFiles: 2, Mergeable: &yes, MergeableState: "clean",
		Head: github.Ref{Ref: "feature", SHA: "abc"}, Base: github.Ref{Ref: "main"}, HTMLURL: "https://github.com/o/r/pull/3",
		Labels: []github.Label{{Name: "bug", Color: "d73a4a"}}, RequestedReviewers: []github.User{{Login: "me"}},
		CreatedAt: time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 10, 10, 9, 30, 0, 0, time.UTC),
	}
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/repos/o/r/pulls", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			body := f.record(r)
			var n github.NewPR
			json.Unmarshal([]byte(body), &n)
			write(w, github.PR{Number: 9, Title: n.Title, State: "open", Draft: n.Draft, User: github.User{Login: "me"}, Head: github.Ref{Ref: n.Head}, Base: github.Ref{Ref: n.Base}})
			return
		}
		if f.listSHA != "" {
			pr.Head.SHA = f.listSHA
		}
		f.mu.Lock()
		f.listed++
		f.mu.Unlock()
		write(w, []github.PR{pr})
	})
	// A second repository, with one pull request of its own, for the pinned ones.
	mux.HandleFunc("/repos/o/other/pulls", func(w http.ResponseWriter, r *http.Request) {
		write(w, []github.PR{{
			Number: 7, Title: "Other work", State: "open", User: github.User{Login: "bob"},
			Head: github.Ref{Ref: "other", SHA: "fed"}, Base: github.Ref{Ref: "main"}, HTMLURL: "https://github.com/o/other/pull/7",
			CreatedAt: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 10, 11, 9, 30, 0, 0, time.UTC),
		}})
	})
	mux.HandleFunc("/repos/o/r/pulls/3", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			f.record(r)
		}
		p := pr
		p.Head.SHA = f.head
		if f.stacked {
			p.Stack = &github.StackRef{Number: 7, Position: 1, Size: 2}
		}
		write(w, p)
	})
	stack7 := func() map[string]any {
		head := cmp.Or(f.stackHead, f.head)
		return map[string]any{"number": 7, "open": true, "base": map[string]string{"ref": "main"}, "pull_requests": []map[string]any{
			{"number": 3, "title": "Add feature", "state": "open", "head": map[string]string{"ref": "feature", "sha": head}, "base": map[string]string{"ref": "main"}},
			{"number": 4, "title": "Second step", "state": cmp.Or(f.stackLayer4, "open"), "draft": f.stackDraft, "head": map[string]string{"ref": "step2", "sha": "def"}, "base": map[string]string{"ref": "feature"}},
		}}
	}
	mux.HandleFunc("/repos/o/r/stacks/7", func(w http.ResponseWriter, r *http.Request) {
		if f.noStacks {
			w.WriteHeader(404)
			write(w, map[string]string{"message": "Not Found"})
			return
		}
		write(w, stack7())
	})
	mux.HandleFunc("/repos/o/r/stacks/7/unstack", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		w.WriteHeader(204)
	})
	mux.HandleFunc("/repos/o/r/stacks/7/add", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		write(w, stack7())
	})
	mux.HandleFunc("/repos/o/r/stacks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			f.record(r)
			write(w, stack7())
			return
		}
		if f.stackOf {
			// #3 is the top of a stack of its own, so that one is added to.
			st := stack7()
			st["pull_requests"] = st["pull_requests"].([]map[string]any)[:1]
			write(w, []map[string]any{st})
			return
		}
		write(w, []map[string]any{})
	})
	mux.HandleFunc("/repos/o/r/pulls/4", func(w http.ResponseWriter, r *http.Request) {
		p := github.PR{Number: 4, Title: "Second step", State: "open", User: github.User{Login: "ada"}, Head: github.Ref{Ref: "step2", SHA: "def"}, Base: github.Ref{Ref: "feature"}, HTMLURL: "https://github.com/o/r/pull/4"}
		if f.stacked {
			p.Stack = &github.StackRef{Number: 7, Position: 2, Size: 2}
		}
		write(w, p)
	})
	for _, prefix := range []string{"/repos/o/r/pulls/4/", "/repos/o/r/issues/4/"} {
		mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) { write(w, []string{}) })
	}
	merged := 0
	answerMerge := func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			f.record(r)
			merged = 0
		} else {
			merged++
		}
		answers := f.merges
		if len(answers) == 0 {
			answers = []map[string]any{{"status": "merged"}}
		}
		write(w, answers[min(merged, len(answers)-1)])
	}
	mux.HandleFunc("/repos/o/r/pulls/3/merge-async", answerMerge)
	mux.HandleFunc("/repos/o/r/pulls/3/merge-async/u1", answerMerge)
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) { write(w, github.User{Login: "me"}) })
	mux.HandleFunc("/repos/o/r/pulls/3/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			f.record(r)
			write(w, map[string]any{"id": 7})
			return
		}
		opener := "bob"
		if f.copilot {
			opener = "Copilot"
		}
		write(w, []map[string]any{
			{"id": 41, "pull_request_review_id": 7, "diff_hunk": "@@ -1,3 +1,3 @@\n package main\n-var a = 1\n+var a = 2", "path": "main.go", "side": "RIGHT", "line": 2, "user": map[string]any{"login": opener}, "body": "Why **two**?", "created_at": "2026-10-10T10:00:00Z"},
			{"id": 42, "in_reply_to_id": 41, "pull_request_review_id": 7, "diff_hunk": "@@ -1,3 +1,3 @@\n package main\n-var a = 1\n+var a = 2", "path": "main.go", "side": "RIGHT", "line": 2, "user": map[string]any{"login": "ada"}, "body": "Because.", "created_at": "2026-10-10T11:00:00Z"},
			{"id": 43, "in_reply_to_id": 41, "pull_request_review_id": 7, "diff_hunk": "@@ -1,3 +1,3 @@\n package main\n-var a = 1\n+var a = 2", "path": "main.go", "side": "RIGHT", "line": 2, "user": map[string]any{"login": "me"}, "body": "Mine, to fix.", "created_at": "2026-10-10T12:00:00Z"},
		})
	})
	mux.HandleFunc("/repos/o/r/labels", func(w http.ResponseWriter, r *http.Request) {
		write(w, []github.Label{{Name: "bug", Color: "d73a4a", Description: "Something isn't working"}, {Name: "enhancement", Color: "a2eeef", Description: "New feature or request"}})
	})
	mux.HandleFunc("/repos/o/r/issues/3/labels", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		write(w, []github.Label{})
	})
	mux.HandleFunc("/repos/o/r/pulls/3/comments/41/replies", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		write(w, map[string]any{"id": 44})
	})
	mux.HandleFunc("/repos/o/r/pulls/comments/43", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		write(w, map[string]any{"id": 43})
	})
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		body := f.record(r)
		if strings.Contains(body, "mutation(") {
			f.mu.Lock()
			f.mutations = append(f.mutations, body)
			f.mu.Unlock()
		}
		switch {
		case strings.Contains(body, "resolveReviewThread") && !strings.Contains(body, "unresolve"):
			f.mu.Lock()
			f.mutations = append(f.mutations, body)
			f.mu.Unlock()
			f.resolved = true
		case strings.Contains(body, "unresolveReviewThread"):
			f.resolved = false
		case strings.Contains(body, "PRStatuses"):
			write(w, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequests": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false},
				"nodes": []map[string]any{{"number": 3, "reviewDecision": "APPROVED", "additions": 12, "deletions": 4, "commits": map[string]any{"nodes": []map[string]any{
					{"commit": map[string]any{"statusCheckRollup": map[string]any{"state": "FAILURE"}}}}}}},
			}}}})
			return
		case strings.Contains(body, "PRStacks"):
			f.stackQueries++
			if f.noStacks {
				write(w, map[string]any{"errors": []map[string]string{{"message": "Field 'stackEntry' doesn't exist on type 'PullRequest'"}}})
				return
			}
			write(w, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequests": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false},
				"nodes":    []map[string]any{{"number": 3, "stackEntry": map[string]any{"position": 2, "stack": map[string]any{"size": 3}}}, {"number": 8, "stackEntry": nil}},
			}}}})
			return
		case strings.Contains(body, "viewerPermission"):
			repo := map[string]string{"nameWithOwner": "o/r", "viewerPermission": "WRITE"}
			if f.readOnly {
				repo["viewerPermission"] = "READ"
			}
			data := map[string]any{}
			for i, n := range regexp.MustCompile(`pullRequest\(number:(\d+)\)`).FindAllStringSubmatch(body, -1) {
				num, _ := strconv.Atoi(n[1])
				head := repo
				if num == 4 && f.forkOf != "" {
					head = map[string]string{"nameWithOwner": f.forkOf, "viewerPermission": "READ"}
				}
				data[fmt.Sprintf("pr%d", i)] = map[string]any{"number": num, "headRepository": head}
			}
			write(w, map[string]any{"data": map[string]any{"repository": data}})
			return
		case strings.Contains(body, "ReviewThreads"):
			write(w, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"reviewThreads": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false},
				"nodes":    []map[string]any{{"id": "PRRT_1", "isResolved": f.resolved, "comments": map[string]any{"nodes": []map[string]any{{"fullDatabaseId": "41"}}}}},
			}}}}})
			return
		}
		write(w, map[string]any{"data": map[string]any{}})
	})
	mux.HandleFunc("/repos/o/r/pulls/9", func(w http.ResponseWriter, r *http.Request) {
		write(w, github.PR{Number: 9, Title: "Created here", State: "open", User: github.User{Login: "me"}, Head: github.Ref{Ref: "topic"}, Base: github.Ref{Ref: "main"}})
	})
	mux.HandleFunc("/repos/o/r/pulls/3/files", func(w http.ResponseWriter, r *http.Request) {
		write(w, []github.File{
			{Filename: "feature.go", Status: "added", Additions: 4, Patch: "@@ -0,0 +1,4 @@\n+package main\n+\n+func feature() {}\n+"},
			{Filename: "main.go", Status: "modified", Additions: 1, Deletions: 1, Patch: "@@ -1,3 +1,3 @@\n package main\n-var a = 1\n+var a = 2\n "},
		})
	})
	mux.HandleFunc("/repos/o/r/pulls/3/commits", func(w http.ResponseWriter, r *http.Request) {
		write(w, []map[string]any{
			{"sha": "1111111aaaa", "author": map[string]any{"login": "ada"}, "commit": map[string]any{"message": "Add feature file\n\nbody", "author": map[string]any{"name": "Ada", "date": "2026-10-10T09:10:00Z"}}},
			{"sha": "2222222bbbb", "commit": map[string]any{"message": "Use it in main", "author": map[string]any{"name": "Ada", "date": "2026-10-10T09:20:00Z"}}},
		})
	})
	mux.HandleFunc("/repos/o/r/issues/3/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			f.record(r)
			write(w, github.Comment{ID: 5, Body: "ok", User: github.User{Login: "me"}})
			return
		}
		write(w, []github.Comment{{ID: 1, Body: "Looks good to me", User: github.User{Login: "bob"}}})
	})
	mux.HandleFunc("/repos/o/r/pulls/3/reviews", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			f.record(r)
			write(w, map[string]any{"id": 1})
			return
		}
		if f.pending {
			write(w, []map[string]any{{"id": 9, "node_id": "PRR_9", "state": "PENDING", "body": ""}})
			return
		}
		if f.reviewed {
			write(w, []map[string]any{{"id": 7, "node_id": "PRR_7", "state": "CHANGES_REQUESTED", "body": "Please look at the thread.", "user": map[string]any{"login": "bob"}, "submitted_at": "2026-10-10T10:00:00Z"}})
			return
		}
		write(w, []any{})
	})
	mux.HandleFunc("/repos/o/r/pulls/3/reviews/9/comments", func(w http.ResponseWriter, r *http.Request) {
		write(w, []map[string]any{{"id": 50, "path": "main.go", "side": "RIGHT", "line": 2, "user": map[string]any{"login": "me"}, "body": "A draft.", "created_at": "2026-10-11T10:00:00Z"}})
	})
	mux.HandleFunc("/repos/o/r/pulls/3/reviews/9/events", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		write(w, map[string]any{"id": 9})
	})
	mux.HandleFunc("/repos/o/r/pulls/3/merge", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		write(w, map[string]bool{"merged": true})
	})
	mux.HandleFunc("/repos/o/r/commits/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/status") {
			write(w, map[string]any{"statuses": []any{}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/1111111aaaa") {
			write(w, map[string]any{"files": []github.File{{Filename: "feature.go", Status: "added", Additions: 2, Patch: "@@ -0,0 +1,2 @@\n+package main\n+func feature() {}"}}})
			return
		}
		write(w, map[string]any{"check_runs": []map[string]string{{"name": "build", "status": "completed", "conclusion": "success"}, {"name": "test", "status": "completed", "conclusion": "success"}}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return f, srv
}

// prApp opens the demo repository, with a GitHub remote the fake answers for.
func prApp(t *testing.T) (*app, *git.Repo, *fakeGitHub) {
	t.Helper()
	r := demoRepo(t)
	if err := r.AddRemote("origin", "https://github.com/o/r.git"); err != nil {
		t.Fatal(err)
	}
	f, srv := newFakeGitHub(t)
	a := newApp(newStore())
	a.mergePoll = func(int) time.Duration { return 0 }
	a.guideDir = t.TempDir()
	// The commits are not fetched from GitHub: a test says where they are.
	a.fetchPR = func(context.Context, *git.Repo, string, int, github.PR) (string, error) {
		return "", errors.New("offline")
	}
	a.newGitHub = func(_ context.Context, host string) (*github.Client, error) {
		if host != "github.com" {
			t.Errorf("signed in to %q", host)
		}
		return &github.Client{BaseURL: srv.URL, Token: "tok"}, nil
	}
	if err := a.openRepo(r.Root); err != nil {
		t.Fatal(err)
	}
	return a, r, f
}

func TestPullRequestsPage(t *testing.T) {
	a, _, f := prApp(t)
	tt := ui.NewTester(a.view, 1280, 1500)
	tab := a.tab()
	tt.Frame()
	if tab.gh == nil || tab.gh.String() != "o/r" || tab.ghRemote != "origin" {
		t.Fatalf("gh = %+v remote %q", tab.gh, tab.ghRemote)
	}
	// The repository's view has no pull requests: they are on a page of
	// their own.
	if tt.HasText("Filter pull requests") || tt.HasText("#3 Add feature") {
		t.Fatalf("the pull requests are in the repository's view: %q", tt.Texts())
	}
	a.showPulls()
	tt.Frame()
	if !tt.HasText("Pull Requests") || tt.HasText("Search SHA, author or message") {
		t.Fatalf("no page of pull requests: %q", tt.Texts())
	}
	// The one the viewer was asked to review is under its own heading, and
	// the list tells the label.
	for _, want := range []string{"Review requested", "#3", "Add feature", "ada", "o/r", "bug", "Sort", "Filters"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in the list: %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "pr-list")

	// Choosing one opens its details beside the list.
	if err := tt.Click("#3 Add feature"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tab.pr == nil || !tab.pr.loaded || a.prExpanded {
		t.Fatalf("pr %+v expanded %v", tab.pr, a.prExpanded)
	}
	for _, want := range []string{"Add feature", "Open", "Checks passed", "Can be merged", "Summary", "Timeline", "Code", "Reviewers", "Labels", "Description", "Comments (2)", "gh pr checkout 3", "Merge pull request"} {
		if !tt.HasText(want) && !strings.Contains(strings.Join(tt.Texts(), "|"), want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	if !tt.HasText("#3 Add feature") {
		t.Errorf("the list is gone: %q", tt.Texts())
	}
	snapshot(t, tt, "pr")

	// Expanded, the details take the page; the list comes back.
	if err := tt.Click("Expand the pull request (⌘↩)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.prExpanded || tt.HasText("Search pull requests") {
		t.Fatalf("expanded %v: %q", a.prExpanded, tt.Texts())
	}
	if tt.HasText("Expand the pull request (⌘↩)") {
		t.Errorf("both icons are shown: %q", tt.Texts())
	}
	snapshot(t, tt, "pr-panel-expanded")
	if err := tt.Click("Show the list again (⌘↩)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.prExpanded || !tt.HasText("Filters") {
		t.Fatalf("expanded %v: %q", a.prExpanded, tt.Texts())
	}

	// ⌘↩ does the same from the keyboard, both ways.
	tt.Key(ui.Cmd, ui.KeyEnter)
	tt.Frame()
	if !a.prExpanded {
		t.Fatalf("⌘↩ did not expand: %q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyEnter)
	tt.Frame()
	if a.prExpanded {
		t.Fatalf("⌘↩ did not show the list again: %q", tt.Texts())
	}

	// The checks are folded away until they are opened.
	if tt.HasText("build") {
		t.Errorf("the checks are open: %q", tt.Texts())
	}
	if err := tt.Click("Checks"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"build", "test"} {
		if !tt.HasText(want) {
			t.Errorf("no check %q: %q", want, tt.Texts())
		}
	}

	// Comment.
	tab.pr.draft = "Thanks, merging soon"
	tt.Frame()
	if err := tt.Click("Comment"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	body, ok := f.called("POST /repos/o/r/issues/3/comments")
	if !ok || !strings.Contains(body, "Thanks, merging soon") {
		t.Fatalf("comment not posted: %q", body)
	}
	if tab.pr.draft != "" {
		t.Errorf("draft kept: %q", tab.pr.draft)
	}

	// The timeline has the commits and the comments.
	if err := tt.Click("Timeline"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"Add feature file", "Use it in main", "Looks good to me", "Pull request opened", "1111111"} {
		if !strings.Contains(strings.Join(tt.Texts(), "|"), want) {
			t.Errorf("no %q in the timeline: %q", want, tt.Texts())
		}
	}
	if !tt.HasText("Newest first") || tt.HasText("Oldest first") {
		t.Errorf("the timeline does not start from the newest: %q", tt.Texts())
	}
	snapshot(t, tt, "pr-timeline")

	// Code: the patches of all the files are cards in one surface, as a
	// commit's, and the tree of files is shown on demand.
	if err := tt.Click("Code"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	rv := tab.pr.rv
	if rv == nil || !rv.pr || len(rv.files) != 2 || rv.files[0].path != "feature.go" || rv.files[1].path != "main.go" {
		t.Fatalf("review = %+v", rv)
	}
	for _, want := range []string{"2 files", "0 / 2 viewed", "feature.go", "main.go", "Viewed"} {
		if !tt.HasText(want) {
			t.Errorf("no %q on the surface: %q", want, tt.Texts())
		}
	}
	if tt.HasText("Filter files") {
		t.Fatalf("the file tree is shown at first: %q", tt.Texts())
	}
	snapshot(t, tt, "pr-files")

	// One commit of the pull request can be shown alone, read from the
	// repository.
	head, err := tab.repo.Resolve("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var cm github.Commit
	cm.SHA = head
	cm.Commit.Message = "The head commit"
	tab.pr.commits = append(tab.pr.commits, cm)
	tab.showPRCommit(tab.pr, head)
	tt.Frame()
	if crv := tab.pr.commitRV; crv == nil || crv.loading || crv.err != "" || len(crv.files) == 0 {
		t.Fatalf("commit review = %+v", crv)
	}
	if !tt.HasText(head[:7] + " The head commit") {
		t.Errorf("the picker does not name the commit: %q", tt.Texts())
	}
	tab.showPRCommit(tab.pr, "")
	tab.pr.commits = tab.pr.commits[:len(tab.pr.commits)-1]
	tt.Frame()
	if err := tt.Click("Show file tree"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.prTree || !tt.HasText("Filter files") {
		t.Fatalf("no file tree: prTree %v", a.prTree)
	}
	snapshot(t, tt, "pr-files-tree")
	// The card of main.go comes first by that name: choose it in the tree by
	// the lines it changed.
	if err := tt.Click("+1 −1"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if rv.current != 1 {
		t.Fatalf("current file %d after choosing main.go", rv.current)
	}
	if f := rv.files[1]; !strings.Contains(f.patch, "+var a = 2") {
		t.Fatalf("main.go's patch: %q", f.patch)
	}
	// ⌘C copies the path of the file chosen in the tree, and ⇧⌘C its path on
	// disk, not the link of the pull request; ⇧⌘O opens it.
	tt.Key(ui.Cmd, ui.KeyC)
	tt.Frame()
	if a.copied != "main.go" {
		t.Errorf("⌘C copied %q", a.copied)
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyC)
	tt.Frame()
	if want := filepath.Join(tab.repo.Root, "main.go"); a.copied != want {
		t.Errorf("⇧⌘C copied %q, want %q", a.copied, want)
	}
	item := find(t, a.menuTemplate(), "open-selected-file")
	item.Click(item, nil)
	if want := filepath.Join(tab.repo.Root, "main.go"); len(a.opened) != 1 || a.opened[0] != want {
		t.Errorf("⇧⌘O opened %q, want %q", a.opened, want)
	}
	if d := rv.files[1].ed; d == nil || !strings.Contains(d.New.String(), "var a = 2") {
		t.Errorf("the lines of main.go are not in its viewer: %+v", rv.files[1])
	}
	if err := tt.Click("Hide file tree"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.prTree || tt.HasText("Filter files") {
		t.Fatalf("the file tree stays: %q", tt.Texts())
	}

	// Review: approve.
	tab.pr.tab = 0
	tt.Frame()
	if err := tt.Click("Review"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.dialog == nil || a.dialog.title != "Finish Your Review" {
		t.Fatalf("dialog = %+v", a.dialog)
	}
	a.dialog.field("event").text = "Approve"
	tt.Frame()
	snapshot(t, tt, "pr-review")
	if err := tt.Click("Submit Review"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	body, ok = f.called("POST /repos/o/r/pulls/3/reviews")
	if !ok || !strings.Contains(body, `"event":"APPROVE"`) {
		t.Fatalf("review call: %q", body)
	}

	// Merge by squashing.
	tab.pr.tab = 0
	tt.Frame()
	if err := tt.Click("Merge pull request"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.dialog == nil {
		t.Fatal("no merge dialog")
	}
	if a.dialog.ok != "Confirm merge" {
		t.Fatalf("ok = %q", a.dialog.ok)
	}
	a.dialog = nil
	a.mergePRDialog(tab, "Squash and merge")
	tt.Frame()
	if err := tt.Click("Confirm squash and merge"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	body, ok = f.called("PUT /repos/o/r/pulls/3/merge")
	if !ok || !strings.Contains(body, `"merge_method":"squash"`) || !strings.Contains(body, "Add feature (#3)") {
		t.Fatalf("merge call: %q", body)
	}
}

func TestCreateAPullRequest(t *testing.T) {
	a, r, f := prApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	if err := r.CreateBranch("topic", "HEAD", true, git.KeepChanges); err != nil {
		t.Fatal(err)
	}
	tab.reload()
	tt.Frame()
	a.createPRDialog(tab, "")
	tt.Frame()
	if a.dialog == nil || !tt.HasText("Create pull request") {
		t.Fatalf("no dialog: %q", tt.Texts())
	}
	snapshot(t, tt, "pr-create")
	a.dialog.field("push").checked = false // there is no remote to push to
	a.dialog.field("draft").checked = true
	a.dialog.field("title").text = "Add the topic"
	a.dialog.field("body").text = "Because."
	tt.Frame()
	if err := tt.Click("Create Pull Request"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	body, ok := f.called("POST /repos/o/r/pulls")
	if !ok {
		t.Fatalf("nothing created; dialog err %q", dialogErr(a))
	}
	var n github.NewPR
	json.Unmarshal([]byte(body), &n)
	if n.Title != "Add the topic" || n.Head != "topic" || n.Base != "main" || !n.Draft || n.Body != "Because." {
		t.Fatalf("created %+v", n)
	}
	if !a.pulls || tab.pr == nil || tab.pr.number != 9 {
		t.Fatalf("the new pull request is not shown: page %v pr %+v", a.pulls, tab.pr)
	}
}

func TestPullRequestWhenNotSignedIn(t *testing.T) {
	a, _, _ := prApp(t)
	a.newGitHub = func(context.Context, string) (*github.Client, error) { return nil, github.ErrNotSignedIn }
	a.clients = map[string]*github.Client{}
	tab := a.tab()
	tab.prsLoaded = false
	a.pulls = true
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	if !strings.Contains(tab.prsErr, "gh auth login") || !tt.HasText("Try Again") {
		t.Fatalf("err %q, texts %q", tab.prsErr, tt.Texts())
	}
}

func TestDetectGitHubRemote(t *testing.T) {
	remotes := []git.Remote{
		{Name: "gitlab", FetchURL: "git@gitlab.com:o/r.git"},
		{Name: "upstream", FetchURL: "https://github.com/up/r.git"},
		{Name: "origin", FetchURL: "git@github.com:me/r.git"},
	}
	repo, name, ok := detectGitHub(remotes)
	if !ok || name != "origin" || repo.String() != "me/r" {
		t.Fatalf("got %v %q %v", repo, name, ok)
	}
	if _, _, ok := detectGitHub(remotes[:1]); ok {
		t.Error("GitLab is not supported")
	}
}

func openPRFiles(t *testing.T, a *app, tt *ui.Tester) *review {
	t.Helper()
	tab := a.tab()
	a.showPulls()
	tt.Frame()
	if err := tt.Click("#3 Add feature"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Code"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	return tab.pr.rv
}

func TestPullRequestLineComments(t *testing.T) {
	a, _, f := prApp(t)
	tt := ui.NewTester(a.view, 1280, 1300)
	rv := openPRFiles(t, a, tt)
	if !rv.canComment {
		t.Fatalf("the viewer cannot comment: %+v", a.tab().pr)
	}
	// The thread on main.go's line 2 shows under it.
	if !strings.Contains(strings.Join(tt.Texts(), "|"), "Because.") {
		t.Errorf("no thread on the surface: %q", tt.Texts())
	}
	snapshot(t, tt, "pr-thread")

	// A comment on a line is added to the review, and kept here: nothing goes
	// to GitHub yet.
	rv.addComment("main.go", sideNew, 2)
	tt.Frame()
	if len(rv.comments) != 1 {
		t.Fatalf("comments = %+v", rv.comments)
	}
	rv.comments[0].text = "Please rename this."
	tt.Frame()
	snapshot(t, tt, "pr-comment")
	if err := tt.Click("Add to Review"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !rv.comments[0].saved || !tt.HasText("Pending") || !tt.HasText("Please rename this.") {
		t.Fatalf("the comment is not shown as added: %+v, %q", rv.comments[0], tt.Texts())
	}
	snapshot(t, tt, "pr-comment-added")
	// A comment on an old line, added too.
	rv.addComment("main.go", sideOld, 2)
	tt.Frame()
	rv.comments[1].text = "This was fine."
	tt.Frame()
	if err := tt.Click("Add to Review"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, call := range []string{"POST /repos/o/r/pulls/3/comments", "POST /repos/o/r/pulls/3/reviews"} {
		if body, ok := f.called(call); ok {
			t.Fatalf("%s before the review is submitted: %q", call, body)
		}
	}
	if rv.draftCount() != 2 {
		t.Fatalf("%d comments written", rv.draftCount())
	}

	// The review takes both with it.
	if err := tt.Click("Review"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("2 comments on lines will be submitted with this review.") {
		t.Errorf("the dialog does not say what goes: %q", tt.Texts())
	}
	if err := tt.Click("Submit Review"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	body, ok := f.called("POST /repos/o/r/pulls/3/reviews")
	for _, want := range []string{`"event":"COMMENT"`, `"comments"`, `"path":"main.go"`, `"side":"RIGHT"`, `"side":"LEFT"`, `"commit_id":"abc"`, "Please rename this.", "This was fine."} {
		if !ok || !strings.Contains(body, want) {
			t.Fatalf("review call lacks %s: %q", want, body)
		}
	}
	if len(rv.comments) != 0 {
		t.Errorf("the comments stay after they were sent: %+v", rv.comments)
	}
}

func TestSubmittingAReviewSendsItsPendingComments(t *testing.T) {
	a, _, f := prApp(t)
	f.pending = true
	tt := ui.NewTester(a.view, 1280, 1300)
	rv := openPRFiles(t, a, tt)
	pv := a.tab().pr
	if pv.reviews == nil || pv.reviews.Pending == nil || pv.reviews.PendingComments() != 1 {
		t.Fatalf("reviews = %+v", pv.reviews)
	}
	if !strings.Contains(strings.Join(tt.Texts(), "|"), "A draft.") {
		t.Errorf("the draft is not on the surface: %q", tt.Texts())
	}
	_ = rv
	pv.tab = 0
	tt.Frame()
	if err := tt.Click("Review"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	// A review with drafts needs no summary.
	a.dialog.field("event").text = "Request changes"
	tt.Frame()
	if err := tt.Click("Submit Review"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	body, ok := f.called("POST /repos/o/r/pulls/3/reviews/9/events")
	if !ok || !strings.Contains(body, `"event":"REQUEST_CHANGES"`) {
		t.Fatalf("submit call: %q", body)
	}
}

func TestPullRequestFilesExpandWhenTheCommitsAreHere(t *testing.T) {
	a, r, f := prApp(t)
	write := func(text string) {
		if err := os.WriteFile(filepath.Join(r.Root, "NOTES.md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := r.Stage(); err != nil {
			t.Fatal(err)
		}
	}
	write(longFile(300))
	if err := r.Commit(git.CommitOptions{Message: "Add notes"}); err != nil {
		t.Fatal(err)
	}
	write(longFile(300, 10, 200))
	if err := r.Commit(git.CommitOptions{Message: "Edit notes"}); err != nil {
		t.Fatal(err)
	}
	head, _ := r.Resolve("HEAD")
	base, _ := r.Resolve("HEAD~1")
	f.head = head
	a.fetchPR = func(context.Context, *git.Repo, string, int, github.PR) (string, error) { return base, nil }
	tt := ui.NewTester(a.view, 1280, 800)
	tt.SetDark(true)
	rv := openPRFiles(t, a, tt)
	tt.Frame()
	if !rv.local || len(rv.files) != 1 || rv.files[0].path != "NOTES.md" {
		t.Fatalf("review: local %v files %+v", rv.local, rv.files)
	}
	file := rv.files[0]
	if file.ed == nil || file.lines != nil {
		t.Fatalf("the file is not read whole: viewer %v, patch lines %v", file.ed != nil, file.lines != nil)
	}
	if !strings.Contains(strings.Join(tt.Texts(), "|"), "unmodified lines") {
		t.Fatalf("no gap on the surface: %q", tt.Texts())
	}
	file.ed.ExpandAll()
	tt.Frame()
	tt.Frame()
	if strings.Contains(strings.Join(tt.Texts(), "|"), "unmodified lines") {
		t.Errorf("the gaps stay after Expand all: %q", tt.Texts())
	}
	snapshot(t, tt, "pr-expanded")
}

func TestThreadsCanBeAnsweredEditedDeletedAndResolved(t *testing.T) {
	a, _, f := prApp(t)
	tt := ui.NewTester(a.view, 1500, 1300)
	rv := openPRFiles(t, a, tt)
	if len(rv.threads) != 1 || rv.threads[0].NodeID != "PRRT_1" || rv.threads[0].Resolved {
		t.Fatalf("threads = %+v", rv.threads)
	}

	// Reply.
	tt.Frame()
	if rv.replies[41] == nil {
		t.Fatal("no reply box under the thread")
	}
	*rv.replies[41] = "Thanks, fixing."
	tt.Frame()
	if err := tt.Click("Reply"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	body, ok := f.called("POST /repos/o/r/pulls/3/comments/41/replies")
	if !ok || !strings.Contains(body, "Thanks, fixing.") {
		t.Fatalf("reply call: %q", body)
	}
	if *rv.replies[41] != "" {
		t.Errorf("the reply stays: %q", *rv.replies[41])
	}

	// Only the viewer's own comment can be edited or deleted.
	edits := 0
	for _, s := range tt.Texts() {
		if s == "Edit comment" {
			edits++
		}
	}
	if edits != 1 {
		t.Errorf("%d edit buttons, want 1: %q", edits, tt.Texts())
	}
	if err := tt.Click("Edit comment"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if rv.editing != 43 || rv.editText != "Mine, to fix." {
		t.Fatalf("editing %d %q", rv.editing, rv.editText)
	}
	rv.editText = "Fixed."
	tt.Frame()
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	body, ok = f.called("PATCH /repos/o/r/pulls/comments/43")
	if !ok || !strings.Contains(body, "Fixed.") || rv.editing != 0 {
		t.Fatalf("edit call: %q (editing %d)", body, rv.editing)
	}

	// Delete asks first.
	if err := tt.Click("Delete comment"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.dialog == nil {
		t.Fatal("no confirmation")
	}
	if _, ok := f.called("DELETE /repos/o/r/pulls/comments/43"); ok {
		t.Fatal("deleted before the answer")
	}
	if err := tt.Click("Delete"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := f.called("DELETE /repos/o/r/pulls/comments/43"); !ok {
		t.Fatal("not deleted")
	}

	// Resolve folds the thread; Show opens it.
	if err := tt.Click("Resolve"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !f.resolved || len(rv.threads) != 1 || !rv.threads[0].Resolved {
		t.Fatalf("resolved %v, threads %+v", f.resolved, rv.threads)
	}
	if strings.Contains(strings.Join(tt.Texts(), "|"), "Because.") {
		t.Error("a resolved thread shows its comments")
	}
	snapshot(t, tt, "pr-resolved")
	if err := tt.Click("Show thread"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !strings.Contains(strings.Join(tt.Texts(), "|"), "Because.") {
		t.Error("Show does not open the thread")
	}
	if err := tt.Click("Unresolve"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if f.resolved || rv.threads[0].Resolved {
		t.Errorf("still resolved: %v %+v", f.resolved, rv.threads)
	}
}

func TestPullRequestsButtonInTheToolbar(t *testing.T) {
	a, _, _ := prApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	if err := tt.Click("Pull Requests"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	tt.Frame()
	if !a.pulls || !tt.HasText("Search pull requests") {
		t.Fatalf("the page is not shown: %q", tt.Texts())
	}
}

func TestRecentRepositoriesLeaveOutThoseGone(t *testing.T) {
	here := t.TempDir()
	s := &store{data: stored{Recent: []string{here, filepath.Join(here, "gone")}}}
	if got := s.recent(); len(got) != 1 || got[0] != here {
		t.Fatalf("recent = %q", got)
	}
}

func TestPullRequestMoreMenu(t *testing.T) {
	a, _, f := prApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	a.showPulls()
	tt.Frame()
	tab.openPR(3)
	tt.Frame()
	if tt.HasText("Check Out") {
		t.Fatalf("a Check Out button is left: %q", tt.Texts())
	}
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"Check Out", "Convert to draft", "Enable auto-merge", "Merge", "Squash and merge", "Rebase and merge", "Open on GitHub", "Copy link", "⇧⌘C", "Copy PR number", "⇧⌘K", "Close pull request"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in the menu: %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "pr-more")

	// A way of merging opens the dialog with that method.
	if err := tt.Click("Squash and merge"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.prMore || a.dialog == nil || a.dialog.ok != "Confirm squash and merge" {
		t.Fatalf("menu open %v, dialog %+v", a.prMore, a.dialog)
	}
	a.dialog.open = false
	a.dialog = nil
	tt.Frame()

	// Convert to draft, which GitHub does through GraphQL.
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Convert to draft"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !f.mutated("convertPullRequestToDraft") {
		t.Fatalf("no conversion to draft: %q", f.mutations)
	}

	// Auto-merge, and the closing of the pull request.
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Enable auto-merge"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !f.mutated("enablePullRequestAutoMerge") {
		t.Fatalf("no auto-merge: %q", f.mutations)
	}
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Close pull request"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if body, ok := f.called("PATCH /repos/o/r/pulls/3"); !ok || !strings.Contains(body, `"state":"closed"`) {
		t.Fatalf("close call: %q", body)
	}

	// The shortcuts copy the link and the number.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyK)
	tt.Frame()
	if got := a.copied; got != "3" {
		t.Errorf("clipboard %q after ⇧⌘K", got)
	}
}

func TestPullRequestCommentOnLines(t *testing.T) {
	a, _, f := prApp(t)
	tt := ui.NewTester(a.view, 1280, 1300)
	rv := openPRFiles(t, a, tt)
	tt.Frame()
	d := rv.files[0].ed
	if d == nil || d.OnGutterButton == nil {
		t.Fatalf("feature.go has no viewer to comment in: %+v", rv.files[0])
	}
	// The + of a line among those selected comments on all of them.
	d.OnGutterButton(editor.DiffLineRange{Side: editor.DiffNew, Start: 0, End: 2})
	tt.Frame()
	if len(rv.comments) != 1 {
		t.Fatalf("comments = %+v", rv.comments)
	}
	cm := rv.comments[0]
	if cm.path != "feature.go" || cm.side != sideNew || cm.start != 1 || cm.line != 3 {
		t.Fatalf("comment = %+v", cm)
	}
	if !tt.HasText("New lines 1-3") {
		t.Errorf("the comment does not say its lines: %q", tt.Texts())
	}
	cm.text = "Why this?"
	tt.Frame()
	snapshot(t, tt, "pr-lines-comment")
	if err := tt.Click("Add to Review"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if body, ok := f.called("POST /repos/o/r/pulls/3/reviews"); ok {
		t.Fatalf("sent before the review is: %q", body)
	}
	// It shows under its last line, as written.
	tt.Frame()
	at := slices.IndexFunc(d.Annotations, func(an editor.DiffAnnotation) bool { return an.Key == cm })
	if at < 0 || d.Annotations[at].Line != 2 || d.Annotations[at].Side != editor.DiffNew {
		t.Fatalf("the comment is not under line 3: %+v", d.Annotations)
	}
	if err := tt.Click("Review"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Submit Review"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	body, ok := f.called("POST /repos/o/r/pulls/3/reviews")
	for _, want := range []string{`"path":"feature.go"`, `"start_line":1`, `"line":3`, `"start_side":"RIGHT"`, "Why this?"} {
		if !ok || !strings.Contains(body, want) {
			t.Fatalf("review call lacks %s: %q", want, body)
		}
	}
	if tt.HasText("Pending") {
		t.Errorf("the comment still waits after the review: %q", tt.Texts())
	}

	// With nothing selected, the + comments on its line alone.
	tt.Frame()
	d.OnGutterButton(editor.DiffLineRange{Side: editor.DiffNew, Start: 0, End: 0})
	tt.Frame()
	if len(rv.comments) != 1 || rv.comments[0].start != 1 || rv.comments[0].line != 1 {
		t.Fatalf("comments = %+v", rv.comments)
	}
}

func TestFilesShowTheirCommentsAndTheWayBetweenThem(t *testing.T) {
	a, _, _ := prApp(t)
	tt := ui.NewTester(a.view, 1280, 700)
	rv := openPRFiles(t, a, tt)
	// main.go has a thread of three comments; feature.go gets one written.
	rv.addComment("feature.go", sideNew, 2)
	rv.comments[0].text = "Name this."
	rv.comments[0].saved = true
	tt.Frame()
	if n := rv.commentsIn(rv.files[0]); n != 1 {
		t.Fatalf("feature.go has %d comments", n)
	}
	if n := rv.commentsIn(rv.files[1]); n != 3 {
		t.Fatalf("main.go has %d comments", n)
	}
	for _, want := range []string{"1 comment", "3 comments", "4 comments"} {
		if !tt.HasText(want) {
			t.Errorf("no %q: %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "pr-comment-counts")

	// Folded files are opened on the way: the thread is in main.go.
	rv.files[1].collapsed = true
	tt.Frame()
	if err := tt.Click("Next comment"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if rv.navFile != 0 || rv.current != 0 {
		t.Fatalf("first stop: file %d, current %d", rv.navFile, rv.current)
	}
	if err := tt.Click("Next comment"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if rv.navFile != 1 || rv.current != 1 || rv.files[1].collapsed {
		t.Fatalf("second stop: file %d, current %d, main.go folded %v", rv.navFile, rv.current, rv.files[1].collapsed)
	}
	// At the end, round to the first; Previous goes back.
	if err := tt.Click("Next comment"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if rv.navFile != 0 {
		t.Fatalf("after the last: file %d", rv.navFile)
	}
	if err := tt.Click("Previous comment"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if rv.navFile != 1 {
		t.Fatalf("back: file %d", rv.navFile)
	}

	// The count in a file's header goes to its first comment.
	rv.list.ScrollTo(0, ui.Start)
	tt.Frame()
	rv.navFile = -1
	if err := tt.Click("3 comments"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if rv.navFile != 1 {
		t.Fatalf("header count: file %d", rv.navFile)
	}
}

func TestTheSummaryScrollsToItsComposer(t *testing.T) {
	a, _, _ := prApp(t)
	const height = 800
	tt := ui.NewTester(a.view, 1280, height)
	a.showPulls()
	tt.Frame()
	tab := a.tab()
	tab.openPR(3)
	tt.Frame()
	tab.pr.pr.Body = strings.Repeat("A paragraph of the description.\n\n", 40)
	// Beside the list, as expanded: the end of the summary is in the window.
	for _, expanded := range []bool{false, true} {
		a.prExpanded = expanded
		tt.Frame()
		for range 20 {
			tt.Scroll(900, 500, 0, 2000)
			tt.Frame()
		}
		b, ok := tt.Find("Comment")
		if !ok || b.H == 0 || b.Y+b.H > height {
			t.Errorf("expanded %v: the Comment button is at %+v in a window %d high", expanded, b, height)
		}
	}
}

func TestPullRequestRowShowsStackAndChanges(t *testing.T) {
	a, _, _ := prApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	a.showPulls()
	tt.Frame()
	texts := strings.Join(tt.Texts(), "|")
	for _, want := range []string{"2/3", "+12", "−4"} {
		if !strings.Contains(texts, want) {
			t.Errorf("the row lacks %q: %q", want, texts)
		}
	}
}

func TestShortAge(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	for d, want := range map[time.Duration]string{30 * time.Second: "just now", 5 * time.Minute: "5m ago", 3 * time.Hour: "3h ago", 39 * 24 * time.Hour: "39d ago", 800 * 24 * time.Hour: "2y ago"} {
		if got := shortAge(now, now.Add(-d)); got != want {
			t.Errorf("%v ago = %q, want %q", d, got, want)
		}
	}
}

func TestPullRequestFilters(t *testing.T) {
	a, _, _ := prApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	a.showPulls()
	tt.Frame()
	if tab.prStatus[3].Review != "approved" || tab.prStatus[3].Checks != "failing" {
		t.Fatalf("status = %+v", tab.prStatus)
	}
	listed := func() bool { return strings.Contains(strings.Join(tt.Texts(), "|"), "Add feature") }
	if !listed() {
		t.Fatalf("the pull request is not listed: %q", tt.Texts())
	}
	if err := tt.Click("Filters"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"State", "Open", "Involvement", "Author", "Anyone", "Labels", "Any", "Draft", "Review", "Checks"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in the filters: %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "pr-filters")
	choose := func(filter, choice string) {
		t.Helper()
		if err := tt.Click(filter + ": " + valueOf(a, filter)); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		snapshot(t, tt, "pr-filters-"+strings.ToLower(filter))
		if err := tt.Click(choice); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	reopen := func() {
		t.Helper()
		if !a.prFilterOpen {
			if err := tt.Click("Filters"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
		}
	}

	choose("Draft", "Drafts only")
	if a.prFilters.draft != "only" || listed() || !tt.HasText("No pull request matches") {
		t.Fatalf("drafts only: %+v, %q", a.prFilters, tt.Texts())
	}
	reopen()
	choose("Draft", "Hide drafts")
	if !listed() {
		t.Fatalf("hide drafts: %q", tt.Texts())
	}
	reopen()
	choose("Review", "Changes requested")
	if listed() {
		t.Fatalf("changes requested lists an approved one: %q", tt.Texts())
	}
	reopen()
	choose("Review", "Approved")
	if !listed() {
		t.Fatalf("approved: %q", tt.Texts())
	}
	reopen()
	choose("Checks", "Passing")
	if listed() {
		t.Fatalf("passing lists a failing one: %q", tt.Texts())
	}
	reopen()
	choose("Checks", "Failing")
	if !listed() {
		t.Fatalf("failing: %q", tt.Texts())
	}
	reopen()
	choose("Involvement", "Authored")
	if listed() {
		t.Fatalf("authored lists ada's, not the viewer's: %q", tt.Texts())
	}
	reopen()
	choose("Involvement", "Reviewing")
	if !listed() {
		t.Fatalf("reviewing: the viewer was asked: %q", tt.Texts())
	}
	reopen()
	choose("Author", "Author ada")
	if a.prFilters.author != "ada" || !listed() {
		t.Fatalf("author: %+v", a.prFilters)
	}
	reopen()
	choose("Labels", "Label bug")
	if !a.prFilters.labels["bug"] || !listed() {
		t.Fatalf("labels: %+v", a.prFilters)
	}
	// Merged ones are read again, and this one is open.
	tab.prsLoaded = true
	if err := tt.Click("State: Open"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Merged"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.prFilters.state != "merged" || a.prFilters.fetchState() != "closed" || listed() {
		t.Fatalf("state: %+v, %q", a.prFilters, tt.Texts())
	}
	reopen()
	if err := tt.Click("Clear filters"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.prFilters.active() != 0 || !listed() {
		t.Fatalf("clear: %+v, %q", a.prFilters, tt.Texts())
	}
}

// valueOf is what a filter shows as its value in the menu.
func valueOf(a *app, filter string) string {
	f := a.prFilters
	switch filter {
	case "State":
		return labelOf(prStateChoices, cmp.Or(f.state, "open"))
	case "Involvement":
		return labelOf(prInvolvementChoices, f.involvement)
	case "Author":
		return cmp.Or(f.author, "Anyone")
	case "Labels":
		for name := range f.labels {
			return name
		}
		return "Any"
	case "Draft":
		return labelOf(prDraftChoices, f.draft)
	case "Review":
		return labelOf(prReviewChoices, f.review)
	}
	return labelOf(prChecksChoices, f.checks)
}

func TestPullRequestTabs(t *testing.T) {
	a, _, f := prApp(t)
	f.stacked = true
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	a.showPulls()
	tt.Frame()
	tab.openPR(3)
	tt.Frame()
	tab.openPR(4)
	tt.Frame()
	if len(tab.prTabs) != 2 || tab.pr == nil || tab.pr.number != 4 {
		t.Fatalf("tabs %d, active %+v", len(tab.prTabs), tab.pr)
	}
	for _, want := range []string{"#3", "#4", "Second step"} {
		if !tt.HasText(want) {
			t.Errorf("no %q: %q", want, tt.Texts())
		}
	}

	// ⇧⌘[ and ⇧⌘] go through the tabs, round the ends.
	for _, step := range []struct {
		key  ui.Key
		want int
	}{{ui.KeyBracketLeft, 3}, {ui.KeyBracketLeft, 4}, {ui.KeyBracketRight, 3}, {ui.KeyBracketRight, 4}} {
		tt.Key(ui.Cmd|ui.Shift, step.key)
		tt.Frame()
		if tab.pr == nil || tab.pr.number != step.want || !a.pulls {
			t.Fatalf("after the key: active %+v, want #%d", tab.pr, step.want)
		}
	}
	// A pull request already open is shown, not opened twice.
	tab.openPR(3)
	if len(tab.prTabs) != 2 || tab.pr.number != 3 {
		t.Fatalf("tabs %d, active #%d", len(tab.prTabs), tab.pr.number)
	}
	tt.Frame()
	if err := tt.Click("Close #3"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(tab.prTabs) != 1 || tab.pr == nil || tab.pr.number != 4 {
		t.Fatalf("after closing #3: tabs %d, active %+v", len(tab.prTabs), tab.pr)
	}
	tab.closePR(tab.pr)
	if tab.pr != nil || len(tab.prTabs) != 0 {
		t.Fatalf("the last tab is still open: %+v", tab.pr)
	}
}

// openStack opens #3, which is the bottom of a stack of it and #4.
func openStack(t *testing.T) (*app, *ui.Tester, *repoTab, *fakeGitHub) {
	t.Helper()
	a, _, f := prApp(t)
	f.stacked = true
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	a.showPulls()
	tt.Frame()
	tab.openPR(3)
	tt.Frame()
	if tab.pr.stack == nil || len(tab.pr.stack.Entries) != 2 {
		t.Fatalf("stack = %+v", tab.pr.stack)
	}
	return a, tt, tab, f
}

func openStackMenu(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click("Stack, 1 of 2"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
}

func TestStackMenu(t *testing.T) {
	a, tt, tab, _ := openStack(t)
	if tab.pr.stackPos() != 0 {
		t.Fatalf("position = %d", tab.pr.stackPos())
	}
	openStackMenu(t, tt)
	for _, want := range []string{"Stack #7", "Second step", "#4 · step2 · Open", "Merge stack (1)", "Rebase stack", "Unstack"} {
		if !tt.HasText(want) {
			t.Errorf("no %q: %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "pr-stack-menu")
	// A pull request of the stack opens in a tab of its own.
	if err := tt.Click("#4 Second step"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(tab.prTabs) != 2 || tab.pr.number != 4 || a.prStack {
		t.Fatalf("tabs %d, active %+v", len(tab.prTabs), tab.pr)
	}
}

func TestMergeTheStack(t *testing.T) {
	a, tt, tab, f := openStack(t)
	f.merges = []map[string]any{{"status": "pending", "details": map[string]any{"uuid": "u1"}}, {"status": "pending", "details": map[string]any{"uuid": "u1"}}, {"status": "merged"}}
	openStackMenu(t, tt)
	if err := tt.Click("Merge stack (1)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.dialog == nil || a.dialog.title != "Merge stack #7" {
		t.Fatalf("dialog = %+v", a.dialog)
	}
	a.dialog.field("method").text = "Squash and merge"
	if err := tt.Click("Merge"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	body, ok := f.called("PUT /repos/o/r/pulls/3/merge-async")
	if !ok || !strings.Contains(body, `"merge_method":"squash"`) || !strings.Contains(body, `"sha":"abc"`) || !strings.Contains(body, `"merge_action":"default"`) {
		t.Fatalf("merge call: %q", body)
	}
	if a.dialog != nil && a.dialog.err != "" {
		t.Fatalf("the merge failed: %s", a.dialog.err)
	}
	if got := strings.Join(tt.Texts(), "|"); !strings.Contains(got, "Merged the stack up to #3") {
		t.Fatalf("texts = %q", got)
	}
	_ = tab
}

func TestMergeTheStackQueued(t *testing.T) {
	_, tt, _, f := openStack(t)
	f.merges = []map[string]any{{"status": "enqueued"}}
	openStackMenu(t, tt)
	tt.Click("Merge stack (1)")
	tt.Frame()
	tt.Click("Merge")
	tt.Frame()
	if got := strings.Join(tt.Texts(), "|"); !strings.Contains(got, "merge queue") || strings.Contains(got, "Merged the stack") {
		t.Fatalf("texts = %q", got)
	}
}

func TestMergeTheStackFailed(t *testing.T) {
	a, tt, _, f := openStack(t)
	f.merges = []map[string]any{{"status": "failed", "details": map[string]any{"message": "Required status check is failing"}}}
	openStackMenu(t, tt)
	tt.Click("Merge stack (1)")
	tt.Frame()
	tt.Click("Merge")
	tt.Frame()
	if a.dialog == nil || !strings.Contains(a.dialog.err, "Required status check is failing") {
		t.Fatalf("dialog = %+v", a.dialog)
	}
}

func TestMergeTheStackRefusesAStackThatChanged(t *testing.T) {
	a, tt, _, f := openStack(t)
	openStackMenu(t, tt)
	tt.Click("Merge stack (1)")
	tt.Frame()
	// Someone pushes to #3 while the dialog is open.
	f.stackHead = "moved"
	tt.Click("Merge")
	tt.Frame()
	if _, ok := f.called("PUT /repos/o/r/pulls/3/merge-async"); ok {
		t.Fatal("the stack was merged though it changed")
	}
	if a.dialog == nil || !strings.Contains(a.dialog.err, "changed since it was read") {
		t.Fatalf("dialog = %+v", a.dialog)
	}
}

func TestMergeTheStackNeedsEveryLayerOpenAndReady(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(f *fakeGitHub)
		want  string
	}{
		{"draft", func(f *fakeGitHub) { f.stackDraft = true }, "#4 is a draft"},
		{"closed", func(f *fakeGitHub) { f.stackLayer4 = "closed" }, "#4 is closed without being merged"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _, f := prApp(t)
			f.stacked = true
			tc.setup(f)
			tt := ui.NewTester(a.view, 1280, 800)
			tab := a.tab()
			a.showPulls()
			tt.Frame()
			tab.openPR(4)
			tt.Frame()
			if got := tab.pr.stackMergeIssue(); !strings.Contains(got, tc.want) {
				t.Fatalf("issue = %q", got)
			}
		})
	}
}

func TestStackMergeIsOffWhenALayerIsADraft(t *testing.T) {
	a, _, f := prApp(t)
	f.stacked = true
	f.stackDraft = true
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	a.showPulls()
	tt.Frame()
	tab.openPR(4)
	tt.Frame()
	if err := tt.Click("Stack, 2 of 2"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Click("Merge stack (2)")
	tt.Frame()
	if a.dialog != nil {
		t.Fatalf("a dialog opens though a layer is a draft: %+v", a.dialog)
	}
}

func TestRebaseTheStackChecksTheBranches(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(f *fakeGitHub)
		want  string
	}{
		{"a fork", func(f *fakeGitHub) { f.forkOf = "someone/r" }, "a stack from a fork cannot be rebased here"},
		{"no right to push", func(f *fakeGitHub) { f.readOnly = true }, "You cannot push to the branch of #3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, tt, _, f := openStack(t)
			tc.setup(f)
			openStackMenu(t, tt)
			if err := tt.Click("Rebase stack"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if a.dialog == nil || a.dialog.title != "Rebase stack #7" {
				t.Fatalf("dialog = %+v", a.dialog)
			}
			if err := tt.Click("Rebase"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if a.dialog == nil || !strings.Contains(a.dialog.err, tc.want) {
				t.Fatalf("dialog = %+v", a.dialog)
			}
		})
	}
}

func TestRebaseTheStackRefusesAStackThatChanged(t *testing.T) {
	a, tt, _, f := openStack(t)
	openStackMenu(t, tt)
	tt.Click("Rebase stack")
	tt.Frame()
	f.stackLayer4 = "closed"
	tt.Click("Rebase")
	tt.Frame()
	if a.dialog == nil || !strings.Contains(a.dialog.err, "changed since it was read") {
		t.Fatalf("dialog = %+v", a.dialog)
	}
}

func TestUnstack(t *testing.T) {
	a, tt, _, f := openStack(t)
	openStackMenu(t, tt)
	if err := tt.Click("Unstack"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.dialog == nil || a.dialog.title != "Unstack #7" || !a.dialog.danger {
		t.Fatalf("dialog = %+v", a.dialog)
	}
	// "Unstack" is the title and the button: the button is the last.
	tt.Click("Unstack")
	tt.Frame()
	if _, ok := f.called("POST /repos/o/r/stacks/7/unstack"); !ok {
		t.Fatalf("calls = %v", f.calls)
	}
	if got := strings.Join(tt.Texts(), "|"); !strings.Contains(got, "Unstacked #7") {
		t.Fatalf("texts = %q", got)
	}
}

func TestStackedPullRequestsAreNotAskedForOnAHostWithoutThem(t *testing.T) {
	a, _, f := prApp(t)
	f.noStacks = true
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	a.showPulls()
	tt.Frame()
	if !tab.stacksOff || f.stackQueries != 1 {
		t.Fatalf("off %v after %d asks", tab.stacksOff, f.stackQueries)
	}
	// The statuses are not lost with the stacks.
	if tab.prStatus[3].Review != "approved" || tab.prStatus[3].Additions != 12 {
		t.Fatalf("status = %+v", tab.prStatus)
	}
	tab.prsLoaded = false
	tt.Frame()
	if f.stackQueries != 1 {
		t.Fatalf("asked again: %d", f.stackQueries)
	}
}

// TestCreatePullRequestDialogAsksForThePullRequestsItStacksOn opens the dialog
// in a tab that has never shown the pull requests, as a worktree's tab does:
// the stack it would join is told once the list they are of arrives.
func TestCreatePullRequestDialogAsksForThePullRequestsItStacksOn(t *testing.T) {
	a, r, f := prApp(t)
	// #3 is the demo repository's last commit; the new branch is built on it.
	head, _ := r.Resolve("HEAD")
	f.head = head
	f.listSHA = head
	r.CreateBranch("topic", "HEAD", true, git.KeepChanges)
	os.WriteFile(filepath.Join(r.Root, "t.txt"), []byte("t\n"), 0o644)
	r.Stage()
	r.Commit(git.CommitOptions{Message: "Add t"})
	r.Do("update-ref", "refs/remotes/origin/feature", head)
	tab := a.tab()
	tab.reload()
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	if tab.prsLoaded {
		t.Fatalf("the pull requests were already read")
	}
	a.hold = true
	a.createPRDialog(tab, "")
	texts := func() string { return strings.Join(tt.Texts(), "|") }
	tt.Frame()
	if !tab.prsLoading {
		t.Fatalf("the dialog did not ask for the pull requests")
	}
	if strings.Contains(texts(), "Stack it on") || strings.Contains(texts(), "This branch is built on") {
		t.Fatalf("a stack said before the pull requests are there: %q", tt.Texts())
	}
	a.runHeld()
	tab.prStacks = nil
	tt.Frame()
	if !strings.Contains(texts(), "This branch is built on #3 “Add feature” (feature). Choose feature as the destination to stack on it.") {
		t.Fatalf("no hint once the pull requests are there: %q", tt.Texts())
	}
	a.dialog.field("base").text = "feature"
	tt.Frame()
	for _, want := range []string{"Stack it on #3 “Add feature”, the pull request of feature", "It starts a stack of #3 and this pull request."} {
		if !strings.Contains(texts(), want) {
			t.Errorf("no %q: %q", want, tt.Texts())
		}
	}
}

func TestCreateAStackedPullRequest(t *testing.T) {
	for _, tc := range []struct {
		name    string
		inStack bool
		want    string
	}{
		{"starts a stack", false, "POST /repos/o/r/stacks"},
		{"adds to a stack", true, "POST /repos/o/r/stacks/7/add"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, r, f := prApp(t)
			f.stackOf = tc.inStack
			tt := ui.NewTester(a.view, 1280, 800)
			tab := a.tab()
			if err := r.CreateBranch("topic", "HEAD", true, git.KeepChanges); err != nil {
				t.Fatal(err)
			}
			// The branch #3 is of is a base to choose.
			r.Do("update-ref", "refs/remotes/origin/feature", "HEAD")
			tab.reload()
			a.showPulls()
			tt.Frame()
			tab.prStacks = nil
			a.createPRDialog(tab, "")
			tt.Frame()
			// Until the branch it is into is the one of #3, there is nothing to stack on.
			if strings.Contains(strings.Join(tt.Texts(), "|"), "Stack it on") {
				t.Fatalf("a stacking option for main: %q", tt.Texts())
			}
			a.dialog.field("base").text = "feature"
			tt.Frame()
			if !tt.HasText("Stack it on #3 “Add feature”, the pull request of feature") {
				t.Fatalf("no stacking option: %q", tt.Texts())
			}
			a.dialog.field("push").checked = false
			tt.Frame()
			if err := tt.Click("Create Pull Request"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			body, ok := f.called(tc.want)
			if !ok {
				t.Fatalf("calls = %v", f.calls)
			}
			if tc.inStack && !strings.Contains(body, "9") || !tc.inStack && !strings.Contains(body, "[3,9]") {
				t.Fatalf("stack call: %q", body)
			}
			if got := strings.Join(tt.Texts(), "|"); !strings.Contains(got, "Opened #9 in stack #7") {
				t.Fatalf("texts = %q", got)
			}
		})
	}
}

func TestStackedAction(t *testing.T) {
	was := github.Stack{Entries: []github.StackEntry{
		{Number: 1, MergedAt: &time.Time{}},
		{Number: 2, State: "open", Head: github.Ref{SHA: "a"}},
		{Number: 3, State: "open", Head: github.Ref{SHA: "b"}},
	}}
	same := was
	if stackChanged(was, same, 0) {
		t.Fatal("the same stack changed")
	}
	moved := github.Stack{Entries: slices.Clone(was.Entries)}
	moved.Entries[2].Head.SHA = "c"
	if !stackChanged(was, moved, 0) || !stackChanged(was, moved, 3) {
		t.Fatal("a push to #3 went unseen")
	}
	// A push above where the action stops does not matter to it.
	if stackChanged(was, moved, 2) {
		t.Fatal("a push to #3 matters to a merge up to #2")
	}
	if grown := (github.Stack{Entries: append(slices.Clone(was.Entries), github.StackEntry{Number: 4, State: "open"})}); !stackChanged(was, grown, 0) {
		t.Fatal("a new layer went unseen")
	}
}

func TestReviewersIncludeThoseWhoAlreadyReviewed(t *testing.T) {
	pv := &prView{
		pr: github.PR{User: github.User{Login: "ada"}, RequestedReviewers: []github.User{{Login: "me"}, {Login: "bob"}}},
		convo: []github.Comment{
			{User: github.User{Login: "bob"}, State: "CHANGES_REQUESTED"},
			{User: github.User{Login: "ada"}, Body: "a comment"},
			{User: github.User{Login: "cy"}, State: "APPROVED"},
			{User: github.User{Login: "bob"}, State: "APPROVED"},
			{User: github.User{Login: "dee"}, State: "APPROVED"},
			{User: github.User{Login: "dee"}, State: "DISMISSED"},
		},
	}
	var got []string
	for _, r := range pv.reviewers() {
		got = append(got, r.User.Login+":"+r.State)
	}
	want := []string{"bob:APPROVED", "cy:APPROVED", "dee:", "me:"}
	if !slices.Equal(got, want) {
		t.Fatalf("reviewers = %v, want %v", got, want)
	}
}

func TestOpenThreadsShowInTheSummaryAndTheTimeline(t *testing.T) {
	for _, reviewed := range []bool{false, true} {
		a, _, f := prApp(t)
		f.reviewed = reviewed
		tt := ui.NewTester(a.view, 1280, 1300)
		tab := a.tab()
		a.showPulls()
		tt.Frame()
		if err := tt.Click("#3 Add feature"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		// The thread is in the summary, with the lines it is on, and says it
		// is not resolved.
		for _, want := range []string{"1 unresolved conversation", "Why two?", "Because.", "main.go", "+var a = 2"} {
			if !strings.Contains(strings.Join(tt.Texts(), "|"), want) {
				t.Errorf("reviewed %v: the summary lacks %q: %q", reviewed, want, tt.Texts())
			}
		}
		snapshot(t, tt, fmt.Sprint("pr-summary-threads-", reviewed))
		if err := tt.Click("Timeline"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if !strings.Contains(strings.Join(tt.Texts(), "|"), "Because.") {
			t.Errorf("reviewed %v: the timeline lacks the thread: %q", reviewed, tt.Texts())
		}
		snapshot(t, tt, fmt.Sprint("pr-timeline-threads-", reviewed))
		if err := tt.Click("Summary"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		// It is resolved from there, and the conversation no longer counts.
		if err := tt.Click("Resolve"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if !f.resolved || tab.pr.unresolved() != 0 {
			t.Errorf("reviewed %v: resolved %v, %d open", reviewed, f.resolved, tab.pr.unresolved())
		}
		if strings.Contains(strings.Join(tt.Texts(), "|"), "unresolved conversation") {
			t.Errorf("reviewed %v: still says it is unresolved: %q", reviewed, tt.Texts())
		}
	}
}

func TestThreadsFoldUnderTheirFileAndCopilotsCanBeResolvedWithAReason(t *testing.T) {
	a, _, f := prApp(t)
	f.copilot = true
	tt := ui.NewTester(a.view, 1280, 1500)
	tab := a.tab()
	a.showPulls()
	tt.Frame()
	if err := tt.Click("#3 Add feature"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	texts := func() string { return strings.Join(tt.Texts(), "|") }

	// The file's header folds the thread, and opens it again.
	if err := tt.Click("Collapse main.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if strings.Contains(texts(), "Because.") || strings.Contains(texts(), "+var a = 2") {
		t.Errorf("a folded thread shows: %q", tt.Texts())
	}
	if err := tt.Click("Expand main.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !strings.Contains(texts(), "Because.") {
		t.Fatalf("expanding does not show the thread: %q", tt.Texts())
	}

	// Copilot's thread says why it is resolved.
	if strings.Contains(texts(), "Won't fix") {
		t.Error("the reasons show before the menu is open")
	}
	if err := tt.Click("Resolve with a reason"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"Addressed", "Won't fix", "Incorrect"} {
		if !strings.Contains(texts(), want) {
			t.Errorf("no reason %q: %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "pr-resolve-reasons")
	if err := tt.Click("Won't fix"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !f.resolved || !f.mutated("WONT_FIX") || tab.pr.unresolved() != 0 {
		t.Errorf("resolved %v, mutations %q, %d open", f.resolved, f.mutations, tab.pr.unresolved())
	}
}

func TestOnlyCopilotsThreadsAreResolvedWithAReason(t *testing.T) {
	a, _, _ := prApp(t)
	tt := ui.NewTester(a.view, 1280, 1500)
	a.showPulls()
	tt.Frame()
	if err := tt.Click("#3 Add feature"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("Resolve") && tt.Click("Resolve with a reason") == nil {
		t.Error("a person's thread has reasons")
	}
}

func TestLabelsCanBeAddedAndTakenOff(t *testing.T) {
	a, _, f := prApp(t)
	tt := ui.NewTester(a.view, 1280, 1500)
	tab := a.tab()
	a.showPulls()
	tt.Frame()
	if err := tt.Click("#3 Add feature"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Edit labels"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Search labels") || !tt.HasText("enhancement") {
		t.Fatalf("the labels are not listed: %q", tt.Texts())
	}
	snapshot(t, tt, "pr-labels-menu")
	if err := tt.Click("enhancement"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	body, ok := f.called("PUT /repos/o/r/issues/3/labels")
	if !ok || !strings.Contains(body, "enhancement") || !strings.Contains(body, "bug") {
		t.Fatalf("labels call: %q", body)
	}
	if n := len(tab.pr.pr.Labels); n != 2 {
		t.Errorf("%d labels", n)
	}
	// Choosing a label that is on the pull request takes it off.
	tab.toggleLabel(tab.pr, github.Label{Name: "bug"})
	tt.Frame()
	body, _ = f.called("PUT /repos/o/r/issues/3/labels")
	if strings.Contains(body, "bug") || len(tab.pr.pr.Labels) != 1 {
		t.Errorf("bug is still there: %q %+v", body, tab.pr.pr.Labels)
	}
}

func TestEditPRTitleAndDescription(t *testing.T) {
	a, _, f := prApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	a.showPulls()
	tt.Frame()
	tab.openPR(3)
	tt.Frame()
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Edit title and description"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	d := a.dialog
	if d == nil || d.title != "Edit pull request" || d.str("title") != tab.pr.pr.Title {
		t.Fatalf("dialog %+v", d)
	}
	d.field("title").text = "A better title"
	d.field("body").text = "![shot](https://example.com/a.png)\n\nNew words"
	if err := d.run(d); err != nil {
		t.Fatal(err)
	}
	body, ok := f.called("PATCH /repos/o/r/pulls/3")
	if !ok || !strings.Contains(body, `"title":"A better title"`) || !strings.Contains(body, "New words") {
		t.Fatalf("edit call: %q", body)
	}
}

func TestPullRequestPageKeysPickTabsAndEscapeLeaves(t *testing.T) {
	a, _, _ := prApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tab.showChanges()
	a.showPulls()
	tt.Frame()
	tab.openPR(3)
	tt.Frame()

	click := func(id string) bool {
		for _, m := range a.menuTemplate() {
			for _, it := range m.Submenu {
				if it.ID == id {
					it.Click(nil, nil)
					return true
				}
			}
		}
		return false
	}

	// ⌥1-⌥3 are the Summary, Timeline and Code tabs of a pull request.
	for want, id := range []int{prTabSummary, prTabTimeline, prTabCode} {
		tab.pr.tab = prTabGuide
		if !click([]string{"pr-tab-summary", "pr-tab-timeline", "pr-tab-code"}[want]) || tab.pr.tab != id || !a.pulls || !tab.onWip {
			t.Fatalf("item %d: tab %d, want %d, page %v, on local changes %v", want, tab.pr.tab, id, a.pulls, tab.onWip)
		}
	}
	if !click("pr-tab-guide") || tab.pr.tab != prTabGuide || a.accel("pr-tab-guide") != "Alt+4" {
		t.Fatalf("⌥4 chose tab %d", tab.pr.tab)
	}

	// ⌘1-⌘3 are the views again: they leave the page.
	if !click("view-commits") || a.pulls || tab.onWip {
		t.Fatalf("⌘2 left the page at %v, on local changes %v", a.pulls, tab.onWip)
	}
	a.showPulls()
	tt.Frame()

	// With no pull request shown, the keys still do not leave the page.
	tab.closePR(tab.pr)
	tt.Frame()
	if !click("pr-tab-guide") || !a.pulls {
		t.Fatalf("⌥4 left the page")
	}

	// Escape goes back to what was shown before.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.pulls {
		t.Fatalf("after escape: page %v", a.pulls)
	}
}

func TestOpeningAPullRequestKeepsThePageExpanded(t *testing.T) {
	a, tt, tab, _ := openStack(t)
	a.prExpanded = true
	tab.openPR(4)
	tt.Frame()
	if !a.prExpanded || tab.pr.number != 4 || len(tab.prTabs) != 2 {
		t.Fatalf("expanded %v, active %+v, tabs %d", a.prExpanded, tab.pr, len(tab.prTabs))
	}
}

func TestTheSidebarShowsTheListAgain(t *testing.T) {
	a, _, _ := prApp(t)
	a.pinRepo(otherRepo(t).Root)
	tt := ui.NewTester(a.view, 1280, 800)
	a.showPulls()
	tt.Frame()
	a.tab().openPR(3)
	for _, click := range []string{"Pull requests of o/other", "All pull requests"} {
		a.prExpanded = true
		tt.Frame()
		if err := tt.Click(click); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if a.prExpanded {
			t.Errorf("%q left the page expanded", click)
		}
	}
}

func TestLinksOpenPullRequestsInTabs(t *testing.T) {
	a, tt, tab, _ := openStack(t)
	a.prExpanded = true
	if !a.openLink("https://github.com/o/r/pull/4#issuecomment-1") || tab.pr.number != 4 || !a.prExpanded {
		t.Fatalf("address: active %+v expanded %v", tab.pr, a.prExpanded)
	}
	if !a.openLink("#3") || tab.pr.number != 3 || len(tab.prTabs) != 2 {
		t.Fatalf("#3: active %+v, tabs %d", tab.pr, len(tab.prTabs))
	}
	tt.Frame()
	for _, link := range []string{"https://github.com/o/r/issues/3", "https://github.com/o/r", "#heading", "https://github.com/o/r/pull/x", "mailto:a@b.c"} {
		if a.openLink(link) {
			t.Errorf("%q opened as a pull request", link)
		}
	}
	got := cleanGitHubMarkdown("Fixes #12 and (#13), see [#14](#14) and [x](#15), `#16`")
	if want := "Fixes [#12](#12) and ([#13](#13)), see [#14](#14) and [x](#15), `#16`"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCommitPushAndOpenThePullRequest(t *testing.T) {
	a, r, _ := prApp(t)
	bare := filepath.Join(t.TempDir(), "remote.git")
	if _, err := r.Do("init", "--bare", bare); err != nil {
		t.Fatal(err)
	}
	// The remote is GitHub's for the app, and this directory for git.
	r.SetConfig("remote.origin.pushurl", bare)
	r.Do("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	r.Do("update-ref", "refs/remotes/origin/main", "HEAD")
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tt.Frame()
	if err := os.WriteFile(filepath.Join(r.Root, "ship.txt"), []byte("ship\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tab.reload()
	if err := tt.Click("Local Changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Stage All"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	// On the default branch, there is no pull request to open.
	if !tt.HasText("Commit & Push") || tt.HasText("Commit, Push & PR") {
		t.Fatalf("on main: %q", tt.Texts())
	}
	if _, err := r.Do("checkout", "-b", "ship-it"); err != nil {
		t.Fatal(err)
	}
	tab.reload()
	tt.Frame()
	if !tt.HasText("Commit, Push & PR") || tt.HasText("Commit & Push") {
		t.Fatalf("on ship-it: %q", tt.Texts())
	}
	tab.msg = "Ship it"
	if err := tt.Click("Commit, Push & PR"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if out, err := r.Read("--git-dir", bare, "log", "-1", "--format=%s", "ship-it"); err != nil || strings.TrimSpace(string(out)) != "Ship it" {
		t.Fatalf("pushed %q: %v", out, err)
	}
	if a.dialog == nil || a.dialog.title != "Create pull request" || a.dialog.str("title") != "Ship it" {
		t.Fatalf("dialog %+v", a.dialog)
	}
	if a.dialog.bool("push") {
		t.Error("the branch is pushed already, and is offered again")
	}
}

func TestPullRequestRowShowsReadinessAndApproval(t *testing.T) {
	a, _, _ := prApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	a.showPulls()
	tt.Frame()
	// #3 is approved, and its checks fail.
	for _, want := range []string{"Approved", "Checks are failing"} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("the row lacks %q: %q", want, tt.Texts())
		}
	}
	if tt.HasText("draft") {
		t.Errorf("a draft bubble is shown: %q", tt.Texts())
	}
	snapshot(t, tt, "pr-list-status")
}

func TestPRReady(t *testing.T) {
	open := github.PR{State: "open"}
	for _, c := range []struct {
		pr    github.PR
		st    github.PRStatus
		ready bool
	}{
		{open, github.PRStatus{}, true},
		{open, github.PRStatus{Review: "approved", Checks: "passing"}, true},
		{github.PR{State: "open", Draft: true}, github.PRStatus{Review: "approved", Checks: "passing"}, false},
		{open, github.PRStatus{Checks: "failing"}, false},
		{open, github.PRStatus{Checks: "pending"}, false},
		{open, github.PRStatus{Review: "changes"}, false},
		{open, github.PRStatus{Review: "required"}, false},
		{open, github.PRStatus{Conflicting: true}, false},
	} {
		if got, why := prReady(c.pr, c.st); got != c.ready || why == "" {
			t.Errorf("%+v %+v: ready %v (%q), want %v", c.pr, c.st, got, why, c.ready)
		}
	}
}
