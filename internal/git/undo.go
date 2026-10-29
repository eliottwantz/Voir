package git

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Snapshot is what an operation can change, as it was at one moment: the
// branches and tags, HEAD, and, when saved, the index and the files of the
// work tree. Two snapshots, taken before and after an operation, are what
// Restore takes to undo it, or to redo it.
type Snapshot struct {
	// Head is the branch checked out ("refs/heads/main"), "" when HEAD is
	// detached; HeadHash is the commit HEAD is at, "" before the first one.
	Head     string
	HeadHash string
	// Refs are the branches and tags, by full name, with the object each
	// names. Remote branches are not in it: undoing a pull leaves what was
	// fetched, and a fetch alone is nothing to undo.
	Refs map[string]string
	// Index is the tree of the index, and Work the tree of every file of the
	// work tree that is not ignored, untracked ones too: both "" unless the
	// snapshot saved them.
	Index, Work string
}

// HasWork reports whether the snapshot saved the index and the files.
func (s Snapshot) HasWork() bool { return s.Work != "" }

// Same reports whether the refs and HEAD are those of o.
func (s Snapshot) Same(o Snapshot) bool {
	return s.Head == o.Head && s.HeadHash == o.HeadHash && maps(s.Refs, o.Refs)
}

func maps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// changed lists the refs whose objects differ between two snapshots.
func changed(a, b Snapshot) []string {
	var names []string
	for n, v := range a.Refs {
		if b.Refs[n] != v {
			names = append(names, n)
		}
	}
	for n := range b.Refs {
		if _, ok := a.Refs[n]; !ok {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	return names
}

// Snapshot reads the refs and HEAD, and with work the index and the files,
// which it writes as trees in the object database.
func (r *Repo) Snapshot(work bool) (Snapshot, error) {
	s := Snapshot{Refs: map[string]string{}}
	out, err := r.Read("for-each-ref", "--format=%(objectname) %(refname)", "refs/heads", "refs/tags")
	if err != nil {
		return s, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if hash, name, ok := strings.Cut(line, " "); ok {
			s.Refs[name] = hash
		}
	}
	s.Head = r.text("symbolic-ref", "-q", "HEAD")
	s.HeadHash = r.text("rev-parse", "-q", "--verify", "HEAD")
	if work {
		if s.Index, s.Work, err = r.saveWork(); err != nil {
			return s, err
		}
	}
	return s, nil
}

// saveWork writes the index as a tree, and the work tree as another: the
// files of a copy of the index to which every change was added.
func (r *Repo) saveWork() (index, work string, err error) {
	out, err := r.Do("write-tree")
	if err != nil {
		return "", "", err
	}
	index = strings.TrimSpace(string(out))
	tmp, err := os.CreateTemp("", "voir-index-*")
	if err != nil {
		return "", "", err
	}
	name := tmp.Name()
	defer os.Remove(name)
	// The copy keeps what the index knows of the files, so that only those
	// changed since are read again.
	if f, err := os.Open(filepath.Join(r.GitDir(), "index")); err == nil {
		_, err = io.Copy(tmp, f)
		f.Close()
		if err != nil {
			tmp.Close()
			return "", "", err
		}
	}
	if err := tmp.Close(); err != nil {
		return "", "", err
	}
	if _, err := r.DoEnv([]string{"GIT_INDEX_FILE=" + name}, "add", "-A", "--", ":/"); err != nil {
		return "", "", err
	}
	out, err = r.DoEnv([]string{"GIT_INDEX_FILE=" + name}, "write-tree")
	if err != nil {
		return "", "", err
	}
	return index, strings.TrimSpace(string(out)), nil
}

// ErrMovedOn is returned by Restore when the repository is no longer as the
// operation left it, so that putting it back would lose what was done since.
var ErrMovedOn = errors.New("the repository changed since")

// Restore takes the repository from the state from, which it must be in, to
// the state to. When both saved their files, the index and the files are
// put back exactly as they were. Else only the refs and HEAD move, and the
// changes not committed stay: with soft, as a commit undone leaves its
// changes staged (git reset --soft); else as a checkout keeps them (git
// reset --keep), which fails rather than overwrite them. why goes in the
// reflog.
func (r *Repo) Restore(to, from Snapshot, soft bool, why string) error {
	if s := r.State(); s != "" {
		return fmt.Errorf("a %s is in progress", s)
	}
	exact := to.HasWork() && from.HasWork()
	cur, err := r.Snapshot(exact)
	if err != nil {
		return err
	}
	names := changed(from, to)
	for _, n := range names {
		if cur.Refs[n] != from.Refs[n] {
			return ErrMovedOn
		}
	}
	headMoves := to.Head != from.Head || to.HeadHash != from.HeadHash
	if (headMoves || exact) && (cur.Head != from.Head || cur.HeadHash != from.HeadHash) {
		return ErrMovedOn
	}
	if exact && (cur.Index != from.Index || cur.Work != from.Work) {
		return ErrMovedOn
	}

	if exact {
		if err := r.setRefs(why, to.Refs, from.Refs, names); err != nil {
			return err
		}
		if headMoves {
			if to.Head != "" {
				_, err = r.Do("symbolic-ref", "-m", why, "HEAD", to.Head)
			} else {
				_, err = r.Do("update-ref", "-m", why, "--no-deref", "HEAD", to.HeadHash)
			}
			if err != nil {
				return err
			}
		}
		// The files untracked now and not in the state restored, which the
		// files put back leave alone: some are to go (a redo deleting them
		// again), and others are only ignored there (its .gitignore keeps
		// them out of its tree), which are kept.
		gone := intersect(r.treeDiff("A", from.Index, from.Work), r.treeDiff("D", from.Work, to.Work))
		for _, args := range [][]string{
			{"read-tree", "--reset", from.Index},
			{"read-tree", "--reset", "-u", to.Work},
			{"read-tree", to.Index},
		} {
			if _, err := r.Do(args...); err != nil {
				return err
			}
		}
		if len(gone) > 0 {
			ignored := map[string]bool{}
			out, _ := r.DoInput(strings.Join(gone, "\x00")+"\x00", "check-ignore", "-z", "--stdin")
			for _, p := range strings.Split(string(out), "\x00") {
				ignored[p] = true
			}
			for _, p := range gone {
				if !ignored[p] {
					os.Remove(filepath.Join(r.Root, filepath.FromSlash(p)))
				}
			}
		}
		r.Do("update-index", "-q", "--refresh")
		return nil
	}

	if !headMoves {
		return r.setRefs(why, to.Refs, from.Refs, names)
	}
	// The branch checked out moves with HEAD, after the others: a branch to
	// go back to may have to be made first, and the one left can only be
	// moved once it is no longer checked out.
	others := slices.DeleteFunc(slices.Clone(names), func(n string) bool { return n == from.Head })
	if err := r.setRefs(why, to.Refs, from.Refs, others); err != nil {
		return err
	}
	var moveErr error
	switch {
	case to.Head != from.Head && to.Head != "":
		_, moveErr = r.Do("checkout", "-q", strings.TrimPrefix(to.Head, "refs/heads/"), "--")
	case to.Head != from.Head:
		_, moveErr = r.Do("checkout", "-q", "--detach", to.HeadHash)
	case to.HeadHash == "":
		// Back to before the first commit: the branch is not born yet.
		_, moveErr = r.Do("update-ref", "-m", why, "-d", from.Head, from.HeadHash)
	case from.HeadHash == "":
		_, moveErr = r.Do("update-ref", "-m", why, "HEAD", to.HeadHash)
	case soft:
		_, moveErr = r.Do("reset", "-q", "--soft", to.HeadHash)
	default:
		_, moveErr = r.Do("reset", "-q", "--keep", to.HeadHash)
	}
	if moveErr != nil {
		r.setRefs(why, from.Refs, to.Refs, others)
		return moveErr
	}
	if to.Head != from.Head && from.Head != "" && to.Refs[from.Head] != from.Refs[from.Head] {
		return r.setRefs(why, to.Refs, from.Refs, []string{from.Head})
	}
	return nil
}

// treeDiff lists the files of a diff between two trees of one kind: "A" for
// those only in b, "D" for those only in a.
func (r *Repo) treeDiff(kind, a, b string) []string {
	out, err := r.Read("diff-tree", "-r", "--no-renames", "--name-only", "-z", "--diff-filter="+kind, a, b)
	if err != nil {
		return nil
	}
	return strings.FieldsFunc(string(out), func(c rune) bool { return c == 0 })
}

func intersect(a, b []string) []string {
	in := map[string]bool{}
	for _, s := range a {
		in[s] = true
	}
	return slices.DeleteFunc(b, func(s string) bool { return !in[s] })
}

// setRefs sets the refs named from the values have to those of want, all or
// none, and fails if one is no longer at the value of have.
func (r *Repo) setRefs(why string, want, have map[string]string, names []string) error {
	var b strings.Builder
	for _, n := range names {
		w, h := want[n], have[n]
		switch {
		case w == h:
		case w == "":
			fmt.Fprintf(&b, "delete %s %s\n", n, h)
		case h == "":
			fmt.Fprintf(&b, "create %s %s\n", n, w)
		default:
			fmt.Fprintf(&b, "update %s %s %s\n", n, w, h)
		}
	}
	if b.Len() == 0 {
		return nil
	}
	_, err := r.DoInput(b.String(), "update-ref", "-m", why, "--stdin")
	return err
}

// ReflogEntry is a move of HEAD, as the reflog keeps it.
type ReflogEntry struct {
	// Selector names the entry, as HEAD@{2}.
	Selector string
	// Action is what moved HEAD ("commit", "checkout", "rebase (finish)",
	// "reset"…), and Message what git said of it.
	Action, Message string
	// Commit is where HEAD went, and Time when.
	Commit Commit
	Time   time.Time
}

// Reflog lists the moves of HEAD, the latest first: at most limit, all of
// them when limit is 0.
func (r *Repo) Reflog(limit int) ([]ReflogEntry, error) {
	if !r.HasHead() {
		return nil, nil
	}
	args := []string{"log", "-g", "--date=unix", "--no-color", "--no-show-signature",
		"--pretty=format:%gd%x1f%gs%x1f" + strings.TrimPrefix(logFormat, "--pretty=format:")}
	if limit > 0 {
		args = append(args, "-n", strconv.Itoa(limit))
	}
	out, err := r.Read(append(args, "HEAD", "--")...)
	if err != nil {
		return nil, err
	}
	var entries []ReflogEntry
	for _, rec := range strings.Split(string(out), recordSep) {
		rec = strings.TrimLeft(rec, "\n")
		sel, rest, ok1 := strings.Cut(rec, fieldSep)
		subject, rest, ok2 := strings.Cut(rest, fieldSep)
		if !ok1 || !ok2 {
			continue
		}
		cs := parseLog([]byte(rest))
		if len(cs) == 0 {
			continue
		}
		e := ReflogEntry{Selector: fmt.Sprintf("HEAD@{%d}", len(entries)), Commit: cs[0], Message: subject}
		// With --date=unix the selector is HEAD@{<seconds>}.
		if i := strings.IndexByte(sel, '{'); i >= 0 {
			e.Time = unix(strings.TrimSuffix(sel[i+1:], "}"))
		}
		if action, msg, ok := strings.Cut(subject, ": "); ok {
			e.Action, e.Message = action, msg
		}
		entries = append(entries, e)
	}
	return entries, nil
}
