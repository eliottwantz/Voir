package app

import (
	"bytes"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/eliott/myfork/internal/fileicon"
	"github.com/eliott/myfork/internal/git"
)

// The section under the graph shows the commit chosen in three tabs: its
// summary, its changes (the files, and the diff of the one chosen) and its
// file tree, the code as it was at that commit. The commit's own window shows
// the same view.

// panelTab is a tab of the commit view.
type panelTab int

const (
	paneCommit panelTab = iota
	paneChanges
	paneTree
)

var paneNames = [...]string{"Summary", "Changes", "File Tree"}

// commitView is the state of the three tabs for one commit.
type commitView struct {
	app *app
	tab *repoTab
	// windowed is set for the view in the commit's own window.
	windowed bool
	pane     panelTab
	commit   *git.Commit
	// base is set when two commits are compared: the view then shows what
	// changed from base to commit. pair holds the two, older first, and
	// swapped says they are the other way round.
	base    *git.Commit
	pair    [2]*git.Commit
	swapped bool
	// rv holds the commit's files, with their patches.
	rv *review

	// The Changes tab: the file chosen, the list of files, and its diff.
	file   string
	filter string
	closed map[string]bool
	files  ui.ListState

	tree codeTree
}

func newCommitView(a *app, t *repoTab) *commitView {
	return &commitView{app: a, tab: t, pane: paneChanges, closed: map[string]bool{}}
}

// show makes the view about a commit, and reads its files.
func (v *commitView) show(c *git.Commit) {
	v.base, v.pair, v.swapped = nil, [2]*git.Commit{}, false
	v.open(c)
}

// showCompare makes the view about what changed between two commits: from the
// older to the newer, until swap.
func (v *commitView) showCompare(x, y *git.Commit) {
	older, newer := x, y
	switch {
	case newer.CommitTime.Before(older.CommitTime):
		older, newer = newer, older
	case newer.CommitTime.Equal(older.CommitTime):
		// Commits made in the same second go by their place in the graph, where
		// the newer is above.
		if ix, iy := v.tab.commitRow(x.Hash), v.tab.commitRow(y.Hash); ix >= 0 && iy >= 0 && ix < iy {
			older, newer = y, x
		}
	}
	v.pair, v.swapped = [2]*git.Commit{older, newer}, false
	v.applyPair()
}

// swap compares the two commits the other way round.
func (v *commitView) swap() {
	v.swapped = !v.swapped
	v.applyPair()
}

func (v *commitView) applyPair() {
	base, head := v.pair[0], v.pair[1]
	if v.swapped {
		base, head = head, base
	}
	v.base = base
	v.open(head)
}

// open reads the changes of the commit the view is about.
func (v *commitView) open(c *git.Commit) {
	v.commit = c
	v.rv = nil
	v.filter = ""
	v.tree.reset("")
	v.load()
	if v.pane == paneTree {
		v.ensureTree()
	}
}

// clear forgets the commit.
func (v *commitView) clear() {
	v.commit, v.rv = nil, nil
	v.base, v.pair, v.swapped = nil, [2]*git.Commit{}, false
	v.tree.reset("")
}

// load reads the patch of the commit, off the main thread, and lays its files
// out.
func (v *commitView) load() {
	c := v.commit
	if c == nil {
		v.rv = nil
		return
	}
	rv := v.rv
	if rv == nil {
		rv = newReview()
		v.rv = rv
	}
	rv.gen++
	gen := rv.gen
	rv.hash, rv.loading, rv.err = c.Hash, true, ""
	rv.base = git.EmptyTree
	if len(c.Parents) > 0 {
		rv.base = c.Parents[0]
	}
	rv.cmp = v.base != nil
	if rv.cmp {
		rv.base = v.base.Hash
	}
	cmp, from := rv.cmp, rv.base
	cc := *c
	repo, o := v.tab.repo, v.tab.diffOptions()
	v.app.background(func() {
		var patch string
		var err error
		if cmp {
			patch, err = repo.RangePatch(from, cc.Hash, o)
		} else {
			patch, err = repo.CommitPatch(&cc, o)
		}
		parts := git.SplitPatchFiles(patch)
		v.app.update(func() {
			if v.rv != rv || rv.gen != gen {
				return
			}
			rv.loading = false
			if err != nil {
				rv.err = errorText(err)
				return
			}
			v.tab.layoutReview(rv, parts)
			v.syncFile()
		})
	})
}

// sideFiles are the files of the commit as the tree lists them: plain
// folders, with no kinds to group them by. The kinds are the pull request
// review's, which groups its files by them.
func (v *commitView) sideFiles() []sideFile {
	if v.rv == nil {
		return nil
	}
	out := make([]sideFile, len(v.rv.files))
	for i, f := range v.rv.files {
		out[i] = sideFile{path: f.path, status: f.status, added: f.added, deleted: f.deleted, binary: f.binary, viewed: f.viewed}
	}
	return out
}

// fileIndex is the index in the review of the file chosen, -1 for none.
func (v *commitView) fileIndex() int {
	if v.rv == nil {
		return -1
	}
	return slices.IndexFunc(v.rv.files, func(f *reviewFile) bool { return f.path == v.file })
}

// syncFile keeps the file chosen to one of the commit's: the one chosen for
// the previous commit when this one changed it too, else the first.
func (v *commitView) syncFile() {
	if v.rv == nil || v.rv.loading || v.fileIndex() >= 0 {
		return
	}
	v.file = ""
	files := v.sideFiles()
	for _, r := range sideRows(files, "", nil, false) {
		if r.kind == sideLeaf {
			v.file = files[r.file].path
			return
		}
	}
}

// choose chooses the file of index i of the review.
func (v *commitView) choose(i int) {
	if v.rv != nil && i >= 0 && i < len(v.rv.files) {
		v.file = v.rv.files[i].path
	}
}

// step chooses the file after or before the one chosen, in the order of the
// tree.
func (v *commitView) step(d int) {
	files := v.sideFiles()
	var order []int
	for _, r := range sideRows(files, v.filter, nil, false) {
		if r.kind == sideLeaf {
			order = append(order, r.file)
		}
	}
	if len(order) == 0 {
		return
	}
	at := slices.Index(order, v.fileIndex())
	v.choose(order[((at+d)%len(order)+len(order))%len(order)])
}

// openFile shows a file's changes.
func (v *commitView) openFile(p string) {
	v.file, v.pane = p, paneChanges
}

// goTo shows another commit: in the graph for the main window, in place for a
// window of its own.
func (v *commitView) goTo(hash string) {
	if !v.windowed {
		v.tab.jumpTo(hash)
		return
	}
	repo := v.tab.repo
	v.app.background(func() {
		cm, err := repo.Show(hash)
		v.app.update(func() {
			if err == nil {
				v.show(&cm)
				v.windowTitle()
			}
		})
	})
}

// setPane shows a tab, and reads what it needs.
func (v *commitView) setPane(p panelTab) {
	v.pane = p
	if p == paneTree && v.commit != nil {
		v.ensureTree()
	}
}

// ---- the view ----

// view is the three tabs, with the commit's line above or under them.
func (v *commitView) view(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Background(t.Background).Children(func() {
		if v.windowed {
			v.commitLine(c)
			v.tabsRow(c)
		} else {
			v.tabsRow(c)
			v.commitLine(c)
		}
		if v.commit == nil {
			emptyPanel(c, "No commit chosen", "Choose a commit in the graph to see it here.", nil)
			return
		}
		switch v.pane {
		case paneCommit:
			v.summary(c)
		case paneChanges:
			v.changes(c)
		default:
			v.treeView(c)
		}
	})
}

// tabsRow is the three tabs, and the button that opens the commit in a window.
func (v *commitView) tabsRow(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Height(38).Padding(0, 12).Gap(8).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
		idx := int(v.pane)
		seg := ui.SegmentedBase(c, &idx, len(paneNames))
		seg.Track.Gap(2).Label("Commit view").Shrink(0).Children(func() {
			for i, name := range paneNames {
				sg := seg.Segment(i).Height(26).Padding(0, 14).Radius(7).Center().Label(name).TextColor(t.TextMuted)
				if i == idx {
					sg.Background(hairline).TextColor(t.Text)
				} else if sg.Hovered() {
					sg.Background(hoverBg)
				}
				if sg.Clicked() {
					v.app.panelCollapsed = false
				}
				sg.Children(func() { ui.Text(c, name).FontSize(13).FontWeight(500) })
			}
		})
		if seg.Track.Changed() && panelTab(idx) != v.pane {
			v.setPane(panelTab(idx))
		}
		ui.Spacer(c)
		if !v.windowed {
			if iconButton(c, iconOpen, "Open in a new window (⇧⌘O)").Disabled(v.commit == nil).Clicked() {
				v.app.openCommitWindow(v.tab)
			}
			maxLabel := "Maximize the commit panel (⌘↩)"
			if v.app.panelMaximized && !v.app.panelCollapsed {
				maxLabel = "Restore the commit panel (⌘↩)"
			}
			mb := iconButton(c, iconMaximize, maxLabel)
			if v.app.panelMaximized && !v.app.panelCollapsed {
				mb.TextColor(c.Theme().Accent)
			}
			if mb.Clicked() || c.Shortcut(ui.Cmd, ui.KeyEnter) {
				v.app.panelMaximized = !v.app.panelMaximized || v.app.panelCollapsed
				v.app.panelCollapsed = false
			}
			label := "Collapse the commit panel (⌘J)"
			if v.app.panelCollapsed {
				label = "Expand the commit panel (⌘J)"
			}
			b := iconButton(c, iconPanelBottom, label)
			if v.app.panelCollapsed {
				b.TextColor(c.Theme().Accent)
			}
			if b.Clicked() {
				v.app.panelCollapsed = !v.app.panelCollapsed
			}
		}
	})
}

// commitLine is who made the commit, its hash, when and what it says. In a
// window of its own it is the title bar. When two commits are compared it is
// the line of each, with the button that swaps them.
func (v *commitView) commitLine(c *ui.Context) {
	if v.base != nil && v.commit != nil {
		v.compareLines(c)
		return
	}
	row := ui.Row(c).Gap(10).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(hairline)
	if v.windowed {
		bar := c.TitleBar()
		row.Height(max(bar.Height, titleBarHeight)).Padding(0, bar.Right+14, 0, bar.Left+8).DragWindow()
	} else {
		row.Height(38).Padding(0, 14, 0, 14)
	}
	row.Children(func() {
		if v.commit != nil {
			v.commitInfo(c, v.commit)
		}
	})
}

// commitInfo is the content of a commit's line.
func (v *commitView) commitInfo(c *ui.Context, cm *git.Commit) {
	t := c.Theme()
	ui.Row(c).Gap(8).Shrink(0).AlignItems(ui.Center).Children(func() {
		v.tab.commitAvatar(c, cm).FontSize(10)
		ui.Text(c, cm.Author).FontSize(13).FontWeight(600).SingleLine().Selectable()
	})
	ui.Text(c, hashAbbrev(cm.Hash)).Font(mono).FontSize(12.5).TextColor(t.TextMuted).Selectable().Shrink(0).Tooltip(cm.Hash)
	ui.Text(c, cm.AuthorTime.Format("Jan 2, 2006 at 15:04")).FontSize(12.5).TextColor(t.TextMuted).SingleLine().Selectable().Shrink(0)
	ui.Text(c, cm.Subject).FontSize(13).SingleLine().Selectable().Grow(1).MinWidth(0)
}

// compareLines are the two commits compared, the one the changes are from
// above the one they are to, with a button that swaps them.
func (v *commitView) compareLines(c *ui.Context) {
	t := c.Theme()
	if v.windowed {
		// The title bar, to drag the window by.
		bar := c.TitleBar()
		ui.Row(c).Key("compare:bar").FillWidth().Height(max(bar.Height, titleBarHeight)).Shrink(0).DragWindow().Children(func() {})
	}
	ui.Row(c).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			for i, cm := range []*git.Commit{v.base, v.commit} {
				name := [...]string{"From", "To"}[i]
				if i > 0 {
					ui.Box(c).Height(1).FillWidth().Background(hairline).Shrink(0)
				}
				ui.Row(c).Key("compare:"+name).Height(38).Padding(0, 14).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, name).FontSize(11.5).FontWeight(600).TextColor(t.TextMuted).SingleLine().Width(36).Shrink(0)
					v.commitInfo(c, cm)
				})
			}
		})
		if iconButton(c, iconSwap, "Swap the commits compared").Margin(0, 12).Clicked() {
			if v.windowed {
				v.swap()
				v.windowTitle()
			} else {
				v.tab.swapCompare()
			}
		}
	})
}

// hashAbbrev is a hash shortened as the commit line shows it.
func hashAbbrev(h string) string {
	if len(h) > 9 {
		return h[:9]
	}
	return h
}

// ---- Commit ----

// summary is the commit's message and details, with the files it changed.
func (v *commitView) summary(c *ui.Context) {
	t := c.Theme()
	cm := v.commit
	now := c.Now()
	if v.base != nil {
		// The commits are described above: what is left is what differs.
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).Padding(18, 22, 24).MaxWidth(900).Children(func() { v.changedFiles(c) })
		})
		return
	}
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Padding(18, 22, 24).Gap(16).MaxWidth(900).Children(func() {
			ui.Column(c).Gap(8).Children(func() {
				ui.Text(c, cm.Subject).FontSize(17).Bold().Selectable()
				if cm.Body != "" {
					markdown.View(c, cm.Body, &markdown.Options{OnLink: openInBrowser}).FillWidth().MaxWidth(760)
				}
				if len(v.tab.refsAt[cm.Hash]) > 0 {
					ui.Row(c).Gap(6).Wrap().Margin(4, 0, 0).Children(func() { v.tab.refChips(c, cm.Hash, false) })
				}
			})
			field := func(label string, body func()) {
				ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
					ui.Text(c, label).FontSize(12.5).TextColor(t.TextMuted).Width(78).Shrink(0)
					ui.Row(c).Gap(6).Wrap().AlignItems(ui.Center).Grow(1).MinWidth(0).Children(body)
				})
			}
			ui.Column(c).Gap(7).Children(func() {
				field("Author", func() {
					ui.Text(c, cm.Author).FontSize(13).FontWeight(600).Selectable()
					ui.Text(c, "<"+cm.AuthorEmail+">").FontSize(12.5).TextColor(t.TextMuted).Selectable()
					ui.Text(c, "· "+cm.AuthorTime.Format("Mon Jan 2, 2006 at 15:04")+" ("+relativeTime(now, cm.AuthorTime)+")").FontSize(12.5).TextColor(t.TextMuted)
				})
				if cm.Committer != "" && (cm.Committer != cm.Author || cm.CommitterEmail != cm.AuthorEmail || !cm.CommitTime.Equal(cm.AuthorTime)) {
					field("Committer", func() {
						ui.Text(c, cm.Committer).FontSize(13).FontWeight(600).Selectable()
						ui.Text(c, "<"+cm.CommitterEmail+">").FontSize(12.5).TextColor(t.TextMuted).Selectable()
						ui.Text(c, "· "+cm.CommitTime.Format("Mon Jan 2, 2006 at 15:04")).FontSize(12.5).TextColor(t.TextMuted)
					})
				}
				field("Commit", func() {
					ui.Text(c, cm.Hash).Font(mono).FontSize(12.5).Selectable()
					if iconButton(c, iconCopy, "Copy hash").Size(24, 24).Clicked() {
						v.app.copy(cm.Hash)
					}
				})
				if len(cm.Parents) > 0 {
					field("Parents", func() {
						for _, p := range cm.Parents {
							p := p
							b := ui.ButtonBase(c).Key("parent"+p).Padding(2, 7).Radius(6).Border(1, hairline).Label("Go to parent " + hashAbbrev(p)).Tooltip("Show this commit")
							if b.Hovered() {
								b.Background(hoverBg)
							}
							if b.Clicked() {
								v.goTo(p)
							}
							b.Children(func() { ui.Text(c, hashAbbrev(p)).Font(mono).FontSize(12.5).TextColor(refColor) })
						}
					})
				}
			})
			v.changedFiles(c)
		})
	})
}

// changedFiles lists the files the commit changed; choosing one shows its
// changes.
func (v *commitView) changedFiles(c *ui.Context) {
	t := c.Theme()
	rv := v.rv
	ui.Column(c).Gap(6).Children(func() {
		switch {
		case rv == nil || rv.loading && len(rv.files) == 0:
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Label("Loading")
				ui.Text(c, "Reading the changes…").FontSize(12.5).TextColor(t.TextMuted)
			})
			return
		case rv.err != "":
			ui.Text(c, rv.err).TextColor(t.Danger).Selectable()
			return
		}
		added, deleted := 0, 0
		for _, f := range rv.files {
			added += f.added
			deleted += f.deleted
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(0, 0, 4).Children(func() {
			ui.Text(c, "Changed files").FontSize(13).FontWeight(600)
			ui.Text(c, plural(len(rv.files), "file", "files")).FontSize(12.5).TextColor(t.TextMuted)
			ui.Textf(c, "+%d", added).Font(mono).FontSize(12).FontWeight(600).TextColor(t.Success)
			ui.Textf(c, "−%d", deleted).Font(mono).FontSize(12).FontWeight(600).TextColor(t.Danger)
		})
		if len(rv.files) == 0 {
			ui.Text(c, v.noChange("No file changed in this commit")).FontSize(12.5).TextColor(t.TextMuted)
		}
		files := slices.Clone(rv.files)
		slices.SortStableFunc(files, func(a, b *reviewFile) int {
			return strings.Compare(strings.ToLower(a.path), strings.ToLower(b.path))
		})
		for _, f := range files {
			f := f
			row := ui.Row(c).Key("file"+f.path).Gap(8).Padding(4, 8).Radius(6).AlignItems(ui.Center).MinWidth(0).Label(f.path)
			if row.Hovered() {
				row.Background(hoverBg)
			}
			if row.Role(ui.RoleButton).Clicked() {
				v.openFile(f.path)
			}
			row.Children(func() {
				statusBadge(c, f.status)
				ic := fileicon.For(f.path)
				ui.Icon(c, ic.SVG).FontSize(14).TextColor(ic.Color(t.Dark)).Shrink(0)
				ui.RichText(c,
					ui.Span{Text: f.dir(), Color: t.TextMuted},
					ui.Span{Text: f.name(), Color: t.Text, Weight: 600},
				).FontSize(13).SingleLine().Grow(1).MinWidth(0)
				if !f.binary {
					ui.Textf(c, "+%d", f.added).Font(mono).FontSize(11.5).FontWeight(600).TextColor(t.Success).Shrink(0)
					ui.Textf(c, "−%d", f.deleted).Font(mono).FontSize(11.5).FontWeight(600).TextColor(t.Danger).Shrink(0)
				}
			})
		}
	})
}

// ---- Changes ----

// changes is the files of the commit as a tree beside the diff of the one
// chosen.
func (v *commitView) changes(c *ui.Context) {
	rv := v.rv
	switch {
	case rv == nil || rv.loading && len(rv.files) == 0:
		ui.Column(c).Grow(1).Center().Children(func() { ui.Spinner(c).Label("Loading") })
		return
	case rv.err != "":
		emptyPanel(c, "Unable to read the changes", rv.err, nil)
		return
	case len(rv.files) == 0:
		emptyPanel(c, "No file changed", v.noChange("This commit changes no file."), nil)
		return
	}
	v.syncFile()
	// The surface shows the file chosen alone, not every file as a card.
	rv.showOnly(v.fileIndex())
	ui.Split(c, &v.app.store.data.FilesW, func() {
		ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
			v.tab.filesTree(c, &v.files, &v.filter, v.closed, v.sideFiles(), v.fileIndex(), false, v.choose)
		})
	}, func() {
		ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
			v.diffToolbar(c)
			v.tab.selectSurface(c, rv)
		})
	}).Fill().MinHeight(0)
}

// noChange is what to say of a view without changed files.
func (v *commitView) noChange(commit string) string {
	if v.base != nil {
		return "These commits have the same files."
	}
	return commit
}

// iconToggle is an icon button that stays pressed while on.
func iconToggle(c *ui.Context, svg *ui.SVG, label string, on bool) bool {
	b := iconButton(c, svg, label).Size(28, 26)
	if on {
		b.Background(hairline).TextColor(c.Theme().Text)
	}
	return b.Clicked()
}

// diffToolbar is the bar above the diff: the way from one file to the next,
// and the settings of diffs.
func (v *commitView) diffToolbar(c *ui.Context) {
	a := v.app
	s := a.store.settings()
	ui.Row(c).Height(34).Padding(0, 8).Gap(4).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
		if iconButton(c, iconChevronUp, "Previous file").Size(26, 26).Clicked() {
			v.step(-1)
		}
		if iconButton(c, iconChevronDown, "Next file").Size(26, 26).Clicked() {
			v.step(1)
		}
		ui.Spacer(c)
		if i := v.fileIndex(); i >= 0 {
			f := v.rv.files[i]
			if !f.binary {
				ui.Textf(c, "+%d", f.added).Font(mono).FontSize(11.5).FontWeight(600).TextColor(c.Theme().Success).Shrink(0)
				ui.Textf(c, "−%d", f.deleted).Font(mono).FontSize(11.5).FontWeight(600).TextColor(c.Theme().Danger).Shrink(0)
			}
		}
		if iconToggle(c, iconPilcrow, "Ignore whitespace changes", s.IgnoreWhitespace) {
			a.setSetting(func(s *Settings) { s.IgnoreWhitespace = !s.IgnoreWhitespace })
		}
		if iconToggle(c, iconWrap, "Wrap long lines", s.WrapLines) {
			a.setSetting(func(s *Settings) { s.WrapLines = !s.WrapLines })
		}
		// A window of its own has no header of the app's, so the layout of
		// diffs is chosen here.
		if v.windowed {
			v.tab.layoutControl(c)
		}
	})
}

// ---- File Tree ----

// codeTree is the files of the commit as it was, and the one chosen.
type codeTree struct {
	hash    string
	loaded  bool
	loading bool
	err     string
	gen     int
	files   []sideFile
	closed  map[string]bool
	filter  string
	list    ui.ListState
	// rows are the folders and files shown, kept until the folders or the
	// filter change.
	rows       []sideRow
	rowsOK     bool
	rowsFilter string
	// path is the file chosen, and file what is read of it.
	path string
	file treeFile
	fgen int
}

// treeFile is a file's content, or why it is not shown.
type treeFile struct {
	path    string
	loading bool
	err     string
	note    string
	size    int
	ed      *editor.State
}

// reset forgets the tree, to read that of another commit.
func (tr *codeTree) reset(hash string) {
	tr.gen++
	tr.fgen++
	tr.hash, tr.loaded, tr.loading, tr.err = hash, false, false, ""
	tr.files, tr.closed, tr.rows, tr.rowsOK = nil, nil, nil, false
	tr.file = treeFile{}
}

// visible are the rows to show.
func (tr *codeTree) visible() []sideRow {
	if !tr.rowsOK || tr.rowsFilter != tr.filter {
		closed := tr.closed
		if strings.TrimSpace(tr.filter) != "" {
			closed = nil // a search shows what it finds, in closed folders too
		}
		tr.rows, tr.rowsOK, tr.rowsFilter = sideRows(tr.files, tr.filter, closed, false), true, tr.filter
	}
	return tr.rows
}

// ensureTree reads the files of the commit shown, unless that is done. All
// folders start closed but those that hold the file chosen.
func (v *commitView) ensureTree() {
	tr := &v.tree
	if v.commit == nil || tr.hash == v.commit.Hash && (tr.loaded || tr.loading) {
		return
	}
	tr.reset(v.commit.Hash)
	tr.loading = true
	gen, hash, repo := tr.gen, tr.hash, v.tab.repo
	want := tr.path
	if want == "" {
		want = v.file
	}
	v.app.background(func() {
		paths, err := repo.Tree(hash)
		var files []sideFile
		closed := map[string]bool{}
		if err == nil {
			files = make([]sideFile, len(paths))
			for i, p := range paths {
				files[i] = sideFile{path: p}
			}
			for _, r := range sideRows(files, "", nil, false) {
				if r.kind == sideDir && !strings.HasPrefix(want, strings.TrimPrefix(r.key, "d:")+"/") {
					closed[r.key] = true
				}
			}
		}
		v.app.update(func() {
			tr := &v.tree
			if tr.gen != gen {
				return
			}
			tr.loading, tr.loaded = false, true
			if err != nil {
				tr.err = errorText(err)
				return
			}
			tr.files, tr.closed = files, closed
			if want != "" && slices.ContainsFunc(files, func(f sideFile) bool { return f.path == want }) {
				v.openTreeFile(want)
			}
		})
	})
}

// openTreeFile reads a file of the tree, off the main thread.
func (v *commitView) openTreeFile(p string) {
	tr := &v.tree
	tr.path = p
	tr.file = treeFile{path: p, loading: true}
	tr.fgen++
	gen, hash, repo := tr.fgen, tr.hash, v.tab.repo
	v.app.background(func() {
		data, err := repo.ReadFile(hash, p)
		var buf *editor.Buffer
		note := ""
		switch {
		case err != nil:
		case len(data) > maxWhole:
			note = fmt.Sprintf("This file is too large to show (%d KB).", len(data)>>10)
		case bytes.IndexByte(data, 0) >= 0:
			note = fmt.Sprintf("Binary file (%d KB).", max(len(data)>>10, 1))
		default:
			buf = editor.NewBuffer(string(data))
			buf.SetLanguage(editor.LanguageFor(path.Base(p)))
		}
		v.app.update(func() {
			if tr.fgen != gen {
				return
			}
			f := &tr.file
			f.loading, f.note, f.size = false, note, len(data)
			if err != nil {
				f.err = errorText(err)
			}
			if buf != nil {
				f.ed = &editor.State{Buffer: buf, ReadOnly: true, LineNumbers: editor.LineNumbersOn}
			}
		})
	})
}

// treeView is the files of the commit as a tree, and the content of the one
// chosen.
func (v *commitView) treeView(c *ui.Context) {
	t := c.Theme()
	v.ensureTree()
	tr := &v.tree
	switch {
	case tr.loading || !tr.loaded && tr.err == "":
		ui.Column(c).Grow(1).Center().Children(func() { ui.Spinner(c).Label("Loading") })
		return
	case tr.err != "":
		emptyPanel(c, "Unable to read the files", tr.err, nil)
		return
	}
	ui.Split(c, &v.app.store.data.FilesW, func() {
		ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
			filterField(c, &tr.filter, "Filter files")
			rows := tr.visible()
			if len(rows) == 0 {
				ui.Text(c, "No matching files").FontSize(12).TextColor(t.TextMuted).Padding(16).TextAlign(ui.Center).FillWidth()
				return
			}
			tr.list.Key = func(i int) any {
				if i < 0 || i >= len(rows) {
					return i
				}
				if r := rows[i]; r.kind == sideLeaf {
					return "f:" + tr.files[r.file].path
				} else {
					return r.key
				}
			}
			ui.List(c, &tr.list, len(rows)).Grow(1).MinHeight(0).Label("Files").Rows(func(lr ui.ListRow) {
				if lr.Index < len(rows) {
					v.treeRow(lr.Context, rows[lr.Index], lr.ListFocused())
				}
			})
		})
	}, func() { v.treeContent(c) }).Fill().MinHeight(0)
}

// treeRow is a folder or a file of the tree.
func (v *commitView) treeRow(c *ui.Context, r sideRow, focused bool) {
	t := c.Theme()
	tr := &v.tree
	row := ui.Row(c).Height(28).Padding(0, 8, 0, 8+float32(r.depth)*14).Gap(6).Radius(6).Margin(0, 6).AlignItems(ui.Center).MinWidth(0)
	tone := rowTone(c, row, r.kind == sideLeaf && tr.files[r.file].path == tr.path, focused)
	row.Role(ui.RoleButton).Focusable().FocusRing(false).Label(r.name)
	if r.kind == sideLeaf {
		p := tr.files[r.file].path
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item("Copy Path").Chosen() {
				v.app.copy(p)
			}
			if !v.windowed {
				if m.Item("File History…").Chosen() {
					v.app.fileHistoryDialog(v.tab, p)
				}
				if m.Item("Blame…").Chosen() {
					v.app.blameDialog(v.tab, p, tr.hash)
				}
			}
		})
	}
	if row.Clicked() {
		if r.kind == sideLeaf {
			v.openTreeFile(tr.files[r.file].path)
		} else {
			tr.closed[r.key] = !tr.closed[r.key]
			tr.rowsOK = false
		}
	}
	row.Children(func() {
		if r.kind != sideLeaf {
			chevron := iconChevronDown
			if tr.closed[r.key] && strings.TrimSpace(tr.filter) == "" {
				chevron = iconChevronRight
			}
			ui.Icon(c, chevron).FontSize(12).TextColor(tone.muted).Shrink(0)
			ui.Icon(c, iconFolder).FontSize(14).TextColor(tone.muted).Shrink(0)
			ui.Text(c, r.name).FontSize(13).SingleLine().Grow(1).MinWidth(0)
			return
		}
		ui.Box(c).Width(12).Shrink(0)
		ic := fileicon.For(tr.files[r.file].path)
		iconColor := ic.Color(t.Dark)
		if tone.onAccent {
			iconColor = t.AccentText
		}
		ui.Icon(c, ic.SVG).FontSize(14).TextColor(iconColor).Shrink(0)
		ui.Text(c, r.name).FontSize(13).SingleLine().Grow(1).MinWidth(0).Tooltip(tr.files[r.file].path)
	})
}

// treeContent is the file chosen, as it was at the commit.
func (v *commitView) treeContent(c *ui.Context) {
	t := c.Theme()
	f := &v.tree.file
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
		if f.path == "" {
			ui.Text(c, "Select a file to see its content at this commit").TextColor(t.TextMuted).Padding(20).TextAlign(ui.Center).FillWidth()
			return
		}
		dir, base := dirAndBase(f.path)
		ui.Row(c).Height(34).Padding(0, 12).Gap(8).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).Justify(ui.Center).Children(func() {
				ui.RichText(c,
					ui.Span{Text: dir, Color: t.TextMuted},
					ui.Span{Text: base, Color: t.Text, Weight: 600},
				).FontSize(12.5).SingleLine().MinWidth(0).Tooltip(f.path)
			})
			if iconButton(c, iconCopy, "Copy path").Size(26, 26).Clicked() {
				v.app.copy(f.path)
			}
		})
		switch {
		case f.loading:
			ui.Row(c).Padding(20).Justify(ui.Center).FillWidth().Children(func() { ui.Spinner(c).Label("Loading") })
		case f.err != "":
			ui.Text(c, f.err).TextColor(t.Danger).Padding(20).Selectable()
		case f.note != "":
			ui.Text(c, f.note).TextColor(t.TextMuted).Padding(20).TextAlign(ui.Center).FillWidth()
		case f.ed != nil:
			editor.View(c, f.ed).Grow(1).FillWidth().MinHeight(0)
		}
	})
}

// ---- the commit's own window ----

// commitWindow is a window about one commit, with the same three tabs.
type commitWindow struct {
	app *app
	win *mygo.Window
	cv  *commitView
}

// openCommitWindow shows the commit chosen in a window of its own, or brings
// forward the one that has it.
func (a *app) openCommitWindow(tab *repoTab) {
	if tab == nil || tab.onWip || tab.commit == nil {
		a.say("Choose a commit to open it in a window")
		return
	}
	for _, cw := range a.cwins {
		if cw.cv.tab == tab && cw.cv.commit.Hash == tab.commit.Hash && sameBase(cw.cv.base, tab.cv.base) {
			if cw.win != nil {
				cw.win.Restore()
				cw.win.Focus()
			}
			return
		}
	}
	cm := *tab.commit
	cw := &commitWindow{app: a, cv: newCommitView(a, tab)}
	cw.cv.windowed = true
	cw.cv.pane = tab.cv.pane
	if tab.comparing() {
		cw.cv.showCompare(tab.cv.pair[0], tab.cv.pair[1])
		cw.cv.swapped = tab.cv.swapped
		cw.cv.applyPair()
	} else {
		cw.cv.show(&cm)
	}
	a.cwins = append(a.cwins, cw)
	tab.windows = append(tab.windows, cw.cv)
	if a.win == nil {
		return
	}
	cw.win = mygo.NewWindow(mygo.WindowOptions{
		Title:                cw.title(),
		Width:                1120,
		Height:               760,
		MinWidth:             640,
		MinHeight:            420,
		Maximized:            true,
		TitleBarStyle:        mygo.TitleBarHiddenInset,
		TitleBarHeight:       titleBarHeight,
		TrafficLightPosition: trafficLights,
		Content:              ui.View(cw.view),
		Page:                 mygo.PageOptions{DevTools: devTools()},
	})
	cw.win.OnClosed(func() { a.update(func() { a.forgetCommitWindow(cw) }) })
}

// sameBase is whether two views compare with the same commit, or neither does.
func sameBase(x, y *git.Commit) bool {
	return x == nil && y == nil || x != nil && y != nil && x.Hash == y.Hash
}

func (cw *commitWindow) title() string {
	cm := cw.cv.commit
	if cm == nil {
		return "MyFork"
	}
	if b := cw.cv.base; b != nil {
		return b.Short() + ".." + cm.Short()
	}
	return cm.Short() + " " + cm.Subject
}

// windowTitle keeps the window's title to the commit it shows.
func (v *commitView) windowTitle() {
	for _, cw := range v.app.cwins {
		if cw.cv == v && cw.win != nil {
			cw.win.SetTitle(cw.title())
		}
	}
}

func (cw *commitWindow) view(c *ui.Context) {
	cw.app.applyThemeChoice(c)
	cw.cv.view(c)
}

// forgetCommitWindow drops a window that closed.
func (a *app) forgetCommitWindow(cw *commitWindow) {
	a.cwins = slices.DeleteFunc(a.cwins, func(o *commitWindow) bool { return o == cw })
	t := cw.cv.tab
	t.windows = slices.DeleteFunc(t.windows, func(o *commitView) bool { return o == cw.cv })
}

// commitWindowOf is the commit window that is win, nil for any other.
func (a *app) commitWindowOf(win *mygo.Window) *commitWindow {
	if win == nil {
		return nil
	}
	for _, cw := range a.cwins {
		if cw.win == win {
			return cw
		}
	}
	return nil
}

// closeCommitWindows closes the windows of commits, as the main window goes.
func (a *app) closeCommitWindows() {
	if a.prefsWin != nil {
		a.prefsWin.Close()
	}
	for _, cw := range slices.Clone(a.cwins) {
		if cw.win != nil {
			cw.win.Close()
		}
	}
}

// invalidateWindows draws the commit windows again.
func (a *app) invalidateWindows() {
	for _, cw := range a.cwins {
		if cw.win != nil {
			cw.win.Invalidate()
		}
	}
}
