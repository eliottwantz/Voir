package aigen

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eliott/voir/internal/git"
)

// repoInstructions are the instructions a style asks the prompts to carry.
func repoInstructions(s Style, r *git.Repo, p Provider) (commit, request string) {
	switch s.Mode {
	case "conventional_commits":
		return conventionalCommit, conventionalRequest
	case "custom":
		return strings.TrimSpace(s.Custom), strings.TrimSpace(s.Custom)
	case "repo_conventions":
		var examples []string
		if out, err := r.Read("log", "-20", "--pretty=format:%s"); err == nil && strings.TrimSpace(string(out)) != "" {
			examples = append(examples, "Recent commit subjects from this repository:\n"+strings.TrimSpace(string(out)))
		}
		read := func(name string) string {
			b, err := os.ReadFile(filepath.Join(r.Root, name))
			if err != nil {
				return ""
			}
			return strings.TrimSpace(limit(string(b), 8000))
		}
		if t := read("AGENTS.md"); t != "" {
			examples = append(examples, "Local AGENTS.md:\n"+t)
		}
		if p == Claude {
			if t := read("CLAUDE.md"); t != "" {
				examples = append(examples, "Local CLAUDE.md:\n"+t)
			}
		}
		if len(examples) == 0 {
			return repoCommit, repoRequest
		}
		all := strings.Join(examples, "\n\n")
		return repoCommit + "\n\n" + all, repoRequest + "\n\n" + all
	}
	return "", ""
}

func read(r *git.Repo, args ...string) string {
	out, err := r.Read(args...)
	if err != nil {
		return ""
	}
	return string(out)
}

// ErrNothingStaged says there is nothing to write a commit message for.
var ErrNothingStaged = errors.New("nothing is staged: stage the changes to commit first")

// WriteCommit writes the message of the commit of what is staged.
func WriteCommit(ctx context.Context, cfg Config, style Style, r *git.Repo) (Commit, error) {
	files := strings.TrimSpace(read(r, "diff", "--cached", "--name-status"))
	if files == "" {
		return Commit{}, ErrNothingStaged
	}
	commit, _ := repoInstructions(style, r, cfg.Provider)
	req := CommitPrompt(CommitInput{
		Branch:       strings.TrimSpace(read(r, "symbolic-ref", "-q", "--short", "HEAD")),
		StagedFiles:  files,
		StagedPatch:  read(r, "diff", "--no-ext-diff", "--cached", "--patch", "--minimal"),
		Instructions: commit,
	})
	req.Dir = r.Root
	raw, err := Generate(ctx, cfg, req)
	if err != nil {
		return Commit{}, err
	}
	var out struct{ Subject, Body string }
	if err := decode(cfg, req, raw, &out); err != nil {
		return Commit{}, err
	}
	return sanitizeCommit(Commit{Subject: out.Subject, Body: out.Body}), nil
}

// templatePaths are where a repository keeps its pull request template.
var templatePaths = []string{
	".github/pull_request_template.md", ".github/PULL_REQUEST_TEMPLATE.md",
	"docs/pull_request_template.md", "pull_request_template.md", "PULL_REQUEST_TEMPLATE.md",
}

// RangeBase is the revision to measure a branch from: the remote's copy of the
// base branch, so that an old local one does not widen the range.
func RangeBase(r *git.Repo, remote, base string) string {
	if remote != "" {
		if sha, err := r.Resolve(remote + "/" + base); err == nil {
			return sha
		}
	}
	return base
}

// WritePR writes the title and body of a pull request of head into base.
func WritePR(ctx context.Context, cfg Config, style Style, r *git.Repo, remote, base, head string) (PR, error) {
	baseRef := RangeBase(r, remote, base)
	headRef := head
	if _, err := r.Resolve(head); err != nil {
		headRef = "HEAD"
	}
	commits := strings.TrimSpace(read(r, "log", "--oneline", "--end-of-options", baseRef+".."+headRef))
	patch := read(r, "diff", "--no-ext-diff", "--patch", "--minimal", "--end-of-options", baseRef+"..."+headRef)
	if commits == "" && strings.TrimSpace(patch) == "" {
		return PR{}, fmt.Errorf("%s has nothing over %s", head, base)
	}
	template := ""
	if style.FollowTemplates {
		for _, path := range templatePaths {
			if t := read(r, "show", baseRef+":"+path); strings.TrimSpace(t) != "" {
				template = t
				break
			}
		}
	}
	_, request := repoInstructions(style, r, cfg.Provider)
	req := PRPrompt(PRInput{
		Base: base, Head: head, Commits: commits,
		DiffStat: strings.TrimSpace(read(r, "diff", "--stat", "--end-of-options", baseRef+"..."+headRef)),
		Patch:    patch, Template: template, Instructions: request,
	})
	req.Dir = r.Root
	raw, err := Generate(ctx, cfg, req)
	if err != nil {
		return PR{}, err
	}
	var out struct{ Title, Body string }
	if err := decode(cfg, req, raw, &out); err != nil {
		return PR{}, err
	}
	return sanitizePR(PR{Title: out.Title, Body: out.Body}), nil
}
