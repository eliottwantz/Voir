package app

import (
	"context"
	"fmt"
	"path"
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
	fetched     int // commits read from the log, before stash commits are folded away
	loadingMore bool
	filter      string // the search field
	searching   bool
	matches     []git.Commit // results of a search, when filter is set
	searchGen   int
	searched    string

	side     string // the sidebar's choice
	sideOpen map[string]*bool

	list    ui.ListState
	sel     int // the row chosen in the graph
	selHash string
	// picked holds the commits chosen in the graph by their hashes: one, or
	// two to compare; pickOrder is the order they were chosen in, and cmpSide
	// the items of the sidebar that were chosen to compare.
	picked    ui.Selection[string]
	pickOrder []string
	cmpSide   map[string]bool
	// sideHit is the item of the sidebar clicked in this pass, and sideCmd
	// whether Cmd was held.
	sideHit string
	sideCmd bool
	cmpGen  int  // counts comparisons started, so that a late one is let go
	onWip   bool // Local Changes fill the area, not the graph
	// wipReturn is the commit that was chosen when Local Changes opened, which
	// the graph goes back to.
	wipReturn string
	commit    *git.Commit
	jump      string // a commit to scroll to once the list has it

	// The work tree.
	unstaged, staged []git.StatusEntry
	uList, sList     ui.ListState
	uSel, sSel       int // the rows chosen: folders are rows too
	uMulti, sMulti   ui.Selection[string]
	uClosed, sClosed map[string]bool // the folders closed
	workPath         string          // the file chosen, whose card the diff scrolls to
	workKey          string          // what the diff of the files was read from
	workRev          int             // the settings revision it was read with
	workGen          int
	msg              string
	amend            bool
	msgBackup        string

	// Layout.
	busy     string
	headHash string
	// scrollSel asks the history list to show the commit chosen, after a
	// choice made elsewhere than in the list.
	scrollSel   bool
	sideList    ui.ListState
	prStatus    map[int]github.PRStatus   // review and checks, by number; nil until read
	prStacks    map[int]github.StackPlace // where the pull requests are in their stacks, by number
	stacksOff   bool                      // the host has no stacked pull requests: they are not asked for again
	prViewer    string                    // the login of the account, which the list tells its pull requests from
	cv          *commitView               // the commit chosen, in three tabs
	windows     []*commitView             // the views in windows of their own
	viewed      map[string]bool
	gh          *github.Repo
	ghRemote    string
	prs         []github.PR
	prsLoaded   bool
	prsLoading  bool
	prsErr      string
	pr          *prView   // the active tab of the pull requests opened, nil when there is none
	aiWriting   bool      // a commit message is being written
	prTabs      []*prView // the pull requests opened, as tabs
	wrv         *review   // the diff of the file chosen in Local Changes
	workStaged  bool
	workStale   bool
	searchOpen  bool // the filter above the graph is shown (⌘F)
	searchFocus bool
	// focusUnstaged and focusStaged hand the keyboard to that list on the next
	// frame; followPaths are the files a shortcut moved to the staged list
	// (followStaged) or out of it, which keep their choice and the focus there.
	focusUnstaged, focusStaged bool
	followPaths                []string
	followStaged               bool
	stayPath                   string // the file to choose once the files moved are gone from the list (stayStaged)
	stayStaged                 bool
	cancel                     context.CancelFunc
	settings                   int // counts changes of the diff settings
}

func newTab(a *app, r *git.Repo) *repoTab {
	t := &repoTab{
		app:      a,
		repo:     r,
		name:     repoName(r.Root),
		side:     "history",
		sideOpen: map[string]*bool{},
		cmpSide:  map[string]bool{},
		refsAt:   map[string][]git.Ref{},
		sel:      -1,
		uSel:     -1,
		sSel:     -1,
		uClosed:  map[string]bool{},
		sClosed:  map[string]bool{},
	}
	t.cv = newCommitView(a, t)
	t.list.Selected = &t.sel
	t.list.Selection = &t.picked
	t.list.Key = func(i int) any { return t.rowKey(i) }
	t.uList.Selected = &t.uSel
	t.uList.Selection = &t.uMulti
	t.sList.Selected = &t.sSel
	t.sList.Selection = &t.sMulti
	return t
}

func (t *repoTab) rowKey(i int) any {
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

func (t *repoTab) rowCount() int { return len(t.visibleCommits()) }

// rowCommit returns the commit of a list row.
func (t *repoTab) rowCommit(i int) *git.Commit {
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

// sync brings the tab up to date with everything outside it: the working
// tree, operations in progress and conflicts (read again at once), the
// remotes (fetched, which reloads once done) and, on a GitHub repository whose
// pull requests are in use, the list and the pull request shown.
func (t *repoTab) sync() {
	t.workStale = true
	t.reload()
	if t.gh != nil && (t.app.pulls || t.prsLoaded) {
		t.syncPulls()
	}
	if t.busy == "" && len(t.remotes) > 0 {
		t.fetch("")
	}
}

// syncPulls reads the pull request list and the pull request shown again.
func (t *repoTab) syncPulls() {
	t.loadPRs()
	if t.pr != nil {
		t.reloadPR()
	}
}

// linked tells the tab shows a linked worktree, not the main one.
func (t *repoTab) linked() bool {
	_, linked := mainCheckout(t.repo.Root, t.trees)
	return linked
}

// reload reads the repository again, off the main thread, and shows the
// result unless a later reload overtook it.
func (t *repoTab) reload() {
	t.gen++
	gen := t.gen
	t.loading = true
	repo := t.repo
	opts := t.logOptions()
	// Read back as many commits as are shown, so that a commit chosen further
	// down the list is still there to stay chosen.
	opts.Limit = max(logPage, t.fetched)
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
	t.fetched = len(d.commits)
	t.commits = cleanStashes(d.commits, d.stashes)
	t.layoutGraph()
	t.indexRefs()
	t.splitStatus()
	t.updateGitHub()
	if t.filter != "" {
		t.search(t.filter)
	}
	t.restoreSelection()
	if t.onWip {
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
	opts.Skip, opts.Limit = t.fetched, logPage
	repo := t.repo
	t.app.background(func() {
		more, _ := repo.Log(opts)
		t.app.update(func() {
			t.loadingMore = false
			if gen != t.gen {
				return
			}
			t.hasMore = len(more) >= logPage
			t.fetched += len(more)
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
	if t.onWip {
		// Local Changes stays shown, even when everything is committed or
		// discarded: it says the tree is clean, and the history is a click away.
		return
	}
	if t.comparing() {
		// Two commits stay compared across a reload: their hashes do not change.
		if i := t.commitRow(t.selHash); i >= 0 {
			t.sel = i
		}
		return
	}
	if t.selHash != "" {
		if i := t.commitRow(t.selHash); i >= 0 {
			t.sel = i
			return
		}
		t.selHash = ""
		t.setCommit(nil)
	}
	if t.commit == nil && t.rowCount() > 0 {
		// The history is what a repository opens on: HEAD, or the first
		// commit of a repository without one.
		first := 0
		if t.status != nil {
			if i := t.commitRow(t.status.Head); i >= 0 {
				first = i
			}
		}
		t.loadRow(min(first, t.rowCount()-1))
		t.scrollSel = true
	}
}

// selectRow chooses a commit of the graph, which shows in the three tabs
// under it.
func (t *repoTab) selectRow(i int) {
	if i < 0 || i >= t.rowCount() {
		return
	}
	t.loadRow(i)
	t.leaveSpecialView()
}

// loadRow reads the commit of a row of the list, and shows it in the three
// tabs under the graph.
func (t *repoTab) loadRow(i int) {
	if i < 0 || i >= t.rowCount() {
		return
	}
	t.sel = i
	c := t.rowCommit(i)
	if c == nil {
		return
	}
	t.onWip = false
	t.cmpGen++
	if t.selHash == c.Hash && t.commit != nil && !t.comparing() {
		return
	}
	t.selHash = c.Hash
	t.pick(c.Hash)
	cc := *c
	t.setCommit(&cc)
}

// pick makes a commit the only one chosen in the graph.
func (t *repoTab) pick(hash string) {
	t.picked.Clear()
	t.picked.Add(hash)
	t.pickOrder = append(t.pickOrder[:0], hash)
	clear(t.cmpSide)
}

// comparing reports whether the panel under the graph shows the difference
// between two commits.
func (t *repoTab) comparing() bool { return t.cv.base != nil }

// pickChanged follows a choice made in the graph: one commit shows as itself,
// and two are compared. Cmd-click adds a commit to the choice or takes it out;
// a third replaces the one chosen last, so that the first stays what the
// others are compared with.
func (t *repoTab) pickChanged() {
	var all []string
	for _, h := range t.pickOrder {
		if t.picked.Has(h) {
			all = append(all, h)
		}
	}
	var fresh []string
	for h := range t.picked.All() {
		if !slices.Contains(all, h) {
			fresh = append(fresh, h)
		}
	}
	// The order of a map is not one: take new commits as the graph has them.
	slices.SortFunc(fresh, func(a, b string) int { return t.commitRow(a) - t.commitRow(b) })
	all = append(all, fresh...)
	lead := ""
	if cm := t.rowCommit(t.sel); cm != nil && t.picked.Has(cm.Hash) {
		lead = cm.Hash
	}
	switch {
	case len(all) == 0:
		return
	case len(all) == 1:
		if i := t.commitRow(all[0]); i >= 0 {
			t.selectRow(i)
		}
		return
	case lead != "":
		// The commit chosen last is the second; the other is the first chosen
		// of those that were.
		i := slices.IndexFunc(all, func(h string) bool { return h != lead })
		all = []string{all[i], lead}
	default:
		all = all[:2]
	}
	if t.comparing() && len(t.pickOrder) == 2 && t.pickOrder[0] == all[0] && t.pickOrder[1] == all[1] {
		return
	}
	clear(t.cmpSide)
	t.startCompare(all[0], all[1])
}

// startCompare shows the difference between two commits, which the graph
// holds chosen, in the panel under it.
func (t *repoTab) startCompare(first, second string) {
	t.pickOrder = append(t.pickOrder[:0], first, second)
	t.picked.Clear()
	t.picked.Add(first)
	t.picked.Add(second)
	t.selHash = second
	if i := t.commitRow(second); i >= 0 {
		t.sel = i
	}
	t.onWip = false
	t.cmpGen++
	gen := t.cmpGen
	show := func(a, b git.Commit) {
		if gen != t.cmpGen {
			return
		}
		t.cv.showCompare(&a, &b)
		t.commit = t.cv.commit
	}
	a, okA := t.loadedCommit(first)
	b, okB := t.loadedCommit(second)
	if okA && okB {
		show(a, b)
		return
	}
	repo := t.repo
	t.app.background(func() {
		var err error
		if !okA {
			a, err = repo.Show(first)
		}
		if err == nil && !okB {
			b, err = repo.Show(second)
		}
		t.app.update(func() {
			if err != nil {
				if gen == t.cmpGen {
					t.app.fail("Compare", err)
				}
				return
			}
			show(a, b)
		})
	})
}

// loadedCommit finds a commit among those read.
func (t *repoTab) loadedCommit(hash string) (git.Commit, bool) {
	for _, list := range [][]git.Commit{t.commits, t.matches} {
		for _, c := range list {
			if c.Hash == hash {
				return c, true
			}
		}
	}
	return git.Commit{}, false
}

// compareRef compares the commit shown, or the first of the two compared, with
// the target of a branch, tag or stash of the sidebar.
func (t *repoTab) compareRef(id string) {
	hash, ok := t.refTarget(id)
	anchor := t.selHash
	if len(t.pickOrder) > 0 {
		anchor = t.pickOrder[0]
	}
	if !ok || anchor == "" || anchor == hash {
		return
	}
	if t.filter != "" {
		t.filter, t.searching = "", false
	}
	t.startCompare(anchor, hash)
	clear(t.cmpSide)
	t.cmpSide[id] = true
	if i := t.commitRow(hash); i >= 0 {
		t.list.ScrollTo(i, ui.Center)
		return
	}
	t.loadUntil(hash, func() {
		if i := t.commitRow(hash); i >= 0 {
			t.sel = i
			t.list.ScrollTo(i, ui.Center)
		}
	})
}

// swapCompare compares the two commits the other way round.
func (t *repoTab) swapCompare() {
	if !t.comparing() {
		return
	}
	t.cv.swap()
	t.commit = t.cv.commit
}

// setCommit makes the commit chosen the one the tabs under the graph show.
func (t *repoTab) setCommit(c *git.Commit) {
	t.commit = c
	if c == nil {
		t.cv.clear()
		return
	}
	t.cv.show(c)
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
	t.loadUntil(hash, func() { t.jump = hash })
}

// loadUntil reads the log far enough to hold a commit, and calls then once it
// is laid out.
func (t *repoTab) loadUntil(hash string, then func()) {
	repo := t.repo
	gen := t.gen
	t.app.background(func() {
		opts := t.logOptions()
		opts.Limit = t.fetched + 5*logPage
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
			t.fetched = len(all)
			t.layoutGraph()
			then()
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
	for _, v := range append([]*commitView{t.cv}, t.windows...) {
		if v.commit != nil {
			v.load()
		}
	}
	for _, pv := range t.prTabs {
		if pv.rv != nil {
			pv.rv.forgetStates()
		}
		// The patches read from the repository were read with the old settings.
		if pv.loaded {
			pv.localHead = ""
			t.loadPRLocal(pv, pv.gen)
		}
	}
	t.wrv.forgetStates()
	if t.onWip {
		t.refreshWorkDiff()
	}
}

// showChanges shows Local Changes: the files to stage beside their diff, in
// place of the graph.
func (t *repoTab) showChanges() {
	t.app.pulls = false
	if t.filter != "" {
		t.filter, t.searching = "", false
	}
	if !t.onWip {
		t.wipReturn = t.selHash
		t.selHash = ""
		t.setCommit(nil)
	}
	t.onWip = true
	t.focusUnstaged = true
	t.splitStatus()
	t.refreshWorkDiff()
}

// showHistory goes back to the graph of commits, on the commit chosen before.
func (t *repoTab) showHistory() {
	t.app.pulls = false
	if t.onWip {
		t.onWip, t.sel = false, -1
		back := t.wipReturn
		t.wipReturn = ""
		if i := t.commitRow(back); back != "" && i >= 0 {
			t.loadRow(i)
			t.scrollSel = true
			return
		}
		t.restoreSelection()
	}
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

// workItem is the file of Local Changes the surface shows: a file with changes
// in the work tree or in the index, or both, is one card or the other.
type workItem struct {
	entry  git.StatusEntry
	staged bool
}

// refreshWorkDiff reads the patch of the file chosen in Local Changes, and
// lays it out as a card: the surface shows one file at a time, the one the
// lists chose.
func (t *repoTab) refreshWorkDiff() {
	t.followMoved()
	if _, _, ok := t.workChoice(); !ok {
		// Keep a choice when the file is still there, as in the other list,
		// else take the first.
		other := t.staged
		if t.workStaged {
			other = t.unstaged
		}
		switch {
		case slices.ContainsFunc(other, func(e git.StatusEntry) bool { return e.Path == t.workPath }):
			t.workStaged = !t.workStaged
			t.moveChoice(t.workPath, t.workStaged)
		case len(t.unstaged) > 0:
			t.workPath, t.workStaged = firstInTree(t.unstaged), false
		case len(t.staged) > 0:
			t.workPath, t.workStaged = firstInTree(t.staged), true
		default:
			t.workPath = ""
		}
	}
	rv := t.wrv
	if rv == nil {
		rv = newReview()
		rv.work = true
		rv.w.reset()
		t.wrv = rv
	}
	e, staged, ok := t.workChoice()
	if !ok {
		// Nothing left to stage.
		t.workKey, t.workStale = "", false
		t.layoutWork(rv, nil, nil)
		return
	}
	// The key is the file chosen and what its status says: another file
	// moving between the lists leaves this card as it is.
	key := fmt.Sprintf("%v:%s:%c%c:%v", staged, e.Path, e.Work, e.Index, e.Untracked)
	if t.workKey == key && t.workRev == t.settings && !t.workStale && len(rv.files) == 1 {
		return
	}
	t.workStale = false
	t.workKey, t.workRev = key, t.settings
	t.workGen++
	gen := t.workGen
	rv.loading = len(rv.files) == 0
	repo := t.repo
	o := t.diffOptions()
	t.app.background(func() {
		var patch string
		var err error
		switch {
		case e.Conflict:
			patch, err = repo.WorkDiff(e.Path, o)
		case staged:
			patch, err = repo.StagedDiff(e.Path, o)
		case e.Untracked:
			patch, err = repo.UntrackedDiff(e.Path)
		default:
			patch, err = repo.WorkDiff(e.Path, o)
		}
		if err != nil {
			patch = ""
		}
		t.app.update(func() {
			if t.workGen != gen || t.wrv != rv {
				return
			}
			rv.loading = false
			t.layoutWork(rv, []workItem{{entry: e, staged: staged}}, []string{patch})
		})
	})
}

// layoutWork makes the card of the file of Local Changes from the patch read,
// keeping whether it is collapsed.
func (t *repoTab) layoutWork(rv *review, items []workItem, patches []string) {
	type id struct {
		path   string
		staged bool
	}
	old := map[id]*reviewFile{}
	for _, f := range rv.files {
		old[id{f.path, f.staged}] = f
	}
	rv.files = rv.files[:0]
	for i, it := range items {
		e := it.entry
		f := &reviewFile{path: e.Path, oldPath: e.Path, entry: e, staged: it.staged}
		if parts := git.SplitPatchFiles(patches[i]); len(parts) > 0 {
			part := parts[0]
			f.status, f.patch, f.added, f.deleted, f.binary = part.Status, part.Patch, part.Added, part.Deleted, part.Binary
			if part.OldPath != "" {
				f.oldPath = part.OldPath
			}
		}
		if e.Untracked {
			f.status = 'A'
		}
		f.big = len(f.patch) > bigPatch
		f.group = kindOf(f.path, f.binary)
		if o := old[id{f.path, f.staged}]; o != nil {
			f.collapsed, f.open = o.collapsed, o.open
		}
		rv.files = append(rv.files, f)
	}
	rv.groups, rv.grouped, rv.current, rv.dirty = nil, false, -1, true
	if len(rv.files) > 0 {
		g := reviewGroup{files: make([]int, len(rv.files))}
		for i, f := range rv.files {
			g.files[i] = i
			g.added += f.added
			g.deleted += f.deleted
		}
		rv.groups = []reviewGroup{g}
	}
	// The rows are built again before they are drawn (dirty), which indexes
	// them; until then the old rows are of other files.
	rv.sel.rowsOn, rv.sel.on = false, false
}

// chooseWork chooses a file in the Local Changes lists, and shows its card.
func (t *repoTab) chooseWork(staged bool, path string) {
	t.workStaged, t.workPath = staged, path
	t.chooseOneList(staged)
	t.refreshWorkDiff()
	if rv := t.wrv; rv != nil {
		for i, f := range rv.files {
			if f.path == path && f.staged == staged {
				rv.reveal(i)
				break
			}
		}
	}
}

// chooseOneList keeps the choice to one of the two lists of Local Changes: the
// files and folders of the other one are let go, so that one file is marked at
// a time.
func (t *repoTab) chooseOneList(staged bool) {
	if staged {
		t.uSel = -1
		t.uMulti.Clear()
		return
	}
	t.sSel = -1
	t.sMulti.Clear()
}

// followMoved keeps the files a shortcut moved between the lists chosen, in
// the list they went to, which gets the keyboard.
func (t *repoTab) followMoved() {
	if p := t.stayPath; p != "" {
		t.stayPath = ""
		list := t.unstaged
		if t.stayStaged {
			list = t.staged
		}
		if slices.ContainsFunc(list, func(e git.StatusEntry) bool { return e.Path == p }) {
			t.workPath, t.workStaged = p, t.stayStaged
			t.chooseOneList(t.stayStaged)
			sel := &t.uMulti
			if t.stayStaged {
				sel = &t.sMulti
			}
			sel.Clear()
			sel.Add(p)
			t.focusStaged, t.focusUnstaged = t.stayStaged, !t.stayStaged
		}
	}
	paths := t.followPaths
	if len(paths) == 0 {
		return
	}
	t.followPaths = nil
	from, to, dest := &t.sMulti, &t.uMulti, t.unstaged
	if t.followStaged {
		from, to, dest = &t.uMulti, &t.sMulti, t.staged
	}
	to.Clear()
	first := true
	for _, p := range paths {
		if !slices.ContainsFunc(dest, func(e git.StatusEntry) bool { return e.Path == p }) {
			continue
		}
		from.Remove(p)
		to.Add(p)
		if first {
			t.workPath, t.workStaged, first = p, t.followStaged, false
		}
	}
	if !first {
		t.focusStaged, t.focusUnstaged = t.followStaged, !t.followStaged
	}
}

// moveChoice takes a file out of the choice of the list it left and puts it in
// the other list's, alone, so that the mark stays on the file whose diff shows
// as it moves between the two lists.
func (t *repoTab) moveChoice(path string, staged bool) {
	// staged says which list the file went to: it leaves the other one.
	from, to := &t.sMulti, &t.uMulti
	if staged {
		from, to = &t.uMulti, &t.sMulti
	}
	if !from.Has(path) {
		return
	}
	from.Remove(path)
	to.Clear()
	to.Add(path)
}

// inTreeOrder sorts files as a list shown as a tree has them.
func inTreeOrder(entries []git.StatusEntry) []git.StatusEntry {
	files := make([]sideFile, len(entries))
	for i, e := range entries {
		files[i] = sideFile{path: e.Path}
	}
	var out []git.StatusEntry
	for _, r := range sideRows(files, "", nil, false) {
		if r.kind == sideLeaf {
			out = append(out, entries[r.file])
		}
	}
	return out
}

// firstInTree is the file a list shown as a tree starts with.
func firstInTree(entries []git.StatusEntry) string {
	if sorted := inTreeOrder(entries); len(sorted) > 0 {
		return sorted[0].Path
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

// commitStaged commits the index with the message typed, or with a generated
// one when the box is empty.
func (t *repoTab) commitStaged() {
	msg := strings.TrimSpace(t.msg)
	if msg == "" {
		if len(t.staged) > 0 && !t.aiWriting {
			t.writeCommitMessage(func() {
				if strings.TrimSpace(t.msg) != "" {
					t.commitStaged()
				}
			})
		}
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

// leaveSpecialView is called when a commit is chosen: Local Changes, which
// have no commit to choose, give way to it.
func (t *repoTab) leaveSpecialView() {
	t.onWip = false
}
