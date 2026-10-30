package git

import (
	"fmt"
	"strings"
	"testing"
)

func lines(n int, change map[int]string) string {
	var s strings.Builder
	for i := 1; i <= n; i++ {
		if v, ok := change[i]; ok {
			s.WriteString(v + "\n")
			continue
		}
		fmt.Fprintf(&s, "%d\n", i)
	}
	return s.String()
}

func TestRangeDiffTellsHowEachCommitChanged(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "a\n")
	commit(t, r, "Base")
	base, _ := r.Resolve("HEAD")
	write(t, r, "x.txt", lines(20, nil))
	commit(t, r, "One")
	write(t, r, "y.txt", lines(30, nil))
	commit(t, r, "Two")
	write(t, r, "z.txt", "z\n")
	commit(t, r, "Dropped later")
	old, _ := r.Resolve("HEAD")

	// The branch is written again: One as it was, Two changed, the last
	// dropped, and one more.
	if _, err := r.Do("reset", "-q", "--hard", base); err != nil {
		t.Fatal(err)
	}
	write(t, r, "x.txt", lines(20, nil))
	commit(t, r, "One")
	write(t, r, "y.txt", lines(30, map[int]string{15: "fifteen"}))
	commit(t, r, "Two")
	write(t, r, "w.txt", lines(10, nil))
	commit(t, r, "Added")
	head, _ := r.Resolve("HEAD")

	if r.IsAncestor(old, head) || !r.IsAncestor(base, head) {
		t.Fatal("IsAncestor is wrong")
	}
	pairs, err := r.RangeDiff(base, old, base, head)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range pairs {
		got = append(got, fmt.Sprintf("%d%c%d %s", p.OldN, p.Status, p.NewN, p.Subject))
	}
	want := []string{"1=1 One", "2!2 Two", "3<0 Dropped later", "0>3 Added"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("pairs %q, want %q", got, want)
	}
	two := pairs[1]
	if !strings.Contains(two.Diff, "-+15\n") || !strings.Contains(two.Diff, "++fifteen\n") || strings.HasPrefix(two.Diff, " ") {
		t.Errorf("diff of Two: %q", two.Diff)
	}
	if pairs[0].Diff != "" || pairs[2].NewSHA != "" || pairs[3].OldSHA != "" || pairs[0].OldSHA == "" {
		t.Errorf("pairs %+v", pairs)
	}
}
