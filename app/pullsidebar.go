package app

import (
	"github.com/egoist/mygo/ui"
)

// toggleRepoFilter adds a repository to the ones the list is narrowed to by
// the filters, or takes it off.
func (a *app) toggleRepoFilter(name string) {
	next := a.prFilters
	next.repos = map[string]bool{}
	for k := range a.prFilters.repos {
		next.repos[k] = true
	}
	if a.prFilters.repos[name] {
		delete(next.repos, name)
	} else {
		next.repos[name] = true
	}
	a.setPRFilters(next)
}

// sideRepo is the repository chosen in the sidebar, "" for All or when it is
// no longer on the page.
func (a *app) sideRepo() string {
	for _, tab := range a.prRepos() {
		if tab.gh.String() == a.prSideRepo {
			return a.prSideRepo
		}
	}
	return ""
}

// listsRepo reports whether the list shows the pull requests of a repository.
func (a *app) listsRepo(name string) bool {
	if side := a.sideRepo(); side != "" {
		return name == side
	}
	return a.prFilters.listsRepo(name)
}

// prSidebar is the repositories of the pull requests page: the Local ones,
// open or pinned, and the Remote ones, open or watched. Choosing one narrows
// the list to it; the pin keeps a repository on the page.
func (a *app) prSidebar(c *ui.Context, repos []*repoTab) {
	t := c.Theme()
	var local, remote []*repoTab
	for _, tab := range repos {
		if tab.remote() {
			remote = append(remote, tab)
		} else {
			local = append(local, tab)
		}
	}
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Background(sidebarBg(c)).BorderWidth(0, 1, 0, 0).BorderColor(hairline).Children(func() {
		sidebarBar(c, func() {
			if iconButton(c, iconSidebar, "Hide sidebar (⌘B)").Clicked() {
				a.prSideHidden = true
			}
			a.backButton(c)
		})
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(12, 8, 12).Gap(2).Children(func() {
				all := a.sideRepo() == ""
				allRow := ui.ButtonBase(c.Key("side-all")).Gap(8).Padding(6, 10).Radius(7).FillWidth().AlignItems(ui.Center).Label("All pull requests").Tooltip("Pull requests of every repository")
				if all {
					allRow.Background(subtleBg)
				} else if allRow.Hovered() {
					allRow.Background(hoverBg)
				}
				if allRow.Clicked() {
					a.prSideRepo = ""
				}
				allRow.Children(func() {
					ui.Icon(c, iconLayers).FontSize(14).TextColor(t.TextMuted).Shrink(0)
					ui.Text(c, "All").FontSize(13).FontWeight(cmpWeight(all)).Grow(1)
				})
				ui.Box(c.Key("side-all-gap")).Height(8).Shrink(0)
				a.prSideHeader(c, "local", "Local", iconFolder, func() {
					if iconButton(c, iconPlus, "Pin a local repository").Size(24, 24).Clicked() {
						a.browseForPin()
					}
				})
				if len(local) == 0 {
					ui.Text(c, "No local repository on GitHub is open or pinned.").FontSize(12).TextColor(t.TextMuted).Padding(2, 10, 6)
				}
				for _, tab := range local {
					pinned := a.store.isPinned(tab.repo.Root)
					a.prSideRow(c, tab, abbreviateHome(tab.repo.Root), pinned, func() { a.togglePin(tab) }, nil)
				}
				ui.Box(c.Key("side-gap")).Height(10).Shrink(0)
				a.prSideHeader(c, "remote", "Remote", iconCloud, nil)
				if len(remote) == 0 {
					ui.Text(c, "Name a repository above to list its pull requests here.").FontSize(12).TextColor(t.TextMuted).Padding(2, 10, 6)
				}
				for _, tab := range remote {
					watched := a.store.isWatched(*tab.gh)
					var close func()
					if !watched {
						close = func() { a.removeRemote(tab) }
					}
					a.prSideRow(c, tab, tab.gh.WebURL(), watched, func() {
						if watched {
							a.unwatchRepo(tab)
						} else {
							a.watchRepo(tab)
						}
					}, close)
				}
			})
		})
	})
}

// prSideHeader is the title of a section of the sidebar, with the control
// that adds to it, if any.
func (a *app) prSideHeader(c *ui.Context, key, title string, icon *ui.SVG, add func()) {
	t := c.Theme()
	ui.Row(c.Key("side-"+key)).Height(28).Padding(0, 0, 0, 10).Gap(6).Shrink(0).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon).FontSize(13).TextColor(t.TextMuted).Shrink(0)
		ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(t.TextMuted).Grow(1)
		if add != nil {
			add()
		}
	})
}

// prSideRow is a repository of the sidebar: its name, which narrows the list
// to it, the pin that keeps it on the page, and the way to take it off when
// nothing else keeps it.
func (a *app) prSideRow(c *ui.Context, tab *repoTab, where string, pinned bool, togglePin func(), close func()) {
	t := c.Theme()
	name := tab.gh.String()
	on := a.sideRepo() == name
	row := ui.Row(c.Key("side-repo-" + name)).Gap(2).Radius(7).AlignItems(ui.Center).FillWidth()
	if on {
		row.Background(subtleBg)
	} else if row.Hovered() {
		row.Background(hoverBg)
	}
	row.Children(func() {
		b := ui.ButtonBase(c.Key("choose")).Gap(8).Padding(6, 10).Radius(7).Grow(1).MinWidth(0).AlignItems(ui.Center).Label("Pull requests of " + name).Tooltip(where)
		if b.Clicked() {
			a.prSideRepo = name
		}
		b.Children(func() {
			ui.Text(c, name).FontSize(13).FontWeight(cmpWeight(on)).SingleLine().MinWidth(0).Grow(1)
			if tab.prsLoading {
				spinner(c, "Loading "+name)
			} else if n := len(tab.prs); n > 0 && !tab.only {
				ui.Textf(c, "%d", n).FontSize(11.5).TextColor(t.TextMuted).Shrink(0)
			}
		})
		if close != nil {
			if iconButton(c.Key("remove"), iconClose, "Remove "+name).Size(24, 24).Clicked() {
				close()
			}
		}
		if pinned {
			if iconButton(c.Key("unpin"), iconPinFilled, "Unpin "+name).Size(24, 24).Clicked() {
				togglePin()
			}
		} else if iconButton(c.Key("pin"), iconPin, "Pin "+name).Size(24, 24).Clicked() {
			togglePin()
		}
	})
}

// cmpWeight is the weight of the name of a repository: heavier when chosen.
func cmpWeight(on bool) int {
	if on {
		return 600
	}
	return 400
}
