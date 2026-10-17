package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stackRepo is a repository with a remote that holds main and a stack of two
// branches, one and two, built on an older main; main has since moved on.
func stackRepo(t *testing.T) (r *Repo, remote string) {
	t.Helper()
	r = newRepo(t)
	remote = filepath.Join(t.TempDir(), "remote.git")
	if _, err := Init(remote); err != nil {
		t.Fatal(err)
	}
	if out, err := runEnv(context.Background(), remote, nil, false, nil, "config", "receive.denyCurrentBranch", "ignore"); err != nil {
		t.Fatal(string(out), err)
	}
	write(t, r, "base.txt", "base\n")
	commit(t, r, "base")
	r.Do("branch", "-M", "main")
	r.Do("remote", "add", "origin", remote)
	for _, b := range []string{"one", "two"} {
		r.Do("checkout", "-q", "-b", b)
		write(t, r, b+".txt", b+"\n")
		commit(t, r, "add "+b)
	}
	r.Do("checkout", "-q", "main")
	write(t, r, "main.txt", "moved\n")
	commit(t, r, "main moves on")
	if _, err := r.Do("push", "-q", "origin", "main", "one", "two"); err != nil {
		t.Fatal(err)
	}
	return r, remote
}

func TestRebaseStack(t *testing.T) {
	r, _ := stackRepo(t)
	r.Do("checkout", "-q", "one")
	pushed, err := r.RebaseStack(context.Background(), "origin", "main", "", []string{"one", "two"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(pushed, ",") != "one,two" {
		t.Fatalf("pushed = %v", pushed)
	}
	if got := r.text("symbolic-ref", "--short", "HEAD"); got != "one" {
		t.Fatalf("the branch checked out is %q", got)
	}
	r.Fetch(context.Background(), "origin", false, false)
	log, _ := r.Read("log", "--pretty=%s", "origin/two")
	if got := strings.Join(strings.Fields(strings.ReplaceAll(string(log), " ", "_")), "|"); got != "add_two|add_one|main_moves_on|base" {
		t.Fatalf("origin/two = %s", got)
	}
	// Again, there is nothing to do.
	if pushed, err = r.RebaseStack(context.Background(), "origin", "main", "", []string{"one", "two"}); err != nil || len(pushed) != 0 {
		t.Fatalf("again: pushed %v, %v", pushed, err)
	}
}

func TestRebaseStackConflictChangesNothing(t *testing.T) {
	r, _ := stackRepo(t)
	// main and two both write the same file.
	r.Do("checkout", "-q", "two")
	write(t, r, "clash.txt", "two\n")
	commit(t, r, "two clashes")
	r.Do("push", "-q", "origin", "two")
	r.Do("checkout", "-q", "main")
	write(t, r, "clash.txt", "main\n")
	commit(t, r, "main clashes")
	r.Do("push", "-q", "origin", "main")
	before, _ := r.Resolve("origin/two")
	if _, err := r.RebaseStack(context.Background(), "origin", "main", "", []string{"one", "two"}); err == nil {
		t.Fatal("no conflict")
	}
	r.Fetch(context.Background(), "origin", false, false)
	if after, _ := r.Resolve("origin/two"); after != before {
		t.Fatal("the remote changed")
	}
	if got := r.text("symbolic-ref", "--short", "HEAD"); got != "main" {
		t.Fatalf("the branch checked out is %q", got)
	}
	if _, err := os.Stat(filepath.Join(r.Root, ".git", "rebase-merge")); err == nil {
		t.Fatal("a rebase is left going")
	}
}

func TestRebaseStackRefusesChanges(t *testing.T) {
	r, _ := stackRepo(t)
	write(t, r, "base.txt", "dirty\n")
	if _, err := r.RebaseStack(context.Background(), "origin", "main", "", []string{"one"}); err == nil {
		t.Fatal("rebased over changes")
	}
}
