package git

import (
	"context"
	"fmt"
	"strings"
)

// RebaseStack rebases the branches of a stack of pull requests, from the one
// nearest the base branch: the first onto the remote's base, each other onto
// the one before. firstParent is the commit the first branch was built on, so
// that only its own commits are replayed (the pull request under it may have
// been merged); "" replays everything the base lacks. The branches are read
// from the remote, whatever is checked out of them here, and nothing is pushed
// until all of them rebased: a conflict leaves the repository and the remote
// as they were. It returns the branches it pushed with a lease on their old
// tips; those already in place are left alone.
func (r *Repo) RebaseStack(ctx context.Context, remote, base, firstParent string, branches []string) ([]string, error) {
	if out, err := r.Read("status", "--porcelain", "--untracked-files=no"); err != nil {
		return nil, err
	} else if strings.TrimSpace(string(out)) != "" {
		return nil, fmt.Errorf("commit or stash your changes first: the rebase checks the branches out")
	}
	if err := r.Fetch(ctx, remote, false, false); err != nil {
		return nil, err
	}
	home := r.text("symbolic-ref", "-q", "--short", "HEAD")
	if home == "" {
		home = r.text("rev-parse", "HEAD")
	}
	defer r.Do("checkout", "-q", home)

	onto, err := r.Resolve(remote + "/" + base)
	if err != nil {
		return nil, err
	}
	parent := firstParent
	olds, news := make([]string, len(branches)), make([]string, len(branches))
	for i, b := range branches {
		if olds[i], err = r.Resolve(remote + "/" + b); err != nil {
			return nil, err
		}
		if _, err = r.DoContext(ctx, "checkout", "-q", "--detach", olds[i]); err != nil {
			return nil, err
		}
		args := []string{"rebase", onto}
		if parent != "" {
			args = []string{"rebase", "--onto", onto, parent}
		}
		if _, err = r.DoContext(ctx, args...); err != nil {
			r.Do("rebase", "--abort")
			return nil, fmt.Errorf("could not rebase %s: %w", b, err)
		}
		if news[i], err = r.Resolve("HEAD"); err != nil {
			return nil, err
		}
		onto, parent = news[i], olds[i]
	}
	var pushed []string
	for i, b := range branches {
		if news[i] == olds[i] {
			continue
		}
		lease := fmt.Sprintf("--force-with-lease=refs/heads/%s:%s", b, olds[i])
		if _, err := r.DoContext(ctx, "push", lease, remote, news[i]+":refs/heads/"+b); err != nil {
			return pushed, fmt.Errorf("could not push %s: %w", b, err)
		}
		pushed = append(pushed, b)
	}
	return pushed, nil
}
