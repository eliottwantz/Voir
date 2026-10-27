package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseRemote(t *testing.T) {
	for in, want := range map[string]Repo{
		"git@github.com:egoist/mygo.git":           {"github.com", "egoist", "mygo"},
		"https://github.com/egoist/mygo":           {"github.com", "egoist", "mygo"},
		"https://github.com/egoist/mygo.git/":      {"github.com", "egoist", "mygo"},
		"ssh://git@github.com/egoist/mygo.git":     {"github.com", "egoist", "mygo"},
		"https://user:tok@ghe.example.com/o/r.git": {"ghe.example.com", "o", "r"},
		"git@ghe.example.com:team/service.git":     {"ghe.example.com", "team", "service"},
	} {
		got, ok := ParseRemote(in)
		if !ok || got != want {
			t.Errorf("ParseRemote(%q) = %+v, %v; want %+v", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "/local/path", "https://github.com/onlyowner", "file:///x/y/z"} {
		if r, ok := ParseRemote(bad); ok {
			t.Errorf("ParseRemote(%q) = %+v, want none", bad, r)
		}
	}
}

// fake is a GitHub that knows one repository.
func fake(t *testing.T) (*Client, *[]string) {
	t.Helper()
	var calls []string
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/repos/o/r/pulls", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			write(w, map[string]string{"message": "Bad credentials"})
			return
		}
		if r.Method == "POST" {
			var n NewPR
			json.NewDecoder(r.Body).Decode(&n)
			if n.Head == "" {
				w.WriteHeader(422)
				write(w, map[string]any{"message": "Validation Failed", "errors": []map[string]string{{"message": "head is missing"}}})
				return
			}
			write(w, PR{Number: 7, Title: n.Title, State: "open", Draft: n.Draft})
			return
		}
		if r.URL.Query().Get("page") == "2" {
			write(w, []PR{{Number: 1, Title: "old", State: "open"}})
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/o/r/pulls?page=2>; rel="next"`, r.Host))
		write(w, []PR{{Number: 3, Title: "new", State: "open", User: User{Login: "ada"}}})
	})
	mux.HandleFunc("/repos/o/r/pulls/3/files", func(w http.ResponseWriter, r *http.Request) {
		write(w, []File{{Filename: "a.go", Status: "modified", Additions: 1, Deletions: 1, Patch: "@@ -1 +1 @@\n-x\n+y"}, {Filename: "new.go", Status: "added", Patch: "@@ -0,0 +1 @@\n+z"}, {Filename: "bin.png", Status: "added"}})
	})
	mux.HandleFunc("/repos/o/r/issues/3/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			write(w, Comment{ID: 9, Body: b["body"], User: User{Login: "me"}})
			return
		}
		write(w, []Comment{{ID: 1, Body: "first", CreatedAt: mustTime("2026-01-02T00:00:00Z")}})
	})
	mux.HandleFunc("/repos/o/r/pulls/3/reviews", func(w http.ResponseWriter, r *http.Request) {
		write(w, []map[string]any{
			{"id": 2, "state": "APPROVED", "body": "", "submitted_at": "2026-01-01T00:00:00Z", "user": map[string]string{"login": "bob"}},
			{"id": 3, "state": "COMMENTED", "body": "", "submitted_at": "2026-01-03T00:00:00Z"},
			{"id": 4, "state": "PENDING", "body": "draft", "submitted_at": "2026-01-04T00:00:00Z"},
		})
	})
	mux.HandleFunc("/repos/o/r/pulls/3/merge", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		if b["merge_method"] == "rebase" {
			w.WriteHeader(405)
			write(w, map[string]string{"message": "Rebase merges are not allowed"})
			return
		}
		write(w, map[string]bool{"merged": true})
	})
	mux.HandleFunc("/repos/o/r/commits/abc/check-runs", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"check_runs": []map[string]string{{"name": "build", "status": "completed", "conclusion": "success"}, {"name": "lint", "status": "in_progress"}}})
	})
	mux.HandleFunc("/repos/o/r/commits/abc/status", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"statuses": []map[string]string{{"context": "ci/legacy", "state": "error"}}})
	})
	mux.HandleFunc("/repos/o/r/commits/abc", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"author": map[string]string{"login": "octo", "avatar_url": "https://a/octo"}})
	})
	mux.HandleFunc("/repos/o/r/commits/unlinked", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"author": nil})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, Token: "tok"}, &calls
}

func mustTime(s string) (t time.Time) {
	t.UnmarshalText([]byte(s))
	return
}

func asError(err error, target **Error) bool { return errors.As(err, target) }

func TestListFollowsPages(t *testing.T) {
	c, _ := fake(t)
	prs, err := c.PRs(context.Background(), Repo{Owner: "o", Name: "r"}, "open")
	if err != nil || len(prs) != 2 || prs[0].Number != 3 || prs[1].Number != 1 {
		t.Fatalf("prs = %+v, %v", prs, err)
	}
	if prs[0].StateLabel() != "Open" {
		t.Errorf("state = %s", prs[0].StateLabel())
	}
}

func TestErrorsCarryGitHubsMessage(t *testing.T) {
	c, _ := fake(t)
	c.Token = "wrong"
	_, err := c.PRs(context.Background(), Repo{Owner: "o", Name: "r"}, "open")
	var e *Error
	if err == nil || !asError(err, &e) || e.Status != 401 || !strings.Contains(err.Error(), "Bad credentials") {
		t.Fatalf("err = %v", err)
	}
	c.Token = "tok"
	_, err = c.Create(context.Background(), Repo{Owner: "o", Name: "r"}, NewPR{Title: "x"})
	if err == nil || !strings.Contains(err.Error(), "head is missing") {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateMergeComment(t *testing.T) {
	c, calls := fake(t)
	ctx, r := context.Background(), Repo{Owner: "o", Name: "r"}
	pr, err := c.Create(ctx, r, NewPR{Title: "Add things", Head: "topic", Base: "main", Draft: true})
	if err != nil || pr.Number != 7 || pr.StateLabel() != "Draft" {
		t.Fatalf("pr = %+v, %v", pr, err)
	}
	if err := c.Merge(ctx, r, 3, "squash", "Squashed", ""); err != nil {
		t.Fatal(err)
	}
	if err := c.Merge(ctx, r, 3, "rebase", "", ""); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("rebase merge: %v", err)
	}
	cm, err := c.AddComment(ctx, r, 3, "looks good")
	if err != nil || cm.Body != "looks good" {
		t.Fatalf("comment = %+v, %v", cm, err)
	}
	if len(*calls) == 0 {
		t.Fatal("no calls recorded")
	}
}

func TestFilesAndPatchHeader(t *testing.T) {
	c, _ := fake(t)
	files, err := c.Files(context.Background(), Repo{Owner: "o", Name: "r"}, 3)
	if err != nil || len(files) != 3 {
		t.Fatalf("files = %+v, %v", files, err)
	}
	want := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-x\n+y\n"
	if got := files[0].FilePatch(); got != want {
		t.Errorf("patch = %q", got)
	}
	if got := files[1].FilePatch(); !strings.Contains(got, "new file mode") || !strings.Contains(got, "--- /dev/null") {
		t.Errorf("added patch = %q", got)
	}
	if files[2].FilePatch() != "" {
		t.Error("a binary file has no patch")
	}
}

func TestConversationMergesReviewsAndComments(t *testing.T) {
	c, _ := fake(t)
	got, err := c.Conversation(context.Background(), Repo{Owner: "o", Name: "r"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	// The approval (Jan 1) comes before the comment (Jan 2); the empty
	// "commented" review and the pending one are left out.
	if len(got) != 2 || got[0].State != "APPROVED" || got[1].Body != "first" {
		t.Fatalf("conversation = %+v", got)
	}
}

func TestChecksSummary(t *testing.T) {
	c, _ := fake(t)
	checks, summary, err := c.Checks(context.Background(), Repo{Owner: "o", Name: "r"}, "abc")
	if err != nil || len(checks) != 3 || summary != "failure" {
		t.Fatalf("checks = %+v, summary %q, %v", checks, summary, err)
	}
}

func TestCommitAuthor(t *testing.T) {
	c, _ := fake(t)
	r := Repo{Owner: "o", Name: "r"}
	u, ok, err := c.CommitAuthor(context.Background(), r, "abc")
	if err != nil || !ok || u.Login != "octo" || u.AvatarURL != "https://a/octo" {
		t.Fatalf("author = %+v, %v, %v", u, ok, err)
	}
	if _, ok, err := c.CommitAuthor(context.Background(), r, "unlinked"); ok || err != nil {
		t.Fatalf("unlinked: ok = %v, err = %v", ok, err)
	}
}

func TestParseRef(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Repo
		n    int
		bad  bool
	}{
		{"o/r #12", Repo{"github.com", "o", "r"}, 12, false},
		{"o/r#12", Repo{"github.com", "o", "r"}, 12, false},
		{"  o/r 12 ", Repo{"github.com", "o", "r"}, 12, false},
		{"o/r/pull/12", Repo{"github.com", "o", "r"}, 12, false},
		{"o/r", Repo{"github.com", "o", "r"}, 0, false},
		{"o/r.git", Repo{"github.com", "o", "r"}, 0, false},
		{"https://github.com/o/r/pull/12/files", Repo{"github.com", "o", "r"}, 12, false},
		{"https://ghe.example.com/o/r/pull/3", Repo{"ghe.example.com", "o", "r"}, 3, false},
		{"https://github.com/o/r", Repo{"github.com", "o", "r"}, 0, false},
		{"o/r #x", Repo{}, 0, true},
		{"o/r #0", Repo{}, 0, true},
		{"o", Repo{}, 0, true},
		{"a/b/c", Repo{}, 0, true},
		{"", Repo{}, 0, true},
	} {
		got, n, err := ParseRef(tc.in)
		if (err != nil) != tc.bad || got != tc.want || n != tc.n {
			t.Errorf("ParseRef(%q) = %+v, %d, %v; want %+v, %d, bad=%v", tc.in, got, n, err, tc.want, tc.n, tc.bad)
		}
	}
}

func TestInvolvedFindsPullRequestsOfAnyRepository(t *testing.T) {
	var queries []string
	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(User{Login: "me"})
	})
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Variables struct {
				Q string `json:"q"`
			} `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		queries = append(queries, body.Variables.Q)
		node := func(repo string, n int, title string) map[string]any {
			return map[string]any{
				"number": n, "title": title, "state": "OPEN", "isDraft": false, "reviewDecision": "APPROVED", "mergeable": "CONFLICTING",
				"author": map[string]any{"login": "ada"}, "repository": map[string]any{"nameWithOwner": repo},
				"commits": map[string]any{"totalCount": 1, "nodes": []map[string]any{{"commit": map[string]any{"statusCheckRollup": map[string]any{"state": "SUCCESS"}}}}},
			}
		}
		var nodes []map[string]any
		switch {
		case strings.Contains(body.Variables.Q, "review-requested:me"):
			nodes = []map[string]any{node("acme/api", 5, "Asked"), node("x/y", 2, "Both")}
		case strings.Contains(body.Variables.Q, "involves:me"):
			nodes = []map[string]any{node("x/y", 2, "Both")}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"search": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": false}, "nodes": nodes,
		}}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	cl := &Client{BaseURL: srv.URL, Token: "tok"}

	viewer, found, err := cl.Involved(context.Background(), "github.com", "open")
	if err != nil {
		t.Fatal(err)
	}
	if viewer.Login != "me" || len(found) != 2 {
		t.Fatalf("viewer %q, found %+v", viewer.Login, found)
	}
	byTitle := map[string]Found{}
	for _, f := range found {
		byTitle[f.PR.Title] = f
	}
	asked := byTitle["Asked"]
	if asked.Repo != (Repo{Host: "github.com", Owner: "acme", Name: "api"}) || asked.PR.Number != 5 || asked.PR.User.Login != "ada" {
		t.Errorf("asked %+v", asked)
	}
	// A request that does not name the account (a team's) still lists it.
	if len(asked.PR.RequestedReviewers) != 1 || asked.PR.RequestedReviewers[0].Login != "me" {
		t.Errorf("reviewers %+v", asked.PR.RequestedReviewers)
	}
	if asked.Status.Review != "approved" || asked.Status.Checks != "passing" || !asked.Status.Conflicting {
		t.Errorf("status %+v", asked.Status)
	}
	if len(queries) != 3 || !strings.Contains(queries[0], "is:open") {
		t.Errorf("queries %q", queries)
	}
}
