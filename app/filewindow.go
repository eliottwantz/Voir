package app

import (
	"fmt"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/eliott/voir/internal/fileicon"
	"github.com/eliott/voir/internal/git"
	"github.com/eliott/voir/internal/highlight"
)

type fileWindowKind uint8

const (
	historyWindow fileWindowKind = iota
	blameWindowKind
)

// maxFileCommits is how many of a file's commits its windows read.
const maxFileCommits = 1000

// fileWindow is a window about one file: its History, or its Blame with a
// timeline, both over the commits that changed it.
type fileWindow struct {
	app  *app
	tab  *repoTab
	win  *mygo.Window
	kind fileWindowKind
	path string

	loading bool
	err     error
	commits []git.FileCommit
	// sel is the commit chosen, an index of commits; start is the revision the
	// window was opened at, which it first chooses the commit of.
	sel   int
	start string

	// History: the list of the commits, and the patch of the one chosen.
	list ui.ListState
	pane diffPane

	// Blame: what the file was at each commit read, and the one shown.
	blames       map[string]*blameData
	blameKey     string
	blameLoading bool
	blameList    ui.ListState
}

// blameData is the blame of a file at a commit, with where its lines were
// before the commit.
type blameData struct {
	lines []git.BlameLine
	lmap  *git.LineMap
	segs  [][]highlight.Seg
	err   string
}

// fileHistoryWindow shows the commits that changed a file, with their patch.
func (a *app) fileHistoryWindow(tab *repoTab, p string) {
	a.openFileWindow(tab, p, historyWindow, "")
}

// blameWindow shows who last changed each line of a file, at a commit.
func (a *app) blameWindow(tab *repoTab, p, rev string) {
	a.openFileWindow(tab, p, blameWindowKind, rev)
}

func (a *app) openFileWindow(tab *repoTab, p string, kind fileWindowKind, rev string) {
	for _, fw := range a.fwins {
		if fw.tab == tab && fw.path == p && fw.kind == kind {
			if rev != "" {
				fw.start = rev
				fw.chooseStart()
			}
			if fw.win != nil {
				fw.win.Restore()
				fw.win.Focus()
			}
			return
		}
	}
	fw := &fileWindow{app: a, tab: tab, kind: kind, path: p, start: rev, loading: true, blames: map[string]*blameData{}}
	fw.list.Selected = &fw.sel
	fw.list.Key = func(i int) any {
		if i < 0 || i >= len(fw.commits) {
			return i
		}
		return fw.commits[i].Hash
	}
	a.fwins = append(a.fwins, fw)
	fw.load()
	if a.win == nil {
		return
	}
	fw.win = mygo.NewWindow(mygo.WindowOptions{
		Title:                fw.title(),
		Width:                1180,
		Height:               780,
		MinWidth:             720,
		MinHeight:            460,
		TitleBarStyle:        mygo.TitleBarHiddenInset,
		TitleBarHeight:       titleBarHeight,
		TrafficLightPosition: trafficLights,
		Content:              ui.View(fw.view),
		Page:                 mygo.PageOptions{DevTools: devTools()},
	})
	fw.win.OnClosed(func() { a.update(func() { a.forgetFileWindow(fw) }) })
}

func (fw *fileWindow) title() string {
	_, base := dirAndBase(fw.path)
	if fw.kind == historyWindow {
		return "History — " + base
	}
	return "Blame — " + base
}

// forgetFileWindow drops a window that closed.
func (a *app) forgetFileWindow(fw *fileWindow) {
	a.fwins = slices.DeleteFunc(a.fwins, func(o *fileWindow) bool { return o == fw })
}

// closeFileWindows closes the windows of files, as the main window goes.
func (a *app) closeFileWindows() {
	for _, fw := range slices.Clone(a.fwins) {
		if fw.win != nil {
			fw.win.Close()
		}
	}
}

// load reads the commits that changed the file, and chooses one.
func (fw *fileWindow) load() {
	a, repo, p, start := fw.app, fw.tab.repo, fw.path, fw.start
	fw.loading = true
	a.background(func() {
		cs, err := repo.FileHistory(p, maxFileCommits)
		at := ""
		if err == nil && start != "" && !slices.ContainsFunc(cs, func(c git.FileCommit) bool { return c.Hash == start }) {
			// A commit that did not change the file: the last that did, before it.
			if found, e := repo.Log(git.LogOptions{Revs: []string{start}, Path: p, Follow: true, Limit: 1}); e == nil && len(found) > 0 {
				at = found[0].Hash
			}
		}
		a.update(func() {
			fw.loading, fw.commits, fw.err = false, cs, err
			if at != "" {
				fw.start = at
			}
			fw.chooseStart()
		})
	})
}

// chooseStart chooses the commit the window was opened at, or the newest.
func (fw *fileWindow) chooseStart() {
	if len(fw.commits) == 0 {
		return
	}
	i := slices.IndexFunc(fw.commits, func(c git.FileCommit) bool { return c.Hash == fw.start })
	fw.choose(max(i, 0))
}

// choose chooses the commit of index i.
func (fw *fileWindow) choose(i int) {
	if i < 0 || i >= len(fw.commits) {
		return
	}
	fw.sel = i
	if first, last := fw.list.Visible(); i < first || i > last {
		fw.list.ScrollTo(i, ui.Center)
	}
	if fw.kind == historyWindow {
		fw.showPatch()
	} else {
		fw.showBlame()
	}
}

// paths are the names the file had in a commit: the one before a rename, and
// the one after.
func commitPaths(fc git.FileCommit) []string {
	var paths []string
	for _, p := range []string{fc.OldPath, fc.Path} {
		if p != "" && !slices.Contains(paths, p) {
			paths = append(paths, p)
		}
	}
	return paths
}

// firstParentOrEmpty is what a commit is compared with: its first parent, the
// empty tree for the first commit.
func firstParentOrEmpty(c *git.Commit) string {
	if len(c.Parents) > 0 {
		return c.Parents[0]
	}
	return git.EmptyTree
}

// showPatch reads the patch of the commit chosen, for History.
func (fw *fileWindow) showPatch() {
	if fw.sel < 0 || fw.sel >= len(fw.commits) {
		return
	}
	fc := fw.commits[fw.sel]
	repo, o := fw.tab.repo, fw.tab.diffOptions()
	fw.tab.loadDiff(&fw.pane, "fh:"+fc.Hash, fc.Path, func() (string, error) {
		return repo.PathsDiff(firstParentOrEmpty(&fc.Commit), fc.Hash, o, commitPaths(fc)...)
	})
}

// showBlame reads the blame of the file at the commit chosen, for Blame.
func (fw *fileWindow) showBlame() {
	if fw.sel < 0 || fw.sel >= len(fw.commits) {
		return
	}
	fc := fw.commits[fw.sel]
	fw.blameKey = fc.Hash
	if _, ok := fw.blames[fc.Hash]; ok {
		fw.blameLoading = false
		return
	}
	fw.blameLoading = true
	a, repo := fw.app, fw.tab.repo
	a.background(func() {
		d := &blameData{}
		lines, err := repo.Blame(fc.Hash, fc.Path)
		if err != nil {
			d.err = errorText(err)
		} else {
			d.lines = lines
			patch, _ := repo.PathsDiff(firstParentOrEmpty(&fc.Commit), fc.Hash, git.DiffOptions{Context: 3}, commitPaths(fc)...)
			d.lmap = git.ParseLineMap(patch)
			texts := make([]string, len(lines))
			for i, l := range lines {
				texts[i] = l.Text
			}
			d.segs = highlight.Lines(fc.Path, texts)
		}
		a.update(func() {
			fw.blames[fc.Hash] = d
			if fw.blameKey == fc.Hash {
				fw.blameLoading = false
			}
		})
	})
}

// revealCommit shows a commit in the graph of the main window.
func (a *app) revealCommit(tab *repoTab, hash string) {
	a.update(func() {
		if i := slices.Index(a.tabs, tab); i >= 0 {
			a.cur = i
		}
		a.pulls = false
		tab.jumpTo(hash)
		if a.win != nil {
			a.win.Restore()
			a.win.Focus()
		}
	})
}

// ---- the view ----

func (fw *fileWindow) view(c *ui.Context) {
	fw.app.applyThemeChoice(c)
	t := c.Theme()
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Background(t.Background).Children(func() {
		fw.header(c)
		if fw.loading || fw.err != nil || len(fw.commits) == 0 {
			fw.empty(c)
			return
		}
		fw.timeline(c)
		if fw.kind == historyWindow {
			fw.historyBody(c)
		} else {
			fw.blameBody(c)
		}
	})
}

func (fw *fileWindow) empty(c *ui.Context) {
	t := c.Theme()
	switch {
	case fw.loading:
		ui.Column(c).Grow(1).Center().Children(func() { spinner(c, "Loading") })
	case fw.err != nil:
		ui.Text(c, errorText(fw.err)).TextColor(t.Danger).Padding(20).Selectable()
	default:
		ui.Text(c, "No commit changed this file").TextColor(t.TextMuted).Padding(20).TextAlign(ui.Center).FillWidth()
	}
}

// header is the title bar: what the window is, the file, and on the right the
// branch (History) or the choice of commit (Blame).
func (fw *fileWindow) header(c *ui.Context) {
	t := c.Theme()
	bar := c.TitleBar()
	dir, base := dirAndBase(fw.path)
	ui.Row(c).Height(max(bar.Height, titleBarHeight)).Padding(0, bar.Right+14, 0, bar.Left+8).Gap(10).Shrink(0).DragWindow().AlignItems(ui.Center).Children(func() {
		name := "History"
		if fw.kind == blameWindowKind {
			name = "Blame"
		}
		ui.Text(c, name).FontSize(17).FontWeight(500).Shrink(0)
		ic := fileicon.For(fw.path)
		ui.Icon(c, ic.SVG).FontSize(16).TextColor(ic.Color(t.Dark)).Shrink(0)
		path := ui.RichText(c,
			ui.Span{Text: dir, Color: t.TextMuted},
			ui.Span{Text: base, Color: t.Text, Weight: 600},
		).FontSize(14).SingleLine().MinWidth(0).Shrink(1)
		if fw.kind == historyWindow {
			path.Grow(1)
			ui.Row(c).Gap(6).Shrink(0).AlignItems(ui.Center).TextColor(t.TextMuted).Children(func() {
				ui.Icon(c, iconBranch).FontSize(15)
				branch := "HEAD"
				if b := fw.tab.currentBranch(); b != nil {
					branch = b.Name
				}
				ui.Text(c, branch).FontSize(13.5).SingleLine().Shrink(0)
			})
			return
		}
		ui.Spacer(c)
		fw.stepper(c)
	})
}

// stepper is the buttons that go to the commit before and after the one
// shown, and the list of every commit that changed the file.
func (fw *fileWindow) stepper(c *ui.Context) {
	if len(fw.commits) == 0 {
		return
	}
	t := c.Theme()
	ui.Row(c).Gap(0).Shrink(0).Radius(7).Background(hoverBg).AlignItems(ui.Center).Children(func() {
		older := iconButton(c, iconChevronLeft, "Previous commit").Disabled(fw.sel >= len(fw.commits)-1)
		if older.Clicked() {
			fw.choose(fw.sel + 1)
		}
		ui.Box(c).Width(1).Height(14).Background(hairline)
		newer := iconButton(c, iconChevronRight, "Next commit").Disabled(fw.sel <= 0)
		if newer.Clicked() {
			fw.choose(fw.sel - 1)
		}
	})
	labels := make([]string, len(fw.commits))
	for i := range fw.commits {
		labels[i] = fw.commitLabel(i)
	}
	v := labels[min(fw.sel, len(labels)-1)]
	w, _ := c.Size()
	if ui.Select(c.Key("blame-commit"), &v, labels).Width(min(float32(w)*0.5, 620)).Shrink(1).MinWidth(120).Label("Commit").TextColor(t.Text).Changed() {
		if i := slices.Index(labels, v); i >= 0 {
			fw.choose(i)
		}
	}
}

// commitLabel is a commit in the list of choices: its hash, its subject and
// its date.
func (fw *fileWindow) commitLabel(i int) string {
	fc := fw.commits[i]
	return fmt.Sprintf("%s  %s (%s)", fc.Short(), fc.Subject, fc.AuthorTime.Format("Jan 2, 2006"))
}

// ---- the timeline ----

// timeline is a strip of the dates of the commits that changed the file, the
// one chosen marked, as the months it covers are named under it.
func (fw *fileWindow) timeline(c *ui.Context) {
	t := c.Theme()
	n := len(fw.commits)
	// Oldest first, as time reads.
	times := make([]int64, n)
	for i, fc := range fw.commits {
		times[n-1-i] = fc.AuthorTime.Unix()
	}
	first, last := time.Unix(times[0], 0), time.Unix(times[n-1], 0)
	start := time.Date(first.Year(), first.Month(), 1, 0, 0, 0, 0, time.Local)
	end := time.Date(last.Year(), last.Month()+1, 1, 0, 0, 0, 0, time.Local)
	lo, hi := start.Unix(), end.Unix()
	span := float64(hi - lo)

	tick := t.TextMuted.Alpha(0.55)
	ui.Column(c).Padding(0, 14, 6).Gap(0).Shrink(0).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
		ui.Row(c).Height(30).FillWidth().Radius(3).Border(1, hairline).Background(t.Background).Children(func() {
			for k := 0; k < n; k++ {
				i := n - 1 - k // the commit's index in commits
				// The cell spans from half way to the commit before to half
				// way to the one after, so that a click picks the nearest.
				left, right := float64(lo), float64(hi)
				if k > 0 {
					left = float64(times[k-1]+times[k]) / 2
				}
				if k < n-1 {
					right = float64(times[k]+times[k+1]) / 2
				}
				width := max(right-left, 1)
				frac := float32((float64(times[k]) - left) / width)
				fc := fw.commits[i]
				cell := ui.ButtonBase(c.Key("tl:" + fc.Hash)).Grow(float32(width)).Shrink(1).MinWidth(0).Height(28).Padding(0).
					Label(fc.Short() + " " + fc.Subject).Tooltip(fc.Subject + " · " + fc.AuthorTime.Format("Jan 2, 2006"))
				chosen := i == fw.sel
				accent := ui.Hex("#f0b90b")
				cell.Draw(func(p *ui.Painter, r ui.Rect) {
					x := r.X + r.W*frac
					p.Fill(ui.Rect{X: x - 0.5, Y: r.Y + r.H - 12, W: 1, H: 12}, tick, 0)
					if chosen {
						var path ui.Path
						path.MoveTo(x-4.5, r.Y+1).LineTo(x+4.5, r.Y+1).LineTo(x, r.Y+9).Close()
						p.FillPath(&path, accent)
						p.Fill(ui.Rect{X: x - 0.5, Y: r.Y + 9, W: 1, H: r.H - 9}, accent, 0)
					}
				})
				if cell.Clicked() {
					fw.choose(i)
				}
			}
		})
		// The months, each from its first day.
		ui.Row(c).Height(18).FillWidth().Children(func() {
			for m := start; m.Before(end); m = m.AddDate(0, 1, 0) {
				next := m.AddDate(0, 1, 0)
				w := float32(float64(next.Unix()-m.Unix()) / span)
				ui.Row(c.Key("m:"+m.Format("2006-01"))).Grow(w).Shrink(1).MinWidth(0).Padding(0, 0, 0, 4).BorderWidth(0, 0, 0, 1).BorderColor(hairline).Children(func() {
					ui.Text(c, fmt.Sprintf("%d/%d", int(m.Month()), m.Year())).FontSize(11.5).TextColor(t.TextMuted).SingleLine().MinWidth(0)
				})
			}
		})
	})
}

// ---- History ----

func (fw *fileWindow) historyBody(c *ui.Context) {
	now := c.Now()
	ui.Split(c, &fw.app.store.data.HistoryW, func() {
		ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
			list := ui.List(c, &fw.list, len(fw.commits), func(i int) {
				fc := fw.commits[i]
				ui.Column(c).Padding(7, 10).Gap(3).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						fw.tab.authorAvatar(c, fc.Author, fc.AuthorEmail, fc.Hash).Size(24, 24)
						ui.Text(c, fc.Author).FontSize(13).SingleLine().Grow(1).MinWidth(0)
						ui.Text(c, fc.AuthorTime.Format("Jan 2, 2006")).FontSize(12).Opacity(0.7).SingleLine().Shrink(0)
						ui.Text(c, fc.Short()).Font(mono).FontSize(11.5).Opacity(0.7).Shrink(0)
						fw.revealButton(c, fc.Hash)
					})
					ui.Text(c, fc.Subject).FontSize(13.5).FontWeight(600).SingleLine().MinWidth(0)
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						statusBadge(c, fc.Status)
						ui.Text(c, fc.Path).FontSize(11.5).Opacity(0.7).SingleLine().MinWidth(0).Grow(1)
					})
				}).Tooltip(fc.Subject + "\n" + relativeTime(now, fc.AuthorTime))
			}).Fill().MinHeight(0).Label("Commits")
			if list.Changed() {
				fw.choose(fw.sel)
			}
		})
	}, func() { fw.tab.diffArea(c, &fw.pane) }).Fill().MinHeight(0)
}

// revealButton is the button that shows a commit in the graph of the main
// window.
func (fw *fileWindow) revealButton(c *ui.Context, hash string) {
	b := ui.ButtonBase(c.Key("reveal:"+hash)).Size(18, 18).Radius(4).Center().Shrink(0).Opacity(0.7).Label("Show in the graph").Tooltip("Show the commit in the graph")
	if b.Hovered() {
		b.Background(hoverBg)
	}
	if b.Clicked() {
		fw.app.revealCommit(fw.tab, hash)
	}
	b.Children(func() { ui.Icon(c, iconOpen).FontSize(13) })
}

// ---- Blame ----

const (
	blameInfoW = 330
	blameRowH  = 21
)

func (fw *fileWindow) blameBody(c *ui.Context) {
	t := c.Theme()
	d := fw.blames[fw.blameKey]
	switch {
	case d == nil || fw.blameLoading:
		ui.Column(c).Grow(1).Center().Children(func() { spinner(c, "Loading") })
		return
	case d.err != "":
		ui.Text(c, d.err).TextColor(t.Danger).Padding(20).Selectable()
		return
	}
	pal := diffPaletteFor(t)
	ui.List(c, &fw.blameList, len(d.lines), func(i int) {
		fw.blameRow(c, d, i, pal)
	}).Grow(1).MinHeight(0).Label("Blame")
}

// blameRow is a line of the file: who last changed it when it starts a run of
// lines of one commit, its numbers before and after the commit chosen, and
// its code, in green when that commit added it.
func (fw *fileWindow) blameRow(c *ui.Context, d *blameData, i int, pal *diffPalette) {
	if i >= len(d.lines) {
		return
	}
	t := c.Theme()
	l := d.lines[i]
	first := i == 0 || d.lines[i-1].Hash != l.Hash
	old, added := d.lmap.Old(l.Line)
	row := ui.Row(c).Height(blameRowH).AlignItems(ui.Center)
	if first && i > 0 {
		row.BorderWidth(1, 0, 0, 0).BorderColor(hairline)
	}
	row.Children(func() {
		ui.Row(c).Width(blameInfoW).Shrink(0).Padding(0, 6, 0, 12).Gap(8).AlignItems(ui.Center).BorderWidth(0, 1, 0, 0).BorderColor(hairline).Children(func() {
			if !first {
				return
			}
			fw.tab.authorAvatar(c, l.Author, l.Email, l.Hash).Size(18, 18).Shrink(0)
			ui.Text(c, l.Summary).FontSize(12).SingleLine().Grow(1).MinWidth(0)
			ui.Text(c, time.Unix(l.Time, 0).Format("Jan 2, 2006")).FontSize(11.5).TextColor(t.TextMuted).SingleLine().Shrink(0)
			ui.Text(c, l.Hash[:min(7, len(l.Hash))]).Font(mono).FontSize(11).TextColor(t.TextMuted).Shrink(0)
			fw.revealButton(c, l.Hash)
		}).Tooltip(l.Author + " · " + l.Summary)
		gutter, code := ui.Color{}, ui.Color{}
		if added {
			gutter, code = pal.addGutter, pal.addBg
		}
		ui.Row(c).Shrink(0).Height(blameRowH).Padding(0, 6).Gap(6).AlignItems(ui.Center).Background(gutter).Children(func() {
			oldText := ""
			if !added {
				oldText = fmt.Sprint(old)
			}
			ui.Text(c, oldText).Font(mono).FontSize(11).TextColor(pal.lineNumber).Width(30).TextAlign(ui.End).SingleLine()
			ui.Text(c, fmt.Sprint(l.Line)).Font(mono).FontSize(11).TextColor(pal.lineNumber).Width(30).TextAlign(ui.End).SingleLine()
		})
		var segs []highlight.Seg
		if i < len(d.segs) {
			segs = d.segs[i]
		}
		ui.Row(c).Grow(1).MinWidth(0).Height(blameRowH).Padding(0, 10).AlignItems(ui.Center).Background(code).Children(func() {
			ui.RichText(c, styleCode(l.Text, segs, pal)...).Font(mono).FontSize(12).SingleLine().MinWidth(0)
		})
	})
}
