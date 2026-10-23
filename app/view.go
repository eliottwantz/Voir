package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

const titleBarHeight = 52

// trafficLights places the window controls so that they are centered in the
// title bar, level with the buttons the toolbar draws there (they are 14 DIPs
// tall).
var trafficLights = &mygo.Point{X: 18, Y: (titleBarHeight - 14) / 2}

var (
	muted    = ui.RGBA(127, 127, 127, 1)
	hoverBg  = ui.RGBA(127, 127, 127, 0.12)
	subtleBg = ui.RGBA(127, 127, 127, 0.07)
	hairline = ui.RGBA(127, 127, 127, 0.22)
)

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
	if tab := a.activePR(); tab != nil && a.pulls {
		if c.Shortcut(ui.Cmd, ui.Key4) {
			tab.pr.tab = 3
		}
		if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyC) {
			tab.copyPRLink()
		}
		if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyK) {
			tab.copyPRNumber()
		}
	}
	// Escape leaves the pull requests page for the view it was opened from.
	if a.pulls && c.Shortcut(0, ui.KeyEscape) {
		a.pulls = false
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

func (tab *repoTab) hasChanges() bool { return tab.status != nil && len(tab.status.Entries) > 0 }

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
			if ui.Button(c, "Pull Requests").Clicked() {
				a.showPulls()
			}
		})
		pinned := a.store.pinned()
		ui.Column(c).Width(420).Gap(2).Margin(10, 0, 0).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(0, 0, 4).Children(func() {
				ui.Text(c, "Pinned").FontSize(12).TextColor(t.TextMuted).Grow(1)
				pin := ui.ButtonBase(c).Padding(2, 6).Gap(4).Radius(6).AlignItems(ui.Center).Label("Pin Repository").Tooltip("Pin a repository, to see its pull requests without opening it")
				if pin.Hovered() {
					pin.Background(hoverBg)
				}
				if pin.Clicked() {
					a.browseForPin()
				}
				pin.Children(func() {
					ui.Icon(c, iconPlus).FontSize(12).TextColor(t.TextMuted)
					ui.Text(c, "Pin Repository…").FontSize(12).TextColor(t.TextMuted)
				})
			})
			if len(pinned) == 0 {
				ui.Text(c, "Pinned repositories have their pull requests listed without being open.").FontSize(12).TextColor(t.TextMuted).Padding(2, 10)
			}
			for _, root := range pinned {
				a.welcomeRepo(c, "pinned-"+root, root, func() { a.openRepoReporting(root) }, func() { a.unpinRepo(root) })
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
					a.welcomeRepo(c, root, root, func() { a.openRepoReporting(root) }, nil)
				}
			})
		}
	})
}

// welcomeRepo is a repository of the welcome page: its name and where it is,
// which open it, and, when it can be, a button that unpins it.
func (a *app) welcomeRepo(c *ui.Context, key, root string, open, unpin func()) {
	t := c.Theme()
	row := ui.Row(c).Key(key).Gap(2).Radius(7).AlignItems(ui.Center)
	hovered := row.Hovered()
	if hovered {
		row.Background(hoverBg)
	}
	row.Children(func() {
		b := ui.ButtonBase(c).Key("open").Gap(10).Padding(6, 10).Radius(7).Grow(1).MinWidth(0).AlignItems(ui.Center).Label(root)
		if b.Clicked() {
			open()
		}
		b.Children(func() {
			ui.Icon(c, iconFolder).FontSize(15).TextColor(t.TextMuted)
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, filepath.Base(root)).FontSize(13).Bold().SingleLine()
				ui.Text(c, abbreviateHome(filepath.Dir(root))).FontSize(11).TextColor(t.TextMuted).SingleLine()
			})
		})
		if unpin != nil && hovered {
			if iconButton(c.Key("unpin"), iconPinOff, "Unpin "+filepath.Base(root)).Size(24, 24).Clicked() {
				unpin()
			}
		}
	})
}

// quickLaunch is the field that opens a recent repository by typing a part
// of its name.
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
