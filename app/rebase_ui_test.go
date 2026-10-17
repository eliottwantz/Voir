package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

// linearRepo makes a repository of a few commits in a row.
func linearRepo(t *testing.T, n int) *git.Repo {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	r, err := git.Init(filepath.Join(dir, "linear"))
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][2]string{{"user.name", "Ada Lovelace"}, {"user.email", "ada@example.com"}, {"commit.gpgsign", "false"}} {
		r.SetConfig(kv[0], kv[1])
	}
	r.Do("checkout", "-q", "-b", "main")
	for i := 0; i < n; i++ {
		name := string(rune('a'+i)) + ".txt"
		os.WriteFile(filepath.Join(r.Root, name), []byte(name+"\n"), 0o644)
		r.Stage()
		if err := r.Commit(git.CommitOptions{Message: "commit " + string(rune('a'+i))}); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func appFor(t *testing.T, r *git.Repo) *app {
	t.Helper()
	a := newApp(&store{data: stored{Settings: defaultSettings(), SidebarW: 230, ListH: 330, FilesW: 280}})
	if err := a.openRepo(r.Root); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestInteractiveRebaseSheet(t *testing.T) {
	r := linearRepo(t, 5) // a b c d e
	a := appFor(t, r)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	base, _ := r.Resolve("HEAD~4") // after commit a
	a.rebaseDialog(tab, base, "commit a")
	tt.Frame()
	st := tabSheetRebase(t, a)
	if len(st.rows) != 4 || st.rows[0].subject != "commit e" || st.rows[3].subject != "commit b" {
		t.Fatalf("rows = %+v", st.rows)
	}
	snapshot(t, tt, "rebase")

	// e: pick, d: squash into c, c: reword, b: drop.
	st.rows[1].action = "Squash"
	st.rows[2].action = "Reword"
	st.rows[2].message = "commit c, reworded"
	st.rows[3].action = "Drop"
	tt.Frame()
	if err := tt.Click("Start Rebase"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	cs, _ := r.Log(git.LogOptions{})
	var got []string
	for _, c := range cs {
		got = append(got, c.Subject)
	}
	// d melded into c (whose message is the reworded one plus d's), b is gone.
	if len(got) != 3 || got[0] != "commit e" || got[1] != "commit c, reworded" || got[2] != "commit a" {
		t.Fatalf("log = %q", got)
	}
	if a.sheet != nil {
		t.Errorf("the sheet is still open")
	}
}

func TestRebaseSheetRefusesWhatGitRefuses(t *testing.T) {
	r := linearRepo(t, 3)
	a := appFor(t, r)
	tt := ui.NewTester(a.view, 1280, 800)
	base, _ := r.Resolve("HEAD~2")
	a.rebaseDialog(a.tab(), base, "commit a")
	tt.Frame()
	st := tabSheetRebase(t, a)
	st.rows[len(st.rows)-1].action = "Fixup" // the oldest one
	if p := st.problem(); !strings.Contains(p, "oldest") {
		t.Fatalf("problem = %q", p)
	}
	st.rows[len(st.rows)-1].action = "Drop" // then the next one is the oldest
	st.rows[0].action = "Squash"
	if p := st.problem(); !strings.Contains(p, "oldest") {
		t.Fatalf("problem with a drop below = %q", p)
	}
	st.rows[0].action = "Pick"
	st.rows[0].action = "Reword"
	st.rows[0].message = "  "
	if p := st.problem(); !strings.Contains(p, "message") {
		t.Fatalf("problem = %q", p)
	}
	st.rows[0].action = "Pick"
	if p := st.problem(); p != "" {
		t.Fatalf("problem = %q", p)
	}
}

func tabSheetRebase(t *testing.T, a *app) *rebaseState {
	t.Helper()
	if a.sheet == nil {
		t.Fatal("no sheet")
	}
	st, ok := a.sheet.state.(*rebaseState)
	if !ok {
		t.Fatalf("sheet state = %T", a.sheet.state)
	}
	return st
}
