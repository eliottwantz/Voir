package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TodoAction is what an interactive rebase does with a commit.
type TodoAction string

const (
	ActionPick   TodoAction = "pick"
	ActionReword TodoAction = "reword"
	ActionEdit   TodoAction = "edit"
	ActionSquash TodoAction = "squash"
	ActionFixup  TodoAction = "fixup"
	// ActionFixupMessage is "fixup -C": it melds the commit like a fixup, but
	// its message replaces the one of the commit below.
	ActionFixupMessage TodoAction = "fixup -C"
	ActionDrop         TodoAction = "drop"
)

// TodoItem is one line of the rebase's todo list.
type TodoItem struct {
	Action  TodoAction
	Hash    string
	Subject string
	// Message is the new message of a reword.
	Message string
}

// RebaseOptions say how an interactive rebase runs.
type RebaseOptions struct {
	// Backup makes a branch at the current state first, so that a rebase
	// that went wrong can be undone.
	Backup bool
	// Autostash stashes local changes and applies them again afterwards.
	Autostash bool
	// UpdateRefs moves the branches that point into the rebased commits.
	UpdateRefs bool
}

// RebaseCommits lists the commits an interactive rebase onto a revision
// replays, oldest first. Merge commits are left out, as git leaves them.
func (r *Repo) RebaseCommits(onto string) ([]Commit, error) {
	out, err := r.Read("log", logFormat, "--no-color", "--reverse", "--no-merges", "--topo-order", onto+"..HEAD")
	if err != nil {
		return nil, err
	}
	return parseLog(out), nil
}

// RebaseInteractive rebases the branch checked out onto a revision with the
// todo list given: the list git would open in an editor is written for it.
// A reword becomes a pick followed by an amend with the new message. A rebase
// that stops (for an edit, or for conflicts) leaves the repository in the
// middle of it: State reports "rebase", and Continue and Abort go on.
func (r *Repo) RebaseInteractive(onto string, items []TodoItem, o RebaseOptions) (backup string, err error) {
	dir, err := os.MkdirTemp("", "voir-rebase-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	var todo strings.Builder
	for i, it := range items {
		switch it.Action {
		case ActionReword:
			msg := filepath.Join(dir, fmt.Sprintf("message-%d", i))
			if err := os.WriteFile(msg, []byte(it.Message), 0o600); err != nil {
				return "", err
			}
			fmt.Fprintf(&todo, "pick %s\nexec git commit --amend --allow-empty -q -F %s\n", it.Hash, shellQuote(msg))
		case ActionPick, ActionEdit, ActionSquash, ActionFixup, ActionFixupMessage, ActionDrop:
			fmt.Fprintf(&todo, "%s %s\n", it.Action, it.Hash)
		default:
			return "", fmt.Errorf("unknown rebase action %q", it.Action)
		}
	}
	file := filepath.Join(dir, "todo")
	if err := os.WriteFile(file, []byte(todo.String()), 0o600); err != nil {
		return "", err
	}
	if o.Backup {
		name := r.Branch()
		if name == "" {
			name = "HEAD"
		}
		backup = fmt.Sprintf("backup/%s-%s", name, time.Now().Format("20060102-150405"))
		if _, err := r.Do("branch", backup); err != nil {
			return "", err
		}
	}
	args := []string{"rebase", "-i"}
	if o.Autostash {
		args = append(args, "--autostash")
	}
	if o.UpdateRefs {
		args = append(args, "--update-refs")
	}
	args = append(args, onto)
	// git runs "<editor> <todo file>": this one copies ours over it.
	_, err = r.DoEnv([]string{"GIT_SEQUENCE_EDITOR=cp " + shellQuote(file)}, args...)
	return backup, err
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
