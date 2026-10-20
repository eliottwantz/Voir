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

// pullsPage is the pull requests of the repository, on a page of their own:
// the list, and the pull request chosen beside it, or in its place when it is
// expanded.
func (a *app) pullsPage(c *ui.Context, tab *repoTab) {
	t := c.Theme()
	if !tab.prsLoaded && !tab.prsLoading {
		tab.loadPRs()
	}
	ui.Column(c).Fill().MinWidth(0).Background(t.Background).Children(func() {
		a.pullsHeader(c, tab)
		switch {
		case tab.pr != nil && a.prExpanded:
			tab.prPanel(c)
		case tab.pr != nil:
			ui.Split(c, &a.prListW, func() { tab.prListPane(c) }, func() { tab.prPanel(c) }).Grow(1).MinHeight(0)
		default:
			tab.prListPane(c)
		}
	})
}

// pullsHeader is the bar along the top of the page: the way back to the
// repository, and the repository, which opens the others.
func (a *app) pullsHeader(c *ui.Context, tab *repoTab) {
	t := c.Theme()
	bar := c.TitleBar()
	ui.Row(c).Height(max(bar.Height, titleBarHeight)).Padding(0, bar.Right+12, 0, bar.Left+8).Gap(8).Shrink(0).DragWindow().AlignItems(ui.Center).Children(func() {
		if iconButton(c, iconArrowLeft, "Back to the repository (⌘2)").Clicked() {
			a.pulls = false
		}
		ui.Text(c, "Pull Requests").FontSize(14).FontWeight(600).SingleLine().Shrink(0)
		repo := ui.ButtonBase(c).Padding(3, 8).Radius(7).Shrink(1).MinWidth(0).Label("Repository " + tab.name).Tooltip(tab.repo.Root)
		if repo.Hovered() {
			repo.Background(hoverBg)
		}
		repo.Menu(func(m *ui.Menu) { a.repoMenu(m) })
		repo.Children(func() {
			ui.Icon(c, iconFork).FontSize(13).TextColor(t.TextMuted)
			ui.Text(c, tab.name).FontSize(13).TextColor(t.TextMuted).SingleLine().MinWidth(0)
			ui.Icon(c, iconChevronDown).FontSize(12).TextColor(t.TextMuted)
		})
		ui.Spacer(c)
	})
}

// prListPane is the search, and the pull requests under it by whom they
// concern.
func (tab *repoTab) prListPane(c *ui.Context) {
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Padding(12, 16, 0).Gap(8).Children(func() {
		tab.prToolbar(c)
		tab.prRows(c)
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

func (tab *repoTab) prToolbar(c *ui.Context) {
	t := c.Theme()
	compact := tab.pr != nil
	ui.Row(c).Gap(8).Shrink(0).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Height(34).Padding(0, 6, 0, 10).Gap(8).Radius(8).Border(1, hairline).Grow(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconSearch).FontSize(15).TextColor(t.TextMuted)
			ui.TextInputBase(c, &tab.prFilter).Placeholder("Search pull requests, or label:bug").Label("Search pull requests").FontSize(13).Grow(1).MinWidth(0)
			if tab.prFilter != "" {
				if ui.ButtonBase(c).Size(20, 20).Radius(10).Center().TextColor(t.TextMuted).Label("Clear").Tooltip("Clear").Children(func() {
					ui.Icon(c, iconClose).FontSize(11)
				}).Clicked() {
					tab.prFilter = ""
				}
			}
		})
		toolButton(c, iconSort, "Sort", compact).Menu(func(m *ui.Menu) {
			for i, name := range prSorts {
				if m.Item(name).Checked(i == tab.prSort).Chosen() {
					tab.prSort = i
				}
			}
		})
		filters := toolButton(c, iconFilter, "Filters", compact)
		if tab.prFilters.active() > 0 {
			filters.Background(t.Accent.Alpha(0.12)).Border(1, t.Accent.Alpha(0.5))
		}
		if filters.Clicked() {
			tab.app.prFilterOpen, tab.app.prFilterSub = !tab.app.prFilterOpen, ""
		}
		tab.prFilterMenu(c, filters)
		if iconButton(c, iconRefresh, "Refresh the pull requests (⌘R)").Size(34, 34).Border(1, hairline).Radius(8).Clicked() {
			tab.app.refreshNow()
		}
	})
}

// prRow is a row of the list: a heading, or a pull request.
type prRow struct {
	heading string
	icon    *ui.SVG
	count   int
	pr      int // the index in the pull requests, or -1 for a heading
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

// prListRows are the pull requests that match the search and the filters, in
// the order chosen, under the headings of those the account wrote, those it
// is asked to review, and the others.
func (tab *repoTab) prListRows() (rows []prRow, shown int) {
	var idx []int
	for i, p := range tab.prs {
		var st *github.PRStatus
		if s, ok := tab.prStatus[p.Number]; ok {
			st = &s
		} else if tab.prStatus != nil {
			st = &github.PRStatus{}
		}
		if !tab.prFilters.match(p, tab.prViewer, st) {
			continue
		}
		if prMatches(p, tab.prFilter) {
			idx = append(idx, i)
		}
	}
	slices.SortStableFunc(idx, func(x, y int) int {
		a, b := tab.prs[x], tab.prs[y]
		switch tab.prSort {
		case 1:
			return b.CreatedAt.Compare(a.CreatedAt)
		case 2:
			return a.CreatedAt.Compare(b.CreatedAt)
		}
		return b.UpdatedAt.Compare(a.UpdatedAt)
	})
	viewer := tab.prViewer
	group := func(p github.PR) int {
		switch {
		case viewer == "":
			return 2
		case p.User.Login == viewer:
			return 0
		case slices.ContainsFunc(p.RequestedReviewers, func(u github.User) bool { return u.Login == viewer }):
			return 1
		}
		return 2
	}
	heads := []prRow{{heading: "Authored", icon: iconPencil}, {heading: "Review requested", icon: iconReview}, {heading: "Pull requests", icon: iconPR}}
	if viewer != "" {
		heads[2].heading = "Others"
	}
	for g, h := range heads {
		var in []int
		for _, i := range idx {
			if group(tab.prs[i]) == g {
				in = append(in, i)
			}
		}
		if len(in) == 0 {
			continue
		}
		h.count, h.pr = len(in), -1
		rows = append(rows, h)
		for _, i := range in {
			rows = append(rows, prRow{pr: i})
		}
	}
	return rows, len(idx)
}

func (tab *repoTab) prRows(c *ui.Context) {
	t := c.Theme()
	rows, shown := tab.prListRows()
	switch {
	case tab.prsLoading && len(tab.prs) == 0:
		ui.Row(c).Padding(24).Justify(ui.Center).FillWidth().Children(func() { ui.Spinner(c).Label("Loading") })
		return
	case tab.prsErr != "":
		ui.Column(c).Padding(16).Gap(8).Children(func() {
			ui.Text(c, tab.prsErr).FontSize(12).TextColor(t.Danger).Selectable()
			if ui.Button(c, "Try Again").Clicked() {
				tab.prsLoaded = false
			}
		})
		return
	case shown == 0:
		msg := "No open pull request"
		switch {
		case len(tab.prs) > 0 || tab.prFilters.active() > 0:
			msg = "No pull request matches"
		case tab.prFilters.state == "closed":
			msg = "No closed pull request"
		case tab.prFilters.state == "merged":
			msg = "No merged pull request"
		case tab.prFilters.state == "all":
			msg = "No pull request"
		}
		ui.Column(c).Grow(1).Center().Children(func() {
			ui.Text(c, msg).FontSize(13).TextColor(t.TextMuted).TextAlign(ui.Center)
		})
		return
	}
	now := c.Now()
	tab.prList.Key = func(i int) any {
		if i < 0 || i >= len(rows) {
			return -i - 1
		}
		if r := rows[i]; r.pr >= 0 {
			return tab.prs[r.pr].Number
		} else {
			return "group:" + r.heading
		}
	}
	tab.prList.Label = func(i int) string {
		if i < 0 || i >= len(rows) {
			return ""
		}
		if r := rows[i]; r.pr >= 0 {
			return fmt.Sprintf("#%d %s", tab.prs[r.pr].Number, tab.prs[r.pr].Title)
		} else {
			return r.heading
		}
	}
	ui.List(c, &tab.prList, len(rows), func(i int) {
		if i >= len(rows) {
			return
		}
		r := rows[i]
		if r.pr < 0 {
			ui.Row(c).Gap(8).Padding(14, 8, 6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, r.icon).FontSize(14).TextColor(t.TextMuted)
				ui.Text(c, r.heading).FontSize(13).TextColor(t.TextMuted).Shrink(0)
				ui.Textf(c, "%d", r.count).FontSize(12).TextColor(t.TextMuted).Shrink(0)
				ui.Box(c).Height(1).Background(hairline).Grow(1).Margin(0, 0, 0, 4)
			})
			return
		}
		tab.prRow(c, tab.prs[r.pr], now)
	}).Grow(1).MinHeight(0).Label("Pull requests")
}

// prRow is a pull request of the list: its number and title, who wrote it,
// its place in a stack, the lines it changes and when it changed.
func (tab *repoTab) prRow(c *ui.Context, p github.PR, now time.Time) {
	t := c.Theme()
	selected := tab.pr != nil && tab.pr.number == p.Number
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
		if m.Item("Check Out").Chosen() {
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
				ui.Avatar(c, p.User.Login, tab.app.avatars.image(p.User.Login, p.User.AvatarURL)).FontSize(7)
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
