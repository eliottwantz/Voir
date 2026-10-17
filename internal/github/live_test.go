package github

import (
	"context"
	"os"
	"testing"
)

// TestLive reads a public repository with the GitHub CLI's token. It only
// reads, and runs only with MYFORK_LIVE=1.
func TestLive(t *testing.T) {
	if os.Getenv("MYFORK_LIVE") == "" {
		t.Skip("set MYFORK_LIVE=1 to talk to GitHub")
	}
	ctx := context.Background()
	c, err := NewClient(ctx, "github.com")
	if err != nil {
		t.Fatal(err)
	}
	if u, err := c.Viewer(ctx); err != nil || u.Login == "" {
		t.Fatalf("viewer = %+v, %v", u, err)
	} else {
		t.Logf("signed in as %s", u.Login)
	}
	repo := Repo{Host: "github.com", Owner: "egoist", Name: "mygo"}
	branch, err := c.DefaultBranch(ctx, repo)
	if err != nil || branch == "" {
		t.Fatalf("default branch %q, %v", branch, err)
	}
	t.Logf("default branch: %s", branch)
	prs, err := c.PRs(ctx, repo, "all")
	if err != nil || len(prs) == 0 {
		t.Fatalf("prs = %d, %v", len(prs), err)
	}
	n := prs[0].Number
	pr, err := c.PR(ctx, repo, n)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("#%d %q by %s: %s, %d commits, %d files, +%d -%d, mergeable=%v state=%q head=%s base=%s",
		pr.Number, pr.Title, pr.User.Login, pr.StateLabel(), pr.Commits, pr.ChangedFiles, pr.Additions, pr.Deletions, pr.Mergeable, pr.MergeableState, pr.Head.Ref, pr.Base.Ref)
	if pr.Title == "" || pr.Head.SHA == "" || pr.Base.Ref == "" || pr.User.Login == "" || pr.CreatedAt.IsZero() {
		t.Fatalf("missing fields: %+v", pr)
	}
	files, err := c.Files(ctx, repo, n)
	if err != nil || len(files) == 0 {
		t.Fatalf("files = %d, %v", len(files), err)
	}
	patches := 0
	for _, f := range files {
		if f.FilePatch() != "" {
			patches++
		}
	}
	t.Logf("%d files, %d with a patch; first %s (%s)", len(files), patches, files[0].Filename, files[0].Status)
	commits, err := c.Commits(ctx, repo, n)
	if err != nil || len(commits) == 0 || commits[0].Subject() == "" {
		t.Fatalf("commits = %+v, %v", commits, err)
	}
	convo, err := c.Conversation(ctx, repo, n)
	if err != nil {
		t.Fatal(err)
	}
	checks, summary, err := c.Checks(ctx, repo, pr.Head.SHA)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d commits, %d conversation items, %d checks (%s)", len(commits), len(convo), len(checks), summary)
}
