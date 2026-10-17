package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// EmptyTree is the hash of git's empty tree, the base of a repository
// without commits.
const EmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// maxPatch is the size beyond which a file's lines are not shown.
const maxPatch = 4 << 20

// Change is a file changed by a commit or between two revisions.
type Change struct {
	Path     string
	OrigPath string
	Status   byte // A M D R C T
	Score    int
}

// Changes lists the files a commit changed against its first parent (against
// the empty tree for the first commit).
func (r *Repo) Changes(c *Commit) ([]Change, error) {
	base := EmptyTree
	if len(c.Parents) > 0 {
		base = c.Parents[0]
	}
	return r.ChangesBetween(base, c.Hash)
}

// ChangesBetween lists the files that differ between two revisions.
func (r *Repo) ChangesBetween(a, b string) ([]Change, error) {
	out, err := r.Read("-c", "core.quotePath=false", "diff", "--name-status", "-z", "-M", "--no-color", "--no-ext-diff", a, b, "--")
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}

func parseNameStatus(out []byte) []Change {
	var list []Change
	parts := strings.Split(string(out), "\x00")
	for i := 0; i < len(parts); i++ {
		st := parts[i]
		if st == "" {
			continue
		}
		ch := Change{Status: st[0]}
		if len(st) > 1 {
			fmt.Sscanf(st[1:], "%d", &ch.Score)
		}
		if (st[0] == 'R' || st[0] == 'C') && i+2 < len(parts)+0 {
			if i+2 >= len(parts) {
				break
			}
			ch.OrigPath, ch.Path = parts[i+1], parts[i+2]
			i += 2
		} else {
			if i+1 >= len(parts) {
				break
			}
			ch.Path = parts[i+1]
			i++
		}
		list = append(list, ch)
	}
	return list
}

// diffBase are the options of every diff: no colors, external tools or
// prefixes of the user's configuration, and renames detected.
func diffBase(context int, ignoreSpace bool) []string {
	args := []string{"-c", "core.quotePath=false", "diff", "--no-color", "--no-ext-diff", "--no-textconv",
		"--src-prefix=a/", "--dst-prefix=b/", "-M", fmt.Sprintf("-U%d", context), "--submodule=short"}
	if ignoreSpace {
		args = append(args, "-w")
	}
	return args
}

// DiffOptions change how patches are read.
type DiffOptions struct {
	Context     int // lines around changes; 0 is 3
	IgnoreSpace bool
}

func (o DiffOptions) args() []string {
	c := o.Context
	if c == 0 {
		c = 3
	}
	return diffBase(c, o.IgnoreSpace)
}

// WorkDiff is the patch of a file in the work tree against the index.
func (r *Repo) WorkDiff(path string, o DiffOptions) (string, error) {
	out, err := r.Read(append(o.args(), "--", path)...)
	return limit(out), err
}

// StagedDiff is the patch of a file in the index against HEAD.
func (r *Repo) StagedDiff(path string, o DiffOptions) (string, error) {
	args := append(o.args(), "--cached")
	if !r.HasHead() {
		args = append(args, EmptyTree)
	}
	out, err := r.Read(append(args, "--", path)...)
	return limit(out), err
}

// UntrackedDiff is the patch that adds an untracked file.
func (r *Repo) UntrackedDiff(path string) (string, error) {
	cmd := exec.CommandContext(context.Background(), binary(), "-c", "core.quotePath=false", "diff", "--no-color", "--no-ext-diff",
		"--no-index", "--src-prefix=a/", "--dst-prefix=b/", "--", os.DevNull, path)
	cmd.Dir = r.Root
	cmd.Env = env(true)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	var ee *exec.ExitError
	if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 1) {
		return "", err
	}
	return limit(stdout.Bytes()), nil
}

// CommitDiff is the patch of one file of a commit against its first parent.
func (r *Repo) CommitDiff(c *Commit, path string, o DiffOptions) (string, error) {
	base := EmptyTree
	if len(c.Parents) > 0 {
		base = c.Parents[0]
	}
	return r.DiffBetween(base, c.Hash, path, o)
}

// DiffBetween is the patch of one file (all of them for "") between two
// revisions.
func (r *Repo) DiffBetween(a, b, path string, o DiffOptions) (string, error) {
	args := append(o.args(), a, b, "--")
	if path != "" {
		args = append(args, path)
	}
	out, err := r.Read(args...)
	return limit(out), err
}

// Binary reports whether a patch says the file is binary.
func Binary(patch string) bool {
	return strings.Contains(patch, "\nBinary files ") || strings.HasPrefix(patch, "Binary files ")
}

func limit(out []byte) string {
	if len(out) > maxPatch {
		return ""
	}
	return string(out)
}

// SplitPatch cuts the patch of one file into its header and its hunks.
func SplitPatch(patch string) (header string, hunks []string) {
	lines := strings.SplitAfter(patch, "\n")
	var cur strings.Builder
	var head strings.Builder
	inHunk := false
	for _, l := range lines {
		if strings.HasPrefix(l, "@@") {
			if inHunk {
				hunks = append(hunks, cur.String())
				cur.Reset()
			}
			inHunk = true
		}
		if inHunk {
			cur.WriteString(l)
		} else {
			head.WriteString(l)
		}
	}
	if inHunk {
		hunks = append(hunks, cur.String())
	}
	return head.String(), hunks
}

// ApplyPatch applies a patch to the index (cached), the work tree, or both,
// optionally in reverse.
func (r *Repo) ApplyPatch(patch string, cached, reverse bool) error {
	args := []string{"apply", "--recount", "--whitespace=nowarn"}
	if cached {
		args = append(args, "--cached")
	}
	if reverse {
		args = append(args, "-R")
	}
	_, err := r.DoInput(patch, append(args, "-")...)
	return err
}

// StageHunk stages one hunk of a file's work tree patch.
func (r *Repo) StageHunk(patch string, hunk int) error {
	head, hunks := SplitPatch(patch)
	if hunk < 0 || hunk >= len(hunks) {
		return fmt.Errorf("no hunk %d", hunk)
	}
	return r.ApplyPatch(head+hunks[hunk], true, false)
}

// UnstageHunk takes one hunk of a file's staged patch out of the index.
func (r *Repo) UnstageHunk(patch string, hunk int) error {
	head, hunks := SplitPatch(patch)
	if hunk < 0 || hunk >= len(hunks) {
		return fmt.Errorf("no hunk %d", hunk)
	}
	return r.ApplyPatch(head+hunks[hunk], true, true)
}

// DiscardHunk throws away one hunk of a file's work tree patch.
func (r *Repo) DiscardHunk(patch string, hunk int) error {
	head, hunks := SplitPatch(patch)
	if hunk < 0 || hunk >= len(hunks) {
		return fmt.Errorf("no hunk %d", hunk)
	}
	return r.ApplyPatch(head+hunks[hunk], false, true)
}

// ReadFile reads a file of a revision; rev "" reads the index.
func (r *Repo) ReadFile(rev, path string) ([]byte, error) {
	return r.Read("show", rev+":"+path)
}

// WorkFile reads a file of the work tree.
func (r *Repo) WorkFile(path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(r.Root, path))
}

// TreeEntry is a file or folder of a revision's tree.
type TreeEntry struct {
	Path string
	Dir  bool
}

// Tree lists every file of a revision.
func (r *Repo) Tree(rev string) ([]string, error) {
	out, err := r.Read("-c", "core.quotePath=false", "ls-tree", "-r", "-z", "--name-only", rev)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			files = append(files, p)
		}
	}
	return files, nil
}

// BlameLine is a line of a file with the commit that last changed it.
type BlameLine struct {
	Hash    string
	Author  string
	Time    int64
	Text    string
	Line    int
	Summary string
}

// Blame annotates a file of a revision ("" for the work tree).
func (r *Repo) Blame(rev, path string) ([]BlameLine, error) {
	args := []string{"blame", "--porcelain"}
	if rev != "" {
		args = append(args, rev)
	}
	out, err := r.Read(append(args, "--", path)...)
	if err != nil {
		return nil, err
	}
	type meta struct {
		author, summary string
		time            int64
	}
	metas := map[string]*meta{}
	var lines []BlameLine
	var cur string
	var lineNo int
	for _, l := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(l, "\t"):
			m := metas[cur]
			lines = append(lines, BlameLine{Hash: cur, Author: m.author, Time: m.time, Summary: m.summary, Text: l[1:], Line: lineNo})
		case len(l) >= 40 && isHex(l[:40]) && (len(l) == 40 || l[40] == ' '):
			f := strings.Fields(l)
			cur = f[0]
			if len(f) >= 3 {
				fmt.Sscanf(f[2], "%d", &lineNo)
			}
			if metas[cur] == nil {
				metas[cur] = &meta{}
			}
		case strings.HasPrefix(l, "author "):
			metas[cur].author = strings.TrimPrefix(l, "author ")
		case strings.HasPrefix(l, "author-time "):
			fmt.Sscanf(strings.TrimPrefix(l, "author-time "), "%d", &metas[cur].time)
		case strings.HasPrefix(l, "summary "):
			metas[cur].summary = strings.TrimPrefix(l, "summary ")
		}
	}
	return lines, nil
}

func isHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// maxCommitPatch is the size of a whole commit's patch beyond which none of
// it is read.
const maxCommitPatch = 64 << 20

// CommitPatch is the patch of every file of a commit against its first
// parent (the empty tree for the first commit).
func (r *Repo) CommitPatch(c *Commit, o DiffOptions) (string, error) {
	base := EmptyTree
	if len(c.Parents) > 0 {
		base = c.Parents[0]
	}
	out, err := r.Read(append(o.args(), base, c.Hash, "--")...)
	if len(out) > maxCommitPatch {
		return "", fmt.Errorf("the patch of this commit is larger than %d MB", maxCommitPatch>>20)
	}
	return string(out), err
}

// FilePart is the patch of one file out of a patch of many.
type FilePart struct {
	Path    string
	OldPath string // before a rename or copy
	// Status is A, D, R, C or M.
	Status  byte
	Binary  bool
	Patch   string
	Added   int
	Deleted int
}

// SplitPatchFiles cuts a patch of many files into one part per file.
func SplitPatchFiles(patch string) []FilePart {
	var parts []FilePart
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		parts = append(parts, parseFilePart(patch[start:end]))
	}
	for i := 0; i < len(patch); {
		if strings.HasPrefix(patch[i:], "diff --git ") {
			flush(i)
			start = i
		}
		j := strings.IndexByte(patch[i:], '\n')
		if j < 0 {
			break
		}
		i += j + 1
	}
	flush(len(patch))
	return parts
}

func parseFilePart(text string) FilePart {
	p := FilePart{Patch: text, Status: 'M'}
	inHunk := false
	for _, l := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(l, "@@"):
			inHunk = true
		case inHunk && strings.HasPrefix(l, "+"):
			p.Added++
		case inHunk && strings.HasPrefix(l, "-"):
			p.Deleted++
		case inHunk:
		case strings.HasPrefix(l, "diff --git "):
			// diff --git a/old b/new: the two names, when neither has a space.
			rest := strings.TrimPrefix(l, "diff --git ")
			if i := strings.Index(rest, " b/"); i >= 0 {
				p.OldPath = strings.TrimPrefix(rest[:i], "a/")
				p.Path = rest[i+3:]
			}
		case strings.HasPrefix(l, "new file mode"):
			p.Status = 'A'
		case strings.HasPrefix(l, "deleted file mode"):
			p.Status = 'D'
		case strings.HasPrefix(l, "rename from "):
			p.Status, p.OldPath = 'R', strings.TrimPrefix(l, "rename from ")
		case strings.HasPrefix(l, "rename to "):
			p.Path = strings.TrimPrefix(l, "rename to ")
		case strings.HasPrefix(l, "copy from "):
			p.Status, p.OldPath = 'C', strings.TrimPrefix(l, "copy from ")
		case strings.HasPrefix(l, "copy to "):
			p.Path = strings.TrimPrefix(l, "copy to ")
		case strings.HasPrefix(l, "Binary files "), strings.HasPrefix(l, "GIT binary patch"):
			p.Binary = true
		case strings.HasPrefix(l, "+++ b/") && p.Path == "":
			p.Path = strings.TrimPrefix(l, "+++ b/")
		case strings.HasPrefix(l, "--- a/") && p.OldPath == "":
			p.OldPath = strings.TrimPrefix(l, "--- a/")
		}
	}
	if p.Status == 'D' {
		p.Path = p.OldPath
	}
	if p.OldPath == "" {
		p.OldPath = p.Path
	}
	return p
}
