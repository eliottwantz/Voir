package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

// changesView is Local Changes: the files to stage on the left, the patch of
// the one chosen on the right.
func (tab *repoTab) changesView(c *ui.Context) {
	t := c.Theme()
	if !tab.hasChanges() && tab.opState == "" {
		emptyPanel(c, "No local changes", "The working directory of “"+tab.name+"” is clean.", func() {
			if ui.Button(c, "Show All Commits").Clicked() {
				tab.side = "history"
				tab.showHistory()
			}
		})
		return
	}
	ui.Split(c, &tab.filesW, func() {
		ui.Column(c).Fill().MinHeight(0).MinWidth(0).Children(func() {
			tab.fileSection(c, "Unstaged", false)
			ui.Box(c).Height(1).Background(hairline).Shrink(0)
			tab.fileSection(c, "Staged", true)
			ui.Box(c).Height(1).Background(hairline).Shrink(0)
			tab.commitBox(c)
		})
	}, func() {
		tab.workDiffArea(c)
	}).Grow(1).MinHeight(0)
	_ = t
}

// fileSection lists the unstaged or the staged files, with the buttons that
// move them all.
func (tab *repoTab) fileSection(c *ui.Context, title string, staged bool) {
	t := c.Theme()
	entries, list, sel := tab.unstaged, &tab.uList, &tab.uSel
	multi := &tab.uMulti
	if staged {
		entries, list, sel, multi = tab.staged, &tab.sList, &tab.sSel, &tab.sMulti
	}
	ui.Column(c).Grow(1).MinHeight(0).Children(func() {
		ui.Row(c).Padding(4, 10).Gap(6).Shrink(0).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(12).FontWeight(600)
			ui.Badge(c, fmt.Sprint(len(entries)))
			ui.Spacer(c)
			if !staged {
				on := tab.showUntracked
				if ui.Checkbox(c, &on, "Untracked").Tooltip("Show untracked files").Changed() {
					tab.showUntracked = on
					tab.splitStatus()
				}
				if ui.Button(c, "Stage All").Disabled(len(entries) == 0 || tab.busy != "").Clicked() {
					tab.stagePaths()
				}
			} else if ui.Button(c, "Unstage All").Disabled(len(entries) == 0 || tab.busy != "").Clicked() {
				tab.unstagePaths()
			}
		})
		if len(entries) == 0 {
			text := "No unstaged files"
			if staged {
				text = "Nothing staged"
			}
			ui.Text(c, text).FontSize(12).TextColor(t.TextMuted).Padding(6, 14)
			ui.Spacer(c)
			return
		}
		l := ui.List(c, list, len(entries), func(i int) {
			tab.workRow(c, entries[i], staged)
		}).Grow(1).MinHeight(0).Label(title)
		if l.Changed() {
			tab.chooseWork(staged, *sel)
		}
		// Return stages and unstages the files chosen.
		if l.Submitted() {
			paths := tab.chosenPaths(entries, multi, *sel)
			if staged {
				tab.unstagePaths(paths...)
			} else {
				tab.stagePaths(paths...)
			}
		}
	})
}

// chosenPaths are the files chosen in a list, or its current row.
func (tab *repoTab) chosenPaths(entries []git.StatusEntry, multi *ui.Selection[string], row int) []string {
	var paths []string
	if multi.Len() > 1 {
		for _, e := range entries {
			if multi.Has(e.Path) {
				paths = append(paths, e.Path)
			}
		}
		return paths
	}
	if row >= 0 && row < len(entries) {
		paths = append(paths, entries[row].Path)
	}
	return paths
}

func (tab *repoTab) workRow(c *ui.Context, e git.StatusEntry, staged bool) {
	t := c.Theme()
	dir, base := dirAndBase(e.Path)
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
	row := ui.Row(c).Gap(7).Padding(3, 8).AlignItems(ui.Center).Tooltip(e.Path)
	row.ContextMenu(func(m *ui.Menu) { tab.workMenu(m, e, staged) })
	row.Children(func() {
		statusBadge(c, letter)
		ui.Text(c, base).SingleLine().FontSize(12.5).Shrink(0).MaxWidth(220)
		if e.OrigPath != "" {
			ui.Text(c, "← "+e.OrigPath).SingleLine().FontSize(11).Opacity(0.55).Shrink(1)
		}
		ui.Text(c, dir).SingleLine().FontSize(11.5).Opacity(0.6).Grow(1).MinWidth(0)
		label, svg := "Stage", iconPlus
		if staged {
			label, svg = "Unstage", iconMinus
		}
		b := ui.ButtonBase(c).Size(20, 20).Radius(5).Center().TextColor(t.TextMuted).Label(label + " " + base).Tooltip(label).Shrink(0)
		if b.Hovered() {
			b.Background(hoverBg)
		}
		if b.Disabled(tab.busy != "").Clicked() {
			if staged {
				tab.unstagePaths(e.Path)
			} else {
				tab.stagePaths(e.Path)
			}
		}
		b.Children(func() { ui.Icon(c, svg).FontSize(12) })
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
		paths = tab.chosenPaths(entries, multi, -1)
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
		in := ui.TextArea(c, &tab.msg).Placeholder("Commit message").Lines(3, 7).Label("Commit message")
		if in.Focused() && c.Shortcut(ui.Cmd, ui.KeyEnter) {
			tab.commitStaged()
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			on := tab.amend
			if ui.Checkbox(c, &on, "Amend").Tooltip("Replace the last commit").Changed() {
				tab.setAmend(on)
			}
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
			ok := (staged > 0 || tab.amend) && strings.TrimSpace(tab.msg) != "" && tab.busy == ""
			b := ui.PrimaryButton(c, label).Disabled(!ok)
			if !ok {
				switch {
				case staged == 0 && !tab.amend:
					b.Tooltip("Stage files to commit")
				case strings.TrimSpace(tab.msg) == "":
					b.Tooltip("Write a commit message")
				}
			}
			if b.Clicked() {
				tab.commitStaged()
			}
		})
	})
}

// workDiffArea is the right side of Local Changes.
func (tab *repoTab) workDiffArea(c *ui.Context) {
	t := c.Theme()
	e, staged, ok := tab.workChoice()
	if !ok {
		ui.Text(c, "Select a file to see its changes").TextColor(t.TextMuted).Padding(20).TextAlign(ui.Center).FillWidth()
		return
	}
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
		ui.Row(c).Padding(4, 10).Gap(8).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
			ui.Text(c, e.Path).FontSize(12.5).FontWeight(600).SingleLine().Grow(1).MinWidth(0)
			if !e.Conflict {
				on := tab.lines.on
				if ui.Toggle(c, &on, "Select Lines").Tooltip("Choose single lines to stage, unstage or discard").Changed() {
					tab.setLineMode(on, e)
				}
			}
			switch {
			case e.Conflict:
				if ui.PrimaryButton(c, "Resolve…").Tooltip("Merge the two sides in MyFork").Disabled(tab.busy != "").Clicked() {
					tab.app.mergeEditor(tab, e.Path)
				}
				if ui.Button(c, "Use Mine").Tooltip("Take our side of the file, and stage it").Disabled(tab.busy != "").Clicked() {
					tab.do("Resolve", func(r *git.Repo) error { return r.ResolveConflict(e.Path, "ours") })
				}
				if ui.Button(c, "Use Theirs").Tooltip("Take their side of the file, and stage it").Disabled(tab.busy != "").Clicked() {
					tab.do("Resolve", func(r *git.Repo) error { return r.ResolveConflict(e.Path, "theirs") })
				}
				if ui.Button(c, "Mark Resolved").Tooltip("Stage the file as it is").Disabled(tab.busy != "").Clicked() {
					tab.stagePaths(e.Path)
				}
			case staged:
				if ui.Button(c, "Unstage File").Disabled(tab.busy != "").Clicked() {
					tab.unstagePaths(e.Path)
				}
			default:
				if ui.Button(c, "Discard…").Disabled(tab.busy != "").Clicked() {
					tab.discardEntries([]git.StatusEntry{e})
				}
				if ui.PrimaryButton(c, "Stage File").Disabled(tab.busy != "").Clicked() {
					tab.stagePaths(e.Path)
				}
			}
		})
		var actions func(c *ui.Context, index int)
		if !e.Untracked && !e.Conflict {
			actions = func(c *ui.Context, index int) { tab.hunkButtons(c, index, staged, e.Path) }
		}
		if tab.lines.on && !e.Conflict {
			tab.lineView(c, &tab.work, staged, e)
			return
		}
		tab.diffArea(c, &tab.work, actions)
	})
}

// hunkButtons stage, unstage or discard one hunk of the file shown.
func (tab *repoTab) hunkButtons(c *ui.Context, index int, staged bool, file string) {
	patch := tab.work.patch
	idle := tab.busy == ""
	if staged {
		if ui.Button(c, "Unstage Hunk").Disabled(!idle).Clicked() {
			tab.do("Unstage hunk", func(r *git.Repo) error { return r.UnstageHunk(patch, index) })
		}
		return
	}
	if ui.Button(c, "Discard Hunk").Disabled(!idle).Clicked() {
		tab.app.confirm("Discard this hunk?", "The change in “"+file+"” will be lost.", "Discard", func() error {
			return tab.repo.DiscardHunk(patch, index)
		}, nil)
	}
	if ui.PrimaryButton(c, "Stage Hunk").Disabled(!idle).Clicked() {
		tab.do("Stage hunk", func(r *git.Repo) error { return r.StageHunk(patch, index) })
	}
}
