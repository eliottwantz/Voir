package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/egoist/mygo"
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

	tab int // 0 conversation, 1 commits, 2 files
	// rv is the files, as the surface of cards the commits have.
	rv    *review
	draft string
	busy  string
	gen   int

	// The commits of the pull request read from the repository: the commit
	// it branched from, and the patches of the files against it, which show
	// the lines between changes. localErr says why they could not be read.
	localHead, localBase string
	localParts           []git.FilePart
	localLoading         bool
	localErr             string

	// viewer is the login of the token's account, who can comment.
	viewer string
	// reviews are the review threads, and the viewer's pending review.
	reviews *github.Reviews
	// posting counts the comments going to GitHub.
	posting int
}

// canComment reports whether the viewer can write review comments.
func (pv *prView) canComment() bool { return pv != nil && pv.viewer != "" && pv.loaded }

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
	t.app.sideTab = sidePulls
	t.pr = &prView{number: n}
	for _, p := range t.prs {
		if p.Number == n {
			t.pr.pr = p
		}
	}
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
	needViewer := pv.viewer == ""
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
			reviews *github.Reviews
			viewer  github.User
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
			// Comments are read as far as they can be: the pull request is
			// shown without them.
			run(func() error { reviews, _ = cl.Reviews(ctx, repo, n); return nil })
			if needViewer {
				run(func() error { viewer, _ = cl.Viewer(ctx); return nil })
			}
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
			if viewer.Login != "" {
				pv.viewer = viewer.Login
			}
			if reviews != nil {
				pv.reviews = reviews
			}
			t.layoutPRFiles(pv)
			t.loadPRLocal(pv, gen)
			// Keep the list's entry fresh.
			for i := range t.prs {
				if t.prs[i].Number == n {
					t.prs[i] = pr
				}
			}
		})
	})
}

// layoutPRFiles lays the files of the pull request out as cards: from the
// patches of its commits when the repository holds them, else from GitHub's.
// It keeps what was opened and viewed of the files shown.
func (t *repoTab) layoutPRFiles(pv *prView) {
	if pv.rv == nil {
		pv.rv = newReview()
		pv.rv.pr = true
	}
	rv := pv.rv
	head := pv.pr.Head.SHA
	local := pv.localHead == head && pv.localHead != ""
	var parts []git.FilePart
	if local {
		parts = pv.localParts
	} else {
		parts = make([]git.FilePart, len(pv.files))
		for i, f := range pv.files {
			status := map[string]byte{"added": 'A', "removed": 'D', "renamed": 'R', "copied": 'C'}[f.Status]
			if status == 0 {
				status = 'M'
			}
			// GitHub sends no patch for a binary file, nor for one too big.
			parts[i] = git.FilePart{
				Path: f.Filename, OldPath: cmp.Or(f.PreviousFilename, f.Filename), Status: status,
				Binary: f.Patch == "" && f.Additions+f.Deletions == 0,
				Patch:  f.FilePatch(), Added: f.Additions, Deleted: f.Deletions,
			}
		}
	}
	rv.canComment = pv.canComment()
	rv.threads = nil
	if pv.reviews != nil {
		rv.threads = pv.reviews.Threads
	}
	rv.dirty = true
	key := fmt.Sprintf("%s|%v|%d", head, local, len(parts))
	if rv.layoutKey == key {
		return
	}
	rv.layoutKey = key
	rv.hash, rv.local = head, local
	if local {
		rv.base = pv.localBase
	}
	old := map[string]*reviewFile{}
	for _, f := range rv.files {
		old[f.path] = f
	}
	t.layoutReview(rv, parts)
	for _, f := range rv.files {
		if o := old[f.path]; o != nil {
			f.collapsed, f.open = o.collapsed, o.open
		}
	}
}

// loadPRLocal reads the commits of the pull request into the repository, off
// the main thread, and the patches of its files against where it branched, so
// that the lines between its changes can be shown.
func (t *repoTab) loadPRLocal(pv *prView, gen int) {
	head := pv.pr.Head.SHA
	if pv.localLoading || (pv.localHead == head && head != "") || head == "" {
		return
	}
	pv.localLoading, pv.localErr = true, ""
	repo, remote, n, pr, o, a := t.repo, t.ghRemote, pv.number, pv.pr, t.diffOptions(), t.app
	a.background(func() {
		base, err := a.fetchPR(context.Background(), repo, remote, n, pr)
		var parts []git.FilePart
		if err == nil {
			var patch string
			if patch, err = repo.RangePatch(base, head, o); err == nil {
				parts = git.SplitPatchFiles(patch)
			}
		}
		a.update(func() {
			pv.localLoading = false
			if t.pr != pv {
				return
			}
			if err != nil {
				pv.localErr = errorText(err)
				return
			}
			pv.localHead, pv.localBase, pv.localParts = head, base, parts
			if pv.pr.Head.SHA == head {
				t.layoutPRFiles(pv)
			}
		})
	})
}

// fetchPRCommits makes the repository hold the commits of a pull request, and
// returns where it branched: GitHub keeps the head at pull/<n>/head, and the
// base branch moves on, its old head possibly gone.
func (a *app) fetchPRCommits(ctx context.Context, r *git.Repo, remote string, n int, pr github.PR) (string, error) {
	head, base := pr.Head.SHA, pr.Base.SHA
	if !r.HasCommit(head) {
		err := r.FetchRef(ctx, remote, fmt.Sprintf("pull/%d/head", n))
		if !r.HasCommit(head) {
			if err == nil {
				err = errors.New("GitHub did not send them")
			}
			return "", fmt.Errorf("could not fetch the commits of #%d from %s: %s", n, remote, errorText(err))
		}
	}
	if !r.HasCommit(base) {
		if err := r.FetchRef(ctx, remote, "refs/heads/"+pr.Base.Ref); err == nil {
			if tip, err := r.Resolve("FETCH_HEAD"); err == nil {
				base = tip
			}
		}
	}
	return r.MergeBase(base, head)
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
