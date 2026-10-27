package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/fileicon"
	"github.com/eliott/myfork/internal/git"
)

// panel is the section under the graph: the three tabs of the commit chosen,
// or for the uncommitted changes the files to stage beside their diff.
func (tab *repoTab) panel(c *ui.Context) {
	if !tab.onWip {
		tab.cv.view(c)
		return
	}
	t := c.Theme()
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Background(t.Background).Children(func() {
		ui.Row(c).Height(38).Padding(0, 14).Gap(8).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
			ui.Text(c, "Local Changes").FontSize(13).FontWeight(600)
			if tab.status != nil {
				ui.Text(c, plural(len(tab.status.Entries), "change", "changes")).FontSize(12.5).TextColor(t.TextMuted)
			}
		})
		// Shift+Cmd+S moves every file across, to the staged list unless all
		// are staged already, wherever the focus is.
		if tab.busy == "" && tab.app.pressed(c, "stage-all-toggle") {
			tab.toggleStaged(nil, len(tab.unstaged) > 0)
		}
		ui.Split(c, &tab.app.store.data.FilesW, func() {
			ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() { tab.changesSidebar(c) })
		}, func() { tab.workDiffArea(c) }).Fill().MinHeight(0)
	})
}

// changesSidebar is the files to stage, those staged, and the message to
// commit them with. The line between the two lists moves, so that either one
// gets the room it needs.
func (tab *repoTab) changesSidebar(c *ui.Context) {
	ui.SplitVertical(c, &tab.app.store.data.StageH, func() {
		tab.fileSection(c, "Unstaged", false)
	}, func() {
		ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
			tab.fileSection(c, "Staged", true)
			ui.Box(c).Height(1).Background(hairline).Shrink(0)
			tab.commitBox(c)
		})
	}).Fill().MinHeight(0)
}

// fileSection lists the unstaged or the staged files, as a tree of folders,
// with the buttons that move them all.
func (tab *repoTab) fileSection(c *ui.Context, title string, staged bool) {
	t := c.Theme()
	entries, list, sel := tab.unstaged, &tab.uList, &tab.uSel
	multi, closed := &tab.uMulti, tab.uClosed
	if staged {
		entries, list, sel, multi, closed = tab.staged, &tab.sList, &tab.sSel, &tab.sMulti, tab.sClosed
	}
	ui.Column(c).Grow(1).MinHeight(0).Children(func() {
		ui.Row(c).Padding(4, 10).Gap(6).Shrink(0).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(12).FontWeight(600)
			ui.Badge(c, fmt.Sprint(len(entries)))
			ui.Spacer(c)
			if !staged {
				if ui.Button(c, "Stage All").Tooltip(tab.app.tipAny("Stage every file", "stage-all", "stage-all-toggle")).Disabled(len(entries) == 0 || tab.busy != "").Clicked() {
					tab.stagePaths()
				}
			} else if ui.Button(c, "Unstage All").Tooltip(tab.app.tipAny("Unstage every file", "unstage-all", "stage-all-toggle")).Disabled(len(entries) == 0 || tab.busy != "").Clicked() {
				tab.unstagePaths()
			}
		})
		if len(entries) == 0 {
			if !staged {
				tab.focusUnstaged = false
			} else {
				tab.focusStaged = false
			}
			text := "No unstaged files"
			if staged {
				text = "Nothing staged"
			}
			ui.Text(c, text).FontSize(12).TextColor(t.TextMuted).Padding(6, 14)
			ui.Spacer(c)
			return
		}
		files := make([]sideFile, len(entries))
		for i, e := range entries {
			files[i] = sideFile{path: e.Path}
		}
		rows := sideRows(files, "", closed, false)
		list.Key = func(i int) any {
			if i < 0 || i >= len(rows) {
				return "\x00" + strconv.Itoa(i)
			}
			if r := rows[i]; r.kind == sideLeaf {
				return files[r.file].path
			} else {
				return "\x00" + r.key
			}
		}
		list.Label = func(i int) string {
			if i < 0 || i >= len(rows) {
				return ""
			}
			return rows[i].name
		}
		// The row of the file whose patch is shown is the one chosen, unless a
		// folder is.
		if cur := *sel; cur < 0 || cur >= len(rows) || rows[cur].kind == sideLeaf {
			*sel = -1
			if tab.workStaged == staged {
				for i, r := range rows {
					if r.kind == sideLeaf && files[r.file].path == tab.workPath {
						*sel = i
					}
				}
				// A file chosen by the view, not by a click, is marked too.
				if *sel >= 0 && multi.Len() == 0 && tab.uMulti.Len()+tab.sMulti.Len() == 0 {
					multi.Add(tab.workPath)
				}
			}
		}
		l := ui.List(c, list, len(rows), func(i int) {
			if i >= len(rows) {
				return
			}
			tab.workRow(c, rows[i], entries, staged, closed)
		}).Grow(1).MinHeight(0).Label(title)
		if staged && tab.focusStaged || !staged && tab.focusUnstaged {
			l.Focus()
			tab.focusStaged, tab.focusUnstaged = false, false
		}
		if l.Changed() {
			// One of the two lists holds the choice: choosing a file or a
			// folder here lets the choice of the other list go.
			tab.chooseOneList(staged)
			if *sel >= 0 && *sel < len(rows) {
				if r := rows[*sel]; r.kind == sideLeaf {
					tab.chooseWork(staged, files[r.file].path)
				}
			}
		}
		// The files chosen: several at once when the multi choice has
		// them, or else those of the row the list is on.
		var paths []string
		if multi.Len() > 1 {
			paths = tab.chosenPaths(entries, multi)
		} else if *sel >= 0 && *sel < len(rows) {
			paths = rowPaths(rows[*sel], entries)
		}
		// Return stages and unstages the files chosen, or the folder.
		if l.Submitted() && len(paths) > 0 {
			if staged {
				tab.unstagePaths(paths...)
			} else {
				tab.stagePaths(paths...)
			}
		}
		// ⌘C copies the path of the file chosen, ⇧⌘C its path on disk.
		if *sel >= 0 && *sel < len(rows) && rows[*sel].kind == sideLeaf {
			tab.fileKeys(l, files[rows[*sel].file].path)
		}
		// While the list has the focus, Delete unstages the files chosen,
		// or offers to discard them, Cmd+S stages or unstages them, and
		// Cmd+Enter commits; Cmd+A is the List's own.
		if l.Focused() && tab.busy == "" {
			if tab.app.pressed(l, "commit-box") {
				tab.commitStaged()
			}
			// Cmd+S moves those chosen to the other list and stays in this
			// one, on the next file.
			if len(paths) > 0 && tab.app.pressed(l, "stage-toggle") {
				tab.moveAndStay(entries, paths, staged)
			}
			if len(paths) == 0 {
				return
			}
			if tab.app.pressedDelete(l) {
				if staged {
					tab.unstagePaths(paths...)
				} else {
					tab.discardPaths(entries, paths)
				}
			}
		}
	})
}

// toggleStaged stages the files the paths name, or all when there are none,
// or unstages them, and has them keep their choice in the list they go to.
func (tab *repoTab) toggleStaged(paths []string, stage bool) {
	src := tab.staged
	if stage {
		src = tab.unstaged
	}
	moved := paths
	if len(paths) == 0 {
		moved = nil
		for _, e := range src {
			moved = append(moved, e.Path)
		}
	}
	if len(moved) == 0 {
		return
	}
	tab.followPaths, tab.followStaged = moved, stage
	if stage {
		tab.stagePaths(paths...)
	} else {
		tab.unstagePaths(paths...)
	}
}

// moveAndStay stages (or unstages, when staged) the files the paths name, and
// has the choice and the focus stay in their list, on the file that follows
// them, or the one before when they were the last.
func (tab *repoTab) moveAndStay(entries []git.StatusEntry, paths []string, staged bool) {
	ordered := inTreeOrder(entries)
	next, prev := "", ""
	last := -1
	for i, e := range ordered {
		if slices.Contains(paths, e.Path) {
			last = i
		} else if last < 0 {
			prev = e.Path
		} else if next == "" {
			next = e.Path
		}
	}
	if next == "" {
		next = prev
	}
	tab.stayPath, tab.stayStaged = next, staged
	if staged {
		tab.unstagePaths(paths...)
	} else {
		tab.stagePaths(paths...)
	}
}

// discardPaths offers to discard the changes to the files the paths name.
func (tab *repoTab) discardPaths(entries []git.StatusEntry, paths []string) {
	var es []git.StatusEntry
	for _, e := range entries {
		if slices.Contains(paths, e.Path) {
			es = append(es, e)
		}
	}
	tab.discardEntries(es)
}

// chosenPaths are the files chosen in a list, several at once.
func (tab *repoTab) chosenPaths(entries []git.StatusEntry, multi *ui.Selection[string]) []string {
	var paths []string
	for _, e := range entries {
		if multi.Has(e.Path) {
			paths = append(paths, e.Path)
		}
	}
	return paths
}

// rowPaths are the files a row of the tree stands for: a file, or those in a
// folder.
func rowPaths(r sideRow, entries []git.StatusEntry) []string {
	if r.kind == sideLeaf {
		return []string{entries[r.file].Path}
	}
	dir := strings.TrimPrefix(r.key, "d:") + "/"
	var paths []string
	for _, e := range entries {
		if strings.HasPrefix(e.Path, dir) {
			paths = append(paths, e.Path)
		}
	}
	return paths
}

// workRow is a row of a list of Local Changes: a folder, or a file with its
// status.
func (tab *repoTab) workRow(c *ui.Context, r sideRow, entries []git.StatusEntry, staged bool, closed map[string]bool) {
	t := c.Theme()
	pad := 8 + float32(r.depth)*14
	label, svg := "Stage", iconPlus
	if staged {
		label, svg = "Unstage", iconMinus
	}
	move := func(paths []string) {
		if staged {
			tab.unstagePaths(paths...)
		} else {
			tab.stagePaths(paths...)
		}
	}
	// The button that stages or unstages what the row stands for.
	act := func(name string, paths []string) {
		b := ui.ButtonBase(c).Size(20, 20).Radius(5).Center().TextColor(t.TextMuted).Label(label + " " + name).Tooltip(tab.app.tip(label, "stage-toggle")).Shrink(0)
		if b.Hovered() {
			b.Background(hoverBg)
		}
		if b.Disabled(tab.busy != "").Clicked() {
			move(paths)
		}
		b.Children(func() { ui.Icon(c, svg).FontSize(12) })
	}

	if r.kind != sideLeaf {
		paths := rowPaths(r, entries)
		row := ui.Row(c).Gap(6).Padding(3, 8, 3, pad).AlignItems(ui.Center).MinWidth(0)
		if row.Clicked() {
			closed[r.key] = !closed[r.key]
		}
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item(label + " Folder").Chosen() {
				move(paths)
			}
			if !staged && m.Item("Discard…").Chosen() {
				var es []git.StatusEntry
				for _, e := range entries {
					if slices.Contains(paths, e.Path) {
						es = append(es, e)
					}
				}
				tab.discardEntries(es)
			}
		})
		row.Children(func() {
			chevron := iconChevronDown
			if closed[r.key] {
				chevron = iconChevronRight
			}
			ui.Icon(c, chevron).FontSize(12).TextColor(t.TextMuted).Shrink(0)
			ui.Icon(c, iconFolder).FontSize(14).TextColor(t.TextMuted).Shrink(0)
			ui.Text(c, r.name).FontSize(12.5).SingleLine().Grow(1).MinWidth(0)
			act(r.name, paths)
		})
		return
	}

	e := entries[r.file]
	letter := e.Work
	if staged {
		letter = e.Index
	}
	if e.Untracked {
		letter = '?'
	}
	if e.Conflict {
		letter = 'U'
	}
	row := ui.Row(c).Gap(7).Padding(3, 8, 3, pad).AlignItems(ui.Center).MinWidth(0).Tooltip(e.Path)
	row.ContextMenu(func(m *ui.Menu) { tab.workMenu(m, e, staged) })
	row.Children(func() {
		statusBadge(c, letter)
		ic := fileicon.For(e.Path)
		ui.Icon(c, ic.SVG).FontSize(14).TextColor(ic.Color(t.Dark)).Shrink(0)
		ui.Text(c, r.name).SingleLine().FontSize(12.5).MinWidth(0).Grow(1)
		if e.OrigPath != "" {
			ui.Text(c, "← "+e.OrigPath).SingleLine().FontSize(11).Opacity(0.55).MinWidth(0).Shrink(2)
		}
		act(r.name, []string{e.Path})
	})
}

// commitBox is the message and the Commit button.
func (tab *repoTab) commitBox(c *ui.Context) {
	t := c.Theme()
	s := tab.app.store.settings()
	staged := len(tab.staged)
	changed := tab.indexChanged()
	subject := firstLine(tab.msg)
	ui.Column(c).Padding(8, 10, 10).Gap(6).Shrink(0).Children(func() {
		in := ui.TextArea(c, &tab.msg).Placeholder("Leave empty to auto-generate").Lines(3, 7).Label("Commit message")
		if in.Focused() && tab.app.pressed(c, "commit-box") {
			tab.commitStaged()
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			on := tab.amend
			noHead := tab.status == nil || tab.status.Head == ""
			amend := ui.Checkbox(c, &on, "Amend").Tooltip(tab.app.tip("Replace the last commit", "amend")).Disabled(noHead && !tab.amend)
			if amend.Changed() {
				tab.setAmend(on)
			}
			ai := ui.ButtonBase(c).Size(26, 26).Radius(6).Center().Label("Write the commit message").Disabled(changed == 0 || tab.aiWriting).TextColor(t.TextMuted)
			cfg, _ := tab.app.aiConfig()
			ai.Tooltip(tab.app.tip("Write the commit message with "+cfg.Provider.Label(), "ai-message"))
			if changed == 0 {
				ai.Tooltip("Stage files to write a message for")
			}
			if ai.Hovered() && changed > 0 {
				ai.Background(hoverBg)
			}
			if ai.Clicked() {
				tab.writeCommitMessage()
			}
			ai.Children(func() {
				if tab.aiWriting {
					spinner(c, "Writing")
				} else {
					ui.Icon(c, iconSparkles).FontSize(15)
				}
			})
			ui.Spacer(c)
			if s.SubjectLimit > 0 {
				col := t.TextMuted
				if n := len([]rune(subject)); n > s.SubjectLimit {
					col = t.Danger
				}
				ui.Textf(c, "%d", len([]rune(subject))).FontSize(11).TextColor(col).Tooltip("Characters in the subject")
			}
			label := "Commit"
			if staged > 0 {
				label = fmt.Sprintf("Commit %s", plural(staged, "file", "files"))
			}
			if tab.amend {
				label = "Amend Commit"
			}
			empty := strings.TrimSpace(tab.msg) == ""
			ok := (staged > 0 || tab.amend && !empty) && tab.busy == "" && !tab.aiWriting
			b := ui.PrimaryButton(c, label).Disabled(!ok)
			switch {
			case staged == 0 && !tab.amend:
				b.Tooltip("Stage files to commit")
			case !ok && staged == 0:
				b.Tooltip("Write a commit message")
			case empty && ok:
				b.Tooltip(tab.app.tip("Writes the commit message, then commits", "commit-box"))
			default:
				b.Tooltip(tab.app.tip("Commit the staged changes", "commit-box"))
			}
			if b.Clicked() {
				tab.commitStaged()
			}
		})
		tab.shipButton(c, staged)
	})
}

// shipButton commits, pushes the branch and, away from the default branch,
// opens the pull request of it: all in one go.
func (tab *repoTab) shipButton(c *ui.Context, staged int) {
	branch := tab.currentBranch()
	if branch == nil || len(tab.remotes) == 0 {
		return
	}
	pr := tab.gh != nil && !tab.onDefaultBranch()
	label := "Commit & Push"
	if pr {
		label = "Commit, Push & PR"
	}
	ok := staged > 0 && !tab.amend && tab.busy == "" && !tab.aiWriting
	b := ui.Button(c.Key("ship"), label).FillWidth().Disabled(!ok)
	switch {
	case tab.amend:
		b.Tooltip("An amended commit needs a forced push: use Amend Commit, then push")
	case staged == 0:
		b.Tooltip("Stage files to commit")
	case pr:
		b.Tooltip(tab.app.tip("Commits, pushes "+branch.Name+", then opens a pull request for it", "commit-pr"))
	default:
		b.Tooltip(tab.app.tip("Commits, then pushes "+branch.Name, "commit-push"))
	}
	if b.Clicked() {
		tab.commitAndShip(true, pr)
	}
}

// workDiffArea is the right side of Local Changes: the card of every file.
func (tab *repoTab) workDiffArea(c *ui.Context) {
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() { tab.workSurface(c) })
}
