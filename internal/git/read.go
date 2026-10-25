package git

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Commit is a commit of the history.
type Commit struct {
	Hash           string
	Parents        []string
	Author         string
	AuthorEmail    string
	AuthorTime     time.Time
	Committer      string
	CommitterEmail string
	CommitTime     time.Time
	Subject        string
	Body           string
}

// Short is the abbreviated hash.
func (c *Commit) Short() string {
	if len(c.Hash) > 7 {
		return c.Hash[:7]
	}
	return c.Hash
}

// IsMerge reports whether the commit joins branches.
func (c *Commit) IsMerge() bool { return len(c.Parents) > 1 }

const (
	fieldSep  = "\x1f"
	recordSep = "\x1e"
)

// logFormat has the fields parseLog reads, which none of a commit's text
// can contain.
const logFormat = "--pretty=format:%H" + "%x1f%P%x1f%aN%x1f%aE%x1f%at%x1f%cN%x1f%cE%x1f%ct%x1f%s%x1f%b%x1e"

// LogOptions say which commits Log lists.
type LogOptions struct {
	// Revs are the revisions to start from; none, with All false, is HEAD.
	Revs []string
	// All lists every branch, tag and stash: Fork's "All Commits".
	All bool
	// FirstParent follows only the first parent of merges.
	FirstParent bool
	// Skip and Limit page the list; a Limit of 0 lists every commit.
	Skip, Limit int
	// Path lists the commits that changed a file or folder.
	Path string
	// Follow follows a file across renames (needs Path).
	Follow bool
	// ByDate orders commits by date rather than topologically.
	ByDate bool
	// Grep keeps the commits whose message matches; Author those by a
	// person; Pickaxe those that added or removed the text.
	Grep, Author, Pickaxe string
	// HideTags and HideStashes leave those refs out of an All listing.
	HideTags, HideStashes bool
}

// Log lists commits, newest first.
func (r *Repo) Log(o LogOptions) ([]Commit, error) {
	if !r.HasHead() && !o.All && len(o.Revs) == 0 {
		return nil, nil
	}
	args := []string{"log", logFormat, "--no-color", "--no-show-signature"}
	if o.ByDate {
		args = append(args, "--date-order")
	} else {
		args = append(args, "--topo-order")
	}
	if o.FirstParent {
		args = append(args, "--first-parent")
	}
	if o.Skip > 0 {
		args = append(args, "--skip="+strconv.Itoa(o.Skip))
	}
	if o.Limit > 0 {
		args = append(args, "-n", strconv.Itoa(o.Limit))
	}
	if o.Grep != "" {
		args = append(args, "-i", "--grep="+o.Grep)
	}
	if o.Author != "" {
		args = append(args, "-i", "--author="+o.Author)
	}
	if o.Pickaxe != "" {
		args = append(args, "-S"+o.Pickaxe)
	}
	if o.Follow && o.Path != "" {
		args = append(args, "--follow")
	}
	switch {
	case o.All:
		args = append(args, "--branches", "--remotes", "HEAD")
		if !o.HideTags {
			args = append(args, "--tags")
		}
		if !o.HideStashes {
			if r.hasStashRef() {
				args = append(args, "refs/stash")
			}
		}
	case len(o.Revs) > 0:
		args = append(args, o.Revs...)
	}
	if o.Path != "" {
		args = append(args, "--", o.Path)
	}
	out, err := r.Read(args...)
	if err != nil {
		return nil, err
	}
	return parseLog(out), nil
}

func (r *Repo) hasStashRef() bool {
	_, err := r.Read("rev-parse", "--verify", "-q", "refs/stash")
	return err == nil
}

func parseLog(out []byte) []Commit {
	var commits []Commit
	for _, rec := range strings.Split(string(out), recordSep) {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.Split(rec, fieldSep)
		if len(f) < 10 {
			continue
		}
		c := Commit{
			Hash:           f[0],
			Parents:        strings.Fields(f[1]),
			Author:         f[2],
			AuthorEmail:    f[3],
			AuthorTime:     unix(f[4]),
			Committer:      f[5],
			CommitterEmail: f[6],
			CommitTime:     unix(f[7]),
			Subject:        f[8],
			Body:           strings.TrimSpace(f[9]),
		}
		commits = append(commits, c)
	}
	return commits
}

func unix(s string) time.Time {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

// Show reads one commit.
func (r *Repo) Show(rev string) (Commit, error) {
	out, err := r.Read("show", "-s", logFormat, "--no-color", rev)
	if err != nil {
		return Commit{}, err
	}
	cs := parseLog(out)
	if len(cs) == 0 {
		return Commit{}, fmt.Errorf("unknown revision %q", rev)
	}
	return cs[0], nil
}

// RefKind tells what a ref is.
type RefKind uint8

const (
	LocalBranch RefKind = iota
	RemoteBranch
	Tag
	StashRef
)

// Ref is a branch or a tag.
type Ref struct {
	Kind RefKind
	// Name is the short name: main, origin/main, v1.0.
	Name string
	// Full is the complete name: refs/heads/main.
	Full string
	// Target is the commit it points to; for an annotated tag, the commit
	// the tag object leads to.
	Target string
	// Head is set for the branch checked out.
	Head bool
	// Upstream is the branch a local branch tracks, "" for none; Ahead and
	// Behind count the commits not yet pushed and not yet pulled.
	Upstream      string
	Ahead, Behind int
	Gone          bool
}

// Remote returns the remote a remote branch is on.
func (f Ref) Remote() string {
	if f.Kind != RemoteBranch {
		return ""
	}
	if i := strings.Index(f.Name, "/"); i > 0 {
		return f.Name[:i]
	}
	return ""
}

// Refs lists the branches and tags.
func (r *Repo) Refs() ([]Ref, error) {
	format := strings.Join([]string{"%(refname)", "%(objectname)", "%(*objectname)", "%(HEAD)", "%(upstream:short)", "%(upstream:track)"}, fieldSep)
	out, err := r.Read("for-each-ref", "--format="+format, "refs/heads", "refs/remotes", "refs/tags")
	if err != nil {
		return nil, err
	}
	var refs []Ref
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, fieldSep)
		if len(f) < 6 {
			continue
		}
		ref := Ref{Full: f[0], Target: f[1], Head: f[3] == "*", Upstream: f[4]}
		if f[2] != "" {
			ref.Target = f[2]
		}
		switch {
		case strings.HasPrefix(f[0], "refs/heads/"):
			ref.Kind, ref.Name = LocalBranch, strings.TrimPrefix(f[0], "refs/heads/")
			ref.Ahead, ref.Behind, ref.Gone = parseTrack(f[5])
		case strings.HasPrefix(f[0], "refs/remotes/"):
			ref.Kind, ref.Name = RemoteBranch, strings.TrimPrefix(f[0], "refs/remotes/")
			if strings.HasSuffix(ref.Name, "/HEAD") {
				continue
			}
		case strings.HasPrefix(f[0], "refs/tags/"):
			ref.Kind, ref.Name = Tag, strings.TrimPrefix(f[0], "refs/tags/")
		default:
			continue
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// parseTrack reads "[ahead 1, behind 2]", "[gone]" and the like.
func parseTrack(s string) (ahead, behind int, gone bool) {
	s = strings.Trim(s, "[]")
	if s == "gone" {
		return 0, 0, true
	}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if v, ok := strings.CutPrefix(part, "ahead "); ok {
			ahead, _ = strconv.Atoi(v)
		}
		if v, ok := strings.CutPrefix(part, "behind "); ok {
			behind, _ = strconv.Atoi(v)
		}
	}
	return
}

// Stash is an entry of the stash.
type Stash struct {
	Index   int
	Name    string // stash@{0}
	Hash    string
	Message string
	Time    time.Time
	// Parents are the commit the stash was made on, then the commits git
	// keeps for its index and its untracked files.
	Parents []string
}

// Stashes lists the stash, newest first.
func (r *Repo) Stashes() ([]Stash, error) {
	out, err := r.Read("stash", "list", "--format=%gd"+fieldSep+"%H"+fieldSep+"%gs"+fieldSep+"%ct"+fieldSep+"%P")
	if err != nil {
		return nil, err
	}
	var list []Stash
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, fieldSep)
		if len(f) < 5 {
			continue
		}
		s := Stash{Name: f[0], Hash: f[1], Message: f[2], Time: unix(f[3]), Parents: strings.Fields(f[4])}
		s.Index = len(list)
		list = append(list, s)
	}
	return list, nil
}

// Remote is a remote repository.
type Remote struct {
	Name     string
	FetchURL string
	PushURL  string
}

// DefaultBranch is the branch the remotes call their default, as origin/HEAD
// says: main. It is "" when no remote HEAD is known.
func (r *Repo) DefaultBranch() string {
	out, err := r.Read("for-each-ref", "--format=%(symref:short)", "refs/remotes/*/HEAD")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if _, name, ok := strings.Cut(strings.TrimSpace(line), "/"); ok && name != "" {
			return name
		}
	}
	return ""
}

// Remotes lists the remotes.
func (r *Repo) Remotes() []Remote {
	out, err := r.Read("remote", "-v")
	if err != nil {
		return nil
	}
	byName := map[string]*Remote{}
	var order []string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		rem := byName[f[0]]
		if rem == nil {
			rem = &Remote{Name: f[0]}
			byName[f[0]] = rem
			order = append(order, f[0])
		}
		if f[2] == "(fetch)" {
			rem.FetchURL = f[1]
		} else {
			rem.PushURL = f[1]
		}
	}
	var list []Remote
	for _, n := range order {
		list = append(list, *byName[n])
	}
	return list
}

// Submodule is a submodule of the repository.
type Submodule struct {
	Path   string
	Hash   string
	Status byte // ' ' up to date, '+' changed, '-' not initialized, 'U' conflicts
}

// Submodules lists the submodules.
func (r *Repo) Submodules() []Submodule {
	out, err := r.Read("submodule", "status")
	if err != nil {
		return nil
	}
	var list []Submodule
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 3 {
			continue
		}
		st := line[0]
		f := strings.Fields(line[1:])
		if len(f) < 2 {
			continue
		}
		list = append(list, Submodule{Path: f[1], Hash: f[0], Status: st})
	}
	return list
}

// Worktree is a linked work tree.
type Worktree struct {
	Path   string
	Branch string
	Head   string
	Main   bool
}

// Worktrees lists the work trees, the main one first.
func (r *Repo) Worktrees() []Worktree {
	out, err := r.Read("worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	var list []Worktree
	var cur *Worktree
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			list = append(list, Worktree{Path: strings.TrimPrefix(line, "worktree "), Main: len(list) == 0})
			cur = &list[len(list)-1]
		case cur == nil:
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	return list
}

// Status is the state of the work tree and the index.
type Status struct {
	Branch   string // "" when detached
	Head     string // the commit of HEAD, "" before the first
	Upstream string
	Ahead    int
	Behind   int
	Entries  []StatusEntry
}

// StatusEntry is one changed path.
type StatusEntry struct {
	Path     string
	OrigPath string // before a rename or copy
	// Index and Work are git's status letters of the index against HEAD
	// and of the work tree against the index: M A D R C T U or '.'.
	Index, Work byte
	Untracked   bool
	Conflict    bool
	Submodule   bool
}

// Staged reports whether the index holds a change of the path.
func (e StatusEntry) Staged() bool { return !e.Untracked && !e.Conflict && e.Index != '.' }

// Unstaged reports whether the work tree holds a change not in the index.
func (e StatusEntry) Unstaged() bool { return e.Untracked || e.Conflict || e.Work != '.' }

// Status reads the status, untracked files included.
func (r *Repo) Status() (*Status, error) {
	out, err := r.Read("-c", "core.quotePath=false", "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return nil, err
	}
	return parseStatus(out), nil
}

func parseStatus(out []byte) *Status {
	st := &Status{}
	parts := bytes.Split(out, []byte{0})
	for i := 0; i < len(parts); i++ {
		line := string(parts[i])
		if line == "" {
			continue
		}
		switch line[0] {
		case '#':
			f := strings.Fields(line)
			if len(f) < 3 {
				continue
			}
			switch f[1] {
			case "branch.oid":
				if f[2] != "(initial)" {
					st.Head = f[2]
				}
			case "branch.head":
				if f[2] != "(detached)" {
					st.Branch = f[2]
				}
			case "branch.upstream":
				st.Upstream = f[2]
			case "branch.ab":
				if len(f) >= 4 {
					st.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f[2], "+"))
					st.Behind, _ = strconv.Atoi(strings.TrimPrefix(f[3], "-"))
				}
			}
		case '1':
			// 1 XY sub mH mI mW hH hI path
			f := strings.SplitN(line, " ", 9)
			if len(f) < 9 {
				continue
			}
			st.Entries = append(st.Entries, StatusEntry{Path: f[8], Index: f[1][0], Work: f[1][1], Submodule: strings.HasPrefix(f[2], "S")})
		case '2':
			// 2 XY sub mH mI mW hH hI Xscore path \0 orig
			f := strings.SplitN(line, " ", 10)
			if len(f) < 10 {
				continue
			}
			e := StatusEntry{Path: f[9], Index: f[1][0], Work: f[1][1]}
			if i+1 < len(parts) {
				e.OrigPath = string(parts[i+1])
				i++
			}
			st.Entries = append(st.Entries, e)
		case 'u':
			// u XY sub m1 m2 m3 mW h1 h2 h3 path
			f := strings.SplitN(line, " ", 11)
			if len(f) < 11 {
				continue
			}
			st.Entries = append(st.Entries, StatusEntry{Path: f[10], Index: f[1][0], Work: f[1][1], Conflict: true})
		case '?':
			st.Entries = append(st.Entries, StatusEntry{Path: line[2:], Index: '.', Work: '?', Untracked: true})
		}
	}
	return st
}
