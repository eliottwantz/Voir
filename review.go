package main

import (
	"bytes"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/eliott/myfork/internal/git"
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
	rowCommit rowKind = iota // the commit's message
	rowPills                 // the groups, as pills
	rowGroup                 // a group's heading
	rowHeader                // a file's header, which stays at the top
	rowBody                  // a file's lines
	rowNote                  // nothing to show
)

type reviewRow struct {
	kind  rowKind
	file  int
	group int
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

	state *editor.DiffState
	// full is set once the state compares the files whole, so that the
	// lines between changes can be shown.
	full, fullLoading bool
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
}

func newReview() *review {
	r := &review{closedDirs: map[string]bool{}, current: -1}
	r.list.Header = func(i int) bool { return i >= 0 && i < len(r.rows) && r.rows[i].kind == rowHeader }
	r.list.Key = func(i int) any {
		if i < 0 || i >= len(r.rows) {
			return i
		}
		row := r.rows[i]
		return fmt.Sprintf("%d/%d/%d", row.kind, row.file, row.group)
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
			rv.rows = append(rv.rows, reviewRow{kind: rowHeader, file: fi, group: gi})
			if !rv.files[fi].collapsed {
				rv.rows = append(rv.rows, reviewRow{kind: rowBody, file: fi, group: gi})
			}
		}
	}
	rv.dirty = false
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

// diffState makes the diff viewer of a file from its patch, or from the
// whole files when they have been read.
func (t *repoTab) diffState(f *reviewFile) *editor.DiffState {
	if f.state != nil {
		return f.state
	}
	d := t.newDiffState()
	d.FitContent = true
	if f.status == 'A' || f.status == 'D' {
		// The whole file is the change, which has one side: its text is
		// read from the patch, with no hunk headers over it.
		p, err := git.ParsePatchLines(f.patch)
		if err != nil {
			return nil
		}
		var lines []string
		for _, h := range p.Hunks {
			for _, l := range h.Lines {
				if l.Kind == '+' || l.Kind == '-' {
					lines = append(lines, l.Text)
				}
			}
		}
		text := strings.Join(lines, "\n")
		d.Layout = editor.DiffUnified
		if f.status == 'A' {
			d.Old, d.New = editor.NewBuffer(""), editor.NewBuffer(text)
		} else {
			d.Old, d.New = editor.NewBuffer(text), editor.NewBuffer("")
		}
	} else {
		files, err := editor.ParsePatch(f.patch)
		if err != nil || len(files) == 0 {
			return nil
		}
		d.SetPatch(files[0])
	}
	if lang := editor.LanguageFor(path.Base(f.path)); lang != nil {
		d.Old.SetLanguage(lang)
		d.New.SetLanguage(lang)
	}
	f.state = d
	return d
}

// newDiffState is a diff viewer with the settings of the preferences.
func (t *repoTab) newDiffState() *editor.DiffState {
	s := t.app.store.settings()
	d := &editor.DiffState{
		Layout:         editor.DiffSplit,
		Wrap:           s.WrapLines,
		ContextLines:   max(s.ContextLines, 1),
		ExpansionLines: 100,
	}
	if s.DiffLayout == "unified" {
		d.Layout = editor.DiffUnified
	}
	return d
}

// maxWhole is the size of file beyond which its lines between changes are
// not offered.
const maxWhole = 1 << 20

// upgrade reads a changed file whole, old and new, so that the diff can show
// the unmodified lines between its changes, which a patch does not hold.
func (t *repoTab) upgrade(rv *review, f *reviewFile) {
	if rv == nil || rv.pr || f.full || f.fullLoading || f.binary || f.big || f.status == 'A' || f.status == 'D' || t.app.store.settings().IgnoreWhitespace {
		return
	}
	f.fullLoading = true
	hash, base, repo := rv.hash, rv.base, t.repo
	p, oldP := f.path, f.oldPath
	t.app.background(func() {
		oldData, errO := repo.ReadFile(base, oldP)
		newData, errN := repo.ReadFile(hash, p)
		t.app.update(func() {
			f.fullLoading = false
			f.full = true
			if rv.hash != hash || errO != nil || errN != nil ||
				len(oldData) > maxWhole || len(newData) > maxWhole || bytes.IndexByte(newData, 0) >= 0 || bytes.IndexByte(oldData, 0) >= 0 {
				return
			}
			d := t.newDiffState()
			d.FitContent = true
			d.Old, d.New = editor.NewBuffer(string(oldData)), editor.NewBuffer(string(newData))
			if lang := editor.LanguageFor(path.Base(p)); lang != nil {
				d.Old.SetLanguage(lang)
				d.New.SetLanguage(lang)
			}
			f.state = d
		})
	})
}

// forgetStates drops the diff viewers, which the next frame makes anew, as
// after a setting changed.
func (rv *review) forgetStates() {
	if rv == nil {
		return
	}
	for _, f := range rv.files {
		f.state, f.full = nil, false
	}
}
