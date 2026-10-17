// Package aigen writes commit messages, pull request titles and bodies, and
// review guides with a model, by running the command line of Claude Code,
// Codex or OpenCode. What it asks, and what it does with the answer, follows
// docs/ai-generation.md.
package aigen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Provider is the program that runs the model.
type Provider string

const (
	Claude   Provider = "claude"
	Codex    Provider = "codex"
	OpenCode Provider = "opencode"
)

// Providers are the ones that can be chosen, in the order to offer them.
var Providers = []Provider{Claude, Codex, OpenCode}

// Label is the name to show.
func (p Provider) Label() string {
	switch p {
	case Codex:
		return "Codex"
	case OpenCode:
		return "OpenCode"
	}
	return "Claude Code"
}

// Config says which model writes.
type Config struct {
	Provider Provider
	// Binary is the path of the program, "" to look for it by name.
	Binary string
	// Model is what the program is told with --model; "" leaves it to the
	// program. OpenCode takes provider/model.
	Model string
	// Effort is the reasoning effort: Claude's --effort or Codex's
	// model_reasoning_effort; "" is the program's own (low for Codex).
	Effort string
}

// Request is one generation: a prompt, and the shape the answer must have.
type Request struct {
	Operation string
	// Dir is where the program runs.
	Dir    string
	Prompt string
	// Schema is the JSON schema of the answer.
	Schema string
	// Investigate lets the model read the repository in Dir (read-only), which
	// takes longer; OpenCode cannot.
	Investigate bool
}

const (
	defaultTimeout     = 3 * time.Minute
	investigateTimeout = 15 * time.Minute
)

// Error is a failure of the program, with what it said.
type Error struct {
	Operation string
	Detail    string
}

func (e *Error) Error() string { return e.Detail }

func fail(op, format string, args ...any) error {
	return &Error{Operation: op, Detail: fmt.Sprintf(format, args...)}
}

// Generate runs the model and returns its answer: a JSON object matching the
// request's schema.
func Generate(ctx context.Context, cfg Config, req Request) (json.RawMessage, error) {
	timeout := defaultTimeout
	if req.Investigate {
		timeout = investigateTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var out json.RawMessage
	var err error
	switch cfg.Provider {
	case Codex:
		out, err = runCodex(ctx, cfg, req)
	case OpenCode:
		out, err = runOpenCode(ctx, cfg, req)
	default:
		out, err = runClaude(ctx, cfg, req)
	}
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fail(req.Operation, "%s timed out", cfg.Provider.Label())
	}
	return out, err
}

// searchDirs are where the programs usually are, which an app opened from the
// Finder does not have on its PATH.
func searchDirs() []string {
	home, _ := os.UserHomeDir()
	dirs := filepath.SplitList(os.Getenv("PATH"))
	dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin")
	if home != "" {
		for _, d := range []string{".local/bin", ".claude/local", ".npm-global/bin", ".bun/bin", ".cargo/bin", ".local/share/mise/shims", ".volta/bin", "go/bin"} {
			dirs = append(dirs, filepath.Join(home, d))
		}
	}
	return dirs
}

// environment is the program's, with the usual places on its PATH: a program
// that is a script needs its interpreter.
func environment() []string {
	env := os.Environ()
	path := strings.Join(searchDirs(), string(filepath.ListSeparator))
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + path
			return env
		}
	}
	return append(env, "PATH="+path)
}

// Find is the path of a provider's program: the one chosen, or the first by
// its name in the usual places.
func Find(p Provider, override string) (string, error) {
	if override != "" {
		if strings.ContainsRune(override, filepath.Separator) {
			if _, err := os.Stat(override); err != nil {
				return "", fmt.Errorf("%s is not there", override)
			}
			return override, nil
		}
	}
	name := cmpOr(override, string(p))
	for _, d := range searchDirs() {
		path := filepath.Join(d, name)
		if st, err := os.Stat(path); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s is required but was not found: install it, or choose its path in the preferences", p.Label())
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// exec runs the program with the prompt on its standard input.
func execute(ctx context.Context, cfg Config, req Request, args []string) (stdout, stderr string, err error) {
	bin, err := Find(cfg.Provider, cfg.Binary)
	if err != nil {
		return "", "", &Error{Operation: req.Operation, Detail: err.Error()}
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = req.Dir
	cmd.Env = environment()
	cmd.Stdin = strings.NewReader(req.Prompt)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	cmd.WaitDelay = 2 * time.Second
	err = cmd.Run()
	return so.String(), se.String(), err
}

// failed says why a program that did not succeed did not.
func failed(cfg Config, req Request, stdout, stderr string, err error) error {
	detail := strings.TrimSpace(stderr)
	if detail == "" {
		detail = strings.TrimSpace(stdout)
	}
	if detail == "" {
		return fail(req.Operation, "%s failed: %v", cfg.Provider.Label(), err)
	}
	return fail(req.Operation, "%s failed: %s", cfg.Provider.Label(), detail)
}

// The tools an investigation by Claude can see, and what it may run: reading,
// and git's read-only commands. --output writes a file anywhere, so it is
// denied, and deny wins over allow.
const (
	claudeInvestigationTools   = "Read,Grep,Glob,Bash"
	claudeInvestigationAllowed = "Read,Grep,Glob,Bash(git diff:*),Bash(git log:*),Bash(git show:*)"
	claudeInvestigationDenied  = "Bash(git * --output*),Bash(git * -o *)"
)

func runClaude(ctx context.Context, cfg Config, req Request) (json.RawMessage, error) {
	args := []string{"-p", "--output-format", "json", "--json-schema", req.Schema}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	if cfg.Effort != "" {
		args = append(args, "--effort", cfg.Effort)
	}
	args = append(args, "--settings", `{"disableAllHooks":true}`)
	if req.Investigate {
		args = append(args, "--tools", claudeInvestigationTools, "--allowedTools", claudeInvestigationAllowed, "--disallowedTools", claudeInvestigationDenied)
	} else {
		args = append(args, "--tools", "")
	}
	args = append(args, "--disable-slash-commands", "--strict-mcp-config", "--permission-mode", "dontAsk")
	stdout, stderr, err := execute(ctx, cfg, req, args)
	if err != nil {
		// The answer says why, in its result, when the program got that far.
		if msg := claudeError(stdout); msg != "" {
			return nil, fail(req.Operation, "Claude Code failed: %s", msg)
		}
		return nil, failed(cfg, req, stdout, stderr, err)
	}
	var envelope struct {
		Type             string          `json:"type"`
		StructuredOutput json.RawMessage `json:"structured_output"`
	}
	raw := strings.TrimSpace(stdout)
	if strings.HasPrefix(raw, "[") {
		// A list of messages: the last result is the answer.
		var msgs []json.RawMessage
		if json.Unmarshal([]byte(raw), &msgs) == nil {
			for i := len(msgs) - 1; i >= 0; i-- {
				var e struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(msgs[i], &e) == nil && e.Type == "result" {
					raw = string(msgs[i])
					break
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil || len(envelope.StructuredOutput) == 0 || string(envelope.StructuredOutput) == "null" {
		return nil, fail(req.Operation, "Claude Code returned unexpected output")
	}
	return envelope.StructuredOutput, nil
}

// claudeError is the message of a result that says it is an error.
func claudeError(stdout string) string {
	var e struct {
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(stdout)), &e) == nil && e.IsError {
		return e.Result
	}
	return ""
}

func runCodex(ctx context.Context, cfg Config, req Request) (json.RawMessage, error) {
	dir, err := os.MkdirTemp("", "myfork-codex-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	schemaPath, outPath := filepath.Join(dir, "schema.json"), filepath.Join(dir, "output.json")
	if err := os.WriteFile(schemaPath, []byte(req.Schema), 0o600); err != nil {
		return nil, err
	}
	effort := cmpOr(cfg.Effort, "low")
	args := []string{"exec", "--config", "features.plugins=false", "--config", "features.apps=false", "--ephemeral", "--skip-git-repo-check", "-s", "read-only"}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	args = append(args, "--config", fmt.Sprintf("model_reasoning_effort=%q", effort), "--output-schema", schemaPath, "--output-last-message", outPath, "-")
	stdout, stderr, err := execute(ctx, cfg, req, args)
	if err != nil {
		return nil, failed(cfg, req, stdout, stderr, err)
	}
	out, err := os.ReadFile(outPath)
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		return nil, fail(req.Operation, "Codex wrote no answer")
	}
	return json.RawMessage(bytes.TrimSpace(out)), nil
}

func runOpenCode(ctx context.Context, cfg Config, req Request) (json.RawMessage, error) {
	if req.Investigate {
		return nil, fail(req.Operation, "OpenCode cannot investigate a checkout read-only: choose another provider for review guides")
	}
	if cfg.Model != "" && !strings.Contains(cfg.Model, "/") {
		return nil, fail(req.Operation, "OpenCode takes a model as provider/model, such as anthropic/claude-sonnet-4-5")
	}
	// A private server of its own, which stops with the command: no service
	// is left running.
	args := []string{"run", "--format", "json", "--standalone"}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	stdout, stderr, err := execute(ctx, cfg, req, args)
	var text strings.Builder
	var problem string
	for _, line := range strings.Split(stdout, "\n") {
		var ev struct {
			Type  string                `json:"type"`
			Part  struct{ Text string } `json:"part"`
			Error struct {
				Message string `json:"message"`
				Data    struct {
					Message string `json:"message"`
				} `json:"data"`
				Name string `json:"name"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(line)), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "text":
			text.WriteString(ev.Part.Text)
		case "error":
			problem = cmpOr(cmpOr(ev.Error.Data.Message, ev.Error.Message), ev.Error.Name)
		}
	}
	if problem != "" {
		return nil, fail(req.Operation, "OpenCode failed: %s", problem)
	}
	if err != nil {
		return nil, failed(cfg, req, stdout, stderr, err)
	}
	answer := ExtractJSONObject(text.String())
	if strings.TrimSpace(answer) == "" {
		return nil, fail(req.Operation, "OpenCode returned empty output")
	}
	return json.RawMessage(answer), nil
}

// ExtractJSONObject is the first JSON object in text, the rest dropped: a
// model that cannot be held to a schema may fence or introduce its answer.
func ExtractJSONObject(raw string) string {
	s := strings.TrimSpace(raw)
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return s
	}
	depth, inString, escaping := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case inString:
			switch {
			case escaping:
				escaping = false
			case c == '\\':
				escaping = true
			case c == '"':
				inString = false
			}
		case c == '"':
			inString = true
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}

// decode reads the answer into v, naming the program when it is not what the
// schema asked.
func decode(cfg Config, req Request, raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fail(req.Operation, "%s returned invalid structured output: %v", cfg.Provider.Label(), err)
	}
	return nil
}
