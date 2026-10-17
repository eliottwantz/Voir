package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Stage adds paths to the index; no paths stages everything.
func (r *Repo) Stage(paths ...string) error {
	if len(paths) == 0 {
		_, err := r.Do("add", "-A")
		return err
	}
	_, err := r.Do(append([]string{"add", "-A", "--"}, paths...)...)
	return err
}

// Unstage takes paths out of the index; none unstages everything.
func (r *Repo) Unstage(paths ...string) error {
	var args []string
	if r.HasHead() {
		args = []string{"reset", "-q", "HEAD", "--"}
	} else {
		args = []string{"rm", "-r", "-q", "--cached", "--"}
		if len(paths) == 0 {
			paths = []string{"."}
		}
	}
	_, err := r.Do(append(args, paths...)...)
	return err
}

// Discard throws away the changes of tracked paths in the work tree.
func (r *Repo) Discard(paths ...string) error {
	_, err := r.Do(append([]string{"checkout", "--"}, paths...)...)
	return err
}

// DeleteUntracked deletes untracked files.
func (r *Repo) DeleteUntracked(paths ...string) error {
	for _, p := range paths {
		if err := os.RemoveAll(filepath.Join(r.Root, p)); err != nil {
			return err
		}
	}
	return nil
}

// CommitOptions say how to commit what is staged.
type CommitOptions struct {
	Message string
	Amend   bool
	SignOff bool
}

// Commit commits the index.
func (r *Repo) Commit(o CommitOptions) error {
	args := []string{"commit", "-F", "-"}
	if o.Amend {
		args = append(args, "--amend")
	}
	if o.SignOff {
		args = append(args, "--signoff")
	}
	_, err := r.DoInput(o.Message, args...)
	return err
}

// LastMessage is the message of HEAD, for amending.
func (r *Repo) LastMessage() string {
	out, err := r.Read("log", "-1", "--pretty=format:%B")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Fetch fetches a remote, or every remote for "".
func (r *Repo) Fetch(ctx context.Context, remote string, tags, prune bool) error {
	args := []string{"fetch"}
	if remote == "" {
		args = append(args, "--all")
	} else {
		args = append(args, remote)
	}
	if tags {
		args = append(args, "--tags")
	}
	if prune {
		args = append(args, "--prune")
	}
	_, err := r.DoContext(ctx, args...)
	return err
}

// Pull pulls a branch of a remote into the branch checked out; with an
// empty remote it pulls what the branch tracks.
func (r *Repo) Pull(ctx context.Context, remote, branch string, rebase bool) error {
	args := []string{"pull"}
	if rebase {
		args = append(args, "--rebase")
	} else {
		args = append(args, "--no-rebase")
	}
	if remote != "" {
		args = append(args, remote)
		if branch != "" {
			args = append(args, branch)
		}
	}
	_, err := r.DoContext(ctx, args...)
	return err
}

// PushOptions say what to push where.
type PushOptions struct {
	Remote string
	// Local is the branch to push; Remote branch is the name it takes there
	// ("" is the same).
	Local, RemoteBranch string
	SetUpstream         bool
	Force               bool
	Tags                bool
}

// Push pushes a branch.
func (r *Repo) Push(ctx context.Context, o PushOptions) error {
	args := []string{"push"}
	if o.SetUpstream {
		args = append(args, "-u")
	}
	if o.Force {
		args = append(args, "--force-with-lease")
	}
	if o.Tags {
		args = append(args, "--tags")
	}
	if o.Remote != "" {
		args = append(args, o.Remote)
	}
	if o.Local != "" {
		spec := o.Local
		if o.RemoteBranch != "" && o.RemoteBranch != o.Local {
			spec += ":" + o.RemoteBranch
		}
		args = append(args, spec)
	}
	_, err := r.DoContext(ctx, args...)
	return err
}

// PushTag pushes one tag.
func (r *Repo) PushTag(ctx context.Context, remote, tag string) error {
	_, err := r.DoContext(ctx, "push", remote, "refs/tags/"+tag)
	return err
}

// LocalChanges says what a checkout does to uncommitted changes.
type LocalChanges uint8

const (
	KeepChanges    LocalChanges = iota // checkout, and fail if they conflict
	StashChanges                       // stash them, checkout, and apply them again
	DiscardChanges                     // checkout --force
)

// Checkout switches to a branch, tag or commit.
func (r *Repo) Checkout(rev string, lc LocalChanges) error {
	switch lc {
	case StashChanges:
		if _, err := r.Do("stash", "push", "--include-untracked", "-m", "Fork: before checkout of "+rev); err != nil {
			return err
		}
		if _, err := r.Do("checkout", rev); err != nil {
			r.Do("stash", "pop", "--index")
			return err
		}
		_, err := r.Do("stash", "pop", "--index")
		return err
	case DiscardChanges:
		_, err := r.Do("checkout", "--force", rev)
		return err
	}
	_, err := r.Do("checkout", rev)
	return err
}

// CheckoutRemote creates a local branch tracking a remote one and checks it
// out.
func (r *Repo) CheckoutRemote(remoteBranch, local string) error {
	_, err := r.Do("checkout", "-b", local, "--track", remoteBranch)
	return err
}

// CreateBranch creates a branch at a revision, and checks it out if asked.
func (r *Repo) CreateBranch(name, at string, checkout bool, lc LocalChanges) error {
	if checkout && lc == KeepChanges {
		_, err := r.Do("checkout", "-b", name, at)
		return err
	}
	if _, err := r.Do("branch", name, at); err != nil {
		return err
	}
	if checkout {
		return r.Checkout(name, lc)
	}
	return nil
}

// RenameBranch renames a local branch.
func (r *Repo) RenameBranch(old, name string) error {
	_, err := r.Do("branch", "-m", old, name)
	return err
}

// DeleteBranch deletes a local branch.
func (r *Repo) DeleteBranch(name string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := r.Do("branch", flag, name)
	return err
}

// DeleteRemoteBranch deletes a branch on its remote.
func (r *Repo) DeleteRemoteBranch(ctx context.Context, remote, branch string) error {
	_, err := r.DoContext(ctx, "push", remote, "--delete", branch)
	return err
}

// SetUpstream makes a local branch track a remote one; "" stops tracking.
func (r *Repo) SetUpstream(branch, upstream string) error {
	if upstream == "" {
		_, err := r.Do("branch", "--unset-upstream", branch)
		return err
	}
	_, err := r.Do("branch", "--set-upstream-to="+upstream, branch)
	return err
}

// MergeOptions say how to merge.
type MergeOptions struct {
	NoFastForward   bool
	FastForwardOnly bool
	Squash          bool
}

// Merge merges a branch or commit into the branch checked out.
func (r *Repo) Merge(rev string, o MergeOptions) error {
	args := []string{"merge"}
	switch {
	case o.Squash:
		args = append(args, "--squash")
	case o.FastForwardOnly:
		args = append(args, "--ff-only")
	case o.NoFastForward:
		args = append(args, "--no-ff")
	}
	_, err := r.Do(append(args, rev)...)
	return err
}

// Rebase rebases the branch checked out onto a revision.
func (r *Repo) Rebase(onto string) error {
	_, err := r.Do("rebase", onto)
	return err
}

// CherryPick applies commits to the branch checked out.
func (r *Repo) CherryPick(hashes ...string) error {
	_, err := r.Do(append([]string{"cherry-pick"}, hashes...)...)
	return err
}

// Revert creates a commit undoing another.
func (r *Repo) Revert(hash string) error {
	args := []string{"revert", "--no-edit"}
	if c, err := r.Show(hash); err == nil && c.IsMerge() {
		args = append(args, "-m", "1")
	}
	_, err := r.Do(append(args, hash)...)
	return err
}

// ResetMode is how a reset treats the index and the work tree.
type ResetMode string

const (
	ResetSoft  ResetMode = "--soft"
	ResetMixed ResetMode = "--mixed"
	ResetHard  ResetMode = "--hard"
)

// Reset moves the branch checked out to a revision.
func (r *Repo) Reset(rev string, mode ResetMode) error {
	_, err := r.Do("reset", string(mode), rev)
	return err
}

// Continue carries on an operation stopped by conflicts that are resolved.
func (r *Repo) Continue() error {
	switch r.State() {
	case "rebase":
		_, err := r.Do("-c", "core.editor=true", "rebase", "--continue")
		return err
	case "cherry-pick":
		_, err := r.Do("-c", "core.editor=true", "cherry-pick", "--continue")
		return err
	case "revert":
		_, err := r.Do("-c", "core.editor=true", "revert", "--continue")
		return err
	case "merge":
		_, err := r.Do("commit", "--no-edit")
		return err
	}
	return fmt.Errorf("nothing to continue")
}

// Abort abandons an operation stopped by conflicts.
func (r *Repo) Abort() error {
	var args []string
	switch r.State() {
	case "rebase":
		args = []string{"rebase", "--abort"}
	case "cherry-pick":
		args = []string{"cherry-pick", "--abort"}
	case "revert":
		args = []string{"revert", "--abort"}
	case "merge":
		args = []string{"merge", "--abort"}
	default:
		return fmt.Errorf("nothing to abort")
	}
	_, err := r.Do(args...)
	return err
}

// ResolveConflict takes our side ("ours") or theirs of a conflicted file and
// stages it.
func (r *Repo) ResolveConflict(path string, side string) error {
	if _, err := r.Do("checkout", "--"+side, "--", path); err != nil {
		return err
	}
	_, err := r.Do("add", "--", path)
	return err
}

// CreateTag tags a revision; a message makes the tag annotated.
func (r *Repo) CreateTag(name, at, message string) error {
	args := []string{"tag"}
	if message != "" {
		args = append(args, "-a", "-m", message)
	}
	args = append(args, name)
	if at != "" {
		args = append(args, at)
	}
	_, err := r.Do(args...)
	return err
}

// DeleteTag deletes a tag.
func (r *Repo) DeleteTag(name string) error {
	_, err := r.Do("tag", "-d", name)
	return err
}

// StashPush saves local changes to a new stash.
func (r *Repo) StashPush(message string, untracked bool) error {
	args := []string{"stash", "push"}
	if untracked {
		args = append(args, "--include-untracked")
	}
	if message != "" {
		args = append(args, "-m", message)
	}
	_, err := r.Do(args...)
	return err
}

// StashApply applies a stash and keeps it.
func (r *Repo) StashApply(name string) error {
	_, err := r.Do("stash", "apply", "--index", name)
	return err
}

// StashPop applies a stash and drops it.
func (r *Repo) StashPop(name string) error {
	_, err := r.Do("stash", "pop", "--index", name)
	return err
}

// StashDrop deletes a stash.
func (r *Repo) StashDrop(name string) error {
	_, err := r.Do("stash", "drop", name)
	return err
}

// StashChanges lists the files of a stash.
func (r *Repo) StashChangesList(name string) ([]Change, error) {
	return r.ChangesBetween(name+"^1", name)
}

// AddRemote adds a remote.
func (r *Repo) AddRemote(name, url string) error {
	_, err := r.Do("remote", "add", name, url)
	return err
}

// SetRemoteURL changes where a remote is.
func (r *Repo) SetRemoteURL(name, url string) error {
	_, err := r.Do("remote", "set-url", name, url)
	return err
}

// RemoveRemote removes a remote.
func (r *Repo) RemoveRemote(name string) error {
	_, err := r.Do("remote", "remove", name)
	return err
}

// AddWorktree creates a work tree for a branch.
func (r *Repo) AddWorktree(path, branch string, create bool) error {
	args := []string{"worktree", "add"}
	if create {
		args = append(args, "-b", branch, path)
	} else {
		args = append(args, path, branch)
	}
	_, err := r.Do(args...)
	return err
}

// RemoveWorktree removes a linked work tree.
func (r *Repo) RemoveWorktree(path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	_, err := r.Do(append(args, path)...)
	return err
}

// AddToGitignore appends a pattern to the repository's .gitignore.
func (r *Repo) AddToGitignore(pattern string) error {
	path := filepath.Join(r.Root, ".gitignore")
	existing, _ := os.ReadFile(path)
	text := string(existing)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return os.WriteFile(path, []byte(text+pattern+"\n"), 0o644)
}

// ApplyPatchFile applies a patch file to the work tree.
func (r *Repo) ApplyPatchFile(path string) error {
	_, err := r.Do("apply", "--whitespace=nowarn", path)
	return err
}

// SavePatch writes the changes of a commit as a patch file.
func (r *Repo) SavePatch(hash, dest string) error {
	out, err := r.Read("format-patch", "-1", "--stdout", hash)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, out, 0o644)
}
