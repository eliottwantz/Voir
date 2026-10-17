package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
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
	mu       sync.Mutex
	calls    []string
	posts    map[string]string
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
		write(w, []github.PR{pr})
	})
	mux.HandleFunc("/repos/o/r/pulls/3", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			f.record(r)
		}
		p := pr
		p.Head.SHA = f.head
		write(w, p)
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) { write(w, github.User{Login: "me"}) })
	mux.HandleFunc("/repos/o/r/pulls/3/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			f.record(r)
			write(w, map[string]any{"id": 7})
			return
		}
		write(w, []map[string]any{
			{"id": 41, "path": "main.go", "side": "RIGHT", "line": 2, "user": map[string]any{"login": "bob"}, "body": "Why **two**?", "created_at": "2026-10-10T10:00:00Z"},
			{"id": 42, "in_reply_to_id": 41, "path": "main.go", "side": "RIGHT", "line": 2, "user": map[string]any{"login": "ada"}, "body": "Because.", "created_at": "2026-10-10T11:00:00Z"},
			{"id": 43, "in_reply_to_id": 41, "path": "main.go", "side": "RIGHT", "line": 2, "user": map[string]any{"login": "me"}, "body": "Mine, to fix.", "created_at": "2026-10-10T12:00:00Z"},
		})
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
		switch {
		case strings.Contains(body, "resolveReviewThread") && !strings.Contains(body, "unresolve"):
			f.resolved = true
		case strings.Contains(body, "unresolveReviewThread"):
			f.resolved = false
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
			{"sha": "1111111aaaa", "commit": map[string]any{"message": "Add feature file\n\nbody", "author": map[string]any{"name": "Ada"}}},
			{"sha": "2222222bbbb", "commit": map[string]any{"message": "Use it in main", "author": map[string]any{"name": "Ada"}}},
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
	a := newApp(&store{data: stored{Settings: defaultSettings(), SidebarW: 230, ListH: 330, FilesW: 280}})
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

func TestPullRequestsInTheSidebarAndView(t *testing.T) {
	a, _, f := prApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tt.Frame()
	if tab.gh == nil || tab.gh.String() != "o/r" || tab.ghRemote != "origin" {
		t.Fatalf("gh = %+v remote %q", tab.gh, tab.ghRemote)
	}
	a.sideTab = sidePulls
	tt.Frame()
	if !tt.HasText("Pull Requests") || !tt.HasText("#3 Add feature") {
		t.Fatalf("sidebar lacks the pull request: %q", tt.Texts())
	}
	if err := tt.Click("#3 Add feature"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tab.mode != modePR || tab.pr == nil || !tab.pr.loaded {
		t.Fatalf("mode %d pr %+v", tab.mode, tab.pr)
	}
	for _, want := range []string{"Add feature", "Open", "ada wants to merge 2 commits into main from feature", "Checks passed", "Can be merged", "Looks good to me", "build", "test"} {
		if !tt.HasText(want) && !strings.Contains(strings.Join(tt.Texts(), "|"), want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "pr")

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

	// Files changed: the sidebar shows the files, and the patches of all
	// of them are cards in one surface, as a commit's.
	if err := tt.Click("Files changed (2)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.sideTab != sideFiles {
		t.Fatalf("sidebar view %d, want the files", a.sideTab)
	}
	rv := tab.pr.rv
	if rv == nil || !rv.pr || len(rv.files) != 2 || rv.files[0].path != "feature.go" || rv.files[1].path != "main.go" {
		t.Fatalf("review = %+v", rv)
	}
	for _, want := range []string{"feature.go", "main.go", "Viewed"} {
		if !tt.HasText(want) {
			t.Errorf("no %q on the surface: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if rv.current != 1 {
		t.Fatalf("current file %d after choosing main.go", rv.current)
	}
	if f := rv.files[1]; len(f.hunks) == 0 || !strings.Contains(f.patch, "+var a = 2") {
		t.Fatalf("main.go's hunks: %+v", f.hunks)
	}
	if !strings.Contains(strings.Join(tt.Texts(), "|"), "var a = 2") {
		t.Errorf("the lines of main.go are not on the surface: %q", tt.Texts())
	}
	snapshot(t, tt, "pr-files")

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
	if err := tt.Click("Merge…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.dialog == nil {
		t.Fatal("no merge dialog")
	}
	a.dialog.field("method").text = "Squash and merge"
	if err := tt.Click("Merge"); err != nil {
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
	if tab.mode != modePR || tab.pr == nil || tab.pr.number != 9 {
		t.Fatalf("the new pull request is not shown: mode %d pr %+v", tab.mode, tab.pr)
	}
}

func TestPullRequestWhenNotSignedIn(t *testing.T) {
	a, _, _ := prApp(t)
	a.newGitHub = func(context.Context, string) (*github.Client, error) { return nil, github.ErrNotSignedIn }
	a.clients = map[string]*github.Client{}
	tab := a.tab()
	tab.prsLoaded = false
	a.sideTab = sidePulls
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
	a.sideTab = sidePulls
	tt.Frame()
	if err := tt.Click("#3 Add feature"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Files changed (2)"); err != nil {
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

	// A comment on a line, posted alone.
	rv.addComment("main.go", sideNew, 2)
	rv.dirty = true
	tt.Frame()
	if len(rv.comments) != 1 {
		t.Fatalf("comments = %+v", rv.comments)
	}
	rv.comments[0].text = "Please rename this."
	tt.Frame()
	snapshot(t, tt, "pr-comment")
	if err := tt.Click("Add Single Comment"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	body, ok := f.called("POST /repos/o/r/pulls/3/comments")
	for _, want := range []string{`"path":"main.go"`, `"line":2`, `"side":"RIGHT"`, `"commit_id":"abc"`, "Please rename this."} {
		if !ok || !strings.Contains(body, want) {
			t.Fatalf("comment call lacks %s: %q", want, body)
		}
	}
	if len(rv.comments) != 0 {
		t.Errorf("the draft stays: %+v", rv.comments)
	}

	// A comment on an old line, started into a review.
	rv.addComment("main.go", sideOld, 2)
	tt.Frame()
	rv.comments[0].text = "This was fine."
	tt.Frame()
	if err := tt.Click("Start a Review"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	body, ok = f.called("POST /repos/o/r/pulls/3/reviews")
	if !ok || !strings.Contains(body, `"comments"`) || !strings.Contains(body, `"side":"LEFT"`) || !strings.Contains(body, "This was fine.") {
		t.Fatalf("review call: %q", body)
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
	if !file.loaded || !file.canExpand() {
		t.Fatalf("the file is not read whole: loaded %v", file.loaded)
	}
	if !strings.Contains(strings.Join(tt.Texts(), "|"), "unmodified lines") {
		t.Fatalf("no gap on the surface: %q", tt.Texts())
	}
	if err := tt.Click("Expand all"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	lines := 0
	for _, row := range rv.rows {
		if row.kind == rowLine && row.hunk < 0 {
			lines++
		}
	}
	if lines < 100 {
		t.Errorf("%d expanded lines", lines)
	}
	snapshot(t, tt, "pr-expanded")
}

func TestThreadsCanBeAnsweredEditedDeletedAndResolved(t *testing.T) {
	a, _, f := prApp(t)
	tt := ui.NewTester(a.view, 1280, 1300)
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
