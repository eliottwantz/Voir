package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newRepo makes a repository with a user and a first commit.
func newRepo(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	r, err := Init(filepath.Join(dir, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][2]string{{"user.name", "Test"}, {"user.email", "t@example.com"}, {"init.defaultBranch", "main"}, {"commit.gpgsign", "false"}} {
		if err := r.SetConfig(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	r.Do("checkout", "-q", "-b", "main")
	return r
}

func write(t *testing.T, r *Repo, name, text string) {
	t.Helper()
	p := filepath.Join(r.Root, name)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, r *Repo, msg string) {
	t.Helper()
	if err := r.Stage(); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(CommitOptions{Message: msg}); err != nil {
		t.Fatal(err)
	}
}

func TestStatusAndStage(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "one\n")
	st, err := r.Status()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Entries) != 1 || !st.Entries[0].Untracked || st.Branch != "main" {
		t.Fatalf("status = %+v", st)
	}
	if err := r.Stage("a.txt"); err != nil {
		t.Fatal(err)
	}
	st, _ = r.Status()
	if !st.Entries[0].Staged() || st.Entries[0].Unstaged() {
		t.Fatalf("after stage: %+v", st.Entries[0])
	}
	if err := r.Unstage("a.txt"); err != nil { // before the first commit
		t.Fatal(err)
	}
	st, _ = r.Status()
	if !st.Entries[0].Untracked {
		t.Fatalf("after unstage: %+v", st.Entries[0])
	}
}

func TestLogRefsAndDiffs(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "one\ntwo\nthree\n")
	commit(t, r, "first\n\nwith a body")
	write(t, r, "a.txt", "one\n2\nthree\n")
	commit(t, r, "second")
	if err := r.CreateTag("v1", "HEAD", ""); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateBranch("feature", "HEAD~1", false, KeepChanges); err != nil {
		t.Fatal(err)
	}

	cs, err := r.Log(LogOptions{All: true})
	if err != nil || len(cs) != 2 {
		t.Fatalf("log = %v, %v", cs, err)
	}
	if cs[0].Subject != "second" || cs[1].Body != "with a body" || len(cs[0].Parents) != 1 {
		t.Fatalf("log = %+v", cs)
	}
	refs, _ := r.Refs()
	kinds := map[string]RefKind{}
	for _, f := range refs {
		kinds[f.Name] = f.Kind
	}
	if len(kinds) != 3 || kinds["v1"] != Tag || kinds["feature"] != LocalBranch {
		t.Fatalf("refs = %+v", refs)
	}

	ch, err := r.Changes(&cs[0])
	if err != nil || len(ch) != 1 || ch[0].Path != "a.txt" || ch[0].Status != 'M' {
		t.Fatalf("changes = %+v, %v", ch, err)
	}
	patch, err := r.CommitDiff(&cs[0], "a.txt", DiffOptions{})
	if err != nil || !strings.Contains(patch, "-two") || !strings.Contains(patch, "+2") {
		t.Fatalf("patch = %q, %v", patch, err)
	}
	// The first commit diffs against the empty tree.
	ch, _ = r.Changes(&cs[1])
	if len(ch) != 1 || ch[0].Status != 'A' {
		t.Fatalf("root changes = %+v", ch)
	}
}

func TestHunkStaging(t *testing.T) {
	r := newRepo(t)
	var b strings.Builder
	for i := 1; i <= 40; i++ {
		b.WriteString("line ")
		b.WriteString(string(rune('a' + i%26)))
		b.WriteString("\n")
	}
	base := b.String()
	write(t, r, "f.txt", base)
	commit(t, r, "base")
	lines := strings.Split(base, "\n")
	lines[1] = "CHANGED TOP"
	lines[36] = "CHANGED BOTTOM"
	write(t, r, "f.txt", strings.Join(lines, "\n"))

	patch, err := r.WorkDiff("f.txt", DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, hunks := SplitPatch(patch)
	if len(hunks) != 2 {
		t.Fatalf("hunks = %d, want 2\n%s", len(hunks), patch)
	}
	if err := r.StageHunk(patch, 1); err != nil {
		t.Fatal(err)
	}
	staged, _ := r.StagedDiff("f.txt", DiffOptions{})
	if !strings.Contains(staged, "CHANGED BOTTOM") || strings.Contains(staged, "CHANGED TOP") {
		t.Fatalf("staged:\n%s", staged)
	}
	rest, _ := r.WorkDiff("f.txt", DiffOptions{})
	if strings.Contains(rest, "CHANGED BOTTOM") || !strings.Contains(rest, "CHANGED TOP") {
		t.Fatalf("unstaged:\n%s", rest)
	}
	if err := r.UnstageHunk(staged, 0); err != nil {
		t.Fatal(err)
	}
	staged, _ = r.StagedDiff("f.txt", DiffOptions{})
	if strings.TrimSpace(staged) != "" {
		t.Fatalf("still staged:\n%s", staged)
	}
}

func TestStashBranchMergeReset(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "1\n")
	commit(t, r, "one")
	write(t, r, "a.txt", "2\n")
	if err := r.StashPush("wip", false); err != nil {
		t.Fatal(err)
	}
	ss, _ := r.Stashes()
	if len(ss) != 1 || !strings.Contains(ss[0].Message, "wip") {
		t.Fatalf("stashes = %+v", ss)
	}
	if err := r.StashPop(ss[0].Name); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(r.Root, "a.txt")); string(b) != "2\n" {
		t.Fatalf("a.txt = %q", b)
	}
	commit(t, r, "two")

	if err := r.CreateBranch("topic", "HEAD", true, KeepChanges); err != nil {
		t.Fatal(err)
	}
	write(t, r, "b.txt", "b\n")
	commit(t, r, "topic work")
	if err := r.Checkout("main", KeepChanges); err != nil {
		t.Fatal(err)
	}
	if err := r.Merge("topic", MergeOptions{NoFastForward: true}); err != nil {
		t.Fatal(err)
	}
	cs, _ := r.Log(LogOptions{})
	if !cs[0].IsMerge() {
		t.Fatalf("head is not a merge: %+v", cs[0])
	}
	if err := r.Reset("HEAD~1", ResetHard); err != nil {
		t.Fatal(err)
	}
	if cs, _ = r.Log(LogOptions{}); cs[0].Subject != "two" {
		t.Fatalf("after reset: %q", cs[0].Subject)
	}
	if err := r.DeleteBranch("topic", true); err != nil {
		t.Fatal(err)
	}
}

func TestUntrackedDiffAndBlame(t *testing.T) {
	r := newRepo(t)
	write(t, r, "n.txt", "hello\nworld\n")
	p, err := r.UntrackedDiff("n.txt")
	if err != nil || !strings.Contains(p, "+hello") || !strings.Contains(p, "new file mode") {
		t.Fatalf("untracked diff = %q, %v", p, err)
	}
	commit(t, r, "add n")
	bl, err := r.Blame("", "n.txt")
	if err != nil || len(bl) != 2 || bl[0].Text != "hello" || bl[0].Author != "Test" {
		t.Fatalf("blame = %+v, %v", bl, err)
	}
}

func TestParseStatusRename(t *testing.T) {
	out := []byte("# branch.oid abc\x00# branch.head main\x00# branch.upstream origin/main\x00# branch.ab +2 -1\x00" +
		"2 R. N... 100644 100644 100644 aaa bbb R100 new.txt\x00old.txt\x00" +
		"u UU N... 100644 100644 100644 100644 a b c conflict.txt\x00? x y.txt\x00")
	st := parseStatus(out)
	if st.Ahead != 2 || st.Behind != 1 || st.Upstream != "origin/main" || len(st.Entries) != 3 {
		t.Fatalf("status = %+v", st)
	}
	if e := st.Entries[0]; e.Path != "new.txt" || e.OrigPath != "old.txt" || e.Index != 'R' {
		t.Fatalf("rename = %+v", e)
	}
	if !st.Entries[1].Conflict || st.Entries[2].Path != "x y.txt" {
		t.Fatalf("entries = %+v", st.Entries)
	}
}

func TestMain(m *testing.M) {
	if _, err := exec.LookPath("git"); err != nil {
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestCommitPatchSplitsIntoFiles(t *testing.T) {
	r := newRepo(t)
	write(t, r, "keep.txt", "one\ntwo\n")
	write(t, r, "gone.txt", "bye\n")
	write(t, r, "old name.txt", "same\ncontent\nhere\nfor\nrename\n")
	commit(t, r, "base")
	write(t, r, "keep.txt", "one\n2\nthree\n")
	os.Remove(filepath.Join(r.Root, "gone.txt"))
	write(t, r, "new.txt", "hello\n")
	os.Rename(filepath.Join(r.Root, "old name.txt"), filepath.Join(r.Root, "renamed.txt"))
	write(t, r, "bin.dat", "a\x00b\x00")
	commit(t, r, "mixed")
	cs, _ := r.Log(LogOptions{Limit: 1})
	patch, err := r.CommitPatch(&cs[0], DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	parts := SplitPatchFiles(patch)
	byPath := map[string]FilePart{}
	for _, p := range parts {
		byPath[p.Path] = p
	}
	if len(parts) != 5 {
		t.Fatalf("parts = %d: %+v", len(parts), parts)
	}
	if k := byPath["keep.txt"]; k.Status != 'M' || k.Added != 2 || k.Deleted != 1 {
		t.Errorf("keep.txt = %+v", k)
	}
	if g := byPath["gone.txt"]; g.Status != 'D' || g.Deleted != 1 {
		t.Errorf("gone.txt = %+v", g)
	}
	if n := byPath["new.txt"]; n.Status != 'A' || n.Added != 1 {
		t.Errorf("new.txt = %+v", n)
	}
	if rn := byPath["renamed.txt"]; rn.Status != 'R' || rn.OldPath != "old name.txt" {
		t.Errorf("renamed.txt = %+v", rn)
	}
	if b := byPath["bin.dat"]; !b.Binary {
		t.Errorf("bin.dat = %+v", b)
	}
	// Each part is a patch of its own.
	if !strings.HasPrefix(byPath["keep.txt"].Patch, "diff --git a/keep.txt b/keep.txt") || strings.Contains(byPath["keep.txt"].Patch, "gone.txt") {
		t.Errorf("keep.txt patch = %q", byPath["keep.txt"].Patch)
	}
}

func TestLooksGenerated(t *testing.T) {
	for p, want := range map[string]bool{
		"go.sum": true, "web/package-lock.json": true, "dist/app.min.js": true, "api/user.pb.go": true,
		"src/__snapshots__/a.snap": true, "main.go": false, "docs/guide.md": false, "src/map.importmap": false, "a.js.map": true,
	} {
		if got := LooksGenerated(p); got != want {
			t.Errorf("LooksGenerated(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestAmendChangesAreTheFilesOfTheLastCommitOnceAmended(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "a\n")
	commit(t, r, "first")
	write(t, r, "b.txt", "b\n")
	write(t, r, "c.txt", "c\n")
	commit(t, r, "second")
	paths := func() map[string]byte {
		list, err := r.AmendChanges()
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]byte{}
		for _, ch := range list {
			m[ch.Path] = ch.Status
		}
		return m
	}
	if got := paths(); len(got) != 2 || got["b.txt"] != 'A' || got["c.txt"] != 'A' {
		t.Fatalf("amend changes = %v", got)
	}
	// What is staged on top joins the commit's files.
	write(t, r, "a.txt", "a2\n")
	r.Stage("a.txt")
	if got := paths(); len(got) != 3 || got["a.txt"] != 'M' {
		t.Fatalf("amend changes with a.txt staged = %v", got)
	}
	patch, err := r.AmendDiff("b.txt", DiffOptions{})
	if err != nil || !strings.Contains(patch, "+b") {
		t.Fatalf("diff of b.txt = %q, %v", patch, err)
	}
	// Taking a file out of the commit leaves it in the work tree.
	if err := r.UnstageAmend("b.txt"); err != nil {
		t.Fatal(err)
	}
	if got := paths(); len(got) != 2 || got["b.txt"] != 0 {
		t.Fatalf("amend changes after unstaging b.txt = %v", got)
	}
	if _, err := os.Stat(filepath.Join(r.Root, "b.txt")); err != nil {
		t.Errorf("b.txt left the work tree: %v", err)
	}
}

func TestAmendingTheFirstCommitMeasuresFromTheEmptyTree(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "a\n")
	write(t, r, "b.txt", "b\n")
	commit(t, r, "first")
	list, err := r.AmendChanges()
	if err != nil || len(list) != 2 {
		t.Fatalf("amend changes = %+v, %v", list, err)
	}
	if err := r.UnstageAmend("b.txt"); err != nil {
		t.Fatal(err)
	}
	if list, _ = r.AmendChanges(); len(list) != 1 || list[0].Path != "a.txt" {
		t.Fatalf("amend changes after unstaging b.txt = %+v", list)
	}
}
