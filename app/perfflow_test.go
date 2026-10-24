package app

// Measurement only (not to commit): times the flows of the three diff views
// on a large change, the UI thread's frames apart from the background work.
//
//	MYFORK_PERF=1 CGO_ENABLED=0 go test -run TestPerf -count=1 -v ./app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
	"github.com/eliott/myfork/internal/github"
)

const perfSrc = "/tmp/nsh-src/crates/nsh-core/src"

type perfStats struct {
	frames, bg []time.Duration
}

// settle draws frames and runs the held work until there is none.
func (s *perfStats) settle(a *app, tt *ui.Tester) {
	for i := 0; i < 20; i++ {
		t0 := time.Now()
		tt.Frame()
		s.frames = append(s.frames, time.Since(t0))
		if len(a.held) == 0 {
			return
		}
		t0 = time.Now()
		a.runHeld()
		s.bg = append(s.bg, time.Since(t0))
	}
}

func (s *perfStats) report(t *testing.T, name string) {
	sum := func(ds []time.Duration) (total, worst, p50 time.Duration) {
		if len(ds) == 0 {
			return
		}
		c := append([]time.Duration(nil), ds...)
		sort.Slice(c, func(i, j int) bool { return c[i] < c[j] })
		for _, d := range c {
			total += d
		}
		return total, c[len(c)-1], c[len(c)/2]
	}
	ft, fw, fp := sum(s.frames)
	bt, bw, _ := sum(s.bg)
	t.Logf("%-28s frames: %4d, total %7.1f ms, p50 %5.2f ms, worst %6.1f ms | background total %7.1f ms, worst %6.1f ms",
		name, len(s.frames), ms(ft), ms(fp), ms(fw), ms(bt), ms(bw))
}

// memReport says what the flow left live, after a collection.
func memReport(t *testing.T) {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	t.Logf("memory: live heap %.0f MB, from the system %.0f MB", float64(m.HeapAlloc)/(1<<20), float64(m.Sys)/(1<<20))
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func perfGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "commit.gpgsign=false", "-c", "user.email=a@b", "-c", "user.name=t"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// perfRepo is a repository of nsh-core's sources, committed, then changed
// the way /tmp/big-repo is: committed as well when commit is set.
func perfRepo(t *testing.T, commit bool, edit ...string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cp", "-R", perfSrc, filepath.Join(dir, "src")).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	perfGit(t, dir, "init", "-q", "-b", "main")
	perfGit(t, dir, "add", "-A")
	perfGit(t, dir, "commit", "-qm", "init")
	script := `find src -name "*.rs" | xargs sed -i '' 's/fn /fn  /'`
	if len(edit) > 0 {
		script = edit[0]
	}
	sed := exec.Command("sh", "-c", script)
	sed.Dir = dir
	if out, err := sed.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if commit {
		perfGit(t, dir, "add", "-A")
		perfGit(t, dir, "commit", "-qm", "change")
	}
	return dir
}

func TestPerf(t *testing.T) {
	if os.Getenv("MYFORK_PERF") == "" {
		t.Skip("set MYFORK_PERF=1")
	}
	if _, err := os.Stat(perfSrc); err != nil {
		t.Skip("no " + perfSrc)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	t.Run("commit", func(t *testing.T) {
		defer memReport(t)
		dir := perfRepo(t, true)
		a := newApp(newStore())
		a.hold = true
		tt := ui.NewTester(a.view, 1500, 950)
		var open perfStats
		t0 := time.Now()
		if err := a.openRepo(dir); err != nil {
			t.Fatal(err)
		}
		open.settle(a, tt)
		t.Logf("open on HEAD's commit: %.1f ms", ms(time.Since(t0)))
		open.report(t, "commit: open")
		v := a.tab().cv
		if v == nil || v.rv == nil {
			t.Fatal("no commit view")
		}
		var step perfStats
		t0 = time.Now()
		for i := range v.rv.files {
			v.choose(i)
			step.settle(a, tt)
		}
		t.Logf("every file chosen in turn: %.1f ms", ms(time.Since(t0)))
		step.report(t, "commit: each file")
		var again perfStats
		for i := range v.rv.files {
			v.choose(i)
			again.settle(a, tt)
		}
		again.report(t, "commit: each file again")
	})

	t.Run("commit-one-edit", func(t *testing.T) {
		defer memReport(t)
		dir := perfRepo(t, true, `find src -name "*.rs" | xargs perl -0pi -e "s/fn /fn  /"`)
		a := newApp(newStore())
		a.hold = true
		tt := ui.NewTester(a.view, 1500, 950)
		var open perfStats
		t0 := time.Now()
		if err := a.openRepo(dir); err != nil {
			t.Fatal(err)
		}
		open.settle(a, tt)
		t.Logf("open on HEAD's commit: %.1f ms", ms(time.Since(t0)))
		open.report(t, "commit-one-edit: open")
		v := a.tab().cv
		if v == nil || v.rv == nil {
			t.Fatal("no commit view")
		}
		var step perfStats
		t0 = time.Now()
		for i := range v.rv.files {
			v.choose(i)
			step.settle(a, tt)
		}
		t.Logf("every file chosen in turn: %.1f ms", ms(time.Since(t0)))
		step.report(t, "commit-one-edit: each file")
		var again perfStats
		for i := range v.rv.files {
			v.choose(i)
			again.settle(a, tt)
		}
		again.report(t, "commit-one-edit: each file again")
	})

	t.Run("bigfile", func(t *testing.T) {
		defer memReport(t)
		dir := perfRepo(t, true)
		a := newApp(newStore())
		a.hold = true
		tt := ui.NewTester(a.view, 1500, 950)
		if err := a.openRepo(dir); err != nil {
			t.Fatal(err)
		}
		var open perfStats
		open.settle(a, tt)
		v := a.tab().cv
		i := slices.IndexFunc(v.rv.files, func(f *reviewFile) bool { return f.path == "src/core/tests.rs" })
		if i < 0 {
			t.Fatal("no tests.rs")
		}
		f := v.rv.files[i]
		t.Logf("tests.rs: %d KB patch, +%d -%d", len(f.patch)>>10, f.added, f.deleted)
		var choose perfStats
		v.choose(i)
		choose.settle(a, tt)
		choose.report(t, "bigfile: show the file")
		// To its last change, as a reader going to the end of the file does.
		var end perfStats
		t0 := time.Now()
		d := f.ed
		if d == nil {
			t.Fatal("no viewer")
		}
		d.GoToHunk(len(d.Hunks()) - 1)
		end.settle(a, tt)
		t.Logf("to the last change: %.1f ms", ms(time.Since(t0)))
		end.report(t, "bigfile: to the last change")
		snapshot(t, tt, "perf-bigfile")
	})

	t.Run("work", func(t *testing.T) {
		defer memReport(t)
		dir := perfRepo(t, false)
		a := newApp(newStore())
		a.hold = true
		tt := ui.NewTester(a.view, 1500, 950)
		r, err := git.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		tab := newTab(a, r)
		a.tabs, a.cur = append(a.tabs, tab), 0
		tab.onWip = true
		var open perfStats
		t0 := time.Now()
		tab.reload()
		open.settle(a, tt)
		t.Logf("open on Local Changes: %.1f ms", ms(time.Since(t0)))
		open.report(t, "work: open")
		var step perfStats
		t0 = time.Now()
		for _, e := range append([]git.StatusEntry(nil), tab.unstaged...) {
			tab.chooseWork(false, e.Path)
			step.settle(a, tt)
		}
		t.Logf("every file chosen in turn: %.1f ms", ms(time.Since(t0)))
		step.report(t, "work: each file")
		var again perfStats
		for _, e := range append([]git.StatusEntry(nil), tab.unstaged...) {
			tab.chooseWork(false, e.Path)
			again.settle(a, tt)
		}
		again.report(t, "work: each file again")
	})

	t.Run("pr", func(t *testing.T) {
		defer memReport(t)
		a, r, f := prApp(t)
		if out, err := exec.Command("cp", "-R", perfSrc, filepath.Join(r.Root, "nsh")).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		perfGit(t, r.Root, "add", "-A")
		perfGit(t, r.Root, "commit", "-qm", "nsh")
		base, _ := r.Resolve("HEAD")
		if out, err := exec.Command("sh", "-c", `cd "$0" && find nsh -name "*.rs" | xargs sed -i '' 's/fn /fn  /'`, r.Root).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		perfGit(t, r.Root, "add", "-A")
		perfGit(t, r.Root, "commit", "-qm", "change")
		head, _ := r.Resolve("HEAD")
		f.head = head
		a.fetchPR = func(context.Context, *git.Repo, string, int, github.PR) (string, error) { return base, nil }
		tt := ui.NewTester(a.view, 1500, 950)
		a.hold = true
		t0 := time.Now()
		var open perfStats
		a.showPulls()
		open.settle(a, tt)
		if err := tt.Click("#3 Add feature"); err != nil {
			t.Fatal(err)
		}
		open.settle(a, tt)
		if err := tt.Click("Code"); err != nil {
			t.Fatal(err)
		}
		open.settle(a, tt)
		rv := a.tab().pr.rv
		t.Logf("open the code of the pull request: %.1f ms, %d files", ms(time.Since(t0)), len(rv.files))
		open.report(t, "pr: open")
		// The list and the number of its items: the rows of our renderer's, or
		// a header and the lines of each file for the editor's.
		wheelDy := float32(600)
		list, n := &rv.list, len(rv.items)
		var scroll perfStats
		t0 = time.Now()
		for i := 0; i < 400; i++ {
			list.ScrollTo(i*n/400, ui.Start)
			scroll.settle(a, tt)
		}
		first, _ := list.Visible()
		t.Logf("400 jumps through the items: %.1f ms, at item %d of %d", ms(time.Since(t0)), first, n)
		scroll.report(t, "pr: jumps")
		// Then the wheel, from the top, the same distance in either.
		list.ScrollTo(0, ui.Start)
		var wheel perfStats
		wheel.settle(a, tt)
		tt.Scroll(800, 500, 0, 600)
		wheel.settle(a, tt)
		if f, _ := list.Visible(); f == 0 {
			t.Logf("a wheel of +600 does not move the list; trying -600")
			wheelDy = -600
		}
		snapshot(t, tt, "perf-pr")
		t0 = time.Now()
		for i := 0; i < 1200; i++ {
			tt.Scroll(800, 500, 0, wheelDy)
			wheel.settle(a, tt)
		}
		first, _ = list.Visible()
		t.Logf("1200 wheel scrolls of 600px: %.1f ms, at item %d of %d", ms(time.Since(t0)), first, n)
		wheel.report(t, "pr: wheel")
	})
}
