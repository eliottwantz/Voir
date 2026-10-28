package app

import (
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/eliott/myfork/internal/git"
	"github.com/eliott/myfork/internal/github"
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
	wheelRest     float64 // wheel scroll over a separator not yet applied
	group         int
	// For the files of Local Changes: the status entry, and whether the card
	// is the staged change of the file or the one not staged.
	entry  git.StatusEntry
	staged bool

	collapsed bool
	viewed    bool
	// open loads the lines of a big file anyway.
	open bool

	// ed is the file in the editor's diff viewer, made once from what edKey
	// says; edReading is set while the texts of a file read whole are read.
	// lines maps the lines of a viewer made of a patch to the file's.
	ed        *editor.DiffState
	edKey     string
	edReading bool
	lines     *patchLines
}

func (f *reviewFile) dir() string  { d, _ := dirAndBase(f.path); return d }
func (f *reviewFile) name() string { _, b := dirAndBase(f.path); return b }

type reviewGroup struct {
	kind    int
	files   []int
	added   int
	deleted int
}

// itemKind is what an item of the list of a pull request's files is.
type itemKind uint8

const (
	itemGroup  itemKind = iota // a group's heading
	itemHeader                 // a file's header, which stays at the top
	itemBody                   // a file's lines
)

// item is a row of the list of files: its kind, its file and its group.
type item struct {
	kind  itemKind
	file  int
	group int
}

// itemKey identifies an item across frames, so that the list keeps its place
// as the files above it fold and unfold.
type itemKey struct {
	kind  itemKind
	path  string
	group int
}

// review is the change of a commit as one surface of files.
type review struct {
	hash    string
	base    string // the parent the files are compared with
	cmp     bool   // base is a commit chosen to compare with, not the parent
	loading bool
	err     string
	files   []*reviewFile
	groups  []reviewGroup
	grouped bool
	// items are the rows of the list of files, made each frame.
	items []item
	list  ui.ListState
	gen   int
	// current is the file at the top of the surface; revealedAt is when a
	// file was last scrolled to, while which the scrolling does not change it.
	current    int
	revealedAt time.Time
	// seen is the file at the top as far as the scrolling went: the expanded
	// files before it were scrolled past, and are viewed. jumped is set when the
	// list is moved other than by scrolling, so that the files skipped are not.
	seen   int
	jumped bool
	// closedDirs are the groups and folders of the files tree that are
	// closed.
	closedDirs map[string]bool
	filter     string
	// side is the sidebar's list of the files.
	side ui.ListState
	// pr is set for a pull request's: it has no commit card, its files are
	// not read whole unless its commits are here, nor are they in the work
	// tree.
	pr bool
	// local is set once a pull request's commits are in the repository, so
	// that its files can be read whole; layoutKey is what its files were
	// last laid out from.
	local     bool
	layoutKey string
	// work is set for the changes of Local Changes: its lines come from the
	// patch, and are staged or discarded by hunk and by line.
	work bool
	// canComment shows the button that comments on a line; comments are the
	// drafts, and threads those of the pull request.
	canComment bool
	comments   []*comment
	// annH are the heights the comments and threads were drawn at, by their
	// key, which the viewer leaves room for under their lines.
	annH map[any]float32
	// navFile is the file of the comment last moved to.
	navFile int
	threads []*github.Thread
	// The state of the threads: the replies being written by the id of their
	// first comment, the threads resolved or outdated shown open, the comment
	// edited and its text, and the writes in flight.
	replies map[int64]*string
	opened  map[int64]bool
	// collapsed has the threads of the conversation whose code and comments
	// are folded under their file, and reasonMenu the thread whose resolve
	// reasons are open.
	collapsed  map[int64]bool
	reasonMenu int64
	// replying has the threads whose reply field is open.
	replying map[int64]bool
	editing  int64
	editText string
	busy     int
	// only is the one file whose lines show, by its index in files, or -1
	// to show every file: the section under the graph shows the file chosen.
	only int
}

func newReview() *review {
	r := &review{navFile: -1, only: -1, closedDirs: map[string]bool{}, current: -1, annH: map[any]float32{},
		replies: map[int64]*string{}, opened: map[int64]bool{}, collapsed: map[int64]bool{}, replying: map[int64]bool{}}
	// Only the headers of pull requests pin: a commit's and Local Changes'
	// files show one at a time.
	r.list.Header = func(i int) bool { return i >= 0 && i < len(r.items) && r.items[i].kind == itemHeader }
	r.list.Key = func(i int) any {
		if i < 0 || i >= len(r.items) {
			return i
		}
		it := r.items[i]
		k := itemKey{kind: it.kind}
		if it.kind == itemGroup {
			k.group = it.group
		} else {
			k.path = r.files[it.file].path
		}
		return k
	}
	return r
}

func viewedKey(hash, p string) string { return hash + "\x00" + p }

// viewID is what the files viewed are kept under: the commit, or the pair of
// commits compared.
func (rv *review) viewID() string {
	if rv.cmp {
		return rv.base + ".." + rv.hash
	}
	return rv.hash
}

// layoutReview makes the files and the groups of a patch's parts.
func (t *repoTab) layoutReview(rv *review, parts []git.FilePart) {
	rv.files = rv.files[:0]
	for _, p := range parts {
		f := &reviewFile{path: p.Path, oldPath: p.OldPath, status: p.Status, patch: p.Patch, added: p.Added, deleted: p.Deleted, binary: p.Binary}
		f.big = len(p.Patch) > bigPatch
		f.group = kindOf(p.Path, p.Binary)
		f.viewed = t.viewed[viewedKey(rv.viewID(), p.Path)]
		// Only a file with a header can be folded or viewed: the others show
		// one at a time, so none of them starts folded.
		f.collapsed = !rv.flat() && (f.viewed || kinds[f.group].key == "generated" || f.big)
		rv.files = append(rv.files, f)
	}
	// Files go together by kind, in the kinds' order, each in the order of
	// the files tree.
	slices.SortStableFunc(rv.files, func(a, b *reviewFile) int {
		if a.group != b.group {
			return a.group - b.group
		}
		return compareTreeOrder(a.path, b.path)
	})
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
	rv.jumped = true
}

// layoutItems makes the rows of the list of files: a heading for each group,
// when there are several, and for each file its header and, unless it is
// folded, its lines.
func (rv *review) layoutItems() {
	rv.items = rv.items[:0]
	for gi, g := range rv.groups {
		if rv.grouped {
			rv.items = append(rv.items, item{kind: itemGroup, group: gi})
		}
		for _, fi := range g.files {
			rv.items = append(rv.items, item{kind: itemHeader, file: fi, group: gi})
			if !rv.files[fi].collapsed {
				rv.items = append(rv.items, item{kind: itemBody, file: fi, group: gi})
			}
		}
	}
}

// showOnly shows the lines of one file, by its index in files, and no other;
// -1 shows every file.
func (rv *review) showOnly(i int) { rv.only = i }

// flat reports whether the files show one at a time, as a commit's and
// Local Changes' do, with no header to fold or view them by.
func (rv *review) flat() bool { return !rv.pr }

// itemOf finds the row of a file's header, or of a group's heading.
func (rv *review) itemOf(kind itemKind, i int) int {
	rv.layoutItems()
	for n, it := range rv.items {
		switch {
		case it.kind != kind:
		case kind == itemGroup && it.group == i, kind != itemGroup && it.file == i:
			return n
		}
	}
	return -1
}

// reveal scrolls the surface to a file's header, unfolding the file.
func (rv *review) reveal(file int) {
	if rv == nil || file < 0 || file >= len(rv.files) {
		return
	}
	if f := rv.files[file]; f.collapsed && !f.viewed {
		f.collapsed = false
	}
	if i := rv.itemOf(itemHeader, file); i >= 0 {
		rv.list.ScrollTo(i, ui.Start)
	}
	rv.current = file
	rv.revealedAt = time.Now()
	rv.jumped = true
}

// autoView marks as viewed the expanded files that top, the file at the top of
// the surface, has scrolled past. The files stay open, so that the lines do not
// move under the reader. A file unmarked is not marked again by scrolling back
// over it, only by reading on from it after a jump.
func (t *repoTab) autoView(rv *review, top int) {
	if rv.jumped {
		rv.seen, rv.jumped = top, false
		return
	}
	for fi := rv.seen; fi < top && fi < len(rv.files); fi++ {
		if f := rv.files[fi]; !f.collapsed && !f.viewed {
			t.markViewed(rv, f, true)
		}
	}
	rv.seen = max(rv.seen, top)
}

func (t *repoTab) setViewed(rv *review, f *reviewFile, on bool) {
	f.collapsed = on
	t.markViewed(rv, f, on)
}

// markViewed sets whether a file is viewed, remembered for the commits or the
// pull request.
func (t *repoTab) markViewed(rv *review, f *reviewFile, on bool) {
	f.viewed = on
	if t.viewed == nil {
		t.viewed = map[string]bool{}
	}
	t.viewed[viewedKey(rv.viewID(), f.path)] = on
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

// forgetStates drops the viewers of the files, which the next frame makes
// anew, as after a setting changed.
func (rv *review) forgetStates() {
	if rv == nil {
		return
	}
	for _, f := range rv.files {
		f.ed, f.edKey, f.lines = nil, "", nil
	}
}
