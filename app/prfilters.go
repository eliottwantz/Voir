package app

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/github"
)

// prFilters narrow the list of pull requests; the zero value shows the open
// ones, all of them.
type prFilters struct {
	// repos are the repositories listed, by owner/name; none is all of them.
	repos       map[string]bool
	state       string // "" or open, closed, merged, all
	involvement string // "", reviewing, authored
	author      string
	labels      map[string]bool
	draft       string // "", only, hide
	review      string // "", approved, changes, required
	checks      string // "", passing, failing, pending
}

// fetchState is the state GitHub lists them for: merged ones are closed.
func (f prFilters) fetchState() string {
	switch f.state {
	case "closed", "merged":
		return "closed"
	case "all":
		return "all"
	}
	return "open"
}

// active counts the filters that are not at their start.
func (f prFilters) active() int {
	n := 0
	for _, s := range []string{f.involvement, f.author, f.draft, f.review, f.checks} {
		if s != "" {
			n++
		}
	}
	if f.state != "" && f.state != "open" {
		n++
	}
	if len(f.labels) > 0 {
		n++
	}
	if len(f.repos) > 0 {
		n++
	}
	return n
}

// listsRepo reports whether the repositories asked for include one.
func (f prFilters) listsRepo(name string) bool { return len(f.repos) == 0 || f.repos[name] }

// match reports whether a pull request passes the filters, for the account
// viewer, with the status read for it.
func (f prFilters) match(p github.PR, viewer string, st *github.PRStatus) bool {
	switch f.state {
	case "", "open":
		if p.State != "open" {
			return false
		}
	case "closed":
		if p.State != "closed" || p.Merged {
			return false
		}
	case "merged":
		if !p.Merged {
			return false
		}
	}
	if viewer != "" {
		switch f.involvement {
		case "authored":
			if p.User.Login != viewer {
				return false
			}
		case "reviewing":
			if !slices.ContainsFunc(p.RequestedReviewers, func(u github.User) bool { return u.Login == viewer }) {
				return false
			}
		}
	}
	if f.author != "" && p.User.Login != f.author {
		return false
	}
	for name := range f.labels {
		if !slices.ContainsFunc(p.Labels, func(l github.Label) bool { return l.Name == name }) {
			return false
		}
	}
	switch f.draft {
	case "only":
		if !p.Draft {
			return false
		}
	case "hide":
		if p.Draft {
			return false
		}
	}
	// What the status says is not known until it is read: it filters then.
	if st != nil {
		if f.review != "" && st.Review != f.review {
			return false
		}
		if f.checks != "" && st.Checks != f.checks {
			return false
		}
	}
	return true
}

// choice is an entry of a filter's menu: the value it sets, and how it shows.
type prChoice struct {
	value string
	label string
	icon  *ui.SVG
}

var (
	prStateChoices       = []prChoice{{"all", "All", iconLayers}, {"open", "Open", iconPR}, {"closed", "Closed", iconPRClosed}, {"merged", "Merged", iconMerge}}
	prInvolvementChoices = []prChoice{{"", "All", iconLayers}, {"reviewing", "Reviewing", iconEye}, {"authored", "Authored", iconPencil}}
	prDraftChoices       = []prChoice{{"", "All", iconLayers}, {"only", "Drafts only", iconDraft}, {"hide", "Hide drafts", iconEyeOff}}
	prReviewChoices      = []prChoice{{"", "All", iconLayers}, {"approved", "Approved", iconReview}, {"changes", "Changes requested", iconClose}, {"required", "Review required", iconEye}}
	prChecksChoices      = []prChoice{{"", "All", iconLayers}, {"passing", "Passing", iconCheck}, {"failing", "Failing", iconClose}, {"pending", "Pending", iconRefresh}}
)

// labelOf is what a choice is called, for the value shown in the menu.
func labelOf(choices []prChoice, value string) string {
	for _, c := range choices {
		if c.value == value {
			return c.label
		}
	}
	return choices[0].label
}

// filterMenuWidth is the width of the menu, and of the one beside it.
const filterMenuWidth = 280

// menuCard is a panel of the filters: a card that floats.
func menuCard(c *ui.Context, width float32, margin float32, build func()) {
	t := c.Theme()
	ui.Column(c).Width(width).Shrink(0).Gap(2).Padding(6).Radius(t.Radius+4).Background(t.Background).Border(1, t.Border).
		Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, 0.18)).Margin(margin, 0, 0, 0).Children(build)
}

// prFilterMenu is the menu of the Filters button: each filter with its value,
// and, beside it, the choice of that filter that was clicked.
func (a *app) prFilterMenu(c *ui.Context, anchor ui.Element) {
	f := &a.prFilters
	t := c.Theme()
	ui.PopoverBase(c, anchor, &a.prFilterOpen, func(panel ui.Element) {
		// Under the button, from its right edge: the choices of a filter open
		// to the left of the menu, which stays where it is.
		panel.AttachTo(anchor, ui.AnchorBottomRight, ui.AnchorTopRight).Margin(6, 0, 0, 0)
		ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
			// The filters, by key.
			type entry struct {
				key, name string
				icon      *ui.SVG
				value     string
			}
			labelsValue := "Any"
			switch n := len(f.labels); {
			case n == 1:
				for name := range f.labels {
					labelsValue = name
				}
			case n > 1:
				labelsValue = fmt.Sprintf("%d labels", n)
			}
			authorValue := cmp.Or(f.author, "Anyone")
			reposValue := "All"
			switch n := len(f.repos); {
			case n == 1:
				for name := range f.repos {
					reposValue = name
				}
			case n > 1:
				reposValue = fmt.Sprintf("%d repos", n)
			}
			entries := []entry{
				{"state", "State", iconPR, labelOf(prStateChoices, cmp.Or(f.state, "open"))},
				{"involvement", "Involvement", iconLayers, labelOf(prInvolvementChoices, f.involvement)},
				{"author", "Author", iconBlame, authorValue},
				{"labels", "Labels", iconTag, labelsValue},
				{"draft", "Draft", iconLayers, labelOf(prDraftChoices, f.draft)},
				{"review", "Review", iconLayers, labelOf(prReviewChoices, f.review)},
				{"checks", "Checks", iconLayers, labelOf(prChecksChoices, f.checks)},
			}
			// The repositories are chosen from the filters only in All; a
			// repository chosen in the sidebar is the only one listed.
			if a.sideRepo() == "" {
				entries = slices.Insert(entries, 0, entry{"repos", "Repos", iconFork, reposValue})
			}
			// Groups of filters are told apart by a line, which moves the choices
			// beside the filters down with it.
			groupStart := map[string]bool{"state": true, "author": true}
			offset, at := float32(0), float32(0)
			for _, e := range entries {
				if groupStart[e.key] {
					at += 13
				}
				if a.prFilterSub == e.key {
					offset = at
				}
				at += 38
			}
			if a.prFilterSub != "" {
				menuCard(c, filterMenuWidth, offset, func() { a.prFilterChoices(c, a.prFilterSub) })
			}
			menuCard(c, filterMenuWidth, 0, func() {
				for _, e := range entries {
					if groupStart[e.key] {
						ui.Box(c.Key("filters-sep-"+e.key)).Height(1).Background(hairline).Margin(4, 4).Shrink(0)
					}
					b := ui.ButtonBase(c.Key("filter-"+e.key)).Height(36).Padding(0, 10).Gap(10).Radius(8).FillWidth().AlignItems(ui.Center).Label(e.name + ": " + e.value)
					if a.prFilterSub == e.key || b.Hovered() {
						b.Background(hoverBg)
					}
					if b.Clicked() {
						if a.prFilterSub == e.key {
							a.prFilterSub = ""
						} else {
							a.prFilterSub = e.key
						}
					}
					b.Children(func() {
						ui.Icon(c, e.icon).FontSize(16).TextColor(t.TextMuted).Shrink(0)
						ui.Text(c, e.name).FontSize(14).Grow(1)
						ui.Text(c, e.value).FontSize(12.5).TextColor(t.TextMuted).SingleLine().MaxWidth(110)
						ui.Icon(c, iconChevronRight).FontSize(14).TextColor(t.TextMuted).Shrink(0)
					})
				}
				if f.active() > 0 {
					ui.Box(c.Key("filters-sep2")).Height(1).Background(hairline).Margin(4, 4).Shrink(0)
					b := ui.ButtonBase(c.Key("filters-clear")).Height(32).Padding(0, 10).Radius(8).FillWidth().Label("Clear filters")
					if b.Hovered() {
						b.Background(hoverBg)
					}
					if b.Clicked() {
						a.setPRFilters(prFilters{})
						a.prFilterOpen, a.prFilterSub = false, ""
					}
					b.Children(func() { ui.Text(c, "Clear filters").FontSize(13.5).TextColor(t.Accent) })
				}
			})
		})
	})
}

// setPRFilters changes the filters, and reads the lists again when what they
// ask GitHub for changed.
func (a *app) setPRFilters(f prFilters) {
	if f.fetchState() != a.prFilters.fetchState() {
		for _, t := range a.prRepos() {
			t.prsLoaded = false
		}
	}
	a.prFilters = f
}

// prFilterChoices is what a filter can be set to.
func (a *app) prFilterChoices(c *ui.Context, key string) {
	t := c.Theme()
	f := a.prFilters
	title := func(s string) {
		ui.Text(c, s).FontSize(13).TextColor(t.TextMuted).Padding(6, 10, 4)
	}
	// row is a choice: its icon and name, with a check where it is chosen.
	row := func(id string, icon *ui.SVG, name, note string, on bool, pick func()) {
		b := ui.ButtonBase(c.Key("choice-"+id)).Height(36).Padding(0, 10).Gap(10).Radius(8).FillWidth().AlignItems(ui.Center).Label(name)
		if on {
			b.Background(hoverBg)
		} else if b.Hovered() {
			b.Background(hoverBg)
		}
		if b.Clicked() {
			pick()
		}
		b.Children(func() {
			if icon != nil {
				ui.Icon(c, icon).FontSize(16).TextColor(t.TextMuted).Shrink(0)
			}
			ui.Text(c, name).FontSize(14).Grow(1).SingleLine().MinWidth(0)
			if note != "" {
				ui.Text(c, note).FontSize(12.5).TextColor(t.TextMuted).Shrink(0)
			}
			if on {
				ui.Icon(c, iconCheck).FontSize(15).Shrink(0)
			}
		})
	}
	done := func() { a.prFilterOpen, a.prFilterSub = false, "" }
	choices := func(name string, list []prChoice, current string, set func(*prFilters, string)) {
		title(name)
		for _, ch := range list {
			row(name+ch.value, ch.icon, ch.label, "", ch.value == current, func() {
				next := f
				set(&next, ch.value)
				a.setPRFilters(next)
				done()
			})
		}
	}
	switch key {
	case "repos":
		a.prRepoChoices(c, title, row)
	case "state":
		choices("State", prStateChoices, cmp.Or(f.state, "open"), func(f *prFilters, v string) { f.state = v })
	case "involvement":
		choices("Involvement", prInvolvementChoices, f.involvement, func(f *prFilters, v string) { f.involvement = v })
	case "draft":
		choices("Draft", prDraftChoices, f.draft, func(f *prFilters, v string) { f.draft = v })
	case "review":
		choices("Review", prReviewChoices, f.review, func(f *prFilters, v string) { f.review = v })
	case "checks":
		choices("Checks", prChecksChoices, f.checks, func(f *prFilters, v string) { f.checks = v })
	case "author":
		title("Author")
		ui.Row(c).Height(34).Padding(0, 6, 0, 10).Gap(8).Radius(8).Border(1, hairline).AlignItems(ui.Center).Margin(0, 2, 4).Children(func() {
			ui.Icon(c, iconSearch).FontSize(15).TextColor(t.TextMuted)
			ui.TextInputBase(c, &a.prAuthorQ).Placeholder("Search authors").Label("Search authors").FontSize(13.5).Grow(1).MinWidth(0).AutoFocus()
		})
		row("anyone", iconLayers, "Anyone", "", f.author == "", func() {
			next := f
			next.author = ""
			a.setPRFilters(next)
			done()
		})
		counts := map[string]int{}
		avatars := map[string]github.User{}
		for _, p := range a.listedPRs() {
			counts[p.User.Login]++
			avatars[p.User.Login] = p.User
		}
		logins := make([]string, 0, len(counts))
		q := strings.ToLower(strings.TrimSpace(a.prAuthorQ))
		for login := range counts {
			if fuzzyMatch(q, login) {
				logins = append(logins, login)
			}
		}
		slices.Sort(logins)
		if q != "" {
			slices.SortStableFunc(logins, func(x, y string) int {
				sx, _ := fuzzyScore(q, x)
				sy, _ := fuzzyScore(q, y)
				return sy - sx
			})
		}
		for _, login := range logins {
			u := avatars[login]
			b := ui.ButtonBase(c.Key("author-"+login)).Height(36).Padding(0, 10).Gap(10).Radius(8).FillWidth().AlignItems(ui.Center).Label("Author " + login)
			if f.author == login || b.Hovered() {
				b.Background(hoverBg)
			}
			if b.Clicked() {
				next := f
				next.author = login
				a.setPRFilters(next)
				done()
			}
			b.Children(func() {
				ui.Avatar(c, login, a.avatars.image(login, u.AvatarURL)).FontSize(8)
				ui.Text(c, login).FontSize(14).Grow(1).SingleLine().MinWidth(0)
				ui.Text(c, plural(counts[login], "pull request", "pull requests")).FontSize(12.5).TextColor(t.TextMuted).Shrink(0)
				if f.author == login {
					ui.Icon(c, iconCheck).FontSize(15).Shrink(0)
				}
			})
		}
	case "labels":
		title("Labels")
		row("any-label", iconLayers, "Any", "", len(f.labels) == 0, func() {
			next := f
			next.labels = nil
			a.setPRFilters(next)
		})
		counts := map[string]int{}
		colors := map[string]string{}
		for _, p := range a.listedPRs() {
			for _, l := range p.Labels {
				counts[l.Name]++
				colors[l.Name] = l.Color
			}
		}
		names := make([]string, 0, len(counts))
		for name := range counts {
			names = append(names, name)
		}
		slices.Sort(names)
		if len(names) == 0 {
			ui.Text(c, "No pull request has a label").FontSize(12.5).TextColor(t.TextMuted).Padding(6, 10)
		}
		for _, name := range names {
			b := ui.ButtonBase(c.Key("label-"+name)).Height(36).Padding(0, 10).Gap(10).Radius(8).FillWidth().AlignItems(ui.Center).Label("Label " + name)
			on := f.labels[name]
			if on || b.Hovered() {
				b.Background(hoverBg)
			}
			if b.Clicked() {
				next := f
				next.labels = map[string]bool{}
				for k := range f.labels {
					next.labels[k] = true
				}
				if on {
					delete(next.labels, name)
				} else {
					next.labels[name] = true
				}
				a.setPRFilters(next)
			}
			b.Children(func() {
				col := ui.Hex("#" + strings.TrimPrefix(cmp.Or(colors[name], "888888"), "#"))
				ui.Box(c).Size(12, 12).Radius(6).Background(col).Shrink(0)
				ui.Text(c, name).FontSize(14).Grow(1).SingleLine().MinWidth(0)
				ui.Text(c, fmt.Sprint(counts[name])).FontSize(12.5).TextColor(t.TextMuted).Shrink(0)
				if on {
					ui.Icon(c, iconCheck).FontSize(15).Shrink(0)
				}
			})
		}
	}
}

// prRepoChoices is the repositories the pull requests are of, as many as are
// asked for. The sidebar pins and removes them.
func (a *app) prRepoChoices(c *ui.Context, title func(string), row func(id string, icon *ui.SVG, name, note string, on bool, pick func())) {
	f := a.prFilters
	title("Repos")
	row("all-repos", iconLayers, "All", "", len(f.repos) == 0, func() {
		next := f
		next.repos = nil
		a.setPRFilters(next)
	})
	for _, tab := range a.prRepos() {
		name := tab.gh.String()
		row("repo-"+name, iconFolder, name, "", f.repos[name], func() { a.toggleRepoFilter(name) })
	}
}
