package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

const titleBarHeight = 40

var (
	muted    = ui.RGBA(127, 127, 127, 1)
	hoverBg  = ui.RGBA(127, 127, 127, 0.12)
	subtleBg = ui.RGBA(127, 127, 127, 0.07)
	hairline = ui.RGBA(127, 127, 127, 0.22)
)

// view builds the window.
func (a *app) view(c *ui.Context) {
	t := c.Theme()
	a.applyThemeChoice(c)
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	a.flushMessages(c)

	ui.Column(c).Fill().Children(func() {
		a.tabBar(c)
		tab := a.tab()
		if tab == nil {
			a.welcomeView(c)
		} else {
			tab.view(c)
		}
	})
	a.shortcuts(c)
	a.sheetView(c)
	a.dialogView(c)
	a.quickLaunchView(c)
	a.prefsView(c)
	_ = t
}

// applyThemeChoice follows the Appearance preference.
func (a *app) applyThemeChoice(c *ui.Context) {
	switch a.store.settings().Theme {
	case "light":
		if c.Theme().Dark {
			c.SetTheme(ui.LightTheme())
		}
	case "dark":
		if !c.Theme().Dark {
			c.SetTheme(ui.DarkTheme())
		}
	}
}

// shortcuts handles the keys the menus do not carry.
func (a *app) shortcuts(c *ui.Context) {
	if a.dialog != nil && a.dialog.open {
		return
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyBracketRight) || c.Shortcut(ui.Ctrl, ui.KeyTab) {
		a.nextTab(1)
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyBracketLeft) || c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyTab) {
		a.nextTab(-1)
	}
}

func (a *app) nextTab(d int) {
	if n := len(a.tabs); n > 1 {
		a.cur = (a.cur + d + n) % n
		a.saveTabs()
	}
}

// newTab shows the welcome page in place of a repository, as Fork's New Tab
// does: Quick Launch lists the repositories to open.
func (a *app) newTab() {
	a.quick.open = true
}

// tabBar is the strip of tabs in the title bar: one per repository.
func (a *app) tabBar(c *ui.Context) {
	t := c.Theme()
	bar := c.TitleBar()
	left := max(bar.Left, 12)
	ui.Row(c).Height(max(bar.Height, titleBarHeight)).Padding(0, bar.Right+10, 0, left).Gap(4).Shrink(0).DragWindow().
		BorderWidth(0, 0, 1, 0).BorderColor(hairline).AlignItems(ui.Center).Children(func() {
		closing := -1
		for i, tab := range a.tabs {
			active := i == a.cur
			chip := ui.Row(c).Key(tab.repo.Root).Height(28).Padding(0, 6, 0, 10).Gap(6).Radius(7).MinWidth(90).MaxWidth(200).Shrink(1).AlignItems(ui.Center)
			if active {
				chip.Background(t.Surface).Border(1, hairline)
			} else if chip.Hovered() {
				chip.Background(hoverBg)
			}
			chip.Role(ui.RoleButton).Label(tab.name).Tooltip(tab.repo.Root)
			if chip.Clicked() && a.cur != i {
				a.cur = i
				a.saveTabs()
			}
			chip.Children(func() {
				ui.Icon(c, iconFork).FontSize(13).TextColor(t.TextMuted)
				title := tab.name
				if tab.status != nil && len(tab.status.Entries) > 0 {
					title += " *"
				}
				lbl := ui.Text(c, title).FontSize(13).SingleLine().Grow(1).MinWidth(0)
				if !active {
					lbl.TextColor(t.TextMuted)
				}
				if ui.ButtonBase(c).Size(18, 18).Radius(5).Center().TextColor(t.TextMuted).Label("Close tab").Tooltip("Close tab").Children(func() {
					ui.Icon(c, iconClose).FontSize(11)
				}).Clicked() {
					closing = i
				}
			})
		}
		if closing >= 0 {
			a.closeTab(closing)
		}
		if ui.ButtonBase(c).Size(28, 28).Radius(7).Center().TextColor(t.TextMuted).Label("New tab").Tooltip("New tab (⌘T)").Children(func() {
			ui.Icon(c, iconPlus).FontSize(14)
		}).Clicked() {
			a.newTab()
		}
		ui.Spacer(c)
	})
}

// toolBtn is a toolbar button: an icon over a small label, as Fork's.
func toolBtn(c *ui.Context, svg *ui.SVG, label, tip string, enabled bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Padding(4, 10).Radius(7).Center().Label(label).Tooltip(tip).MinWidth(54)
	if !enabled {
		b.Disabled(true).Opacity(0.4)
	} else if b.Hovered() {
		b.Background(hoverBg)
	}
	return b.TextColor(t.Text).Children(func() {
		ui.Column(c).Gap(2).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, svg).FontSize(17)
			ui.Text(c, label).FontSize(10.5).TextColor(t.TextMuted)
		})
	})
}

// toolbar is the row of git commands above the repository.
func (tab *repoTab) toolbar(c *ui.Context) {
	t := c.Theme()
	a := tab.app
	idle := tab.busy == ""
	ui.Row(c).Padding(4, 10).Gap(2).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
		if toolBtn(c, iconFetch, "Fetch", "Fetch all remotes", idle).Clicked() {
			tab.fetch("")
		}
		pull := toolBtn(c, iconPull, "Pull", "Pull the branch from its remote", idle)
		if pull.Clicked() {
			tab.pullCurrent(false)
		}
		pull.Menu(func(m *ui.Menu) {
			if m.Item("Pull…").Chosen() {
				a.pullDialog(tab)
			}
			if m.Item("Pull with Rebase").Chosen() {
				tab.pullCurrent(true)
			}
		})
		if toolBtn(c, iconPush, "Push", "Push the branch to its remote", idle).Clicked() {
			tab.pushCurrent()
		}
		ui.Box(c).Width(1).Height(26).Background(hairline).Margin(0, 8)
		if toolBtn(c, iconBranch, "Branch", "Create a branch", true).Clicked() {
			a.newBranchDialog(tab, "")
		}
		if toolBtn(c, iconStash, "Stash", "Save the local changes to a new stash", tab.hasChanges()).Clicked() {
			a.stashDialog(tab)
		}
		if toolBtn(c, iconTag, "Tag", "Create a tag", true).Clicked() {
			a.newTagDialog(tab, "")
		}
		ui.Box(c).Width(1).Height(26).Background(hairline).Margin(0, 8)
		if toolBtn(c, iconRefresh, "Refresh", "Read the repository again (⌘R)", true).Clicked() {
			tab.reload()
		}
		if tab.busy != "" {
			ui.Row(c).Gap(8).Padding(0, 12).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Label(tab.busy)
				ui.Text(c, tab.busy+"…").FontSize(12).TextColor(t.TextMuted)
				if tab.cancel != nil {
					if ui.Button(c, "Stop").Clicked() {
						tab.stop()
					}
				}
			})
		} else if tab.loading && !tab.loaded {
			ui.Spinner(c).Label("Loading")
		}
		ui.Spacer(c)
		tab.headChip(c)
		tab.searchField(c)
	})
}

// headChip shows the branch checked out, and how far it is from its remote.
func (tab *repoTab) headChip(c *ui.Context) {
	t := c.Theme()
	if tab.status == nil {
		return
	}
	ui.Row(c).Gap(6).Padding(3, 9).Radius(11).Background(subtleBg).Border(1, hairline).Margin(0, 10, 0, 0).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconBranch).FontSize(12).TextColor(t.TextMuted)
		ui.Text(c, tab.headBranchName()).FontSize(12).FontWeight(600).SingleLine().MaxWidth(200)
		if tab.status.Ahead > 0 {
			ui.Textf(c, "↑%d", tab.status.Ahead).FontSize(11).TextColor(t.Success)
		}
		if tab.status.Behind > 0 {
			ui.Textf(c, "↓%d", tab.status.Behind).FontSize(11).TextColor(t.Warning)
		}
	})
}

func (tab *repoTab) hasChanges() bool { return tab.status != nil && len(tab.status.Entries) > 0 }

// searchField filters the commit list: "Search SHA, author or message".
func (tab *repoTab) searchField(c *ui.Context) {
	q := tab.filter
	in := ui.SearchField(c, &q).Placeholder("Search SHA, author or message").Width(230).Label("Search commits")
	if in.Changed() || q != tab.filter {
		if q != tab.filter {
			tab.search(q)
			if q != "" {
				tab.mode = modeHistory
				if tab.side == "changes" {
					tab.side = "history"
				}
			}
			tab.sel = -1
			tab.selHash = ""
		}
	}
	if tab.searchFocus {
		in.Focus()
		tab.searchFocus = false
	}
}

// view builds a repository's tab.
func (tab *repoTab) view(c *ui.Context) {
	t := c.Theme()
	a := tab.app
	tab.toolbar(c)
	tab.banner(c)
	if tab.err != nil && tab.status == nil {
		emptyPanel(c, "Unable to read the repository", errorText(tab.err), nil)
		return
	}
	sidebarW := &a.store.data.SidebarW
	body := func() {
		if tab.mode == modeChanges {
			tab.changesView(c)
		} else {
			tab.historyView(c)
		}
	}
	if !a.sidebarShown {
		ui.Column(c).Grow(1).MinWidth(0).Children(body)
		return
	}
	ui.Split(c, sidebarW, func() { tab.sidebarView(c) }, func() {
		ui.Column(c).Fill().MinWidth(0).Background(t.Background).Children(body)
	}).Grow(1).MinHeight(0)
	if tab.jump != "" {
		h := tab.jump
		tab.jump = ""
		tab.jumpTo(h)
	}
}

// banner tells a merge, rebase, cherry-pick or revert is waiting.
func (tab *repoTab) banner(c *ui.Context) {
	if tab.opState == "" {
		return
	}
	t := c.Theme()
	conflicts := 0
	if tab.status != nil {
		for _, e := range tab.status.Entries {
			if e.Conflict {
				conflicts++
			}
		}
	}
	ui.Row(c).Padding(7, 14).Gap(10).Shrink(0).AlignItems(ui.Center).Background(t.Warning.Alpha(0.16)).BorderWidth(0, 0, 1, 0).BorderColor(t.Warning.Alpha(0.5)).Children(func() {
		ui.Icon(c, iconWarning).FontSize(15).TextColor(t.Warning)
		msg := fmt.Sprintf("A %s is in progress", tab.opState)
		if conflicts > 0 {
			msg += fmt.Sprintf(" — %s to resolve", plural(conflicts, "conflict", "conflicts"))
		}
		ui.Text(c, msg).FontSize(13).Grow(1)
		if ui.Button(c, "Abort").Disabled(tab.busy != "").Clicked() {
			tab.app.confirm("Abort the "+tab.opState+"?", "The work done so far is thrown away.", "Abort", func() error { return tab.repo.Abort() }, nil)
		}
		if ui.PrimaryButton(c, "Continue").Disabled(tab.busy != "" || conflicts > 0).Tooltip("Resolve and stage the conflicts first").Clicked() {
			tab.do("Continue", func(r *git.Repo) error { return r.Continue() })
		}
	})
}

// emptyPanel says why there is nothing to show.
func emptyPanel(c *ui.Context, title, detail string, actions func()) {
	t := c.Theme()
	ui.Column(c).Grow(1).Center().Padding(24).Children(func() {
		ui.Column(c).MaxWidth(520).Padding(24).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconFork).FontSize(34).TextColor(t.TextMuted)
			ui.Text(c, title).FontSize(15).Bold().TextAlign(ui.Center)
			if detail != "" {
				ui.Text(c, detail).FontSize(13).TextColor(t.TextMuted).TextAlign(ui.Center).Selectable()
			}
			if actions != nil {
				ui.Row(c).Gap(8).Margin(6, 0, 0).Children(actions)
			}
		})
	})
}

// welcomePage is the state of the page shown without a repository.
type welcomePage struct{}

func (a *app) welcomeView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Grow(1).Center().Gap(18).Padding(24).Children(func() {
		ui.Icon(c, iconFork).FontSize(56).TextColor(t.Accent)
		ui.Text(c, "Welcome to MyFork").FontSize(24).Bold()
		ui.Text(c, "A fast and friendly git client, drawn by MyGo.").TextColor(t.TextMuted)
		ui.Row(c).Gap(8).Children(func() {
			if ui.PrimaryButton(c, "Open Repository…").Clicked() {
				a.browseForRepo()
			}
			if ui.Button(c, "Clone…").Clicked() {
				a.cloneDialog()
			}
			if ui.Button(c, "New Repository…").Clicked() {
				a.initDialog()
			}
		})
		recent := a.store.recent()
		if len(recent) > 0 {
			ui.Column(c).Width(420).Gap(2).Margin(10, 0, 0).Children(func() {
				ui.Text(c, "Recent").FontSize(12).TextColor(t.TextMuted).Padding(0, 0, 4)
				for i, root := range recent {
					if i == 8 {
						break
					}
					row := ui.Row(c).Key(root).Gap(10).Padding(6, 10).Radius(7).AlignItems(ui.Center)
					if row.Hovered() {
						row.Background(hoverBg)
					}
					if row.Role(ui.RoleButton).Label(root).Clicked() {
						a.openRepoReporting(root)
					}
					row.Children(func() {
						ui.Icon(c, iconFolder).FontSize(15).TextColor(t.TextMuted)
						ui.Column(c).Grow(1).MinWidth(0).Children(func() {
							ui.Text(c, filepath.Base(root)).FontSize(13).Bold().SingleLine()
							ui.Text(c, abbreviateHome(filepath.Dir(root))).FontSize(11).TextColor(t.TextMuted).SingleLine()
						})
					})
				}
			})
		}
	})
}

// quickLaunch is the field that opens a recent repository by typing a part
// of its name (⌘⇧O).
type quickLaunch struct {
	open  bool
	query string
	list  ui.ListState
	sel   int
}

func (a *app) quickLaunchView(c *ui.Context) {
	q := &a.quick
	if !q.open {
		return
	}
	t := c.Theme()
	var matches []string
	needle := strings.ToLower(strings.TrimSpace(q.query))
	for _, root := range a.store.recent() {
		if needle == "" || strings.Contains(strings.ToLower(root), needle) {
			matches = append(matches, root)
		}
	}
	choose := func(i int) {
		if i >= 0 && i < len(matches) {
			q.open, q.query = false, ""
			a.openRepoReporting(matches[i])
		}
	}
	ui.Modal(c, &q.open, func() {
		ui.Column(c).Width(520).Gap(8).Children(func() {
			in := ui.TextInput(c, &q.query).Placeholder("Open Recent Repository").AutoFocus().Label("Open recent repository").FontSize(15)
			if in.Changed() {
				q.sel = 0
			}
			if in.Submitted() {
				choose(max(q.sel, 0))
			}
			q.list.Selected = &q.sel
			q.list.Key = func(i int) any { return matches[i] }
			l := ui.List(c, &q.list, len(matches), func(i int) {
				ui.Row(c).Gap(10).Padding(6, 8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconFolder).FontSize(15).TextColor(t.TextMuted)
					ui.Column(c).Grow(1).MinWidth(0).Children(func() {
						ui.Text(c, filepath.Base(matches[i])).FontSize(13).Bold().SingleLine()
						ui.Text(c, abbreviateHome(matches[i])).FontSize(11).TextColor(t.TextMuted).SingleLine()
					})
				})
			}).MaxHeight(320)
			if l.Submitted() {
				choose(q.sel)
			}
			if len(matches) == 0 {
				ui.Text(c, "No recent repository matches").TextColor(t.TextMuted).Padding(8)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Open Other…").Clicked() {
					q.open = false
					a.browseForRepo()
				}
			})
		})
	})
}
