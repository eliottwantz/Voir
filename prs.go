package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
	"github.com/eliott/myfork/internal/github"
)

// prView is a pull request open in the main area.
type prView struct {
	number  int
	pr      github.PR
	loaded  bool
	err     string
	files   []github.File
	commits []github.Commit
	convo   []github.Comment
	checks  []github.Check
	summary string // success, failure, pending, "" for no checks

	tab      int // 0 conversation, 1 commits, 2 files
	fileSel  int
	fileList ui.ListState
	pane     diffPane
	draft    string
	busy     string
	gen      int
}

// detectGitHub finds the remote that holds the repository on GitHub: origin
// when it is there, else the first. Only GitHub is supported.
func detectGitHub(remotes []git.Remote) (github.Repo, string, bool) {
	var found github.Repo
	name, ok := "", false
	for _, r := range remotes {
		repo, parsed := github.ParseRemote(r.FetchURL)
		if !parsed || !(repo.Host == "github.com" || strings.Contains(repo.Host, "github")) {
			continue
		}
		if !ok || r.Name == "origin" {
			found, name, ok = repo, r.Name, true
		}
	}
	return found, name, ok
}

// client signs in to a host with the GitHub CLI's token, once.
func (a *app) client(ctx context.Context, host string) (*github.Client, error) {
	a.mu.Lock()
	cl := a.clients[host]
	a.mu.Unlock()
	if cl != nil {
		return cl, nil
	}
	cl, err := a.newGitHub(ctx, host)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.clients[host] = cl
	a.mu.Unlock()
	return cl, nil
}

// updateGitHub notes the repository's GitHub remote after a reload.
func (t *repoTab) updateGitHub() {
	repo, remote, ok := detectGitHub(t.remotes)
	if !ok {
		t.gh, t.ghRemote = nil, ""
		t.prs, t.prsLoaded, t.pr = nil, false, nil
		return
	}
	if t.gh == nil || *t.gh != repo {
		r := repo
		t.gh = &r
		t.prs, t.prsLoaded, t.prsErr = nil, false, ""
		t.pr = nil
	}
	t.ghRemote = remote
}

// loadPRs reads the open pull requests, off the main thread.
func (t *repoTab) loadPRs() {
	if t.gh == nil || t.prsLoading {
		return
	}
	t.prsLoading, t.prsErr = true, ""
	repo, a := *t.gh, t.app
	a.background(func() {
		ctx := context.Background()
		cl, err := a.client(ctx, repo.Host)
		var prs []github.PR
		if err == nil {
			prs, err = cl.PRs(ctx, repo, "open")
		}
		a.update(func() {
			t.prsLoading, t.prsLoaded = false, true
			if t.gh == nil || *t.gh != repo {
				return
			}
			if err != nil {
				t.prsErr = errorText(err)
				return
			}
			t.prs = prs
		})
	})
}

// openPR shows a pull request, whether the list holds it or not.
func (t *repoTab) openPR(n int) {
	if t.gh == nil {
		return
	}
	t.mode = modePR
	t.side = fmt.Sprintf("pr:%d", n)
	t.pr = &prView{number: n, fileSel: -1}
	for _, p := range t.prs {
		if p.Number == n {
			t.pr.pr = p
		}
	}
	t.pr.fileList.Selected = &t.pr.fileSel
	t.reloadPR()
}

// reloadPR reads a pull request in full: its details, files, commits,
// conversation and checks, side by side.
func (t *repoTab) reloadPR() {
	pv := t.pr
	if pv == nil || t.gh == nil {
		return
	}
	pv.gen++
	gen := pv.gen
	pv.err = ""
	repo, n, a := *t.gh, pv.number, t.app
	a.background(func() {
		ctx := context.Background()
		cl, err := a.client(ctx, repo.Host)
		var (
			pr      github.PR
			files   []github.File
			commits []github.Commit
			convo   []github.Comment
			checks  []github.Check
			summary string
		)
		if err == nil {
			pr, err = cl.PR(ctx, repo, n)
		}
		if err == nil {
			var wg sync.WaitGroup
			var mu sync.Mutex
			note := func(e error) {
				if e != nil {
					mu.Lock()
					if err == nil {
						err = e
					}
					mu.Unlock()
				}
			}
			run := func(fn func() error) {
				wg.Add(1)
				go func() { defer wg.Done(); note(fn()) }()
			}
			run(func() (e error) { files, e = cl.Files(ctx, repo, n); return })
			run(func() (e error) { commits, e = cl.Commits(ctx, repo, n); return })
			run(func() (e error) { convo, e = cl.Conversation(ctx, repo, n); return })
			run(func() error {
				// A missing checks permission is not worth failing for.
				checks, summary, _ = cl.Checks(ctx, repo, pr.Head.SHA)
				return nil
			})
			wg.Wait()
		}
		a.update(func() {
			if t.pr != pv || pv.gen != gen {
				return
			}
			if err != nil {
				pv.err = errorText(err)
				return
			}
			pv.pr, pv.files, pv.commits, pv.convo, pv.checks, pv.summary = pr, files, commits, convo, checks, summary
			pv.loaded = true
			if pv.fileSel < 0 && len(files) > 0 {
				pv.fileSel = 0
				pv.showFile(t, 0)
			}
			// Keep the list's entry fresh.
			for i := range t.prs {
				if t.prs[i].Number == n {
					t.prs[i] = pr
				}
			}
		})
	})
}

// showFile shows the patch of a file of the pull request.
func (pv *prView) showFile(t *repoTab, i int) {
	if i < 0 || i >= len(pv.files) {
		return
	}
	f := pv.files[i]
	pv.pane = diffPane{key: fmt.Sprintf("pr:%d:%s", pv.number, f.Filename), path: f.Filename, rev: t.settings}
	patch := f.FilePatch()
	if patch == "" {
		pv.pane.binary = true
		return
	}
	t.setPatch(&pv.pane, patch)
}

// prDo runs a call on the pull request, and reads it again.
func (t *repoTab) prDo(title string, fn func(ctx context.Context, cl *github.Client, repo github.Repo, n int) error, then func()) {
	pv := t.pr
	if pv == nil || t.gh == nil || pv.busy != "" {
		return
	}
	pv.busy = title
	repo, n, a := *t.gh, pv.number, t.app
	a.background(func() {
		ctx := context.Background()
		cl, err := a.client(ctx, repo.Host)
		if err == nil {
			err = fn(ctx, cl, repo, n)
		}
		a.update(func() {
			pv.busy = ""
			if err != nil {
				a.fail(title+" failed", err)
				return
			}
			if then != nil {
				then()
			}
			t.prsLoaded = false
			t.reloadPR()
		})
	})
}

func (t *repoTab) commentOnPR() {
	pv := t.pr
	body := strings.TrimSpace(pv.draft)
	if body == "" {
		return
	}
	t.prDo("Comment", func(ctx context.Context, cl *github.Client, r github.Repo, n int) error {
		_, err := cl.AddComment(ctx, r, n, body)
		return err
	}, func() { pv.draft = "" })
}

func (t *repoTab) setPRState(state string) {
	title := map[string]string{"closed": "Close", "open": "Reopen"}[state]
	t.prDo(title, func(ctx context.Context, cl *github.Client, r github.Repo, n int) error {
		return cl.SetState(ctx, r, n, state)
	}, nil)
}

func (t *repoTab) markPRReady() {
	t.prDo("Mark ready", func(ctx context.Context, cl *github.Client, r github.Repo, n int) error {
		return cl.MarkReady(ctx, r, n)
	}, nil)
}

// checkoutPR fetches a pull request's head into a local branch, pr/<n>, and
// checks it out.
func (t *repoTab) checkoutPR(n int) {
	if t.gh == nil {
		return
	}
	remote := t.ghRemote
	branch := fmt.Sprintf("pr/%d", n)
	current := t.status != nil && t.status.Branch == branch
	t.doCancelable("Checkout pull request", func(ctx context.Context, r *git.Repo) error {
		if current {
			// The branch cannot be fetched into while it is checked out.
			if err := r.FetchRef(ctx, remote, fmt.Sprintf("pull/%d/head", n)); err != nil {
				return err
			}
			_, err := r.Do("merge", "--ff-only", "FETCH_HEAD")
			return err
		}
		if err := r.FetchRef(ctx, remote, fmt.Sprintf("+pull/%d/head:refs/heads/%s", n, branch)); err != nil {
			return err
		}
		return r.Checkout(branch, git.KeepChanges)
	}, nil)
}

func openInBrowser(url string) {
	go mygo.Shell.OpenExternal(url)
}

// prSummary is "ada wants to merge 3 commits into main from topic".
func prSummary(pr github.PR) string {
	head := pr.Head.Ref
	if pr.Head.Repo != nil && pr.Base.Repo != nil && pr.Head.Repo.FullName != pr.Base.Repo.FullName {
		head = pr.Head.Repo.FullName + ":" + pr.Head.Ref
	}
	verb := "wants to merge"
	if pr.Merged {
		verb = "merged"
	} else if pr.State == "closed" {
		verb = "wanted to merge"
	}
	return fmt.Sprintf("%s %s %s into %s from %s", pr.User.Login, verb, plural(pr.Commits, "commit", "commits"), pr.Base.Ref, head)
}

var _ = slices.Contains[[]string]
