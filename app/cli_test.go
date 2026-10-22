package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

func TestWriteCLI(t *testing.T) {
	// A missing bin dir is a failure, not an install.
	if _, err := writeCLI(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("installed the CLI into a missing directory")
	}

	// The first install writes the script.
	bin := t.TempDir()
	path := filepath.Join(bin, cliName)
	if got, err := writeCLI(bin); err != nil || got != path {
		t.Fatalf("writeCLI: %q, %v", got, err)
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm()&0o111 == 0 {
		t.Fatalf("%s is not executable: %v", path, err)
	}

	// An old symlink, from when the CLI was a bundled binary, is replaced,
	// without writing through it, and so is an older script.
	target := filepath.Join(t.TempDir(), "myfork")
	if err := os.WriteFile(target, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.Remove(path)
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := writeCLI(bin); err != nil {
		t.Fatalf("writeCLI over a symlink: %v", err)
	}
	if st, _ := os.Lstat(path); st.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("%s is still a symlink", path)
	}
	if data, _ := os.ReadFile(target); string(data) != "binary" {
		t.Errorf("the link target changed: %q", data)
	}
	if _, err := writeCLI(bin); err != nil {
		t.Fatalf("writeCLI over a script: %v", err)
	}

	// A file of someone else's is not replaced.
	if err := os.WriteFile(path, []byte("keep me"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := writeCLI(bin); err == nil {
		t.Fatal("replaced a file that is no script of ours")
	}
	if data, _ := os.ReadFile(path); string(data) != "keep me" {
		t.Errorf("the file changed: %q", data)
	}
}

func findItem(t *testing.T, a *app, label string) *mygo.MenuItem {
	t.Helper()
	var found *mygo.MenuItem
	walk(a.menuTemplate(), func(it *mygo.MenuItem) {
		if it.Label == label {
			found = it
		}
	})
	if found == nil {
		t.Fatalf("no menu item %q", label)
	}
	return found
}

func TestCLIPaths(t *testing.T) {
	got := cliPaths([]string{"/repo", ".", "../repo", "-psn_0123", "-h"}, "/home")
	want := []string{"/repo", "/home", "/repo"}
	if repr(got) != repr(want) {
		t.Errorf("cliPaths: %q, want %q", repr(got), repr(want))
	}
}

// repr is a stable string of paths, for comparisons.
func repr(paths []string) string { return fmt.Sprint(paths) }

func TestInstallCLI(t *testing.T) {
	a, _ := testApp(t)
	bin := t.TempDir()
	saved := cliBin
	cliBin = func() (string, error) { return bin, nil }
	t.Cleanup(func() { cliBin = saved })

	item := findItem(t, a, "Install `myfork` CLI…")
	item.Click(nil, nil) // updates and background work run at once in tests

	if data, err := os.ReadFile(filepath.Join(bin, cliName)); err != nil || string(data) != cliScript {
		t.Fatalf("no script after the install: %v", err)
	}

	// When the bin does not exist, the install fails with a dialog.
	cliBin = func() (string, error) { return "", os.ErrNotExist }
	item.Click(nil, nil)
	d := a.dialog
	if d == nil || d.ok != "OK" || d.title != "Could not install the myfork CLI" {
		t.Fatalf("no install fail dialog: %+v", d)
	}
}

// runCLIScript runs the CLI script in dir with a stub `open` on its PATH that
// knows only the installed bundle identifiers, and returns what it was asked
// to open.
func runCLIScript(t *testing.T, installed []string, dir string, args ...string) (opened string, err error) {
	t.Helper()
	stubs := t.TempDir()
	log := filepath.Join(stubs, "log")
	stub := "#!/bin/sh\n" +
		"[ \"$1\" = -b ] || exit 2\n" +
		"for id in" + func() string {
		s := ""
		for _, id := range installed {
			s += " " + id
		}
		return s
	}() + "; do\n[ \"$2\" = \"$id\" ] && { shift 2; echo \"$id $*\" > " + log + "; exit 0; }\ndone\nexit 1\n"
	if err := os.WriteFile(filepath.Join(stubs, "open"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(stubs, "myfork")
	if err := os.WriteFile(script, []byte(cliScript), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", append([]string{script}, args...)...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + stubs + ":/usr/bin:/bin", "PWD=" + dir}
	err = cmd.Run()
	data, _ := os.ReadFile(log)
	return strings.TrimSpace(string(data)), err
}

func TestCLIScriptOpensTheDevAppFirst(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "My Repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	repo, _ = filepath.EvalSymlinks(repo)
	dir = filepath.Dir(repo)
	link := "myfork://open?path=" + strings.ReplaceAll(repo, " ", "%20")

	got, err := runCLIScript(t, []string{"com.develiott.myfork.dev", "com.develiott.myfork"}, dir, "My Repo")
	if err != nil || got != "com.develiott.myfork.dev "+link {
		t.Errorf("both installed: %q, %v", got, err)
	}
	got, err = runCLIScript(t, []string{"com.develiott.myfork"}, dir, "My Repo", "nope/file")
	if want := "com.develiott.myfork " + link + "&path=" + dir + "/nope/file"; err != nil || got != want {
		t.Errorf("only the installed app: %q, %v, want %q", got, err, want)
	}
	// No paths launch the app plain.
	got, err = runCLIScript(t, []string{"com.develiott.myfork"}, dir)
	if err != nil || got != "com.develiott.myfork" {
		t.Errorf("no paths: %q, %v", got, err)
	}
	if _, err := runCLIScript(t, nil, dir, "My Repo"); err == nil {
		t.Error("the script succeeded with no app installed")
	}
}

func TestLinkPaths(t *testing.T) {
	got := linkPaths("myfork://open?path=/Users/me/My%20Repo&path=/tmp/a%26b%3Dc")
	want := []string{"/Users/me/My Repo", "/tmp/a&b=c"}
	if repr(got) != repr(want) {
		t.Errorf("linkPaths: %q, want %q", repr(got), repr(want))
	}
	for _, other := range []string{"https://example.com/open?path=/x", "myfork://settings?path=/x", "::"} {
		if got := linkPaths(other); len(got) != 0 {
			t.Errorf("linkPaths(%q): %q, want none", other, repr(got))
		}
	}
}
