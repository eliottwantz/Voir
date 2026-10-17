package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// StackBranch is a branch of a stack of pull requests.
type StackBranch struct {
	Name string
	// Head is the commit the branch is expected to be at on the remote, which
	// the rebase refuses to go on without; "" takes it as it is.
	Head string
}

// StackConflictError says a branch of a stack conflicts with the one below it.
type StackConflictError struct {
	Branch string
	Below  string
	// Done is how many branches rebased before it; none was pushed.
	Done int
}

func (e *StackConflictError) Error() string {
	return fmt.Sprintf("%s conflicts with %s: resolve it in a checkout of the branch. Nothing was pushed.", e.Branch, e.Below)
}

// RebaseStack rebases the branches of a stack of pull requests, from the one
// nearest the base branch: the first onto the remote's base, each other onto
// the one before. firstParent is the commit the first branch was built on, so
// that only its own commits are replayed (the pull request under it may have
// been merged); "" replays everything the base lacks.
//
// It works in a repository of its own in a temporary directory, which borrows
// this one's objects, so no branch or file of this repository changes, and
// nothing in it has to be clean. The branches are read from the remote, and
// nothing is pushed until all of them rebased: a conflict leaves the remote as
// it was. It returns the branches it pushed with a lease on their old tips;
// those already in place are left alone.
func (r *Repo) RebaseStack(ctx context.Context, remote, base, firstParent string, branches []StackBranch) ([]string, error) {
	url := r.text("remote", "get-url", remote)
	if url == "" {
		return nil, fmt.Errorf("no remote named %q", remote)
	}
	dir, err := os.MkdirTemp("", "myfork-stack-rebase-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	w, err := Init(dir)
	if err != nil {
		return nil, err
	}
	if common := r.text("rev-parse", "--path-format=absolute", "--git-common-dir"); common != "" {
		// Best effort: without them the fetch just brings more.
		alt := filepath.Join(dir, ".git", "objects", "info")
		if os.MkdirAll(alt, 0o755) == nil {
			os.WriteFile(filepath.Join(alt, "alternates"), []byte(filepath.Join(common, "objects")+"\n"), 0o644)
		}
	}
	name, email := r.Config("user.name"), r.Config("user.email")
	if name == "" {
		name = "MyFork"
	}
	if email == "" {
		email = "noreply@myfork.invalid"
	}
	for _, kv := range [][2]string{{"user.name", name}, {"user.email", email}, {"commit.gpgsign", "false"}, {"rebase.autoStash", "false"}} {
		if err := w.SetConfig(kv[0], kv[1]); err != nil {
			return nil, err
		}
	}

	specs := []string{"+refs/heads/" + base + ":refs/remotes/origin/" + base}
	for _, b := range branches {
		specs = append(specs, "+refs/heads/"+b.Name+":refs/remotes/origin/"+b.Name)
	}
	if _, err := w.DoContext(ctx, append([]string{"fetch", "--quiet", "--no-tags", url}, specs...)...); err != nil {
		return nil, err
	}
	onto, err := w.Resolve("origin/" + base)
	if err != nil {
		return nil, err
	}
	parent, below := firstParent, base
	olds, news := make([]string, len(branches)), make([]string, len(branches))
	for i, b := range branches {
		if olds[i], err = w.Resolve("origin/" + b.Name); err != nil {
			return nil, err
		}
		if b.Head != "" && olds[i] != b.Head {
			return nil, fmt.Errorf("%s moved on the remote since the stack was read: read it again", b.Name)
		}
		if _, err = w.DoContext(ctx, "checkout", "-q", "--detach", olds[i]); err != nil {
			return nil, err
		}
		args := []string{"rebase", onto}
		if parent != "" {
			args = []string{"rebase", "--onto", onto, parent}
		}
		if _, err = w.DoContext(ctx, args...); err != nil {
			// A rebase that stopped for a conflict is left going.
			stopped := false
			for _, d := range []string{"rebase-merge", "rebase-apply"} {
				if _, statErr := os.Stat(filepath.Join(dir, ".git", d)); statErr == nil {
					stopped = true
				}
			}
			w.Do("rebase", "--abort")
			if stopped {
				return nil, &StackConflictError{Branch: b.Name, Below: below, Done: i}
			}
			return nil, fmt.Errorf("could not rebase %s: %w", b.Name, err)
		}
		if news[i], err = w.Resolve("HEAD"); err != nil {
			return nil, err
		}
		onto, parent, below = news[i], olds[i], b.Name
	}
	var pushed []string
	for i, b := range branches {
		if news[i] == olds[i] {
			continue
		}
		lease := fmt.Sprintf("--force-with-lease=refs/heads/%s:%s", b.Name, olds[i])
		if _, err := w.DoContext(ctx, "push", "--quiet", lease, url, news[i]+":refs/heads/"+b.Name); err != nil {
			return pushed, fmt.Errorf("could not push %s: %w", b.Name, err)
		}
		pushed = append(pushed, b.Name)
	}
	// So that this repository sees the branches as they are now.
	if len(pushed) > 0 {
		r.Fetch(ctx, remote, false, false)
	}
	return pushed, nil
}
