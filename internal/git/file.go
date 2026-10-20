package git

import (
	"fmt"
	"strconv"
	"strings"
)

// FileCommit is a commit that changed a file, with what it did to it.
type FileCommit struct {
	Commit
	// Path is the file's name in the commit, which a rename makes differ
	// from its name today; OldPath is its name before a rename.
	Path, OldPath string
	// Status is 'A', 'M', 'D' or 'R'.
	Status byte
}

// FileHistory lists the commits that changed a file, newest first, following
// it across renames.
func (r *Repo) FileHistory(path string, limit int) ([]FileCommit, error) {
	commits, err := r.Log(LogOptions{Path: path, Follow: true, Limit: limit})
	if err != nil {
		return nil, err
	}
	// The same walk with the files each commit touched says the name the file
	// had in it, and what the commit did.
	args := []string{"log", "--follow", "--name-status", "--topo-order", "--format=%x1e%H", "--no-color", "--no-show-signature"}
	if limit > 0 {
		args = append(args, "-n", strconv.Itoa(limit))
	}
	out, err := r.Read(append(args, "--", path)...)
	type seen struct {
		path, old string
		status    byte
	}
	byHash := map[string]seen{}
	if err == nil {
		for _, rec := range strings.Split(string(out), recordSep) {
			// The hash, then a line for the file: its status and its name, or
			// the names before and after a rename.
			var lines []string
			for _, l := range strings.Split(rec, "\n") {
				if l = strings.TrimSpace(l); l != "" {
					lines = append(lines, l)
				}
			}
			if len(lines) < 2 {
				continue
			}
			f := strings.Split(lines[1], "\t")
			s := seen{status: f[0][0]}
			switch {
			case len(f) >= 3 && (s.status == 'R' || s.status == 'C'):
				s.status, s.old, s.path = 'R', f[1], f[2]
			case len(f) >= 2:
				s.path = f[1]
			}
			byHash[lines[0]] = s
		}
	}
	files := make([]FileCommit, len(commits))
	for i, c := range commits {
		s, ok := byHash[c.Hash]
		if !ok {
			s = seen{path: path, status: 'M'}
		}
		files[i] = FileCommit{Commit: c, Path: s.path, OldPath: s.old, Status: s.status}
	}
	return files, nil
}

// LineMap says where the lines of a file's new version were in its old one,
// as a patch between them tells.
type LineMap struct {
	// inHunk maps a new line to its old line, 0 for an added one.
	inHunk map[int]int
	// shifts are the hunks, to place the lines between them.
	shifts []lineShift
}

type lineShift struct{ newEnd, delta int }

// ParseLineMap reads the hunks of the patch of one file.
func ParseLineMap(patch string) *LineMap {
	m := &LineMap{inHunk: map[int]int{}}
	var oldAt, newAt int
	delta := 0
	inside := false
	for _, l := range strings.Split(patch, "\n") {
		if strings.HasPrefix(l, "@@") {
			start, on, ns, nn := hunkRange(l)
			// The lines before the hunk are shifted by what the hunks before
			// it changed.
			m.shifts = append(m.shifts, lineShift{newEnd: ns - 1, delta: delta})
			oldAt, newAt = start, ns
			delta += nn - on
			inside = true
			continue
		}
		if !inside || l == "" {
			continue
		}
		switch l[0] {
		case ' ':
			m.inHunk[newAt] = oldAt
			oldAt++
			newAt++
		case '+':
			m.inHunk[newAt] = 0
			newAt++
		case '-':
			oldAt++
		}
	}
	m.shifts = append(m.shifts, lineShift{newEnd: 1 << 30, delta: delta})
	return m
}

// hunkRange reads "@@ -a,b +c,d @@".
func hunkRange(l string) (start, on, ns, nn int) {
	fields := strings.Fields(l)
	if len(fields) < 3 {
		return
	}
	read := func(s string) (int, int) {
		start, n, found := strings.Cut(strings.TrimLeft(s, "-+"), ",")
		a, _ := strconv.Atoi(start)
		b := 1
		if found {
			b, _ = strconv.Atoi(n)
		}
		return a, b
	}
	start, on = read(fields[1])
	ns, nn = read(fields[2])
	return
}

// Old is the line of the old version that a line of the new one is, and
// whether the line was added by the patch, which has no old line.
func (m *LineMap) Old(line int) (old int, added bool) {
	if o, ok := m.inHunk[line]; ok {
		return o, o == 0
	}
	for _, s := range m.shifts {
		if line <= s.newEnd {
			return line - s.delta, false
		}
	}
	return line, false
}

// RestoreFile puts a file back to what it was in a revision, in the work
// tree only.
func (r *Repo) RestoreFile(rev, path string) error {
	_, err := r.Do("restore", "--source="+rev, "--worktree", "--", path)
	return err
}

// StashPushPaths saves the changes to some files to a new stash.
func (r *Repo) StashPushPaths(message string, untracked bool, paths []string) error {
	if len(paths) == 0 {
		return fmt.Errorf("no file to stash")
	}
	args := []string{"stash", "push"}
	if untracked {
		args = append(args, "--include-untracked")
	}
	if message != "" {
		args = append(args, "-m", message)
	}
	_, err := r.Do(append(append(args, "--"), paths...)...)
	return err
}

// WorkPatch is the patch of the changes to some files, staged or not, against
// HEAD, with the untracked ones added whole.
func (r *Repo) WorkPatch(paths []string, untracked map[string]bool) (string, error) {
	var b strings.Builder
	var tracked []string
	for _, p := range paths {
		if untracked[p] {
			patch, err := r.UntrackedDiff(p)
			if err != nil {
				return "", err
			}
			b.WriteString(patch)
			continue
		}
		tracked = append(tracked, p)
	}
	if len(tracked) > 0 {
		args := []string{"diff", "--no-color", "--no-ext-diff", "--binary"}
		if r.HasHead() {
			args = append(args, "HEAD")
		} else {
			args = append(args, "--cached")
		}
		out, err := r.Read(append(args, append([]string{"--"}, tracked...)...)...)
		if err != nil {
			return "", err
		}
		b.Write(out)
	}
	return b.String(), nil
}

// PathsDiff is the patch between two revisions of some paths, which a rename
// among them shows as one.
func (r *Repo) PathsDiff(a, b string, o DiffOptions, paths ...string) (string, error) {
	args := append(o.args(), a, b, "--")
	out, err := r.Read(append(args, paths...)...)
	return limit(out), err
}
