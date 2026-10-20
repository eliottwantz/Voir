package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileHistoryFollowsRenamesAndNamesWhatEachCommitDid(t *testing.T) {
	r := newRepo(t)
	write(t, r, "old.txt", "one\ntwo\nthree\n")
	commit(t, r, "Add old")
	write(t, r, "other.txt", "x\n")
	commit(t, r, "Add other")
	if _, err := r.Do("mv", "old.txt", "new.txt"); err != nil {
		t.Fatal(err)
	}
	commit(t, r, "Rename")
	write(t, r, "new.txt", "one\ntwo\nthree\nfour\n")
	commit(t, r, "Add four")

	hist, err := r.FileHistory("new.txt", 100)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range hist {
		got = append(got, h.Subject+":"+string(h.Status)+":"+h.Path)
	}
	want := "Add four:M:new.txt|Rename:R:new.txt|Add old:A:old.txt"
	if strings.Join(got, "|") != want {
		t.Errorf("history\n got %s\nwant %s", strings.Join(got, "|"), want)
	}
	if hist[1].OldPath != "old.txt" {
		t.Errorf("renamed from %q", hist[1].OldPath)
	}
}

func TestLineMapSaysWhereTheLinesWere(t *testing.T) {
	r := newRepo(t)
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, "line"+string(rune('a'+i%26)))
	}
	write(t, r, "f.txt", strings.Join(lines, "\n")+"\n")
	commit(t, r, "Base")
	// Two lines added at 3, one removed at 20 and one changed at 28.
	next := append([]string{}, lines[:2]...)
	next = append(next, "new1", "new2")
	next = append(next, lines[2:19]...)
	next = append(next, lines[20:27]...)
	next = append(next, "changed")
	next = append(next, lines[28:]...)
	write(t, r, "f.txt", strings.Join(next, "\n")+"\n")
	commit(t, r, "Edit")
	head, _ := r.Resolve("HEAD")
	patch, err := r.PathsDiff(head+"^", head, DiffOptions{Context: 3}, "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	m := ParseLineMap(patch)
	for newLine, text := range next {
		old, added := m.Old(newLine + 1)
		switch text {
		case "new1", "new2", "changed":
			if !added {
				t.Errorf("line %d (%s) is not added", newLine+1, text)
			}
		default:
			if added || old == 0 {
				t.Errorf("line %d (%s) added, old %d", newLine+1, text, old)
				continue
			}
			if lines[old-1] != text {
				t.Errorf("line %d (%s) was line %d (%s)", newLine+1, text, old, lines[old-1])
			}
		}
	}
}

func TestStashRestoreAndPatchOfSomeFiles(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "a\n")
	write(t, r, "b.txt", "b\n")
	commit(t, r, "Base")
	write(t, r, "a.txt", "a2\n")
	write(t, r, "b.txt", "b2\n")
	write(t, r, "c.txt", "c\n")

	patch, err := r.WorkPatch([]string{"a.txt", "c.txt"}, map[string]bool{"c.txt": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "+a2") || !strings.Contains(patch, "+c") || strings.Contains(patch, "b2") {
		t.Errorf("patch:\n%s", patch)
	}

	if err := r.StashPushPaths("only a and c", true, []string{"a.txt", "c.txt"}); err != nil {
		t.Fatal(err)
	}
	st, _ := r.Status()
	if len(st.Entries) != 1 || st.Entries[0].Path != "b.txt" {
		t.Errorf("status after the stash: %+v", st.Entries)
	}
	if err := r.StashPushPaths("", false, nil); err == nil {
		t.Error("a stash of no file")
	}

	// RestoreFile puts a file back to a revision, in the work tree only.
	if err := r.RestoreFile("HEAD", "b.txt"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(r.Root, "b.txt"))
	if string(data) != "b\n" {
		t.Errorf("b.txt is %q", data)
	}
}
