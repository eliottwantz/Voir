// Package git drives the git command line, as Fork does: every read and
// every action is one git process, whose output this package parses.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Repo is a git repository's work tree.
type Repo struct {
	Root string
}

// ErrNotRepository is returned by Open for a directory outside any work tree.
var ErrNotRepository = errors.New("not a git repository")

// Open finds the repository holding dir.
func Open(dir string) (*Repo, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		abs = filepath.Dir(abs)
	}
	out, err := run(context.Background(), abs, nil, true, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", abs, ErrNotRepository)
	}
	root := strings.TrimSpace(string(out))
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return &Repo{Root: root}, nil
}

// Init creates a repository in dir.
func Init(dir string) (*Repo, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if _, err := run(context.Background(), dir, nil, false, "init"); err != nil {
		return nil, err
	}
	return Open(dir)
}

// Clone clones url into dir, reporting git's progress lines to progress.
func Clone(ctx context.Context, url, dir string, progress func(string)) error {
	cmd := exec.CommandContext(ctx, binary(), "clone", "--progress", url, dir)
	cmd.Env = env(false)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var tail bytes.Buffer
	buf := make([]byte, 4096)
	for {
		n, err := stderr.Read(buf)
		if n > 0 {
			tail.Write(buf[:n])
			if progress != nil {
				line := string(buf[:n])
				line = line[strings.LastIndexAny(strings.TrimRight(line, "\r\n"), "\r\n")+1:]
				progress(strings.TrimSpace(line))
			}
		}
		if err != nil {
			break
		}
	}
	if err := cmd.Wait(); err != nil {
		return &Error{Args: []string{"clone", url}, Stderr: tail.String(), Err: err}
	}
	return nil
}

// Error is a git command that failed, with what it printed.
type Error struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return "git " + strings.Join(e.Args, " ") + ": " + msg
}

func (e *Error) Unwrap() error { return e.Err }

// Message is what git said, without the command.
func (e *Error) Message() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return msg
}

var (
	binaryOnce sync.Once
	binaryPath string
)

// binary returns the git to run. On macOS, /usr/bin/git is a shim that
// finds the developer tools' git each time it runs, which doubles what
// starting git takes: the git it would run is found once instead.
func binary() string {
	binaryOnce.Do(func() {
		binaryPath = "git"
		if p := os.Getenv("MYFORK_GIT"); p != "" {
			binaryPath = p
			return
		}
		path, err := exec.LookPath("git")
		if err != nil {
			return
		}
		binaryPath = path
		if runtime.GOOS != "darwin" || path != "/usr/bin/git" {
			return
		}
		out, err := exec.Command("/usr/bin/xcode-select", "-p").Output()
		if err != nil {
			return
		}
		real := filepath.Join(strings.TrimSpace(string(out)), "usr", "bin", "git")
		if fi, err := os.Stat(real); err == nil && !fi.IsDir() {
			binaryPath = real
		}
	})
	return binaryPath
}

// env is the environment of every git: readers do not take the index lock
// from the user's own commands, and none asks for a password in a terminal
// that is not there.
func env(readOnly bool) []string {
	e := append(os.Environ(), "LC_ALL=C", "GIT_PAGER=cat", "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true")
	if readOnly {
		e = append(e, "GIT_OPTIONAL_LOCKS=0")
	}
	return e
}

func run(ctx context.Context, dir string, stdin io.Reader, readOnly bool, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary(), args...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	cmd.Env = env(readOnly)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), &Error{Args: args, Stderr: stderr.String(), Err: err}
	}
	return stdout.Bytes(), nil
}

// Read runs a git command that changes nothing, and returns its output.
func (r *Repo) Read(args ...string) ([]byte, error) {
	return run(context.Background(), r.Root, nil, true, args...)
}

// Do runs a git command that changes the repository.
func (r *Repo) Do(args ...string) ([]byte, error) {
	return run(context.Background(), r.Root, nil, false, args...)
}

// DoContext is Do that can be canceled, as a fetch can.
func (r *Repo) DoContext(ctx context.Context, args ...string) ([]byte, error) {
	return run(ctx, r.Root, nil, false, args...)
}

// DoInput is Do with something to read on its standard input.
func (r *Repo) DoInput(stdin string, args ...string) ([]byte, error) {
	return run(context.Background(), r.Root, strings.NewReader(stdin), false, args...)
}

func (r *Repo) text(args ...string) string {
	out, err := r.Read(args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Branch is the name of the branch checked out, or "" when HEAD is detached.
func (r *Repo) Branch() string {
	return r.text("symbolic-ref", "--short", "-q", "HEAD")
}

// HasHead reports whether the repository has a commit.
func (r *Repo) HasHead() bool {
	_, err := r.Read("rev-parse", "--verify", "-q", "HEAD")
	return err == nil
}

// Resolve returns the full hash of a revision.
func (r *Repo) Resolve(rev string) (string, error) {
	out, err := r.Read("rev-parse", "--verify", "-q", rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("unknown revision %q", rev)
	}
	return strings.TrimSpace(string(out)), nil
}

// Config returns a value of git's configuration, "" when unset.
func (r *Repo) Config(key string) string { return r.text("config", "--get", key) }

// SetConfig sets a value of the repository's configuration.
func (r *Repo) SetConfig(key, value string) error {
	_, err := r.Do("config", key, value)
	return err
}

// GlobalConfig returns a value of the user's configuration.
func GlobalConfig(key string) string {
	out, err := run(context.Background(), os.TempDir(), nil, true, "config", "--global", "--get", key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// SetGlobalConfig sets a value of the user's configuration.
func SetGlobalConfig(key, value string) error {
	_, err := run(context.Background(), os.TempDir(), nil, false, "config", "--global", key, value)
	return err
}

// GitDir returns the repository's .git directory.
func (r *Repo) GitDir() string {
	d := r.text("rev-parse", "--absolute-git-dir")
	if d == "" {
		return filepath.Join(r.Root, ".git")
	}
	return d
}

// State says what the repository is in the middle of: "merge", "rebase",
// "cherry-pick", "revert" or "bisect", "" for none.
func (r *Repo) State() string {
	dir := r.GitDir()
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	switch {
	case has("rebase-merge") || has("rebase-apply"):
		return "rebase"
	case has("MERGE_HEAD"):
		return "merge"
	case has("CHERRY_PICK_HEAD"):
		return "cherry-pick"
	case has("REVERT_HEAD"):
		return "revert"
	case has("BISECT_LOG"):
		return "bisect"
	}
	return ""
}
