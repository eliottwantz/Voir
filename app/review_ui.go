package app

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/fileicon"
	"github.com/eliott/myfork/internal/git"
)

const cardRadius = 10

// headerBg is the face of file headers and cards, over the page.
func headerBg(t *ui.Theme) ui.Color { return diffPaletteFor(t).headerBg }

// reviewView is the change of the commit chosen: its message, then every
// file as a card, in one surface that scrolls.
func (tab *repoTab) reviewView(c *ui.Context) {
	rv := tab.rv
	switch {
	case tab.commit == nil:
		emptyPanel(c, "No commit chosen", "Choose a commit in History to review its changes.", nil)
		return
	case rv == nil || (rv.loading && len(rv.files) == 0):
		ui.Column(c).Grow(1).Center().Children(func() { ui.Spinner(c).Label("Loading") })
		return
	case rv.err != "":
		emptyPanel(c, "Unable to read the changes", rv.err, nil)
		return
	}
	tab.reviewSurface(c, rv)
}

// reviewSurface is the files of a change, as cards that scroll: a commit's,
// or a pull request's.
func (tab *repoTab) reviewSurface(c *ui.Context, rv *review) {
	pal := diffPaletteFor(c.Theme())
	if split := tab.app.store.settings().DiffLayout != "unified"; split != rv.split {
		rv.split, rv.dirty = split, true
	}
	if rv.dirty {
		rv.build()
	}
	rv.list.Label = func(i int) string {
		if i >= 0 && i < len(rv.rows) && rv.rows[i].kind == rowHeader {
			return rv.files[rv.rows[i].file].path
		}
		return ""
	}
	ui.List(c, &rv.list, len(rv.rows), func(i int) {
		if i >= len(rv.rows) {
			return
		}
		tab.reviewRow(c, rv, rv.rows[i], i)
	}).Grow(1).MinHeight(0).Padding(0, 16, 24).Background(pal.appBg).Label("Changes")

	// The tree of files follows what is at the top of the surface.
	first, _ := rv.list.Visible()
	for first >= 0 && first < len(rv.rows) && !rv.rows[first].kind.ofFile() {
		first++
	}
	if first >= 0 && first < len(rv.rows) && time.Since(rv.revealedAt) > 1200*time.Millisecond {
		rv.current = rv.rows[first].file
	}
}

func (tab *repoTab) reviewRow(c *ui.Context, rv *review, r reviewRow, i int) {
	switch r.kind {
	case rowCommit:
		tab.commitCard(c).Margin(12, 0, 0)
	case rowPills:
		tab.groupPills(c, rv)
	case rowGroup:
		tab.groupHeading(c, rv, r.group)
	case rowHeader:
		tab.fileHeader2(c, rv, r.file)
	case rowFileNote:
		tab.fileNote(c, rv, rv.files[r.file], r.count)
	case rowGap:
		tab.gapRow(c, rv, rv.files[r.file], r)
	case rowLine:
		tab.lineRow(c, rv, rv.files[r.file], r, i)
	case rowEnd:
		tab.cardEnd(c, rv.files[r.file])
	case rowComment:
		tab.commentRow(c, rv, r.comment)
	case rowThread:
		tab.threadRow(c, rv, r.thread)
	case rowNote:
		msg := "No file changed in this commit"
		if rv.pr || rv.work {
			msg = "No file changed"
		}
		ui.Text(c, msg).TextColor(c.Theme().TextMuted).Padding(24).TextAlign(ui.Center).FillWidth()
	}
}

// commitCard is the commit's message: who wrote it, when, and what it says.
func (tab *repoTab) commitCard(c *ui.Context) ui.Element {
	t := c.Theme()
	cm := tab.commit
	now := c.Now()
	return ui.Column(c).Padding(14, 18).Gap(6).Radius(cardRadius).Background(headerBg(t)).Border(1, hairline).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
			tab.commitAvatar(c, cm)
			ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, cm.Subject).FontSize(15).Bold().Selectable()
				ui.Textf(c, "%s committed %s", cm.Author, relativeTime(now, cm.AuthorTime)).FontSize(12).TextColor(t.TextMuted).
					Tooltip(cm.AuthorEmail + " · " + cm.AuthorTime.Format("Mon Jan 2 15:04:05 2006"))
				if cm.Body != "" {
					ui.Text(c, cm.Body).FontSize(13).Selectable().Margin(8, 0, 0).MaxWidth(760)
				}
			})
			ui.Text(c, cm.Short()).Font(mono).FontSize(12).TextColor(refColor).Selectable()
		})
		if refs := tab.refsAt[cm.Hash]; len(refs) > 0 {
			ui.Row(c).Gap(6).Wrap().Margin(0, 0, 0, 44).Children(func() { tab.refChips(c, cm.Hash) })
		}
	})
}

var refColor = ui.Hex("#e0932b")

const mono = "SF Mono, Menlo, monospace"

// groupPills are the groups as buttons that go to them, each with how many
// of its files are viewed.
func (tab *repoTab) groupPills(c *ui.Context, rv *review) {
	t := c.Theme()
	ui.Row(c).Gap(8).Wrap().Padding(12, 0, 0).Children(func() {
		for gi, g := range rv.groups {
			gi := gi
			k := kinds[g.kind]
			b := ui.ButtonBase(c).Key("pill"+k.key).Height(30).Padding(0, 12).Gap(7).Radius(15).Border(1, hairline).Label(k.label).Tooltip(k.summary)
			if b.Hovered() {
				b.Background(hoverBg)
			}
			if b.Clicked() {
				if i := rv.groupRow(gi); i >= 0 {
					rv.list.ScrollTo(i, ui.Start)
				}
			}
			b.Children(func() {
				ui.Icon(c, iconLayers).FontSize(14).TextColor(t.TextMuted)
				ui.Text(c, k.label).FontSize(13).FontWeight(600)
				ui.Textf(c, "%d/%d", rv.viewedCount(g), len(g.files)).Font(mono).FontSize(11.5).TextColor(t.TextMuted)
			})
		}
	})
}

func (tab *repoTab) groupHeading(c *ui.Context, rv *review, gi int) {
	t := c.Theme()
	g := rv.groups[gi]
	k := kinds[g.kind]
	collapsed := true
	for _, fi := range g.files {
		if !rv.files[fi].collapsed {
			collapsed = false
		}
	}
	ui.Column(c).Padding(22, 2, 4).Gap(2).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconLayers).FontSize(17).TextColor(t.TextMuted)
			ui.Text(c, k.label).FontSize(17).Bold()
			ui.Row(c).Gap(6).AlignItems(ui.Center).Grow(1).Children(func() {
				ui.Text(c, plural(len(g.files), "file", "files")).FontSize(12.5).TextColor(t.TextMuted)
				ui.Textf(c, "· +%d", g.added).FontSize(12.5).TextColor(t.Success).FontWeight(600)
				ui.Textf(c, "−%d", g.deleted).FontSize(12.5).TextColor(t.Danger).FontWeight(600)
			})
			ui.Textf(c, "%d/%d viewed", rv.viewedCount(g), len(g.files)).FontSize(12.5).TextColor(t.TextMuted)
			label := "Collapse"
			if collapsed {
				label = "Expand"
			}
			b := ui.ButtonBase(c).Key("collapse"+k.key).Padding(2, 6).Radius(5).Label(label + " " + k.label).TextColor(t.TextMuted)
			if b.Hovered() {
				b.Background(hoverBg)
			}
			if b.Clicked() {
				for _, fi := range g.files {
					rv.files[fi].collapsed = !collapsed
				}
				rv.dirty = true
			}
			b.Children(func() { ui.Text(c, label).FontSize(12.5) })
		})
		ui.Text(c, k.summary).FontSize(13).TextColor(t.TextMuted)
	})
}

// fileHeader2 is a file's header: its name, its lines changed and whether it
// is viewed. It stays at the top while the file's lines scroll under it.
func (tab *repoTab) fileHeader2(c *ui.Context, rv *review, idx int) {
	t := c.Theme()
	pal := diffPaletteFor(t)
	f := rv.files[idx]
	collapsed := f.collapsed
	if !collapsed {
		tab.loadFile(rv, f)
	}
	// The band above the header spaces the cards, and hides the lines
	// scrolling under it while it is pinned.
	band := ui.Column(c).Padding(12, 0, 0, 0).Background(pal.appBg)
	var h ui.Element
	band.Children(func() {
		h = ui.Row(c).Height(46).Padding(0, 8, 0, 6).Gap(8).Background(pal.headerBg).Border(1, pal.cardBorder)
	})
	if collapsed {
		h.Radius(cardRadius)
	} else {
		h.Radius(cardRadius, cardRadius, 0, 0)
	}
	if idx == rv.current {
		h.Border(1, t.Accent.Alpha(0.45))
	}
	h.ContextMenu(func(m *ui.Menu) {
		if m.Item("Open in Editor").Disabled(f.status == 'D' || rv.pr).Chosen() {
			go mygo.Shell.OpenPath(tab.repo.Root + "/" + f.path)
		}
		if m.Item("Copy Path").Chosen() {
			copyText(f.path)
		}
		if !rv.work && m.Item("Viewed").Checked(f.viewed).Chosen() {
			tab.setViewed(rv, f, !f.viewed)
		}
		m.Separator()
		if m.Item("File History…").Disabled(rv.pr).Chosen() {
			tab.app.fileHistoryDialog(tab, f.path)
		}
		if m.Item("Blame…").Disabled(f.status == 'D' || rv.pr || tab.commit == nil).Chosen() {
			tab.app.blameDialog(tab, f.path, tab.commit.Hash)
		}
		m.Separator()
		if m.Item("Collapse All Files").Chosen() {
			for _, g := range rv.files {
				g.collapsed = true
			}
			rv.dirty = true
		}
		if m.Item("Expand All Files").Chosen() {
			for _, g := range rv.files {
				g.collapsed = false
			}
			rv.dirty = true
		}
	})
	h.Children(func() {
		toggle := ui.ButtonBase(c).Gap(8).Grow(1).Shrink(1).MinWidth(0).Height(46).Justify(ui.Start).Label(f.path).Tooltip(f.path)
		if toggle.Clicked() {
			f.collapsed = !collapsed
			rv.dirty = true
		}
		toggle.FocusRing(false).Children(func() {
			chevron := ui.Box(c).Size(26, 26).Radius(13).Center().Shrink(0)
			if toggle.Hovered() {
				chevron.Background(hoverBg)
			}
			chevron.Children(func() {
				target := float32(0)
				if collapsed {
					target = -90
				}
				ic := ui.Icon(c, iconChevronDown).FontSize(15).TextColor(t.TextMuted)
				ic.Rotate(ic.Animate("rot", target, 160*time.Millisecond))
			})
			_, meaning := statusLetter(f.status)
			ic := fileicon.For(f.path)
			ui.Icon(c, ic.SVG).FontSize(15).TextColor(ic.Color(t.Dark)).Shrink(0).Tooltip(meaning)
			pathColor := t.Text
			if collapsed || f.viewed {
				pathColor = t.TextMuted
			}
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(1).Children(func() {
				ui.RichText(c,
					ui.Span{Text: f.dir(), Color: t.TextMuted},
					ui.Span{Text: f.name(), Color: pathColor, Weight: 600},
				).Font(mono).FontSize(13).SingleLine()
				if f.oldPath != f.path && f.status != 'A' {
					ui.Text(c, "from "+f.oldPath).Font(mono).FontSize(11).TextColor(t.TextMuted).SingleLine()
				}
			})
		})
		// The copy button shows while the pointer is over the header; it is
		// always built, so that the elements after it keep their state.
		cp := iconButton(c, iconCopy, "Copy path")
		if !h.Hovered() && !cp.FocusVisible() {
			cp.Opacity(0)
		}
		if cp.Clicked() {
			copyText(f.path)
		}
		open := iconButton(c, iconOpen, "Open file in editor").Disabled(f.status == 'D' || rv.pr)
		if open.Clicked() {
			go mygo.Shell.OpenPath(tab.repo.Root + "/" + f.path)
		}
		if rv.work {
			state, col := "Unstaged", t.TextMuted
			if f.staged {
				state, col = "Staged", t.Success
			}
			ui.Text(c, state).FontSize(11.5).FontWeight(600).TextColor(col).Shrink(0)
		}
		ui.Row(c).Gap(6).Padding(3, 10).Radius(11).Background(pal.pill).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.Textf(c, "+%d", f.added).Font(mono).FontSize(12).FontWeight(600).TextColor(t.Success)
			ui.Textf(c, "−%d", f.deleted).Font(mono).FontSize(12).FontWeight(600).TextColor(t.Danger)
		})
		// A change not committed yet is not reviewed: it is staged, or not.
		if rv.work {
			tab.workHeaderActions(c, f)
		} else {
			v := ui.ButtonBase(c).Height(30).Padding(0, 10).Gap(7).Radius(8).Border(1, hairline).Label("Viewed " + f.name()).Shrink(0)
			if v.Hovered() {
				v.Background(hoverBg)
			}
			if v.Clicked() {
				tab.setViewed(rv, f, !f.viewed)
			}
			v.Children(func() {
				box := ui.Box(c).Size(15, 15).Radius(4).Shrink(0).Border(1, hairline.Alpha(0.9))
				if f.viewed {
					box.Background(t.Accent)
				}
				box.Children(func() {
					if f.viewed {
						ui.Icon(c, iconCheck).FontSize(12).TextColor(t.AccentText).Center()
					}
				})
				ui.Text(c, "Viewed").FontSize(13).FontWeight(600)
			})
		}
	})
}

// iconButton is a button that is only an icon, as in the toolbars.
func iconButton(c *ui.Context, svg *ui.SVG, label string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Size(30, 28).Radius(7).Center().Shrink(0).TextColor(t.TextMuted).Label(label).Tooltip(label)
	if b.Hovered() {
		b.Background(hoverBg)
	}
	return b.Children(func() { ui.Icon(c, svg).FontSize(16) })
}

// loadFile starts what a file's lines wait for: its tokens, and its whole
// text, for the lines between its changes.
func (tab *repoTab) loadFile(rv *review, f *reviewFile) {
	tab.highlightHunks(rv, f)
	tab.upgrade(rv, f)
}

// workHeaderActions are the buttons of the header of a file of Local Changes:
// to stage or unstage it, and to settle a conflict.
func (tab *repoTab) workHeaderActions(c *ui.Context, f *reviewFile) {
	idle := tab.busy == ""
	small := func(label string) ui.Element {
		b := ui.ButtonBase(c).Key(label+f.path).Height(26).Padding(0, 9).Radius(7).Border(1, hairline).Label(label + " " + f.path).Shrink(0).Disabled(!idle)
		if b.Hovered() {
			b.Background(hoverBg)
		}
		return b.Children(func() { ui.Text(c, label).FontSize(12.5) })
	}
	path := f.path
	if f.entry.Conflict {
		if small("Resolve…").Clicked() {
			tab.app.mergeEditor(tab, path)
		}
		if small("Use Mine").Clicked() {
			tab.do("Resolve", func(r *git.Repo) error { return r.ResolveConflict(path, "ours") })
		}
		if small("Use Theirs").Clicked() {
			tab.do("Resolve", func(r *git.Repo) error { return r.ResolveConflict(path, "theirs") })
		}
		if small("Mark Resolved").Clicked() {
			tab.stagePaths(path)
		}
		return
	}
	if f.staged {
		if small("Unstage").Clicked() {
			tab.unstagePaths(path)
		}
	} else if small("Stage").Clicked() {
		tab.stagePaths(path)
	}
}
