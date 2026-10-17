package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stackHost is a GitHub that answers what stacks ask, and records the calls
// that carry a body.
func stackHost(t *testing.T, handle func(w http.ResponseWriter, r *http.Request, body string)) (*Client, Repo) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		handle(w, r, string(b))
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, Token: "tok"}, Repo{Host: "github.com", Owner: "o", Name: "r"}
}

func TestPRStacksAreAQueryOfTheirOwn(t *testing.T) {
	var queries []string
	c, r := stackHost(t, func(w http.ResponseWriter, req *http.Request, body string) {
		queries = append(queries, body)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequests": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": false},
			"nodes": []map[string]any{
				{"number": 3, "stackEntry": map[string]any{"position": 2, "stack": map[string]any{"size": 3}}},
				{"number": 4, "stackEntry": nil},
				{"number": 5, "stackEntry": map[string]any{"position": 1, "stack": map[string]any{"size": 1}}},
			},
		}}}})
	})
	got, err := c.PRStacks(context.Background(), r, "all")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[3] != (StackPlace{Position: 2, Size: 3}) {
		t.Fatalf("stacks = %v", got)
	}
	if strings.Contains(prStatusesQuery, "stackEntry") || !strings.Contains(queries[0], "stackEntry") {
		t.Fatal("the stack is in the query of the statuses, or not in its own")
	}
}

func TestAHostWithoutStacks(t *testing.T) {
	c, r := stackHost(t, func(w http.ResponseWriter, req *http.Request, body string) {
		if strings.HasSuffix(req.URL.Path, "/graphql") {
			json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"message": "Field 'stackEntry' doesn't exist on type 'PullRequest'"}}})
			return
		}
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]string{"message": "Not Found"})
	})
	ctx := context.Background()
	if _, err := c.PRStacks(ctx, r, "open"); !errors.Is(err, ErrStacksUnsupported) {
		t.Errorf("PRStacks: %v", err)
	}
	if _, err := c.Stack(ctx, r, 7); !errors.Is(err, ErrStacksUnsupported) {
		t.Errorf("Stack: %v", err)
	}
	if _, _, err := c.StackOf(ctx, r, 3); !errors.Is(err, ErrStacksUnsupported) {
		t.Errorf("StackOf: %v", err)
	}
	// Another failure is not taken for it.
	if err := stacksUnsupported(errors.New("connection reset")); errors.Is(err, ErrStacksUnsupported) {
		t.Errorf("a network error is taken for a host without stacks: %v", err)
	}
}

func TestMergeAStack(t *testing.T) {
	var gotBody, gotPath string
	c, r := stackHost(t, func(w http.ResponseWriter, req *http.Request, body string) {
		gotBody, gotPath = body, req.Method+" "+req.URL.Path
		json.NewEncoder(w).Encode(map[string]any{"status": "pending", "details": map[string]string{"uuid": "u 1"}})
	})
	res, err := c.MergeStack(context.Background(), r, 3, "squash", "abc")
	if err != nil || res.Status != "pending" || res.Details.UUID != "u 1" {
		t.Fatalf("%+v, %v", res, err)
	}
	var b map[string]string
	json.Unmarshal([]byte(gotBody), &b)
	if gotPath != "PUT /repos/o/r/pulls/3/merge-async" || b["sha"] != "abc" || b["merge_method"] != "squash" || b["merge_action"] != "default" {
		t.Fatalf("%s %q", gotPath, gotBody)
	}
	if _, err := c.MergeStack(context.Background(), r, 3, "merge", ""); err != nil || strings.Contains(gotBody, "sha") {
		t.Fatalf("a merge without a head sends one: %q, %v", gotBody, err)
	}
	if _, err := c.MergeStackStatus(context.Background(), r, 3, "u 1"); err != nil || gotPath != "GET /repos/o/r/pulls/3/merge-async/u 1" {
		t.Fatalf("%s, %v", gotPath, err)
	}
}

func TestChangeAStack(t *testing.T) {
	var calls []string
	c, r := stackHost(t, func(w http.ResponseWriter, req *http.Request, body string) {
		calls = append(calls, req.Method+" "+req.URL.RequestURI()+" "+strings.TrimSpace(body))
		switch {
		case strings.HasSuffix(req.URL.Path, "/unstack"):
			w.WriteHeader(204)
		case req.Method == "GET":
			json.NewEncoder(w).Encode([]map[string]any{{"number": 7, "pull_requests": []map[string]any{{"number": 3}}}})
		default:
			json.NewEncoder(w).Encode(map[string]any{"number": 7, "pull_requests": []map[string]any{{"number": 3}, {"number": 4}}})
		}
	})
	ctx := context.Background()
	st, ok, err := c.StackOf(ctx, r, 3)
	if err != nil || !ok || st.Number != 7 {
		t.Fatalf("StackOf = %+v, %v, %v", st, ok, err)
	}
	if _, err := c.CreateStack(ctx, r, []int{3, 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddToStack(ctx, r, 7, []int{5}); err != nil {
		t.Fatal(err)
	}
	if n, err := c.Unstack(ctx, r, 7); err != nil || n != 0 {
		t.Fatalf("Unstack = %d, %v", n, err)
	}
	want := []string{
		"GET /repos/o/r/stacks?pull_request=3 ",
		`POST /repos/o/r/stacks {"pull_requests":[3,4]}`,
		`POST /repos/o/r/stacks/7/add {"pull_requests":[5]}`,
		"POST /repos/o/r/stacks/7/unstack ",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s", strings.Join(calls, "\n"))
	}
}

func TestStackOfAPullRequestInNoStack(t *testing.T) {
	c, r := stackHost(t, func(w http.ResponseWriter, req *http.Request, body string) { w.Write([]byte("[]")) })
	if _, ok, err := c.StackOf(context.Background(), r, 3); ok || err != nil {
		t.Fatalf("ok %v, %v", ok, err)
	}
}

func TestBranchAccess(t *testing.T) {
	c, r := stackHost(t, func(w http.ResponseWriter, req *http.Request, body string) {
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"repository": map[string]any{
			"pr0": map[string]any{"number": 3, "headRepository": map[string]string{"nameWithOwner": "o/r", "viewerPermission": "WRITE"}},
			"pr1": map[string]any{"number": 4, "headRepository": map[string]string{"nameWithOwner": "x/r", "viewerPermission": "READ"}},
			"pr2": map[string]any{"number": 5, "headRepository": nil},
			"pr3": nil,
		}}})
	})
	got, err := c.BranchAccess(context.Background(), r, []int{3, 4, 5, 6})
	if err != nil {
		t.Fatal(err)
	}
	if !got[3].CanPush() || got[3].HeadRepo != "o/r" || got[4].CanPush() || got[5].HeadRepo != "" || got[5].CanPush() {
		t.Fatalf("access = %+v", got)
	}
	if _, ok := got[6]; ok {
		t.Fatal("a pull request that is not there has access")
	}
	for perm, want := range map[string]bool{"ADMIN": true, "MAINTAIN": true, "WRITE": true, "TRIAGE": false, "READ": false, "": false} {
		if (BranchAccess{Permission: perm}).CanPush() != want {
			t.Errorf("%q can push: %v", perm, !want)
		}
	}
}
