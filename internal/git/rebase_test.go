package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func subjects(t *testing.T, r *Repo) []string {
	t.Helper()
	cs, err := r.Log(LogOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var s []string
	for _, c := range cs {
		s = append(s, c.Subject)
	}
	return s
}

func TestInteractiveRebase(t *testing.T) {
	r := newRepo(t)
	write(t, r, "base.txt", "base\n")
	commit(t, r, "base")
	for _, n := range []string{"a", "b", "c", "d"} {
		write(t, r, n+".txt", n+"\n")
		commit(t, r, "add "+n)
	}
	items, err := r.RebaseCommits("HEAD~4")
	if err != nil || len(items) != 4 || items[0].Subject != "add a" {
		t.Fatalf("commits = %+v, %v", items, err)
	}
	todo := []TodoItem{
		{Action: ActionReword, Hash: items[0].Hash, Message: "add a, reworded\n\nwith a body"},
		{Action: ActionDrop, Hash: items[1].Hash},
		{Action: ActionPick, Hash: items[2].Hash},
		{Action: ActionFixup, Hash: items[3].Hash},
	}
	backup, err := r.RebaseInteractive("HEAD~4", todo, RebaseOptions{Backup: true})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(subjects(t, r), "|")
	if got != "add c|add a, reworded|base" {
		t.Fatalf("log = %s", got)
	}
	// d was folded into c, b dropped.
	for name, want := range map[string]bool{"a.txt": true, "b.txt": false, "c.txt": true, "d.txt": true} {
		_, err := os.Stat(filepath.Join(r.Root, name))
		if (err == nil) != want {
			t.Errorf("%s present = %v, want %v", name, err == nil, want)
		}
	}
	cs, _ := r.Log(LogOptions{})
	if cs[1].Body != "with a body" {
		t.Errorf("body = %q", cs[1].Body)
	}
	// The backup branch still has the old history.
	old, _ := r.Log(LogOptions{Revs: []string{backup}})
	if len(old) != 5 {
		t.Errorf("backup %s has %d commits, want 5", backup, len(old))
	}
	if r.State() != "" {
		t.Errorf("state = %q", r.State())
	}
}

func TestInteractiveRebaseFixupKeepsTheMessageOfTheMeld(t *testing.T) {
	r := newRepo(t)
	write(t, r, "base.txt", "base\n")
	commit(t, r, "base")
	for _, n := range []string{"a", "b"} {
		write(t, r, n+".txt", n+"\n")
		commit(t, r, "add "+n)
	}
	items, err := r.RebaseCommits("HEAD~2")
	if err != nil || len(items) != 2 {
		t.Fatalf("commits = %+v, %v", items, err)
	}
	todo := []TodoItem{
		{Action: ActionPick, Hash: items[0].Hash},
		{Action: ActionFixupMessage, Hash: items[1].Hash},
	}
	if _, err := r.RebaseInteractive("HEAD~2", todo, RebaseOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(subjects(t, r), "|"); got != "add b|base" {
		t.Fatalf("log = %s", got)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(r.Root, name)); err != nil {
			t.Errorf("%s missing: %v", name, err)
		}
	}
}

func TestInteractiveRebaseStopsForEdit(t *testing.T) {
	r := newRepo(t)
	write(t, r, "base.txt", "base\n")
	commit(t, r, "base")
	write(t, r, "a.txt", "a\n")
	commit(t, r, "add a")
	write(t, r, "b.txt", "b\n")
	commit(t, r, "add b")
	items, _ := r.RebaseCommits("HEAD~2")
	_, err := r.RebaseInteractive("HEAD~2", []TodoItem{{Action: ActionEdit, Hash: items[0].Hash}, {Action: ActionPick, Hash: items[1].Hash}}, RebaseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.State() != "rebase" {
		t.Fatalf("state = %q, want rebase", r.State())
	}
	write(t, r, "a.txt", "a, edited\n")
	r.Stage()
	if _, err := r.Do("commit", "--amend", "-q", "--no-edit"); err != nil {
		t.Fatal(err)
	}
	if err := r.Continue(); err != nil {
		t.Fatal(err)
	}
	if r.State() != "" || strings.Join(subjects(t, r), "|") != "add b|add a|base" {
		t.Fatalf("state %q, log %v", r.State(), subjects(t, r))
	}
}

func TestInteractiveRebaseConflictAborts(t *testing.T) {
	r := newRepo(t)
	write(t, r, "f.txt", "1\n")
	commit(t, r, "base")
	write(t, r, "f.txt", "2\n")
	commit(t, r, "two")
	write(t, r, "f.txt", "3\n")
	commit(t, r, "three")
	items, _ := r.RebaseCommits("HEAD~2")
	// Dropping "two" makes "three" conflict.
	_, err := r.RebaseInteractive("HEAD~2", []TodoItem{{Action: ActionDrop, Hash: items[0].Hash}, {Action: ActionPick, Hash: items[1].Hash}}, RebaseOptions{})
	if err == nil || r.State() != "rebase" {
		t.Fatalf("err %v, state %q", err, r.State())
	}
	if err := r.Abort(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(subjects(t, r), "|") != "three|two|base" {
		t.Fatalf("log after abort = %v", subjects(t, r))
	}
}
