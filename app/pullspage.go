package app

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/github"
)

// The orders of the pull requests.
var prSorts = []string{"Recently updated", "Newest", "Oldest"}

// pullsPage is the pull requests of the repositories open and pinned, on a
// page of their own: the list, and the pull request chosen beside it, or in
// its place when it is expanded.
func (a *app) pullsPage(c *ui.Context) {
	t := c.Theme()
	repos := a.prRepos()
	for _, tab := range repos {
		if !tab.prsLoaded && !tab.prsLoading {
			tab.loadPRs()
		}
	}
	pt := a.activePR()
	if pt == nil {
		a.prHalved = false
	} else if !a.prHalved {
		a.prHalved = true
		if w, _ := c.Size(); w > 0 {
			a.prListW = float32(w) * 0.35
		}
	}
	ui.Column(c).Fill().Background(t.Background).MinWidth(0).Children(func() {
		a.pullsHeader(c)
		switch {
		case pt != nil && a.prExpanded:
			pt.prPanel(c)
		case pt != nil:
			ui.Split(c, &a.prListW, func() { a.prListPane(c, repos) }, func() { pt.prPanel(c) }).Grow(1).MinHeight(0)
		default:
			a.prListPane(c, repos)
		}
	})
}

// pullsHeader is the bar along the top of the page: the way back.
func (a *app) pullsHeader(c *ui.Context) {
	bar := c.TitleBar()
	ui.Row(c).Height(max(bar.Height, titleBarHeight)).Padding(0, bar.Right+12, 0, bar.Left+8).Gap(8).Shrink(0).DragWindow().AlignItems(ui.Center).Children(func() {
		back := "Back"
		if a.tab() != nil {
			back = "Back to the repository (esc)"
		}
		if iconButton(c, iconArrowLeft, back).Clicked() {
			a.pulls = false
		}
		ui.Text(c, "Pull Requests").FontSize(14).FontWeight(600).SingleLine().Shrink(0)
		ui.Spacer(c)
		a.prOpenField(c)
	})
}

// prOpenField is the field that names a pull request to open, of a repository
// that need not be on this machine: owner/repo #123, or an address.
func (a *app) prOpenField(c *ui.Context) {
	t := c.Theme()
	if a.prOpenErr != "" {
		ui.Text(c, a.prOpenErr).FontSize(12).TextColor(t.Danger).SingleLine().Shrink(1).MinWidth(0).Selectable()
	}
	ui.Row(c).Width(280).Height(32).Padding(0, 6, 0, 10).Gap(8).Radius(8).Border(1, hairline).Shrink(0).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconPR).FontSize(15).TextColor(t.TextMuted)
		in := ui.TextInputBase(c, &a.prOpen).Placeholder("Open owner/repo #123").Label("Open a pull request by repository and number").FontSize(13).Grow(1).MinWidth(0)
		if in.Changed() {
			a.prOpenErr = ""
		}
		if in.Submitted() && strings.TrimSpace(a.prOpen) != "" {
			a.openPRRef(a.prOpen)
		}
	})
	recent := iconButton(c, iconHistory, "Recent repositories and pull requests")
	if recent.Clicked() {
		a.prRecentMenu = !a.prRecentMenu
	}
	var rows []menuRow
	for _, ref := range a.store.recentPRs() {
		icon := iconFolder
		if ref.Number > 0 {
			icon = iconPR
		}
		rows = append(rows, menuRow{icon: icon, label: ref.String(), tooltip: ref.Repo.WebURL(), run: func() { a.openRef(ref.Repo, ref.Number) }})
	}
	if len(rows) == 0 {
		rows = []menuRow{{icon: iconHistory, label: "Nothing opened lately", disabled: true}}
	}
	popMenu(c, recent, &a.prRecentMenu, rows, []menuRow{{icon: iconClose, label: "Clear Recent", disabled: len(a.store.recentPRs()) == 0, run: a.store.clearRecentPRs}})
}

// prListPane is the search, and the pull requests under it by whom they
// concern.
func (a *app) prListPane(c *ui.Context, repos []*repoTab) {
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Padding(12, 16, 0).Gap(8).Children(func() {
		a.prToolbar(c)
		a.prRows(c, repos)
	})
}

// toolButton is a button of a toolbar, with a border: an icon and a name, or
// the icon alone when there is no room.
func toolButton(c *ui.Context, svg *ui.SVG, name string, compact bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Height(34).Padding(0, 12).Gap(6).Radius(8).Border(1, hairline).Center().Shrink(0).Label(name).Tooltip(name)
	if compact {
		b.Padding(0, 9)
	}
	if b.Hovered() {
		b.Background(hoverBg)
	}
	return b.Children(func() {
		ui.Icon(c, svg).FontSize(15).TextColor(t.TextMuted)
		if !compact {
			ui.Text(c, name).FontSize(13).FontWeight(500)
		}
	})
}

func (a *app) prToolbar(c *ui.Context) {
	t := c.Theme()
	compact := a.activePR() != nil
	ui.Row(c).Gap(8).Shrink(0).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Height(34).Padding(0, 6, 0, 10).Gap(8).Radius(8).Border(1, hairline).Grow(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconSearch).FontSize(15).TextColor(t.TextMuted)
			ui.TextInputBase(c, &a.prSearch).Placeholder("Search pull requests, or label:bug").Label("Search pull requests").FontSize(13).Grow(1).MinWidth(0)
			if a.prSearch != "" {
				if ui.ButtonBase(c).Size(20, 20).Radius(10).Center().TextColor(t.TextMuted).Label("Clear").Tooltip("Clear").Children(func() {
					ui.Icon(c, iconClose).FontSize(11)
				}).Clicked() {
					a.prSearch = ""
				}
			}
		})
		toolButton(c, iconSort, "Sort", compact).Menu(func(m *ui.Menu) {
			for i, name := range prSorts {
				if m.Item(name).Checked(i == a.prSort).Chosen() {
					a.prSort = i
				}
			}
		})
		filters := toolButton(c, iconFilter, "Filters", compact)
		if a.prFilters.active() > 0 {
			filters.Background(t.Accent.Alpha(0.12)).Border(1, t.Accent.Alpha(0.5))
		}
		if filters.Clicked() {
			a.prFilterOpen, a.prFilterSub = !a.prFilterOpen, ""
		}
		a.prFilterMenu(c, filters)
		refreshing := false
		for _, tb := range a.prRepos() {
			refreshing = refreshing || tb.prsLoading
		}
		if workingIconButton(c, iconRefresh, "Refresh the pull requests (⌘R)", refreshing).Size(34, 34).Border(1, hairline).Radius(8).Clicked() {
			a.refreshNow()
		}
	})
}

// prRow is a row of the list: a heading, or a pull request of a repository.
type prRow struct {
	heading string
	icon    *ui.SVG
	count   int
	tab     *repoTab // the repository of a pull request, nil for a heading
	pr      github.PR
}

// prMatches reports whether a pull request is what the search asks for: the
// words are in its number, title or author; label: and author: name them.
func prMatches(p github.PR, query string) bool {
	words := []string{}
	for _, tok := range strings.Fields(strings.ToLower(query)) {
		switch {
		case strings.HasPrefix(tok, "label:"):
			want := strings.Trim(strings.TrimPrefix(tok, "label:"), `"`)
			if want == "" {
				continue
			}
			if !slices.ContainsFunc(p.Labels, func(l github.Label) bool { return strings.ToLower(l.Name) == want }) {
				return false
			}
		case strings.HasPrefix(tok, "author:"):
			want := strings.TrimPrefix(tok, "author:")
			if want != "" && strings.ToLower(p.User.Login) != want {
				return false
			}
		default:
			words = append(words, tok)
		}
	}
	hay := strings.ToLower(fmt.Sprintf("#%d %s %s", p.Number, p.Title, p.User.Login))
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// listedPRs are the pull requests of the repositories the filters list,
// whatever else they ask.
func (a *app) listedPRs() []github.PR {
	var all []github.PR
	for _, tab := range a.prRepos() {
		if a.prFilters.listsRepo(tab.gh.String()) {
			all = append(all, tab.prs...)
		}
	}
	return all
}

// prListRows are the pull requests of the repositories that match the search
// and the filters, in the order chosen, under the headings of those the
// account wrote, those it is asked to review, and the others.
func (a *app) prListRows(repos []*repoTab) (rows []prRow, shown int) {
	type item struct {
		tab *repoTab
		pr  github.PR
	}
	var items []item
	for _, tab := range repos {
		if !a.prFilters.listsRepo(tab.gh.String()) {
			continue
		}
		for _, p := range tab.prs {
			var st *github.PRStatus
			if s, ok := tab.prStatus[p.Number]; ok {
				st = &s
			} else if tab.prStatus != nil {
				st = &github.PRStatus{}
			}
			if (tab.only || a.prFilters.match(p, tab.prViewer, st)) && prMatches(p, a.prSearch) {
				items = append(items, item{tab, p})
			}
		}
	}
	slices.SortStableFunc(items, func(x, y item) int {
		switch a.prSort {
		case 1:
			return y.pr.CreatedAt.Compare(x.pr.CreatedAt)
		case 2:
			return x.pr.CreatedAt.Compare(y.pr.CreatedAt)
		}
		return y.pr.UpdatedAt.Compare(x.pr.UpdatedAt)
	})
	group := func(it item) int {
		viewer := it.tab.prViewer
		switch {
		case viewer == "":
			return 2
		case it.pr.User.Login == viewer:
			return 0
		case slices.ContainsFunc(it.pr.RequestedReviewers, func(u github.User) bool { return u.Login == viewer }):
			return 1
		}
		return 2
	}
	known := slices.ContainsFunc(repos, func(t *repoTab) bool { return t.prViewer != "" })
	heads := []prRow{{heading: "Authored", icon: iconPencil}, {heading: "Review requested", icon: iconReview}, {heading: "Pull requests", icon: iconPR}}
	if known {
		heads[2].heading = "Others"
	}
	for g, h := range heads {
		var in []item
		for _, it := range items {
			if group(it) == g {
				in = append(in, it)
			}
		}
		if len(in) == 0 {
			continue
		}
		h.count = len(in)
		rows = append(rows, h)
		for _, it := range in {
			rows = append(rows, prRow{tab: it.tab, pr: it.pr})
		}
	}
	return rows, len(items)
}

// prRowKey identifies a row of the list across frames.
func prRowKey(r prRow) any {
	if r.tab == nil {
		return "group:" + r.heading
	}
	return fmt.Sprintf("%s#%d", r.tab.gh, r.pr.Number)
}

func (a *app) prRows(c *ui.Context, repos []*repoTab) {
	t := c.Theme()
	rows, shown := a.prListRows(repos)
	loading, anyPRs := false, false
	var failed []*repoTab
	for _, tab := range repos {
		loading = loading || tab.prsLoading
		anyPRs = anyPRs || len(tab.prs) > 0
		if tab.prsErr != "" {
			failed = append(failed, tab)
		}
	}
	if len(failed) > 0 {
		ui.Column(c).Padding(4, 0).Gap(8).Shrink(0).Children(func() {
			for _, tab := range failed {
				msg := tab.prsErr
				if len(repos) > 1 {
					msg = tab.gh.String() + ": " + msg
				}
				ui.Text(c, msg).FontSize(12).TextColor(t.Danger).Selectable()
			}
			if ui.Button(c, "Try Again").Clicked() {
				for _, tab := range failed {
					tab.prsLoaded = false
				}
			}
		})
	}
	switch {
	case len(repos) == 0 && len(a.store.pinned()) > 0:
		// The pinned repositories are being read for their remotes.
		ui.Row(c).Padding(24).Justify(ui.Center).FillWidth().Children(func() { spinner(c, "Loading") })
		return
	case len(repos) == 0:
		ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
			ui.Text(c, "No repository to list").FontSize(15).Bold().TextAlign(ui.Center)
			ui.Text(c, "Pin a repository on GitHub, or open one, to see its pull requests here. Or name any repository above, as owner/repo or owner/repo#123.").FontSize(13).TextColor(t.TextMuted).TextAlign(ui.Center)
			ui.Row(c).Justify(ui.Center).Children(func() {
				if ui.PrimaryButton(c, "Pin Repository…").Clicked() {
					a.browseForPin()
				}
			})
		})
		return
	case loading && !anyPRs:
		ui.Row(c).Padding(24).Justify(ui.Center).FillWidth().Children(func() { spinner(c, "Loading") })
		return
	case shown == 0 && len(failed) > 0:
		return
	case shown == 0:
		msg := "No open pull request"
		switch {
		case anyPRs || a.prFilters.active() > 0:
			msg = "No pull request matches"
		case a.prFilters.state == "closed":
			msg = "No closed pull request"
		case a.prFilters.state == "merged":
			msg = "No merged pull request"
		case a.prFilters.state == "all":
			msg = "No pull request"
		}
		ui.Column(c).Grow(1).Center().Children(func() {
			ui.Text(c, msg).FontSize(13).TextColor(t.TextMuted).TextAlign(ui.Center)
		})
		return
	}
	now := c.Now()
	a.prList.Key = func(i int) any {
		if i < 0 || i >= len(rows) {
			return -i - 1
		}
		return prRowKey(rows[i])
	}
	a.prList.Label = func(i int) string {
		if i < 0 || i >= len(rows) {
			return ""
		}
		if r := rows[i]; r.tab != nil {
			return fmt.Sprintf("#%d %s", r.pr.Number, r.pr.Title)
		} else {
			return r.heading
		}
	}
	ui.List(c, &a.prList, len(rows), func(i int) {
		if i >= len(rows) {
			return
		}
		r := rows[i]
		if r.tab == nil {
			ui.Row(c).Gap(8).Padding(14, 8, 6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, r.icon).FontSize(14).TextColor(t.TextMuted)
				ui.Text(c, r.heading).FontSize(13).TextColor(t.TextMuted).Shrink(0)
				ui.Textf(c, "%d", r.count).FontSize(12).TextColor(t.TextMuted).Shrink(0)
				ui.Box(c).Height(1).Background(hairline).Grow(1).Margin(0, 0, 0, 4)
			})
			return
		}
		a.prRow(c, r.tab, r.pr, now)
	}).Grow(1).MinHeight(0).Label("Pull requests")
}

// prRow is a pull request of the list: its number and title, who wrote it,
// its place in a stack, the lines it changes and when it changed.
func (a *app) prRow(c *ui.Context, tab *repoTab, p github.PR, now time.Time) {
	t := c.Theme()
	selected := a.prTab == tab && tab.pr != nil && tab.pr.number == p.Number
	// The list counts neither the changed lines nor the stack; the status and
	// the stacks do.
	st, place := tab.prStatus[p.Number], tab.prStacks[p.Number]
	adds, dels := cmp.Or(st.Additions, p.Additions), cmp.Or(st.Deletions, p.Deletions)
	row := ui.Row(c).Gap(10).Padding(8, 10).AlignItems(ui.Start).Radius(8)
	if selected {
		row.Background(hairline)
	} else if row.Hovered() {
		row.Background(hoverBg)
	}
	row.Role(ui.RoleButton).Label(fmt.Sprintf("#%d %s", p.Number, p.Title))
	if row.Clicked() {
		tab.openPR(p.Number)
	}
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item("Check Out").Disabled(tab.remote()).Chosen() {
			tab.checkoutPR(p.Number)
		}
		if m.Item("Open in Browser").Chosen() {
			openInBrowser(p.HTMLURL)
		}
		if m.Item("Copy Link").Chosen() {
			copyText(p.HTMLURL)
		}
	})
	row.Children(func() {
		ui.Icon(c, prStateIcon(p)).FontSize(16).TextColor(prStateColor(c, p)).Margin(2, 0, 0).Shrink(0)
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Textf(c, "#%d", p.Number).FontSize(13).TextColor(t.TextMuted).Shrink(0)
				ui.Text(c, p.Title).FontSize(13.5).SingleLine().MinWidth(0).Shrink(1)
				if p.Draft {
					ui.Text(c, "draft").FontSize(11).TextColor(t.TextMuted).Padding(0, 6).Radius(8).Border(1, hairline).Shrink(0)
				}
			})
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Avatar(c, p.User.Login, a.avatars.image(p.User.Login, p.User.AvatarURL)).FontSize(7)
				ui.Text(c, p.User.Login).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
				ui.Text(c, tab.gh.String()).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(2).MinWidth(0)
				for _, l := range p.Labels {
					col := ui.Hex("#" + strings.TrimPrefix(cmp.Or(l.Color, "888888"), "#"))
					ui.Text(c, l.Name).FontSize(10.5).FontWeight(600).Padding(0, 6).Radius(8).Background(col.Alpha(0.18)).Border(1, col.Alpha(0.5)).SingleLine().Shrink(0)
				}
			})
		})
		ui.Column(c).Gap(3).AlignItems(ui.End).Shrink(0).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if place.Size > 1 {
					ui.Row(c).Gap(4).AlignItems(ui.Center).Tooltip("Stacked pull request").Children(func() {
						ui.Icon(c, iconLayers).FontSize(13).TextColor(t.TextMuted)
						ui.Textf(c, "%d/%d", place.Position, place.Size).FontSize(12).TextColor(t.TextMuted)
					})
				}
				if adds+dels > 0 {
					ui.Row(c).Gap(6).Children(func() {
						ui.Textf(c, "+%d", adds).Font(mono).FontSize(12).TextColor(t.Success)
						ui.Textf(c, "−%d", dels).Font(mono).FontSize(12).TextColor(t.Danger)
					})
				}
			})
			ui.Text(c, shortAge(now, p.UpdatedAt)).FontSize(12).TextColor(t.TextMuted)
		})
	})
}
