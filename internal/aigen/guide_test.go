package aigen

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eliott/voir/internal/git"
)

func TestParseChange(t *testing.T) {
	patch := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -3,2 +3,3 @@\n-x\n+y\n@@ -10 +11,0 @@\n-gone\n" +
		"diff --git a/new.go b/new.go\n--- /dev/null\n+++ b/new.go\n@@ -0,0 +1,4 @@\n+a\n" +
		"diff --git a/old.go b/old.go\n--- a/old.go\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-a\n"
	ch := parseChange("a.go\x00new.go\x00old.go\x00", patch)
	if strings.Join(ch.files, ",") != "a.go,new.go,old.go" {
		t.Fatalf("files = %v", ch.files)
	}
	if got := ch.hunks["a.go"]; len(got["new"]) != 1 || got["new"][0] != (hunkRange{3, 5}) || len(got["old"]) != 2 || got["old"][1] != (hunkRange{10, 10}) {
		t.Fatalf("a.go = %+v", got)
	}
	if got := ch.hunks["new.go"]; got["new"][0] != (hunkRange{1, 4}) || len(got["old"]) != 0 {
		t.Fatalf("new.go = %+v", got)
	}
	if got := ch.hunks["old.go"]; len(got["new"]) != 0 || got["old"][0] != (hunkRange{1, 2}) {
		t.Fatalf("old.go = %+v", got)
	}
}

func TestNormalizeRepairsAGuide(t *testing.T) {
	ch := parseChange("a.go\x00b.go\x00c.go\x00", "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1,40 @@\n+x\n"+
		"diff --git a/b.go b/b.go\n--- a/b.go\n+++ b/b.go\n@@ -1 +1 @@\n+y\n")
	stops := normalize([]Stop{
		{Title: "", Summary: "dropped: no title"},
		{
			Title:     "First",
			Attention: &Attention{Kind: "risk", Note: "  "},
			Excerpts: []Excerpt{
				{Path: "a.go", Side: "new", Start: 0, End: 100, Caption: "wide"}, // clamped to the hunk, then to 25 lines
				{Path: "nope.go", Side: "new", Start: 1, End: 2},                 // not changed
				{Path: "b.go", Side: "new", Start: 50, End: 60},                  // outside every hunk
				{Path: "b.go", Side: "new", Start: 1, End: 1, Caption: "kept"},
			},
			Files:     []FileNote{{Path: "c.go", Note: " renamed "}, {Path: "ghost.go"}},
			Notes:     []LineNote{{Path: "a.go", Line: 3, Title: "on a line shown", Body: "b"}, {Path: "a.go", Line: 30, Title: "not shown"}, {Path: "a.go", Line: 3, Title: " "}},
			Questions: []string{"Why?", " "},
		},
		{Title: "Second", Files: []FileNote{{Path: "c.go"}, {Path: "a.go"}}},
	}, ch)
	if len(stops) != 2 {
		t.Fatalf("stops = %+v", stops)
	}
	first := stops[0]
	if first.Attention != nil || len(first.Excerpts) != 2 || !reflect.DeepEqual(first.Excerpts[0], Excerpt{Path: "a.go", Side: "new", Start: 1, End: 25, Caption: "wide"}) || first.Excerpts[1].Caption != "kept" {
		t.Fatalf("first = %+v", first)
	}
	if len(first.Files) != 1 || first.Files[0] != (FileNote{Path: "c.go", Note: "renamed"}) {
		t.Fatalf("files = %+v", first.Files)
	}
	if len(first.Notes) != 1 || first.Notes[0].Title != "on a line shown" || len(first.Questions) != 1 {
		t.Fatalf("notes %+v, questions %v", first.Notes, first.Questions)
	}
	// c.go is owned by the first stop, and a.go is excerpted, so the second owns nothing.
	if len(stops[1].Files) != 0 {
		t.Fatalf("second = %+v", stops[1])
	}
}

func TestNormalizeAddsTheFilesNobodyPlaced(t *testing.T) {
	ch := parseChange("a.go\x00b.go\x00", "")
	stops := normalize([]Stop{{Title: "Only", Files: []FileNote{{Path: "a.go"}}}}, ch)
	if len(stops) != 2 || stops[1].Title != "Other files" || len(stops[1].Files) != 1 || stops[1].Files[0].Path != "b.go" {
		t.Fatalf("stops = %+v", stops)
	}
}

func TestMergeExcerpts(t *testing.T) {
	got := mergeExcerpts([]Excerpt{
		{Path: "a", Side: "new", Start: 1, End: 5, Caption: ""},
		{Path: "a", Side: "new", Start: 6, End: 9, Caption: "joined"},
		{Path: "a", Side: "old", Start: 6, End: 9},
		{Path: "a", Side: "new", Start: 20, End: 70},
	})
	if len(got) != 3 || !reflect.DeepEqual(got[0], Excerpt{Path: "a", Side: "new", Start: 1, End: 9, Caption: "joined"}) || got[2].End != 44 {
		t.Fatalf("got = %+v", got)
	}
}

func TestGuideStackPrompt(t *testing.T) {
	req := GuidePrompt(GuideInput{
		Number: 2, Title: "Two", BaseBranch: "main", BaseCommit: "abc123",
		Stack: []GuideLayer{{Number: 1, Title: "One", State: "merged", Head: "h1"}, {Number: 2, Title: "Two", State: "open"}, {Number: 3, Title: "", State: "open"}},
	})
	for _, want := range []string{
		"change request #2", "range abc123...HEAD", "<pr_title>Two</pr_title>", "<pr_description>\n(none)\n</pr_description>", "<settled_review>\n(nothing)\n</settled_review>",
		"1. #1: One, merged, head h1", "2. #2 (this change): Two", "3. #3: (untitled), head not available", "Never excerpt another layer's code",
	} {
		if !strings.Contains(req.Prompt, want) {
			t.Errorf("the prompt lacks %q", want)
		}
	}
	if !req.Investigate || req.Operation != "generateReviewGuide" {
		t.Errorf("request = %+v", req)
	}
	if solo := GuidePrompt(GuideInput{Number: 2, Stack: []GuideLayer{{Number: 2}}}); strings.Contains(solo.Prompt, "one layer of a stack") {
		t.Error("a stack of one is described as a stack")
	}
	if p := GuidePrompt(GuideInput{Instructions: "Be short."}); !strings.Contains(p.Prompt, "The maintainer's preferences for guides:\nBe short.") {
		t.Error("no preferences")
	}
}

func TestWriteGuide(t *testing.T) {
	r := demo(t)
	r.Do("checkout", "-q", "-b", "topic")
	os.WriteFile(filepath.Join(r.Root, "a.txt"), []byte("one\ntwo\nthree\n"), 0o644)
	os.WriteFile(filepath.Join(r.Root, "b.txt"), []byte("b\n"), 0o644)
	r.Stage()
	r.Commit(git.CommitOptions{Message: "Change"})
	head, _ := r.Resolve("HEAD")
	base, _ := r.Resolve("main")
	bin, args, stdin := fakeCLI(t, `pwd > "$0.cwd"; git rev-parse HEAD > "$0.head"
echo '{"type":"result","structured_output":{"stops":[{"title":"The change","summary":"s","attention":null,
"excerpts":[{"path":"a.txt","side":"new","start":2,"end":9,"caption":"c"}],"files":[],"notes":[{"path":"a.txt","line":2,"title":"t","body":"b"}],"questions":[]}]}}'`)
	g, err := WriteGuide(context.Background(), Config{Provider: Claude, Binary: bin, Model: "m"}, r, "", head, GuideInput{Number: 5, Title: "T", BaseBranch: "main", BaseCommit: base})
	if err != nil {
		t.Fatal(err)
	}
	if g.Head != head || g.Number != 5 || len(g.Stops) != 2 {
		t.Fatalf("guide = %+v", g)
	}
	ex := g.Stops[0].Excerpts[0]
	if ex.Start != 2 || ex.End != 3 || strings.Join(ex.Lines, "|") != "two|three" {
		t.Fatalf("excerpt = %+v", ex)
	}
	// b.txt was not placed by the model.
	if g.Stops[1].Title != "Other files" || g.Stops[1].Files[0].Path != "b.txt" {
		t.Fatalf("last = %+v", g.Stops[1])
	}
	// The model ran in a checkout of the head, off to the side, with the tools of an investigation.
	if b, _ := os.ReadFile(bin + ".head"); strings.TrimSpace(string(b)) != head {
		t.Errorf("the model ran at %q", b)
	}
	if cwd, _ := os.ReadFile(bin + ".cwd"); strings.HasPrefix(strings.TrimSpace(string(cwd)), r.Root) {
		t.Errorf("the model ran in the repository: %s", cwd)
	}
	a := argsOf(t, args)
	if !has(a, "--tools", claudeInvestigationTools) || !has(a, "--allowedTools", claudeInvestigationAllowed) || !has(a, "--disallowedTools", claudeInvestigationDenied) {
		t.Errorf("args = %q", a)
	}
	if sent, _ := os.ReadFile(stdin); !strings.Contains(string(sent), "range "+base+"...HEAD") {
		t.Errorf("prompt: %s", sent)
	}
	// The worktree is gone.
	if out, _ := r.Read("worktree", "list", "--porcelain"); strings.Count(string(out), "worktree ") != 1 {
		t.Errorf("worktrees left:\n%s", out)
	}
}
