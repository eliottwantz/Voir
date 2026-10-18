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
		if !tab.hasChanges() && tab.opState == "" {
			emptyPanel(c, "No local changes", "The working directory of “"+tab.name+"” is clean.", func() {
				if ui.Button(c, "Show All Commits").Clicked() {
					tab.showHistory()
				}
			})
			return
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
	t := c.Theme()
	if !tab.hasChanges() && tab.opState == "" {
		ui.Text(c, "No local changes").FontSize(12).TextColor(t.TextMuted).Padding(16).TextAlign(ui.Center).FillWidth()
		return
	}
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
				if ui.Button(c, "Stage All").Disabled(len(entries) == 0 || tab.busy != "").Clicked() {
					tab.stagePaths()
				}
			} else if ui.Button(c, "Unstage All").Disabled(len(entries) == 0 || tab.busy != "").Clicked() {
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
		// While the list has the focus, Delete unstages the files chosen,
		// or offers to discard them, Cmd+S stages or unstages them, and
		// Cmd+Enter commits; Cmd+A is the List's own.
		if l.Focused() && tab.busy == "" {
			if l.Shortcut(ui.Cmd, ui.KeyEnter) {
				tab.commitStaged()
			}
			if len(paths) == 0 {
				return
			}
			if l.Shortcut(0, ui.KeyDelete) || l.Shortcut(0, ui.KeyBackspace) {
				if staged {
					tab.unstagePaths(paths...)
				} else {
					tab.discardPaths(entries, paths)
				}
			}
			if l.Shortcut(ui.Cmd, ui.KeyS) {
				// The keyboard goes with the files to the other list, to move them back.
				tab.followFocus = true
				if staged {
					tab.unstagePaths(paths...)
				} else {
					tab.stagePaths(paths...)
				}
			}
		}
	})
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
		b := ui.ButtonBase(c).Size(20, 20).Radius(5).Center().TextColor(t.TextMuted).Label(label + " " + name).Tooltip(label).Shrink(0)
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

func (tab *repoTab) workMenu(m *ui.Menu, e git.StatusEntry, staged bool) {
	a := tab.app
	paths := []string{e.Path}
	multi, entries := &tab.uMulti, tab.unstaged
	if staged {
		multi, entries = &tab.sMulti, tab.staged
	}
	if multi.Len() > 1 && multi.Has(e.Path) {
		paths = tab.chosenPaths(entries, multi)
	}
	if staged {
		if m.Item("Unstage").Chosen() {
			tab.unstagePaths(paths...)
		}
	} else {
		if m.Item("Stage").Chosen() {
			tab.stagePaths(paths...)
		}
		if m.Item("Discard…").Chosen() {
			var es []git.StatusEntry
			for _, en := range entries {
				for _, p := range paths {
					if en.Path == p {
						es = append(es, en)
					}
				}
			}
			tab.discardEntries(es)
		}
	}
	if e.Conflict && m.Item("Resolve Conflict…").Chosen() {
		a.mergeEditor(tab, e.Path)
	}
	m.Separator()
	if m.Item("Ignore…").Disabled(staged || !e.Untracked && false).Chosen() {
		a.ignoreDialog(tab, e.Path)
	}
	if m.Item("File History…").Chosen() {
		a.fileHistoryDialog(tab, e.Path)
	}
	if m.Item("Blame…").Disabled(e.Untracked).Chosen() {
		a.blameDialog(tab, e.Path, "")
	}
	m.Separator()
	if m.Item("Show in Finder").Chosen() {
		revealInFinder(tab.repo.Root + "/" + e.Path)
	}
	if m.Item("Copy Path").Chosen() {
		copyText(e.Path)
	}
}

// commitBox is the message and the Commit button.
func (tab *repoTab) commitBox(c *ui.Context) {
	t := c.Theme()
	s := tab.app.store.settings()
	staged := len(tab.staged)
	subject := firstLine(tab.msg)
	ui.Column(c).Padding(8, 10, 10).Gap(6).Shrink(0).Children(func() {
		in := ui.TextArea(c, &tab.msg).Placeholder("Leave empty to auto-generate").Lines(3, 7).Label("Commit message")
		if in.Focused() && c.Shortcut(ui.Cmd, ui.KeyEnter) {
			tab.commitStaged()
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			on := tab.amend
			if ui.Checkbox(c, &on, "Amend").Tooltip("Replace the last commit").Changed() {
				tab.setAmend(on)
			}
			ai := ui.ButtonBase(c).Size(26, 26).Radius(6).Center().Label("Write the commit message").Disabled(staged == 0 || tab.aiWriting).TextColor(t.TextMuted)
			cfg, _ := tab.app.aiConfig()
			ai.Tooltip("Write the commit message with " + cfg.Provider.Label())
			if staged == 0 {
				ai.Tooltip("Stage files to write a message for")
			}
			if ai.Hovered() && staged > 0 {
				ai.Background(hoverBg)
			}
			if ai.Clicked() {
				tab.writeCommitMessage()
			}
			ai.Children(func() {
				if tab.aiWriting {
					ui.Spinner(c).Label("Writing")
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
				b.Tooltip("Writes the commit message, then commits")
			}
			if b.Clicked() {
				tab.commitStaged()
			}
		})
	})
}

// workDiffArea is the right side of Local Changes: the card of every file.
func (tab *repoTab) workDiffArea(c *ui.Context) {
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() { tab.workSurface(c) })
}
