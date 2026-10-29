package aigen

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eliott/voir/internal/git"
)

// fakeCLI is a program that records how it was run: its arguments, one to a
// line, in args, and what it was sent on its standard input, in stdin; and
// then runs body.
func fakeCLI(t *testing.T, body string) (bin, args, stdin string) {
	t.Helper()
	dir := t.TempDir()
	bin, args, stdin = filepath.Join(dir, "fake"), filepath.Join(dir, "args"), filepath.Join(dir, "stdin")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > '" + args + "'\ncat > '" + stdin + "'\n" + body + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return
}

func argsOf(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

func has(args []string, want ...string) bool {
	for i := range args {
		if i+len(want) <= len(args) && strings.Join(args[i:i+len(want)], "\x00") == strings.Join(want, "\x00") {
			return true
		}
	}
	return false
}

// demo is a repository with a change staged.
func demo(t *testing.T) *git.Repo {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	r, err := git.Init(filepath.Join(dir, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][2]string{{"user.name", "T"}, {"user.email", "t@example.com"}, {"commit.gpgsign", "false"}} {
		r.SetConfig(kv[0], kv[1])
	}
	r.Do("checkout", "-q", "-b", "main")
	os.WriteFile(filepath.Join(r.Root, "a.txt"), []byte("one\n"), 0o644)
	r.Stage()
	if err := r.Commit(git.CommitOptions{Message: "Add a"}); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestClaudeAsksForAStructuredAnswer(t *testing.T) {
	bin, args, stdin := fakeCLI(t, `echo '{"type":"result","is_error":false,"structured_output":{"subject":"Fix it.","body":" - a "}}'`)
	r := demo(t)
	os.WriteFile(filepath.Join(r.Root, "a.txt"), []byte("two\n"), 0o644)
	r.Stage()
	c, err := WriteCommit(context.Background(), Config{Provider: Claude, Binary: bin, Model: "haiku", Effort: "low"}, Style{Mode: "conventional_commits"}, r)
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "Fix it" || c.Body != "- a" || c.Message() != "Fix it\n\n- a" {
		t.Fatalf("commit = %+v", c)
	}
	a := argsOf(t, args)
	for _, want := range [][]string{{"-p"}, {"--output-format", "json"}, {"--model", "haiku"}, {"--effort", "low"}, {"--tools", ""}, {"--permission-mode", "dontAsk"}, {"--strict-mcp-config"}} {
		if !has(a, want...) {
			t.Errorf("no %v in %q", want, a)
		}
	}
	if has(a, "--allowedTools") {
		t.Error("a commit message may use tools")
	}
	sent, _ := os.ReadFile(stdin)
	for _, want := range []string{"You write concise git commit messages.", "Use Conventional Commits", "Branch: main", "Staged files:\nM\ta.txt", "+two"} {
		if !strings.Contains(string(sent), want) {
			t.Errorf("the prompt lacks %q:\n%s", want, sent)
		}
	}
}

func TestClaudeSaysWhyItFailed(t *testing.T) {
	bin, _, _ := fakeCLI(t, `echo '{"type":"result","is_error":true,"result":"Credit balance is too low"}'; exit 1`)
	_, err := Generate(context.Background(), Config{Provider: Claude, Binary: bin}, Request{Operation: "x", Prompt: "p", Schema: "{}"})
	if err == nil || !strings.Contains(err.Error(), "Credit balance is too low") {
		t.Fatalf("err = %v", err)
	}
}

func TestCodexWritesItsAnswerToAFile(t *testing.T) {
	bin, args, _ := fakeCLI(t, `case " $* " in *" mcp list "*) echo '[{"name":"docs","enabled":true},{"name":"a.b","enabled":true},{"name":"off","enabled":false}]'; exit 0;; esac
out=""; prev=""; for a in "$@"; do [ "$prev" = "--output-last-message" ] && out="$a"; prev="$a"; done
printf '%s' '{"title":"T\nsecond line","body":"B"}' > "$out"`)
	r := demo(t)
	r.Do("checkout", "-q", "-b", "topic")
	os.WriteFile(filepath.Join(r.Root, "b.txt"), []byte("b\n"), 0o644)
	r.Stage()
	r.Commit(git.CommitOptions{Message: "Add b"})
	pr, err := WritePR(context.Background(), Config{Provider: Codex, Binary: bin, Model: "gpt-x"}, Style{}, r, "", "main", "topic")
	if err != nil {
		t.Fatal(err)
	}
	if pr.Title != "T" || pr.Body != "B" {
		t.Fatalf("pr = %+v", pr)
	}
	a := argsOf(t, args)
	for _, want := range [][]string{{"exec"}, {"--ephemeral"}, {"-s", "read-only"}, {"--model", "gpt-x"}, {"--config", `model_reasoning_effort="low"`}, {"--config", "features.plugins=false"}} {
		if !has(a, want...) {
			t.Errorf("no %v in %q", want, a)
		}
	}
	// The servers the user has on are turned off, by name, and only those.
	if !has(a, "--config", `mcp_servers={"docs"={enabled=false},"a.b"={enabled=false}}`) {
		t.Errorf("the MCP servers are left on: %q", a)
	}
	if a[len(a)-1] != "-" || !has(a, "--output-schema") {
		t.Errorf("args = %q", a)
	}
}

func TestOpenCodeIsAskedByItsRunCommand(t *testing.T) {
	bin, args, stdin := fakeCLI(t, `cat <<'EOF'
{"type":"step_start","part":{"type":"step-start"}}
{"type":"text","part":{"type":"text","text":"Here it is:\n{\"subject\":\"Add b\","}}
{"type":"text","part":{"type":"text","text":"\"body\":\"\"}\n"}}
EOF`)
	r := demo(t)
	os.WriteFile(filepath.Join(r.Root, "a.txt"), []byte("two\n"), 0o644)
	r.Stage()
	c, err := WriteCommit(context.Background(), Config{Provider: OpenCode, Binary: bin, Model: "anthropic/claude-x"}, Style{}, r)
	if err != nil || c.Subject != "Add b" || c.Body != "" {
		t.Fatalf("commit = %+v, %v", c, err)
	}
	a := argsOf(t, args)
	if !has(a, "run", "--format", "json", "--standalone") || !has(a, "--model", "anthropic/claude-x") {
		t.Errorf("args = %q", a)
	}
	if sent, _ := os.ReadFile(stdin); !strings.Contains(string(sent), "You write concise git commit messages.") {
		t.Errorf("stdin = %q", sent)
	}
}

func TestOpenCodeErrors(t *testing.T) {
	bin, _, _ := fakeCLI(t, `echo '{"type":"error","error":{"type":"provider.no-route","message":"Model unavailable: nope/x"}}'`)
	_, err := Generate(context.Background(), Config{Provider: OpenCode, Binary: bin, Model: "nope/x"}, Request{Operation: "x", Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "Model unavailable: nope/x") {
		t.Fatalf("err = %v", err)
	}
	if _, err := Generate(context.Background(), Config{Provider: OpenCode, Binary: bin, Model: "plain"}, Request{Operation: "x"}); err == nil || !strings.Contains(err.Error(), "provider/model") {
		t.Fatalf("a model without a provider: %v", err)
	}
	if _, err := Generate(context.Background(), Config{Provider: OpenCode, Binary: bin}, Request{Operation: "x", Investigate: true}); err == nil || !strings.Contains(err.Error(), "cannot investigate") {
		t.Fatalf("an investigation: %v", err)
	}
}

func TestAProgramThatIsNotThere(t *testing.T) {
	_, err := Generate(context.Background(), Config{Provider: Codex, Binary: "/nonexistent/codex"}, Request{Operation: "x"})
	if err == nil || !strings.Contains(err.Error(), "is not there") {
		t.Fatalf("err = %v", err)
	}
}

func TestNothingStaged(t *testing.T) {
	if _, err := WriteCommit(context.Background(), Config{Provider: Claude, Binary: "/bin/true"}, Style{}, demo(t)); err != ErrNothingStaged {
		t.Fatalf("err = %v", err)
	}
}

func TestPRFollowsTheTemplateAndTheRepositoryStyle(t *testing.T) {
	bin, _, stdin := fakeCLI(t, `echo '{"type":"result","structured_output":{"title":"x","body":"y"}}'`)
	r := demo(t)
	os.MkdirAll(filepath.Join(r.Root, ".github"), 0o755)
	os.WriteFile(filepath.Join(r.Root, ".github", "pull_request_template.md"), []byte("## What\n<!-- why -->\n"), 0o644)
	os.WriteFile(filepath.Join(r.Root, "AGENTS.md"), []byte("Be brief.\n"), 0o644)
	os.WriteFile(filepath.Join(r.Root, "CLAUDE.md"), []byte("Claude notes.\n"), 0o644)
	r.Stage()
	r.Commit(git.CommitOptions{Message: "Add templates"})
	r.Do("checkout", "-q", "-b", "topic")
	os.WriteFile(filepath.Join(r.Root, "b.txt"), []byte("b\n"), 0o644)
	r.Stage()
	r.Commit(git.CommitOptions{Message: "Add b"})
	if _, err := WritePR(context.Background(), Config{Provider: Claude, Binary: bin}, Style{Mode: "repo_conventions", FollowTemplates: true}, r, "", "main", "topic"); err != nil {
		t.Fatal(err)
	}
	sent, _ := os.ReadFile(stdin)
	for _, want := range []string{"follow the repository change request template structure", "Repository change request template:\n## What", "Recent commit subjects from this repository:\nAdd b\nAdd templates", "Local AGENTS.md:\nBe brief.", "Local CLAUDE.md:\nClaude notes.", "Base branch: main", "Head branch: topic", "Commits:\n", "Diff patch:"} {
		if !strings.Contains(string(sent), want) {
			t.Errorf("the prompt lacks %q:\n%s", want, sent)
		}
	}
	// Without the template switch, the fixed headings are asked for.
	if _, err := WritePR(context.Background(), Config{Provider: Claude, Binary: bin}, Style{}, r, "", "main", "topic"); err != nil {
		t.Fatal(err)
	}
	sent, _ = os.ReadFile(stdin)
	if !strings.Contains(string(sent), "include a heading '## Summary'") || strings.Contains(string(sent), "Local CLAUDE.md") {
		t.Errorf("prompt:\n%s", sent)
	}
	// Codex is not given CLAUDE.md.
	cbin, _, cstdin := fakeCLI(t, `out=""; prev=""; for a in "$@"; do [ "$prev" = "--output-last-message" ] && out="$a"; prev="$a"; done; echo '{"title":"x","body":"y"}' > "$out"`)
	if _, err := WritePR(context.Background(), Config{Provider: Codex, Binary: cbin}, Style{Mode: "repo_conventions"}, r, "", "main", "topic"); err != nil {
		t.Fatal(err)
	}
	if sent, _ = os.ReadFile(cstdin); strings.Contains(string(sent), "Local CLAUDE.md") || !strings.Contains(string(sent), "Local AGENTS.md") {
		t.Errorf("prompt:\n%s", sent)
	}
}

func TestSanitizeCommit(t *testing.T) {
	long := strings.Repeat("a", 90)
	for in, want := range map[string]string{"  ": "Update project files", "Fix.\nmore": "Fix", "Fix...": "Fix", long: long[:72]} {
		if got := sanitizeCommit(Commit{Subject: in}).Subject; got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	if got := sanitizePR(PR{Title: " \n"}).Title; got != "Update project changes" {
		t.Errorf("title = %q", got)
	}
}

func TestExtractJSONObject(t *testing.T) {
	for in, want := range map[string]string{
		"\x60\x60\x60json\n{\"a\":\"}\"}\n\x60\x60\x60 done": `{"a":"}"}`,
		`x {"a":{"b":1}} y {"c":2}`:                          `{"a":{"b":1}}`,
		"no json":                                            "no json",
	} {
		if got := ExtractJSONObject(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestSchemasAreJSON(t *testing.T) {
	for name, s := range map[string]string{"commit": commitSchema, "pr": prSchema, "guide": guideSchema} {
		var v map[string]any
		if err := json.Unmarshal([]byte(s), &v); err != nil || v["additionalProperties"] != false {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestCodexRunsWhenItCannotListItsServers(t *testing.T) {
	bin, args, _ := fakeCLI(t, `case " $* " in *" mcp list "*) echo 'not json'; exit 0;; esac
out=""; prev=""; for a in "$@"; do [ "$prev" = "--output-last-message" ] && out="$a"; prev="$a"; done
echo '{"subject":"s","body":""}' > "$out"`)
	if _, err := Generate(context.Background(), Config{Provider: Codex, Binary: bin}, Request{Operation: "x", Prompt: "p", Schema: "{}"}); err != nil {
		t.Fatal(err)
	}
	for _, a := range argsOf(t, args) {
		if strings.HasPrefix(a, "mcp_servers=") {
			t.Fatalf("a configuration of servers: %q", a)
		}
	}
}

// TestCodexOffersItsCatalog checks the models the preferences offer for Codex
// are the ones its catalog lists, without the hidden ones.
func TestCodexOffersItsCatalog(t *testing.T) {
	bin, args, _ := fakeCLI(t, `cat <<'JSON'
{"models":[
 {"slug":"gpt-5.6-terra","display_name":"GPT-5.6-Terra","visibility":"list"},
 {"slug":"gpt-reserve","display_name":"GPT-Reserve","visibility":"hide"},
 {"slug":"gpt-6-luna","display_name":"GPT-6-Luna","visibility":"list"},
 {"slug":"gpt-noname","display_name":"","visibility":"list"},
 {"slug":"","display_name":"No slug","visibility":"list"}
]}
JSON`)
	got, err := Models(context.Background(), Config{Provider: Codex, Binary: bin})
	if err != nil {
		t.Fatal(err)
	}
	want := []ModelChoice{{ID: "gpt-5.6-terra", Name: "GPT-5.6-Terra"}, {ID: "gpt-6-luna", Name: "GPT-6-Luna"}, {ID: "gpt-noname", Name: "gpt-noname"}}
	if !slices.Equal(got, want) {
		t.Errorf("models = %v, want %v", got, want)
	}
	if a := argsOf(t, args); !has(a, "debug", "models") {
		t.Errorf("asked with %q, want codex debug models", a)
	}
}

// TestClaudeOffersItsCatalog checks the models the preferences offer for Claude
// are the ones its control protocol lists, without the entries that name none.
func TestClaudeOffersItsCatalog(t *testing.T) {
	bin, args, stdin := fakeCLI(t, `echo '{"type":"control_response","response":{"subtype":"success","request_id":"models","response":{"models":[{"value":"opus","displayName":"Opus 5.5"},{"value":"claude-haiku-4-5-20251001","displayName":""},{"value":"","displayName":"No value"}]}}}'`)
	got, err := Models(context.Background(), Config{Provider: Claude, Binary: bin})
	if err != nil {
		t.Fatal(err)
	}
	want := []ModelChoice{{ID: "opus", Name: "Opus 5.5"}, {ID: "claude-haiku-4-5-20251001", Name: "claude-haiku-4-5-20251001"}}
	if !slices.Equal(got, want) {
		t.Errorf("models = %v, want %v", got, want)
	}
	a := argsOf(t, args)
	if !has(a, "--input-format", "stream-json") || !has(a, "--output-format", "stream-json") {
		t.Errorf("asked with %q", a)
	}
	sent, _ := os.ReadFile(stdin)
	for _, want := range []string{`"subtype":"initialize"`, `"subtype":"list_models"`} {
		if !strings.Contains(string(sent), want) {
			t.Errorf("stdin lacks %s: %q", want, sent)
		}
	}
}
