package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
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
	// A model kept as an alias is shown by its name.
	a.setAI(func(s *AISettings) { s.setPerProvider("model", "sonnet") })
	tt.Frame()
	if !tt.HasText("Runs Sonnet 5.5 (sonnet)") {
		t.Errorf("the model is not named: %q", tt.Texts())
	}
	a.setAI(func(s *AISettings) { s.setPerProvider("model", "") })
	tt.Frame()
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

func TestCreatePullRequestFromTheHeader(t *testing.T) {
	a, r, _ := prApp(t)
	r.CreateBranch("topic", "HEAD", true, git.KeepChanges)
	// The remote's default branch is develop, which is not the usual one.
	r.CreateBranch("develop", "main", false, git.KeepChanges)
	r.Do("update-ref", "refs/remotes/origin/develop", "develop")
	r.Do("update-ref", "refs/remotes/origin/main", "main")
	r.Do("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/develop")
	tab := a.tab()
	tab.reload()
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	if err := tt.Click("Create Pull Request…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	d := a.dialog
	if d == nil || d.title != "Create pull request" {
		t.Fatalf("dialog = %+v", d)
	}
	if got := d.field("base").text; got != "develop" {
		t.Fatalf("base = %q", got)
	}
	opts := d.field("base").options
	for _, want := range []string{"develop", "main", "feature/login", "topic"} {
		if !slices.Contains(opts, want) {
			t.Errorf("no %q among %v", want, opts)
		}
	}
	if slices.Contains(opts, "HEAD") {
		t.Errorf("HEAD among %v", opts)
	}
	// A branch that is not there cannot be chosen, nor can the branch itself.
	d.field("title").text = "x"
	d.field("base").text = "nope"
	if d.validate(d) == "" {
		t.Error("an unknown branch is accepted")
	}
	d.field("base").text = "topic"
	if d.validate(d) == "" {
		t.Error("the branch itself is accepted")
	}
	d.field("base").text = "main"
	if got := d.validate(d); got != "" {
		t.Errorf("validate = %q", got)
	}
}

func TestThePullRequestsPageHasNoCreateButton(t *testing.T) {
	a, _, _ := prApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	a.showPulls()
	tt.Frame()
	if tt.HasText("Create pull request…") {
		t.Fatalf("the page has a create button: %q", tt.Texts())
	}
}

func TestCreatePullRequestFromACommit(t *testing.T) {
	a, r, _ := prApp(t)
	r.CreateBranch("one", "HEAD", false, git.KeepChanges)
	r.CreateBranch("two", "HEAD", false, git.KeepChanges)
	tab := a.tab()
	tab.reload()
	head, _ := r.Resolve("HEAD")
	var cm *git.Commit
	for i := range tab.commits {
		if tab.commits[i].Hash == head {
			cm = &tab.commits[i]
		}
	}
	if cm == nil {
		t.Fatal("no commit")
	}
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	// Both branches are at the commit: the dialog asks which.
	var heads []string
	for _, ref := range tab.refs {
		if ref.Kind == git.LocalBranch && ref.Target == cm.Hash {
			heads = append(heads, ref.Name)
		}
	}
	if len(heads) < 2 {
		t.Fatalf("heads = %v", heads)
	}
	a.createPRDialogFrom(tab, heads...)
	tt.Frame()
	if a.dialog == nil || len(a.dialog.field("head").options) != len(heads) {
		t.Fatalf("dialog = %+v", a.dialog)
	}
	if !tt.HasText("From:") {
		t.Errorf("no choice of the branch: %q", tt.Texts())
	}
}

func TestCommitMenuCreatesAPullRequest(t *testing.T) {
	a, r, _ := prApp(t)
	tab := a.tab()
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	pointed := map[string]bool{}
	for _, ref := range tab.refs {
		if ref.Kind == git.LocalBranch {
			pointed[ref.Target] = true
		}
	}
	var subject, old string
	for _, c := range tab.commits {
		switch {
		case pointed[c.Hash] && subject == "":
			subject = c.Subject
		case !pointed[c.Hash] && old == "":
			old = c.Subject
		}
	}
	// A commit no branch points at says so.
	if err := tt.RightClick(old); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Create Pull Request…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.dialog == nil || !a.dialog.info || !strings.Contains(a.dialog.message, "No branch points at this commit") {
		t.Fatalf("dialog = %+v", a.dialog)
	}
	a.dialog.open = false
	tt.Frame()
	// One that a branch points at opens the dialog for it.
	if err := tt.RightClick(subject); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Create Pull Request…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.dialog == nil || a.dialog.title != "Create pull request" {
		t.Fatalf("dialog = %+v", a.dialog)
	}
	_ = r
}

func TestCreatePullRequestDialogShowsWhereItStacks(t *testing.T) {
	a, r, f := prApp(t)
	// #3 is the demo repository's last commit; the new branch is built on it.
	head, _ := r.Resolve("HEAD")
	f.head = head
	r.CreateBranch("topic", "HEAD", true, git.KeepChanges)
	os.WriteFile(filepath.Join(r.Root, "t.txt"), []byte("t\n"), 0o644)
	r.Stage()
	r.Commit(git.CommitOptions{Message: "Add t"})
	r.Do("update-ref", "refs/remotes/origin/feature", head)
	tab := a.tab()
	tab.reload()
	tt := ui.NewTester(a.view, 1280, 800)
	a.showPulls()
	tt.Frame()
	tab.prStacks = nil
	tab.prs[0].Head.SHA = head
	a.createPRDialog(tab, "")
	tt.Frame()
	texts := func() string { return strings.Join(tt.Texts(), "|") }
	// Into main, which #3 is not of, it says what the branch is built on.
	if !strings.Contains(texts(), "This branch is built on #3 “Add feature” (feature). Choose feature as the destination to stack on it.") {
		t.Fatalf("no hint: %q", tt.Texts())
	}
	d := a.dialog
	d.field("base").text = "feature"
	tt.Frame()
	for _, want := range []string{"Stack it on #3 “Add feature”, the pull request of feature", "It starts a stack of #3 and this pull request."} {
		if !strings.Contains(texts(), want) {
			t.Errorf("no %q: %q", want, tt.Texts())
		}
	}
	if strings.Contains(texts(), "This branch is built on") {
		t.Error("the hint stays once it is into the branch")
	}
	// Unchecked, the detail goes.
	d.field("stack").checked = false
	tt.Frame()
	if strings.Contains(texts(), "It starts a stack") {
		t.Error("the detail stays when the box is not checked")
	}
	// A pull request that is in a stack, and not at its top, has none on it.
	d.field("stack").checked = true
	tab.prStacks = map[int]github.StackPlace{3: {Position: 1, Size: 2}}
	tt.Frame()
	if strings.Contains(texts(), "Stack it on") {
		t.Errorf("stacked on a pull request that is not the top: %q", tt.Texts())
	}
	tab.prStacks = map[int]github.StackPlace{3: {Position: 2, Size: 2}}
	tt.Frame()
	if !strings.Contains(texts(), "It joins the stack of 2 pull requests, on top of #3, as number 3.") {
		t.Errorf("no detail of the stack joined: %q", tt.Texts())
	}
}

func TestCreatePullRequestDialogPreviewsTheDescription(t *testing.T) {
	a, r, _ := prApp(t)
	r.CreateBranch("topic", "HEAD", true, git.KeepChanges)
	tab := a.tab()
	tab.reload()
	tt := ui.NewTester(a.view, 1280, 800)
	a.createPRDialog(tab, "")
	tt.Frame()
	if a.dialog.width <= 440 || !a.dialog.stacked {
		t.Fatalf("width %v, stacked %v", a.dialog.width, a.dialog.stacked)
	}
	body := a.dialog.field("body")
	body.text = "## Summary\n- first point"
	if err := tt.Click("Preview"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if body.tab != 1 || !tt.HasText("Summary") || tt.HasText("## Summary") {
		t.Fatalf("tab %d, texts %q", body.tab, tt.Texts())
	}
	snapshot(t, tt, "pr-create-preview")
	if err := tt.Click("Raw"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if body.tab != 0 || a.dialog.field("body").text != "## Summary\n- first point" {
		t.Fatalf("tab %d, text %q", body.tab, body.text)
	}
}
