package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
)

func TestLinkCLI(t *testing.T) {
	source := filepath.Join(t.TempDir(), "MyFork.app", "Contents", "MacOS", "MyFork")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// A missing bin dir is a failure, not an install.
	if _, err := linkCLI(source, filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("installed the CLI into a missing directory")
	}

	// The first install makes the link.
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(bin, cliName)
	if got, err := linkCLI(source, bin); err != nil || got != link {
		t.Fatalf("linkCLI: %q, %v", got, err)
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is no symlink: %v", link, err)
	}

	// An old symlink is replaced, a real file is not.
	other := filepath.Join(bin, "other")
	if err := os.WriteFile(other, []byte("next"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := linkCLI(source, bin); err != nil || got != link {
		t.Fatalf("linkCLI: %q, %v", got, err)
	}
	target, err := os.Readlink(link)
	if err != nil || target != source {
		t.Fatalf("the link is not %s: %q, %v", source, target, err)
	}
	// A real file is not replaced.
	os.Remove(link)
	if err := os.WriteFile(link, []byte("keep me"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := linkCLI(source, bin); err == nil {
		t.Fatal("replaced a real file with a symlink")
	}
	data, _ := os.ReadFile(link)
	if string(data) != "keep me" {
		t.Errorf("the real file changed: %q", data)
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

	link := filepath.Join(bin, cliName)
	st, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("no link after the install: %v", err)
	}
	if st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is no symlink", link)
	}

	// When the bin does not exist, the install fails with a dialog.
	cliBin = func() (string, error) { return "", os.ErrNotExist }
	item.Click(nil, nil)
	d := a.dialog
	if d == nil || d.ok != "OK" || d.title != "Could not install the myfork CLI" {
		t.Fatalf("no install fail dialog: %+v", d)
	}
}
