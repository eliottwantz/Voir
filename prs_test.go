package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	mu    sync.Mutex
	calls []string
	posts map[string]string
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
	f := &fakeGitHub{}
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
		write(w, pr)
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
		write(w, []any{})
	})
	mux.HandleFunc("/repos/o/r/pulls/3/merge", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		write(w, map[string]bool{"merged": true})
	})
	mux.HandleFunc("/repos/o/r/commits/abc/check-runs", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"check_runs": []map[string]string{{"name": "build", "status": "completed", "conclusion": "success"}, {"name": "test", "status": "completed", "conclusion": "success"}}})
	})
	mux.HandleFunc("/repos/o/r/commits/abc/status", func(w http.ResponseWriter, r *http.Request) { write(w, map[string]any{"statuses": []any{}}) })
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
	if st := tab.diffState(rv.files[1]); st == nil || !strings.Contains(rv.files[1].patch, "+var a = 2") {
		t.Fatalf("main.go's patch: %q", rv.files[1].patch)
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
