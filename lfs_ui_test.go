package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

func TestGitLFSSheet(t *testing.T) {
	if !git.LFSAvailable() {
		t.Skip("git-lfs is not installed")
	}
	r := linearRepo(t, 1)
	a := appFor(t, r)
	tt := ui.NewTester(a.view, 1280, 800)
	a.lfsSheet(a.tab())
	tt.Frame()
	if !tt.HasText("Git LFS is not set up in this repository.") {
		t.Fatalf("texts %q", tt.Texts())
	}
	if err := tt.Click("Install Git LFS"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	st := a.sheet.state.(*lfsState)
	if !st.enabled {
		t.Fatalf("not enabled: err %q", st.err)
	}
	if err := tt.Click("Pattern to track"); err != nil {
		t.Fatal(err)
	}
	tt.Type("*.bin")
	tt.Frame()
	if err := tt.Click("Track"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(st.patterns) != 1 || st.patterns[0].Pattern != "*.bin" {
		t.Fatalf("patterns = %+v, err %q", st.patterns, st.err)
	}
	// A file under the pattern is stored with LFS once committed.
	os.WriteFile(filepath.Join(r.Root, "big.bin"), []byte(strings.Repeat("z", 64)), 0o644)
	r.Stage()
	if err := r.Commit(git.CommitOptions{Message: "add big"}); err != nil {
		t.Fatal(err)
	}
	st.refresh()
	tt.Frame()
	if len(st.files) != 1 || st.files[0].Path != "big.bin" {
		t.Fatalf("files = %+v", st.files)
	}
	snapshot(t, tt, "lfs")
	if err := tt.Click("Untrack"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(st.patterns) != 0 {
		t.Fatalf("still tracked: %+v", st.patterns)
	}
}

func TestLFSPointerNoteInTheDiff(t *testing.T) {
	if !git.LFSAvailable() {
		t.Skip("git-lfs is not installed")
	}
	r := linearRepo(t, 1)
	r.LFSInstall()
	r.LFSTrack("*.bin")
	os.WriteFile(filepath.Join(r.Root, "big.bin"), []byte(strings.Repeat("q", 40)), 0o644)
	r.Stage()
	r.Commit(git.CommitOptions{Message: "add big"})
	a := appFor(t, r)
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	if err := tt.Click("big.bin"); err != nil {
		t.Fatalf("%v; %q", err, tt.Texts())
	}
	tt.Frame()
	if !tt.HasText("This file is stored with Git LFS: the lines are its pointer, not its content.") {
		t.Errorf("no LFS note in %q", tt.Texts())
	}
}
