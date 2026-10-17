package main

import (
	"bytes"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
	"github.com/eliott/myfork/internal/github"
	"github.com/eliott/myfork/internal/highlight"
	"github.com/eliott/myfork/internal/worddiff"
)

// A file kind groups the files of a change, as pulls.review's rules do.
type kind struct{ key, label, summary string }

// kinds are the kinds of files, in the order their groups show.
var kinds = []kind{
	{"docs", "Docs", "Documentation updates."},
	{"code", "Code", "Source changes."},
	{"tests", "Tests", "Test coverage for the change."},
	{"config", "Config", "Configuration changes."},
	{"deps", "Dependencies", "Dependency version changes."},
	{"generated", "Generated", "Generated or build output changes."},
	{"other", "Other", "Other changes that don't fit an existing category."},
}

func kindIndex(key string) int {
	return slices.IndexFunc(kinds, func(k kind) bool { return k.key == key })
}

// kindOf returns the index in kinds of a file's kind. The first rule that
// matches wins: a document under .github is a document.
func kindOf(p string, binary bool) int {
	lower := strings.ToLower(p)
	parts := strings.Split(lower, "/")
	base, dirs := parts[len(parts)-1], parts[:len(parts)-1]
	inDir := func(names ...string) bool {
		for _, d := range dirs {
			if slices.Contains(names, d) {
				return true
			}
		}
		return false
	}
	ext := ""
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		ext = base[i:]
	}
	switch {
	case ext == ".md" || ext == ".mdx" || ext == ".mdc" || ext == ".rst" || ext == ".adoc",
		inDir("docs"), strings.HasPrefix(base, "readme"):
		return kindIndex("docs")
	case strings.Contains(base, ".test.") || strings.Contains(base, ".spec."),
		inDir("__tests__", "test", "tests", "testdata"),
		strings.HasSuffix(base, "_test.go"), strings.HasSuffix(base, "_test.py"), strings.HasPrefix(base, "test_") && ext == ".py",
		strings.HasSuffix(base, "_spec.rb"):
		return kindIndex("tests")
	case git.LooksGenerated(p), inDir("dist"), ext == ".lock":
		return kindIndex("generated")
	case slices.Contains([]string{"package.json", "pnpm-workspace.yaml", "cargo.toml", "go.mod", "gemfile", "requirements.txt"}, base):
		return kindIndex("deps")
	case len(dirs) == 0 && (strings.Contains(base, ".config.") || strings.HasPrefix(base, ".git") ||
		strings.HasPrefix(base, ".") && (strings.HasSuffix(base, "rc") || strings.Contains(base, "rc."))),
		len(dirs) > 0 && dirs[0] == ".github",
		strings.HasPrefix(base, "tsconfig") && ext == ".json",
		base == "dockerfile", base == ".dockerignore", base == "makefile", base == "mygo.json", base == "mise.toml",
		strings.HasPrefix(base, "docker-compose") && (ext == ".yml" || ext == ".yaml"):
		return kindIndex("config")
	case binary:
		return kindIndex("other")
	}
	return kindIndex("code")
}

type rowKind uint8

const (
	rowCommit   rowKind = iota // the commit's message
	rowPills                   // the groups, as pills
	rowGroup                   // a group's heading
	rowHeader                  // a file's header, which stays at the top
	rowFileNote                // why a file's lines do not show
	rowGap                     // unchanged lines not shown
	rowLine                    // a line, or a pair of lines side by side
	rowEnd                     // the bottom of a file's card
	rowComment                 // a draft comment on the line above
	rowThread                  // a review thread of the pull request
	rowNote                    // nothing to show
)

type reviewRow struct {
	kind  rowKind
	file  int
	group int
	// For comments: the draft, or the thread of the pull request.
	comment *comment
	thread  *github.Thread
	// For lines of hunks: the hunk, and the indices of its lines on the
	// left (old) and the right (new) side, -1 for none. Unified rows use a
	// alone. Lines of context the user expanded have hunk -1 and their
	// numbers in old and new.
	hunk     int32
	a, b     int32
	old, new int32
	// For gaps: which gap, and how many lines it hides; for notes, which.
	gap   int32
	count int32
}

// bigPatch is the size of a file's patch beyond which its lines wait for a
// click.
const bigPatch = 400 << 10

// reviewFile is a file of the change shown.
type reviewFile struct {
	path, oldPath string
	status        byte
	patch         string
	added         int
	deleted       int
	binary, big   bool
	group         int

	collapsed bool
	viewed    bool
	// open loads the lines of a big file anyway.
	open bool

	// hunks are the patch's, read when the lines first show.
	hunks      []diffHunk
	parsed     bool
	unreadable bool
	// words are the changed ranges of lines replaced by others, by the
	// index of the hunk and of the line in it.
	words map[[2]int][]worddiff.Range
	// The lines of the old and the new file, and their tokens, once read
	// whole, so that the lines between changes can be shown; loaded is set
	// then, and loading while they are read.
	oldLines, newLines []string
	oldHL, newHL       [][]highlight.Seg
	loaded, loading    bool
	// hunkHL are the tokens of the hunks' lines, by the index of the hunk
	// and of the line in it, for the files not read whole.
	hunkHL     map[[2]int][]highlight.Seg
	hunkHLDone bool
	// expanded is how many lines of each gap of unchanged lines show, from
	// its top and its bottom.
	expanded map[int]gapShown
	// spans keeps the styled code of the lines shown, which does not change
	// from frame to frame.
	spans map[spanKey][]ui.Span
	// metric is the widths the rows share.
	metric      fileMetrics
	metricsDone bool
	// hscroll is how far the lines are scrolled sideways.
	hscroll float32
}

func (f *reviewFile) dir() string  { d, _ := dirAndBase(f.path); return d }
func (f *reviewFile) name() string { _, b := dirAndBase(f.path); return b }

type reviewGroup struct {
	kind    int
	files   []int
	added   int
	deleted int
}

// review is the change of a commit as one surface of file cards.
type review struct {
	hash    string
	base    string // the parent the files are compared with
	loading bool
	err     string
	files   []*reviewFile
	groups  []reviewGroup
	grouped bool
	rows    []reviewRow
	dirty   bool
	list    ui.ListState
	gen     int
	// current is the file at the top of the surface; revealedAt is when a
	// file was last scrolled to, while which the scrolling does not change it.
	current    int
	revealedAt time.Time
	// closedDirs are the groups and folders of the files tree that are
	// closed.
	closedDirs map[string]bool
	filter     string
	// side is the sidebar's list of the files.
	side ui.ListState
	// pr is set for a pull request's: it has no commit card, its files are
	// not read whole, nor are they in the work tree.
	pr bool
	// local is set once a pull request's commits are in the repository, so
	// that its files can be read whole; layoutKey is what its files were
	// last laid out from.
	local     bool
	layoutKey string
	// canComment shows the button that comments on a line; comments are the
	// drafts, and threads those of the pull request.
	canComment bool
	comments   []*comment
	threads    []*github.Thread
	placed     map[any]bool
	// split shows the lines of a file side by side, as the layout chosen.
	split bool
	// charW is the width of a column of code, for the font charKey.
	charW   float32
	charKey string
}

func newReview() *review {
	r := &review{closedDirs: map[string]bool{}, current: -1}
	r.list.Header = func(i int) bool { return i >= 0 && i < len(r.rows) && r.rows[i].kind == rowHeader }
	r.list.Key = func(i int) any {
		if i < 0 || i >= len(r.rows) {
			return i
		}
		return r.key(&r.rows[i])
	}
	return r
}

func viewedKey(hash, p string) string { return hash + "\x00" + p }

// loadReview reads the patch of the commit shown, and lays its files out as
// cards.
func (t *repoTab) loadReview() {
	c := t.commit
	if c == nil {
		t.rv = nil
		return
	}
	rv := t.rv
	if rv == nil {
		rv = newReview()
		t.rv = rv
	}
	rv.gen++
	gen := rv.gen
	rv.hash, rv.loading, rv.err = c.Hash, true, ""
	rv.base = git.EmptyTree
	if len(c.Parents) > 0 {
		rv.base = c.Parents[0]
	}
	cc := *c
	repo, o := t.repo, t.diffOptions()
	t.app.background(func() {
		patch, err := repo.CommitPatch(&cc, o)
		parts := git.SplitPatchFiles(patch)
		t.app.update(func() {
			if t.rv != rv || rv.gen != gen {
				return
			}
			rv.loading = false
			if err != nil {
				rv.err = errorText(err)
				return
			}
			t.layoutReview(rv, parts)
		})
	})
}

// layoutReview makes the files and the groups of a patch's parts.
func (t *repoTab) layoutReview(rv *review, parts []git.FilePart) {
	rv.files = rv.files[:0]
	for _, p := range parts {
		f := &reviewFile{path: p.Path, oldPath: p.OldPath, status: p.Status, patch: p.Patch, added: p.Added, deleted: p.Deleted, binary: p.Binary}
		f.big = len(p.Patch) > bigPatch
		f.group = kindOf(p.Path, p.Binary)
		f.viewed = t.viewed[viewedKey(rv.hash, p.Path)]
		f.collapsed = f.viewed || kinds[f.group].key == "generated" || f.big
		rv.files = append(rv.files, f)
	}
	// Files go together by kind, in the kinds' order, each in its own.
	slices.SortStableFunc(rv.files, func(a, b *reviewFile) int { return a.group - b.group })
	rv.groups = rv.groups[:0]
	for i, f := range rv.files {
		if n := len(rv.groups); n == 0 || rv.groups[n-1].kind != f.group {
			rv.groups = append(rv.groups, reviewGroup{kind: f.group})
		}
		g := &rv.groups[len(rv.groups)-1]
		g.files = append(g.files, i)
		g.added += f.added
		g.deleted += f.deleted
	}
	rv.grouped = len(rv.groups) > 1
	rv.current = -1
	rv.dirty = true
}

// build makes the rows of the surface.
func (rv *review) build() {
	rv.rows = rv.rows[:0]
	rv.placed = map[any]bool{}
	if !rv.pr {
		rv.rows = append(rv.rows, reviewRow{kind: rowCommit})
	}
	if rv.grouped {
		rv.rows = append(rv.rows, reviewRow{kind: rowPills})
	}
	if len(rv.files) == 0 {
		rv.rows = append(rv.rows, reviewRow{kind: rowNote})
	}
	for gi, g := range rv.groups {
		if rv.grouped {
			rv.rows = append(rv.rows, reviewRow{kind: rowGroup, group: gi})
		}
		for _, fi := range g.files {
			f := rv.files[fi]
			rv.rows = append(rv.rows, reviewRow{kind: rowHeader, file: fi, group: gi, a: -1, b: -1})
			if f.collapsed {
				rv.rows = append(rv.rows, reviewRow{kind: rowEnd, file: fi, group: gi, a: -1, b: -1})
				continue
			}
			rv.rows = rv.appendFileRows(rv.rows, fi, f, gi)
		}
	}
	rv.dirty = false
}

// ofFile reports whether a row is a file's, rather than above the files.
func (k rowKind) ofFile() bool {
	switch k {
	case rowHeader, rowFileNote, rowGap, rowLine, rowEnd, rowComment, rowThread:
		return true
	}
	return false
}

func (rv *review) rowOf(kind rowKind, file int) int {
	for i, r := range rv.rows {
		if r.kind == kind && r.file == file {
			return i
		}
	}
	return -1
}

func (rv *review) groupRow(g int) int {
	for i, r := range rv.rows {
		if r.kind == rowGroup && r.group == g {
			return i
		}
	}
	return -1
}

// reveal scrolls the surface to a file's card.
func (rv *review) reveal(file int) {
	if rv == nil || file < 0 || file >= len(rv.files) {
		return
	}
	if rv.dirty {
		rv.build()
	}
	if f := rv.files[file]; f.collapsed && !f.viewed {
		f.collapsed = false
		rv.build()
	}
	if i := rv.rowOf(rowHeader, file); i >= 0 {
		rv.list.ScrollTo(i, ui.Start)
	}
	rv.current = file
	rv.revealedAt = time.Now()
}

func (t *repoTab) setViewed(rv *review, f *reviewFile, on bool) {
	f.viewed = on
	f.collapsed = on
	if t.viewed == nil {
		t.viewed = map[string]bool{}
	}
	t.viewed[viewedKey(rv.hash, f.path)] = on
	rv.dirty = true
}

func (rv *review) viewedCount(g reviewGroup) int {
	n := 0
	for _, fi := range g.files {
		if rv.files[fi].viewed {
			n++
		}
	}
	return n
}

// maxWhole is the size of file beyond which its lines between changes are
// not offered.
const maxWhole = 1 << 20

// upgrade reads a changed file whole, old and new, so that the diff can show
// the unmodified lines between its changes, which a patch does not hold, and
// color them by the whole file's tokens.
func (t *repoTab) upgrade(rv *review, f *reviewFile) {
	if rv == nil || (rv.pr && !rv.local) || f.loaded || f.loading || f.binary || f.big || f.status == 'A' || f.status == 'D' || t.app.store.settings().IgnoreWhitespace {
		return
	}
	f.loading = true
	hash, base, repo := rv.hash, rv.base, t.repo
	p, oldP := f.path, f.oldPath
	t.app.background(func() {
		oldData, errO := repo.ReadFile(base, oldP)
		newData, errN := repo.ReadFile(hash, p)
		var oldLines, newLines []string
		var oldHL, newHL [][]highlight.Seg
		ok := errO == nil && errN == nil && len(oldData) <= maxWhole && len(newData) <= maxWhole &&
			bytes.IndexByte(newData, 0) < 0 && bytes.IndexByte(oldData, 0) < 0
		if ok {
			oldLines, newLines = splitLines(string(oldData)), splitLines(string(newData))
			oldHL, newHL = highlight.Lines(p, oldLines), highlight.Lines(p, newLines)
		}
		t.app.update(func() {
			f.loading = false
			f.loaded = true
			if !ok || rv.hash != hash {
				return
			}
			// Empty files read as no lines, not as none known.
			f.oldLines, f.newLines = append([]string{}, oldLines...), append([]string{}, newLines...)
			f.oldHL, f.newHL = oldHL, newHL
			f.spans, f.metricsDone = nil, false
			rv.dirty = true
		})
	})
}

// highlightHunks colors the lines of a file's hunks by their tokens, off the
// main thread, as much as the hunks tell of the code around them.
func (t *repoTab) highlightHunks(rv *review, f *reviewFile) {
	if f.hunkHLDone || !f.parsed {
		return
	}
	f.hunkHLDone = true
	hunks, path := f.hunks, f.path
	t.app.background(func() {
		out := map[[2]int][]highlight.Seg{}
		for hi, h := range hunks {
			var oldText, newText []string
			var oldAt, newAt []int
			for li, l := range h.Lines {
				if l.Kind != '+' {
					oldText, oldAt = append(oldText, l.Text), append(oldAt, li)
				}
				if l.Kind != '-' {
					newText, newAt = append(newText, l.Text), append(newAt, li)
				}
			}
			// A line that is on both sides takes the new side's tokens.
			for i, segs := range highlight.Lines(path, oldText) {
				if hunks[hi].Lines[oldAt[i]].Kind == '-' && segs != nil {
					out[[2]int{hi, oldAt[i]}] = segs
				}
			}
			for i, segs := range highlight.Lines(path, newText) {
				if segs != nil {
					out[[2]int{hi, newAt[i]}] = segs
				}
			}
		}
		t.app.update(func() {
			f.hunkHL = out
			f.spans = nil
		})
	})
}

// splitLines cuts a text into its lines.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// forgetStates drops what was made of the files' lines, which the next frame
// makes anew, as after a setting changed.
func (rv *review) forgetStates() {
	if rv == nil {
		return
	}
	for _, f := range rv.files {
		f.spans, f.metricsDone = nil, false
	}
	rv.dirty = true
}
