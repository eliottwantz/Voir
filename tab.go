package main

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/eliott/myfork/internal/git"
	"github.com/eliott/myfork/internal/github"
	"github.com/eliott/myfork/internal/graph"
)

// logPage is how many commits the list loads at a time.
const logPage = 400

type viewMode uint8

const (
	modeHistory viewMode = iota // a commit
	modeChanges                 // Local Changes: stage, and commit
	modePR                      // a pull request
)

// tabData is what a reload reads, off the main thread.
type tabData struct {
	status    *git.Status
	statusErr error
	refs      []git.Ref
	stashes   []git.Stash
	remotes   []git.Remote
	subs      []git.Submodule
	trees     []git.Worktree
	opState   string
	commits   []git.Commit
	logErr    error
}

// diffPane shows the patch of one file.
type diffPane struct {
	key     string // what it shows: "commit:<hash>:<path>"
	path    string
	state   *editor.DiffState
	patch   string
	hunks   []string // the patch's hunks, as git prints them
	err     string
	binary  bool
	empty   bool
	loading bool
	rev     int // the settings revision the state was built with
}

// repoTab is a repository open in a tab.
type repoTab struct {
	app  *app
	repo *git.Repo
	name string

	gen     int
	loading bool
	loaded  bool
	err     error

	status  *git.Status
	refs    []git.Ref
	stashes []git.Stash
	remotes []git.Remote
	subs    []git.Submodule
	trees   []git.Worktree
	opState string

	// The history.
	commits     []git.Commit
	rows        []graph.Row
	layout      graph.Layout
	refsAt      map[string][]git.Ref
	hasMore     bool
	loadingMore bool
	filter      string // the search field
	searching   bool
	matches     []git.Commit // results of a search, when filter is set
	searchGen   int
	searched    string

	mode     viewMode
	side     string // the sidebar's choice
	sideOpen map[string]*bool

	list    ui.ListState
	sel     int // the row chosen: 0 is the uncommitted changes when shown
	selHash string
	commit  *git.Commit
	jump    string // a commit to scroll to once the list has it

	// The work tree.
	unstaged, staged []git.StatusEntry
	uList, sList     ui.ListState
	uSel, sSel       int // the rows chosen: folders are rows too
	uMulti, sMulti   ui.Selection[string]
	uClosed, sClosed map[string]bool // the folders closed
	workPath         string          // the file whose patch is shown
	workKey          string
	work             diffPane
	msg              string
	amend            bool
	msgBackup        string

	// Layout.
	stageW float32

	busy     string
	headHash string
	// scrollSel asks the history list to show the commit chosen, after a
	// choice made elsewhere than in the list.
	scrollSel   bool
	sideList    ui.ListState
	prList      ui.ListState
	prFilter    string
	rv          *review
	viewed      map[string]bool
	gh          *github.Repo
	ghRemote    string
	prs         []github.PR
	prsLoaded   bool
	prsLoading  bool
	prsErr      string
	pr          *prView
	pview       patchView
	workStaged  bool
	workStale   bool
	searchFocus bool
	cancel      context.CancelFunc
	settings    int // counts changes of the diff settings
}

func newTab(a *app, r *git.Repo) *repoTab {
	t := &repoTab{
		app:      a,
		repo:     r,
		name:     repoName(r.Root),
		mode:     modeHistory,
		side:     "history",
		sideOpen: map[string]*bool{},
		refsAt:   map[string][]git.Ref{},
		stageW:   300,
		sel:      -1,
		uSel:     -1,
		sSel:     -1,
		uClosed:  map[string]bool{},
		sClosed:  map[string]bool{},
	}
	t.list.Selected = &t.sel
	t.list.Key = func(i int) any { return t.rowKey(i) }
	t.uList.Selected = &t.uSel
	t.uList.Selection = &t.uMulti
	t.sList.Selected = &t.sSel
	t.sList.Selection = &t.sMulti
	return t
}

// hasWip reports whether the list starts with the uncommitted changes.
func (t *repoTab) hasWip() bool {
	return t.status != nil && len(t.status.Entries) > 0 && t.filter == ""
}

func (t *repoTab) rowKey(i int) any {
	if t.hasWip() {
		if i == 0 {
			return "wip"
		}
		i--
	}
	list := t.visibleCommits()
	if i < 0 || i >= len(list) {
		return i
	}
	return list[i].Hash
}

// visibleCommits are the commits the list shows: the log, or a search's.
func (t *repoTab) visibleCommits() []git.Commit {
	if t.filter != "" {
		return t.matches
	}
	return t.commits
}

func (t *repoTab) rowCount() int {
	n := len(t.visibleCommits())
	if t.hasWip() {
		n++
	}
	return n
}

// rowCommit returns the commit of a list row, nil for the uncommitted changes.
func (t *repoTab) rowCommit(i int) *git.Commit {
	if t.hasWip() {
		if i == 0 {
			return nil
		}
		i--
	}
	list := t.visibleCommits()
	if i < 0 || i >= len(list) {
		return nil
	}
	return &list[i]
}

// commitRow returns the list row of a commit, -1 when it is not loaded.
func (t *repoTab) commitRow(hash string) int {
	for i, c := range t.visibleCommits() {
		if c.Hash == hash {
			if t.hasWip() {
				return i + 1
			}
			return i
		}
	}
	return -1
}

func (t *repoTab) logOptions() git.LogOptions {
	s := t.app.store.settings()
	o := git.LogOptions{All: true, ByDate: s.SortByDate, HideTags: s.HideTags, HideStashes: s.HideStashes, FirstParent: s.FirstParent}
	if t.side != "" && strings.HasPrefix(t.side, "only:") {
		o.All = false
		o.Revs = []string{strings.TrimPrefix(t.side, "only:")}
	}
	return o
}

// refreshStatus reads only the status, for a tab that is not shown.
func (t *repoTab) refreshStatus() {
	repo := t.repo
	t.app.background(func() {
		st, err := repo.Status()
		trees := repo.Worktrees()
		t.app.update(func() {
			if err == nil {
				t.status = st
			}
			t.trees = trees
		})
	})
}

// linked tells the tab shows a linked worktree, not the main one.
func (t *repoTab) linked() bool {
	for _, w := range t.trees {
		if w.Main {
			main := w.Path
			if r, err := filepath.EvalSymlinks(main); err == nil {
				main = r
			}
			return main != t.repo.Root
		}
	}
	return false
}

// reload reads the repository again, off the main thread, and shows the
// result unless a later reload overtook it.
func (t *repoTab) reload() {
	t.gen++
	gen := t.gen
	t.loading = true
	repo := t.repo
	opts := t.logOptions()
	opts.Limit = logPage
	t.app.background(func() {
		var d tabData
		var wg sync.WaitGroup
		do := func(fn func()) {
			wg.Add(1)
			go func() { defer wg.Done(); fn() }()
		}
		do(func() { d.status, d.statusErr = repo.Status() })
		do(func() { d.refs, _ = repo.Refs() })
		do(func() { d.stashes, _ = repo.Stashes() })
		do(func() { d.remotes = repo.Remotes() })
		do(func() { d.subs = repo.Submodules() })
		do(func() { d.trees = repo.Worktrees() })
		do(func() { d.opState = repo.State() })
		do(func() { d.commits, d.logErr = repo.Log(opts) })
		wg.Wait()
		t.app.update(func() {
			if gen != t.gen {
				return
			}
			t.apply(&d, opts.Limit)
		})
	})
}

func (t *repoTab) apply(d *tabData, limit int) {
	t.loading = false
	t.loaded = true
	switch {
	case d.statusErr != nil:
		t.err = d.statusErr
	default:
		t.err = nil
	}
	t.status, t.refs, t.stashes, t.remotes, t.subs, t.trees, t.opState = d.status, d.refs, d.stashes, d.remotes, d.subs, d.trees, d.opState
	t.hasMore = len(d.commits) >= limit
	t.commits = cleanStashes(d.commits, d.stashes)
	t.layoutGraph()
	t.indexRefs()
	t.splitStatus()
	t.updateGitHub()
	if t.filter != "" {
		t.search(t.filter)
	}
	if t.side == "history" && t.mode == modeChanges && t.status != nil && len(t.status.Entries) == 0 && t.opState == "" {
		// Nothing left to stage: back to the history.
	}
	t.restoreSelection()
	if t.mode == modeChanges {
		t.refreshWorkDiff()
	}
}

// layoutGraph lays out every commit loaded.
func (t *repoTab) layoutGraph() {
	t.layout = graph.Layout{}
	t.rows = t.rows[:0]
	for i := range t.commits {
		t.rows = append(t.rows, t.layout.Add(t.commits[i].Hash, t.commits[i].Parents))
	}
}

func (t *repoTab) indexRefs() {
	t.refsAt = map[string][]git.Ref{}
	for _, r := range t.refs {
		t.refsAt[r.Target] = append(t.refsAt[r.Target], r)
	}
	for _, s := range t.stashes {
		t.refsAt[s.Hash] = append(t.refsAt[s.Hash], git.Ref{Kind: git.StashRef, Name: s.Name, Target: s.Hash})
	}
	if t.status != nil && t.status.Head != "" {
		// HEAD marks the commit checked out, a branch or not.
		t.headHash = t.status.Head
	}
}

// loadMore reads the next page of the log.
func (t *repoTab) loadMore() {
	if !t.hasMore || t.loadingMore || t.filter != "" {
		return
	}
	t.loadingMore = true
	gen := t.gen
	opts := t.logOptions()
	opts.Skip, opts.Limit = len(t.commits), logPage
	repo := t.repo
	t.app.background(func() {
		more, _ := repo.Log(opts)
		t.app.update(func() {
			t.loadingMore = false
			if gen != t.gen {
				return
			}
			t.hasMore = len(more) >= logPage
			more = cleanStashes(more, t.stashes)
			for i := range more {
				t.rows = append(t.rows, t.layout.Add(more[i].Hash, more[i].Parents))
			}
			t.commits = append(t.commits, more...)
		})
	})
}

// search finds commits whose message, author or hash match.
func (t *repoTab) search(q string) {
	t.filter = q
	t.searchGen++
	gen := t.searchGen
	q = strings.TrimSpace(q)
	if q == "" {
		t.matches, t.searched = nil, ""
		return
	}
	repo := t.repo
	stashes := slices.Clone(t.stashes)
	hashLike := len(q) >= 4 && isHexString(q)
	t.app.background(func() {
		var out []git.Commit
		seen := map[string]bool{}
		add := func(cs []git.Commit) {
			for _, c := range cs {
				if !seen[c.Hash] {
					seen[c.Hash] = true
					out = append(out, c)
				}
			}
		}
		if hashLike {
			if c, err := repo.Show(q); err == nil {
				add([]git.Commit{c})
			}
		}
		msg, _ := repo.Log(git.LogOptions{All: true, Grep: q, Limit: 500})
		add(msg)
		by, _ := repo.Log(git.LogOptions{All: true, Author: q, Limit: 500})
		add(by)
		out = cleanStashes(out, stashes)
		slices.SortStableFunc(out, func(a, b git.Commit) int { return b.CommitTime.Compare(a.CommitTime) })
		t.app.update(func() {
			if gen != t.searchGen {
				return
			}
			t.matches, t.searched = out, q
		})
	})
}

func isHexString(s string) bool {
	for _, c := range strings.ToLower(s) {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// restoreSelection keeps the commit chosen across a reload.
func (t *repoTab) restoreSelection() {
	if t.mode != modeHistory {
		return
	}
	if t.selHash != "" {
		if i := t.commitRow(t.selHash); i >= 0 {
			t.sel = i
			return
		}
		t.selHash, t.commit = "", nil
	}
	if t.sel < 0 && t.rowCount() > 0 {
		// The first commit, not the uncommitted changes above it: those
		// open Local Changes, and the history is what a repository opens on.
		first := 0
		if t.hasWip() {
			first = 1
		}
		if t.status != nil {
			if i := t.commitRow(t.status.Head); i >= 0 {
				first = i // HEAD, which the history is read from
			}
		}
		if first < t.rowCount() {
			t.selectRow(first)
			t.scrollSel = true
		}
	}
}

// selectRow shows the commit of a row of the list.
func (t *repoTab) selectRow(i int) {
	if i < 0 || i >= t.rowCount() {
		return
	}
	t.sel = i
	c := t.rowCommit(i)
	if c == nil {
		t.selHash, t.commit = "", nil
		t.showChanges()
		return
	}
	if t.selHash == c.Hash && t.commit != nil {
		return
	}
	t.selHash = c.Hash
	cc := *c
	t.commit = &cc
	t.leaveSpecialView()
	t.rv = nil
	t.loadReview()
}

// jumpTo scrolls the list to a commit and chooses it.
func (t *repoTab) jumpTo(hash string) {
	t.leaveSpecialView()
	if t.filter != "" {
		t.filter, t.searching = "", false
	}
	if i := t.commitRow(hash); i >= 0 {
		t.selectRow(i)
		t.list.ScrollTo(i, ui.Center)
		t.scrollSel = true
		return
	}
	// Not loaded: read the log up to it.
	repo := t.repo
	gen := t.gen
	t.app.background(func() {
		opts := t.logOptions()
		opts.Limit = len(t.commits) + 5*logPage
		all, _ := repo.Log(opts)
		t.app.update(func() {
			if gen != t.gen {
				return
			}
			if !slices.ContainsFunc(all, func(c git.Commit) bool { return c.Hash == hash }) {
				t.app.say("That commit is not in the history shown")
				return
			}
			t.commits = cleanStashes(all, t.stashes)
			t.hasMore = len(all) >= opts.Limit
			t.layoutGraph()
			t.jump = hash
		})
	})
}

func (t *repoTab) diffOptions() git.DiffOptions {
	s := t.app.store.settings()
	return git.DiffOptions{Context: max(s.ContextLines, 1), IgnoreSpace: s.IgnoreWhitespace}
}

// loadDiff reads a patch off the main thread and shows it in a pane.
func (t *repoTab) loadDiff(p *diffPane, key, file string, read func() (string, error)) {
	*p = diffPane{key: key, path: file, loading: true, rev: t.settings}
	t.app.background(func() {
		patch, err := read()
		t.app.update(func() {
			if p.key != key {
				return
			}
			p.loading = false
			if err != nil {
				p.err = errorText(err)
				return
			}
			t.setPatch(p, patch)
		})
	})
}

// setPatch parses a patch into a pane's diff viewer.
func (t *repoTab) setPatch(p *diffPane, patch string) {
	p.patch, p.err, p.binary, p.empty, p.state = patch, "", false, false, nil
	if git.Binary(patch) {
		p.binary = true
		return
	}
	if strings.TrimSpace(patch) == "" {
		p.empty = true
		return
	}
	files, err := editor.ParsePatch(patch)
	if err != nil || len(files) == 0 {
		p.err = "The patch could not be read"
		if err != nil {
			p.err += ": " + err.Error()
		}
		return
	}
	fp := files[0]
	if fp.Binary {
		p.binary = true
		return
	}
	_, p.hunks = git.SplitPatch(patch)
	s := t.app.store.settings()
	d := &editor.DiffState{
		Layout:        editor.DiffSplit,
		Wrap:          s.WrapLines,
		ContextLines:  max(s.ContextLines, 1),
		LineSelection: false,
	}
	if s.DiffLayout == "unified" {
		d.Layout = editor.DiffUnified
	}
	d.SetPatch(fp)
	name := fp.NewName
	if name == "" {
		name = fp.OldName
	}
	if lang := editor.LanguageFor(path.Base(name)); lang != nil {
		d.Old.SetLanguage(lang)
		d.New.SetLanguage(lang)
	}
	p.state = d
}

// resetDiffs rebuilds the diffs after a setting changed.
func (t *repoTab) resetDiffs() {
	t.settings++
	if t.rv != nil {
		t.rv.forgetStates()
		t.loadReview()
	}
	if t.pr != nil {
		t.pr.rv.forgetStates()
	}
	for _, p := range []*diffPane{&t.work} {
		if p.key != "" && p.patch != "" {
			t.setPatch(p, p.patch)
			p.rev = t.settings
		}
	}
}

// showChanges goes to Local Changes.
func (t *repoTab) showChanges() {
	t.app.sideTab = sideChanges
	t.enterSide(sideChanges)
}

// showHistory goes to the history of commits.
func (t *repoTab) showHistory() {
	t.app.sideTab = sideHistory
	t.enterSide(sideHistory)
}

// enterSide makes the tab show what a view of the sidebar is about: Local
// Changes, a pull request, or a commit. It does nothing for a view the tab is
// already in.
func (t *repoTab) enterSide(view int) {
	was := t.mode
	switch view {
	case sideChanges:
		t.mode = modeChanges
		if was != modeChanges {
			t.splitStatus()
			t.refreshWorkDiff()
		}
	case sidePulls:
		t.mode = modeHistory
		if t.pr != nil {
			t.mode = modePR
		}
	case sideFiles:
		// The files of the commit, or of the pull request, whichever the tab
		// was showing.
		if was == modeChanges {
			t.mode = modeHistory
		}
	default:
		t.mode = modeHistory
	}
	if was == modeChanges && t.mode == modeHistory {
		t.restoreSelection()
	}
}

// hasFiles reports whether there is a change whose files can be listed: a
// commit chosen, or a pull request.
func (t *repoTab) hasFiles() bool {
	if t.mode == modePR {
		return t.pr != nil
	}
	return t.commit != nil
}

// splitStatus divides the status into what is staged and what is not.
func (t *repoTab) splitStatus() {
	t.unstaged, t.staged = t.unstaged[:0], t.staged[:0]
	if t.status == nil {
		return
	}
	for _, e := range t.status.Entries {
		if e.Unstaged() {
			t.unstaged = append(t.unstaged, e)
		}
		if e.Staged() {
			t.staged = append(t.staged, e)
		}
	}
	byName := func(a, b git.StatusEntry) int {
		return strings.Compare(strings.ToLower(a.Path), strings.ToLower(b.Path))
	}
	slices.SortStableFunc(t.unstaged, byName)
	slices.SortStableFunc(t.staged, byName)
}

// workChoice is the file chosen in the Local Changes view.
func (t *repoTab) workChoice() (git.StatusEntry, bool, bool) {
	entries := t.unstaged
	if t.workStaged {
		entries = t.staged
	}
	for _, e := range entries {
		if e.Path == t.workPath {
			return e, t.workStaged, true
		}
	}
	return git.StatusEntry{}, false, false
}

// refreshWorkDiff shows the patch of the file chosen in Local Changes.
func (t *repoTab) refreshWorkDiff() {
	e, staged, ok := t.workChoice()
	if !ok {
		// Keep a choice when the file is still there, as in the other list,
		// else take the first.
		other := t.staged
		if t.workStaged {
			other = t.unstaged
		}
		switch {
		case slices.ContainsFunc(other, func(e git.StatusEntry) bool { return e.Path == t.workPath }):
			t.workStaged = !t.workStaged
		case len(t.unstaged) > 0:
			t.workPath, t.workStaged = firstInTree(t.unstaged), false
		case len(t.staged) > 0:
			t.workPath, t.workStaged = firstInTree(t.staged), true
		default:
			t.workPath = ""
			t.work = diffPane{}
			return
		}
		e, staged, _ = t.workChoice()
	}
	repo := t.repo
	o := t.diffOptions()
	key := fmt.Sprintf("work:%v:%s:%v:%v", staged, e.Path, e.Work, e.Index)
	if t.work.key == key && t.work.rev == t.settings && !t.workStale {
		return
	}
	t.workStale = false
	entry := e
	t.loadDiff(&t.work, key, e.Path, func() (string, error) {
		switch {
		case entry.Conflict:
			return repo.WorkDiff(entry.Path, o)
		case staged:
			return repo.StagedDiff(entry.Path, o)
		case entry.Untracked:
			return repo.UntrackedDiff(entry.Path)
		}
		return repo.WorkDiff(entry.Path, o)
	})
}

// chooseWork chooses a file in the Local Changes lists.
func (t *repoTab) chooseWork(staged bool, path string) {
	t.workStaged, t.workPath = staged, path
	if staged {
		t.uSel = -1
	} else {
		t.sSel = -1
	}
	t.refreshWorkDiff()
}

// firstInTree is the file a list shown as a tree starts with.
func firstInTree(entries []git.StatusEntry) string {
	files := make([]sideFile, len(entries))
	for i, e := range entries {
		files[i] = sideFile{path: e.Path}
	}
	for _, r := range sideRows(files, "", nil) {
		if r.kind == sideLeaf {
			return files[r.file].path
		}
	}
	return entries[0].Path
}

// ---- actions ----

// do runs a git operation off the main thread, shows that it works, and
// reloads when it is done.
func (t *repoTab) do(title string, fn func(r *git.Repo) error) {
	t.doThen(title, fn, nil)
}

func (t *repoTab) doThen(title string, fn func(r *git.Repo) error, then func()) {
	if t.busy != "" {
		t.app.say("%s is still running", t.busy)
		return
	}
	t.busy = title
	repo := t.repo
	t.app.background(func() {
		err := fn(repo)
		t.app.update(func() {
			t.busy, t.cancel = "", nil
			t.workStale = true
			if err != nil {
				t.app.fail(title+" failed", err)
			} else if then != nil {
				then()
			}
			t.reload()
		})
	})
}

// doCancelable is do for what takes long: fetch, pull, push, which the
// toolbar's Stop button cancels.
func (t *repoTab) doCancelable(title string, fn func(ctx context.Context, r *git.Repo) error, then func()) {
	if t.busy != "" {
		t.app.say("%s is still running", t.busy)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	t.doThen(title, func(r *git.Repo) error {
		defer cancel()
		err := fn(ctx, r)
		if ctx.Err() != nil {
			return nil
		}
		return err
	}, then)
}

func (t *repoTab) stop() {
	if t.cancel != nil {
		t.cancel()
	}
}

func (t *repoTab) stagePaths(paths ...string) {
	t.do("Stage", func(r *git.Repo) error { return r.Stage(paths...) })
}

func (t *repoTab) unstagePaths(paths ...string) {
	t.do("Unstage", func(r *git.Repo) error { return r.Unstage(paths...) })
}

func (t *repoTab) discardEntries(es []git.StatusEntry) {
	if len(es) == 0 {
		return
	}
	title := "Discard changes?"
	msg := fmt.Sprintf("Changes to %s will be lost. This cannot be undone.", plural(len(es), "file", "files"))
	if len(es) == 1 {
		msg = fmt.Sprintf("Changes to %q will be lost. This cannot be undone.", es[0].Path)
	}
	t.app.confirm(title, msg, "Discard", func() error {
		var tracked, untracked []string
		for _, e := range es {
			if e.Untracked {
				untracked = append(untracked, e.Path)
			} else {
				tracked = append(tracked, e.Path)
			}
		}
		if len(tracked) > 0 {
			if err := t.repo.Discard(tracked...); err != nil {
				return err
			}
		}
		return t.repo.DeleteUntracked(untracked...)
	}, func() { t.workStale = true })
}

// commitStaged commits the index with the message typed.
func (t *repoTab) commitStaged() {
	msg := strings.TrimSpace(t.msg)
	if msg == "" {
		return
	}
	amend := t.amend
	push := t.app.store.settings().PushOnCommit
	branch := ""
	if t.status != nil {
		branch = t.status.Branch
	}
	t.doCancelable("Commit", func(ctx context.Context, r *git.Repo) error {
		if err := r.Commit(git.CommitOptions{Message: msg, Amend: amend}); err != nil {
			return err
		}
		if push && branch != "" {
			return r.Push(ctx, git.PushOptions{Local: branch})
		}
		return nil
	}, func() {
		t.msg, t.amend = "", false
		t.app.say("Committed")
	})
}

// setAmend toggles amending, filling in the last message.
func (t *repoTab) setAmend(on bool) {
	if on == t.amend {
		return
	}
	t.amend = on
	if on {
		t.msgBackup = t.msg
		if strings.TrimSpace(t.msg) == "" {
			t.msg = t.repo.LastMessage()
		}
	} else if t.msg == t.repo.LastMessage() {
		t.msg = t.msgBackup
	}
}

func (t *repoTab) checkout(rev string) {
	dirty := t.status != nil && len(t.status.Entries) > 0
	if !dirty {
		t.do("Checkout", func(r *git.Repo) error { return r.Checkout(rev, git.KeepChanges) })
		return
	}
	d := &dialog{
		title:   "Checkout " + rev,
		message: "You have local changes.",
		ok:      "Checkout",
		fields:  []*field{choice("lc", "Local changes:", "Stash and reapply", "Don't change", "Stash and reapply", "Discard")},
		run: func(d *dialog) error {
			lc := git.KeepChanges
			switch d.field("lc").text {
			case "Stash and reapply":
				lc = git.StashChanges
			case "Discard":
				lc = git.DiscardChanges
			}
			return t.repo.Checkout(rev, lc)
		},
	}
	t.app.showDialog(d)
}

func (t *repoTab) checkoutRemote(remoteBranch string) {
	_, name, _ := strings.Cut(remoteBranch, "/")
	for _, r := range t.refs {
		if r.Kind == git.LocalBranch && r.Name == name {
			t.checkout(name)
			return
		}
	}
	t.do("Checkout", func(r *git.Repo) error { return r.CheckoutRemote(remoteBranch, name) })
}

func (t *repoTab) fetch(remote string) {
	s := t.app.store.settings()
	t.doCancelable("Fetch", func(ctx context.Context, r *git.Repo) error {
		return r.Fetch(ctx, remote, s.AlwaysFetchTags, true)
	}, nil)
}

// pullCurrent pulls what the branch checked out tracks.
func (t *repoTab) pullCurrent(rebase bool) {
	t.doCancelable("Pull", func(ctx context.Context, r *git.Repo) error { return r.Pull(ctx, "", "", rebase) }, nil)
}

// pushCurrent pushes the branch checked out, setting its upstream when it
// has none.
func (t *repoTab) pushCurrent() {
	b := t.currentBranch()
	if b == nil {
		t.app.fail("Push", fmt.Errorf("HEAD is detached: check out a branch to push it"))
		return
	}
	remote := "origin"
	if len(t.remotes) > 0 && !slices.ContainsFunc(t.remotes, func(r git.Remote) bool { return r.Name == "origin" }) {
		remote = t.remotes[0].Name
	}
	if len(t.remotes) == 0 {
		t.app.fail("Push", fmt.Errorf("This repository has no remote. Add one in Repository ▸ Settings."))
		return
	}
	set := b.Upstream == ""
	local := b.Name
	t.doCancelable("Push", func(ctx context.Context, r *git.Repo) error {
		return r.Push(ctx, git.PushOptions{Remote: remote, Local: local, SetUpstream: set})
	}, nil)
}

func (t *repoTab) currentBranch() *git.Ref {
	for i := range t.refs {
		if t.refs[i].Kind == git.LocalBranch && t.refs[i].Head {
			return &t.refs[i]
		}
	}
	return nil
}

// headBranchName is what the toolbar shows for HEAD.
func (t *repoTab) headBranchName() string {
	if t.status == nil {
		return ""
	}
	if t.status.Branch != "" {
		return t.status.Branch
	}
	if len(t.status.Head) >= 7 {
		return "detached at " + t.status.Head[:7]
	}
	return "no commits yet"
}

// tickClock returns the time to draw "x ago" with.
func tickClock(c *ui.Context) time.Time { return c.Now() }

// cleanStashes folds the commits git makes for a stash (the index, and the
// untracked files) into the stash's own, so that a stash shows as one node
// hanging from the commit it was made on.
func cleanStashes(commits []git.Commit, stashes []git.Stash) []git.Commit {
	if len(stashes) == 0 {
		return commits
	}
	isStash := map[string]bool{}
	drop := map[string]bool{}
	for _, s := range stashes {
		isStash[s.Hash] = true
		for _, p := range s.Parents[min(1, len(s.Parents)):] {
			drop[p] = true
		}
	}
	for i := range commits {
		if isStash[commits[i].Hash] && len(commits[i].Parents) > 1 {
			commits[i].Parents = commits[i].Parents[:1]
		}
	}
	out := commits[:0:0]
	for _, c := range commits {
		if !drop[c.Hash] {
			out = append(out, c)
		}
	}
	return out
}

// leaveSpecialView is called when a commit is chosen: Local Changes and the
// pull requests, which have no commits to choose, give way to the history.
func (t *repoTab) leaveSpecialView() {
	t.mode = modeHistory
	if v := t.app.sideTab; v == sideChanges || v == sidePulls {
		t.app.sideTab = sideHistory
	}
}
