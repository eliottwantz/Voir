package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/aigen"
	"github.com/eliott/myfork/internal/git"
	"github.com/eliott/myfork/internal/github"
)

// fakeProvider is a Claude Code that answers with what it is given, as the
// structured output of its result.
func fakeProvider(t *testing.T, a *app, answer string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\ncat > /dev/null\ncat <<'EOF'\n{\"type\":\"result\",\"is_error\":false,\"structured_output\":" + answer + "}\nEOF\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	a.setAI(func(s *AISettings) { s.setPerProvider("binary", bin) })
}

func TestAISettingsAreKeptForEachProvider(t *testing.T) {
	a, _ := testApp(t)
	a.setAI(func(s *AISettings) { s.setPerProvider("model", " haiku ") })
	a.setAI(func(s *AISettings) {
		s.Provider = "codex"
		s.setPerProvider("model", "gpt-x")
		s.setPerProvider("effort", "high")
	})
	cfg, style := a.aiConfig()
	if cfg.Provider != aigen.Codex || cfg.Model != "gpt-x" || cfg.Effort != "high" || !style.FollowTemplates {
		t.Fatalf("config = %+v, style = %+v", cfg, style)
	}
	a.setAI(func(s *AISettings) { s.Provider = "claude" })
	if cfg, _ := a.aiConfig(); cfg.Provider != aigen.Claude || cfg.Model != "haiku" {
		t.Fatalf("config = %+v", cfg)
	}
	a.setAI(func(s *AISettings) {
		s.Provider = "nonsense"
		s.Style = "custom"
		s.Custom = "Be brief"
		s.IgnoreTemplates = true
	})
	cfg, style = a.aiConfig()
	if cfg.Provider != aigen.Claude || style.Mode != "custom" || style.Custom != "Be brief" || style.FollowTemplates {
		t.Fatalf("config = %+v, style = %+v", cfg, style)
	}
}

func TestPreferencesHaveAnAIPage(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	a.prefsOpen, a.prefsTab = true, 3
	tt.Frame()
	for _, want := range []string{"Provider:", "Claude Code", "Model:", "Effort:", "Program:", "Style:", "Follow the repository's pull request template", "Preferences for review guides:"} {
		if !tt.HasText(want) {
			t.Errorf("no %q: %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "prefs-ai")
	a.setAI(func(s *AISettings) { s.Provider = "opencode" })
	tt.Frame()
	if tt.HasText("Effort:") {
		t.Errorf("OpenCode has no effort: %q", tt.Texts())
	}
	a.setAI(func(s *AISettings) { s.Provider = "codex"; s.setPerProvider("binary", "/nonexistent/codex") })
	tt.Frame()
	if !tt.HasText("is not there") {
		t.Errorf("a program that is not there goes unsaid: %q", tt.Texts())
	}
}

func TestWriteTheCommitMessageWithAI(t *testing.T) {
	a, r := testApp(t)
	fakeProvider(t, a, `{"subject":"Say bye.","body":"- one"}`)
	os.WriteFile(filepath.Join(r.Root, "bye.txt"), []byte("bye\n"), 0o644)
	r.Stage()
	tab := a.tab()
	tab.reload()
	a.runHeld()
	tab.showChanges()
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	if err := tt.Click("Write the commit message"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tab.msg != "Say bye\n\n- one" || tab.aiWriting {
		t.Fatalf("msg = %q, writing %v", tab.msg, tab.aiWriting)
	}
}

func TestWriteThePullRequestWithAI(t *testing.T) {
	a, r, _ := prApp(t)
	fakeProvider(t, a, `{"title":"Add the topic","body":"## Summary\n- a"}`)
	r.CreateBranch("topic", "HEAD", true, git.KeepChanges)
	os.WriteFile(filepath.Join(r.Root, "topic.txt"), []byte("t\n"), 0o644)
	r.Stage()
	r.Commit(git.CommitOptions{Message: "Add topic"})
	tab := a.tab()
	tab.reload()
	tt := ui.NewTester(a.view, 1280, 800)
	a.showPulls()
	tt.Frame()
	a.createPRDialog(tab, "")
	tt.Frame()
	if err := tt.Click("Write the title and description with AI"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := a.dialog.field("title").text; got != "Add the topic" {
		t.Fatalf("title = %q (dialog error %q)", got, a.dialog.err)
	}
	if got := a.dialog.field("body").text; got != "## Summary\n- a" {
		t.Fatalf("body = %q", got)
	}
}

func TestWriteTheReviewGuide(t *testing.T) {
	a, r, f := prApp(t)
	// The pull request is the last commit of the demo repository.
	head, _ := r.Resolve("HEAD")
	base, _ := r.Resolve("HEAD~1")
	f.head = head
	a.fetchPR = func(ctx context.Context, _ *git.Repo, _ string, _ int, _ github.PR) (string, error) { return base, nil }
	patch, _ := r.Read("diff", "--name-only", base, head)
	file := strings.Fields(string(patch))[0]
	fakeProvider(t, a, `{"stops":[{"title":"The whole change","summary":"It does a thing.","attention":{"kind":"risk","note":"Look here."},"excerpts":[{"path":"`+file+`","side":"new","start":1,"end":2,"caption":"the start"}],"files":[],"notes":[],"questions":["Why?"]}]}`)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	a.showPulls()
	tt.Frame()
	tab.openPR(3)
	tt.Frame()
	tab.pr.tab = 1
	tt.Frame()
	if !tt.HasText("No review guide yet") {
		t.Fatalf("texts = %q", tt.Texts())
	}
	if err := tt.Click("Write the Guide"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	pv := tab.pr
	if pv.guide == nil || pv.guideErr != "" || pv.guideRunning {
		t.Fatalf("guide = %+v, err %q", pv.guide, pv.guideErr)
	}
	for _, want := range []string{"The whole change", "It does a thing.", "Risk", "Look here.", "Why?", "Written by Claude Code"} {
		if !tt.HasText(want) && !strings.Contains(strings.Join(tt.Texts(), "|"), want) {
			t.Errorf("no %q: %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "pr-guide")
	// It is kept, and found again by a pull request that is opened afresh.
	if g := a.loadGuide(*tab.gh, 3); g == nil || g.Head != head || len(g.Stops) == 0 {
		t.Fatalf("kept guide = %+v", g)
	}
	// A push makes it stale.
	pv.pr.Head.SHA = "ffff"
	tt.Frame()
	if !tt.HasText("The pull request changed since this guide was written.") {
		t.Errorf("no warning of a stale guide: %q", tt.Texts())
	}
}

func TestReviewGuideFailureIsShown(t *testing.T) {
	a, _, _ := prApp(t)
	a.setAI(func(s *AISettings) { s.setPerProvider("binary", "/nonexistent/claude") })
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	a.showPulls()
	tt.Frame()
	tab.openPR(3)
	tab.pr.tab = 1
	tt.Frame()
	tt.Click("Write the Guide")
	tt.Frame()
	if tab.pr.guideErr == "" || !tt.HasText(tab.pr.guideErr) && !strings.Contains(strings.Join(tt.Texts(), "|"), "not there") {
		t.Fatalf("err = %q, texts = %q", tab.pr.guideErr, tt.Texts())
	}
}
