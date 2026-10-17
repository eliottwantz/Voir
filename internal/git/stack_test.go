package git

import (
	"context"
	"errors"
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

func branchesOf(names ...string) []StackBranch {
	bs := make([]StackBranch, len(names))
	for i, n := range names {
		bs[i] = StackBranch{Name: n}
	}
	return bs
}

func logOf(t *testing.T, r *Repo, rev string) string {
	t.Helper()
	out, err := r.Read("log", "--pretty=%s", rev)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(strings.Split(strings.TrimSpace(string(out)), "\n"), "|")
}

func TestRebaseStack(t *testing.T) {
	r, _ := stackRepo(t)
	r.Do("checkout", "-q", "one")
	headBefore, _ := r.Resolve("HEAD")
	pushed, err := r.RebaseStack(context.Background(), "origin", "main", "", branchesOf("one", "two"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(pushed, ",") != "one,two" {
		t.Fatalf("pushed = %v", pushed)
	}
	// This repository's own branch is where it was.
	if got := r.text("symbolic-ref", "--short", "HEAD"); got != "one" {
		t.Fatalf("the branch checked out is %q", got)
	}
	if now, _ := r.Resolve("HEAD"); now != headBefore {
		t.Fatal("the branch checked out moved")
	}
	if got := logOf(t, r, "origin/two"); got != "add two|add one|main moves on|base" {
		t.Fatalf("origin/two = %s", got)
	}
	// Again, there is nothing to do.
	if pushed, err = r.RebaseStack(context.Background(), "origin", "main", "", branchesOf("one", "two")); err != nil || len(pushed) != 0 {
		t.Fatalf("again: pushed %v, %v", pushed, err)
	}
}

func TestRebaseStackLeavesTheWorkTreeAlone(t *testing.T) {
	r, _ := stackRepo(t)
	write(t, r, "base.txt", "dirty\n")
	if _, err := r.RebaseStack(context.Background(), "origin", "main", "", branchesOf("one", "two")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(r.Root, "base.txt")); string(b) != "dirty\n" {
		t.Fatalf("base.txt = %q", b)
	}
}

func TestRebaseStackReplaysOnlyTheFirstBranchesOwnCommits(t *testing.T) {
	r, _ := stackRepo(t)
	oneHead, _ := r.Resolve("one")
	// "one" is squash-merged into main, as a commit that is not its own.
	r.Do("checkout", "-q", "main")
	write(t, r, "one.txt", "one\n")
	commit(t, r, "squash one")
	r.Do("push", "-q", "origin", "main")
	pushed, err := r.RebaseStack(context.Background(), "origin", "main", oneHead, branchesOf("two"))
	if err != nil || len(pushed) != 1 {
		t.Fatalf("pushed %v, %v", pushed, err)
	}
	if got := logOf(t, r, "origin/two"); got != "add two|squash one|main moves on|base" {
		t.Fatalf("origin/two = %s", got)
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
	_, err := r.RebaseStack(context.Background(), "origin", "main", "", branchesOf("one", "two"))
	var conflict *StackConflictError
	if !errors.As(err, &conflict) || conflict.Branch != "two" || conflict.Below != "one" || conflict.Done != 1 {
		t.Fatalf("err = %v", err)
	}
	r.Fetch(context.Background(), "origin", false, false)
	if after, _ := r.Resolve("origin/two"); after != before {
		t.Fatal("the remote changed")
	}
	if got := r.text("symbolic-ref", "--short", "HEAD"); got != "main" {
		t.Fatalf("the branch checked out is %q", got)
	}
}

func TestRebaseStackRefusesABranchThatMoved(t *testing.T) {
	r, _ := stackRepo(t)
	bs := []StackBranch{{Name: "one", Head: "0000000000000000000000000000000000000000"}}
	if _, err := r.RebaseStack(context.Background(), "origin", "main", "", bs); err == nil || !strings.Contains(err.Error(), "moved") {
		t.Fatalf("err = %v", err)
	}
	before, _ := r.Resolve("origin/one")
	if got, _ := r.Resolve("one"); got != before {
		t.Fatal("a branch changed")
	}
}
