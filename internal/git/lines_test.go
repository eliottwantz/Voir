package git

import (
	"strings"
	"testing"
)

const lineBase = "one\ntwo\nthree\nfour\nfive\n"

func linesRepo(t *testing.T) *Repo {
	r := newRepo(t)
	write(t, r, "f.txt", lineBase)
	commit(t, r, "base")
	return r
}

func ids(t *testing.T, patch string, pick func(h int, l PatchLine) bool) map[LineID]bool {
	t.Helper()
	p, err := ParsePatchLines(patch)
	if err != nil {
		t.Fatal(err)
	}
	sel := map[LineID]bool{}
	for hi, h := range p.Hunks {
		for li, l := range h.Lines {
			if l.Changed() && pick(hi, l) {
				sel[LineID{hi, li}] = true
			}
		}
	}
	return sel
}

func TestStageSomeLinesOfAHunk(t *testing.T) {
	r := linesRepo(t)
	// Delete "two", change "three" to "THREE", add "six".
	write(t, r, "f.txt", "one\nTHREE\nfour\nfive\nsix\n")
	patch, _ := r.WorkDiff("f.txt", DiffOptions{})
	// Stage the replacement of three, and nothing else.
	sel := ids(t, patch, func(_ int, l PatchLine) bool {
		return strings.Contains(l.Text, "hree") || strings.Contains(l.Text, "THREE")
	})
	if err := r.StageLines(patch, sel); err != nil {
		t.Fatal(err)
	}
	staged, _ := r.StagedDiff("f.txt", DiffOptions{})
	if !strings.Contains(staged, "-three") || !strings.Contains(staged, "+THREE") || strings.Contains(staged, "-two") || strings.Contains(staged, "+six") {
		t.Fatalf("staged:\n%s", staged)
	}
	rest, _ := r.WorkDiff("f.txt", DiffOptions{})
	if !strings.Contains(rest, "-two") || !strings.Contains(rest, "+six") || strings.Contains(rest, "+THREE") || strings.Contains(rest, "-three") {
		t.Fatalf("unstaged:\n%s", rest)
	}
	// The index holds the file with only that change.
	got, _ := r.ReadFile("", "f.txt")
	if string(got) != "one\ntwo\nTHREE\nfour\nfive\n" {
		t.Fatalf("index = %q", got)
	}

	// Unstage just the deletion half of it: the "+THREE" stays staged.
	sel = ids(t, staged, func(_ int, l PatchLine) bool { return l.Kind == '-' })
	if err := r.UnstageLines(staged, sel); err != nil {
		t.Fatal(err)
	}
	got, _ = r.ReadFile("", "f.txt")
	if string(got) != "one\ntwo\nthree\nTHREE\nfour\nfive\n" {
		t.Fatalf("index after unstaging the deletion = %q", got)
	}
}

func TestDiscardSomeLines(t *testing.T) {
	r := linesRepo(t)
	write(t, r, "f.txt", "ONE\ntwo\nthree\nfour\nFIVE\n")
	patch, _ := r.WorkDiff("f.txt", DiffOptions{})
	sel := ids(t, patch, func(_ int, l PatchLine) bool { return strings.Contains(l.Text, "ONE") || l.Text == "one" })
	if err := r.DiscardLines(patch, sel); err != nil {
		t.Fatal(err)
	}
	got, _ := r.WorkFile("f.txt")
	if string(got) != "one\ntwo\nthree\nfour\nFIVE\n" {
		t.Fatalf("work file = %q", got)
	}
}

func TestStageLinesOfANewFile(t *testing.T) {
	r := linesRepo(t)
	write(t, r, "new.txt", "a\nb\nc\n")
	if err := r.IntentToAdd("new.txt"); err != nil {
		t.Fatal(err)
	}
	patch, _ := r.WorkDiff("new.txt", DiffOptions{})
	sel := ids(t, patch, func(_ int, l PatchLine) bool { return l.Text == "a" || l.Text == "c" })
	if err := r.StageLines(patch, sel); err != nil {
		t.Fatalf("%v\n%s", err, patch)
	}
	got, _ := r.ReadFile("", "new.txt")
	if string(got) != "a\nc\n" {
		t.Fatalf("index = %q", got)
	}
}

func TestStageLinesWithoutTrailingNewline(t *testing.T) {
	r := newRepo(t)
	write(t, r, "n.txt", "x\ny")
	commit(t, r, "base")
	write(t, r, "n.txt", "x\ny\nz")
	patch, _ := r.WorkDiff("n.txt", DiffOptions{})
	sel := ids(t, patch, func(int, PatchLine) bool { return true })
	if err := r.StageLines(patch, sel); err != nil {
		t.Fatalf("%v\n%s", err, patch)
	}
	got, _ := r.ReadFile("", "n.txt")
	if string(got) != "x\ny\nz" {
		t.Fatalf("index = %q", got)
	}
}

func TestParseShortHunkHeader(t *testing.T) {
	r := newRepo(t)
	write(t, r, "s.txt", "only\n")
	commit(t, r, "base")
	write(t, r, "s.txt", "changed\n")
	patch, _ := r.WorkDiff("s.txt", DiffOptions{})
	p, err := ParsePatchLines(patch)
	if err != nil || len(p.Hunks) != 1 || p.Hunks[0].OldStart != 1 || len(p.ChangedLines()) != 2 {
		t.Fatalf("patch = %+v, %v\n%s", p, err, patch)
	}
}
