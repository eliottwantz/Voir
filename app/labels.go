package app

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/eliott/myfork/internal/github"
)

func labelColor(l github.Label) ui.Color {
	return ui.Hex("#" + strings.TrimPrefix(cmp.Or(l.Color, "888888"), "#"))
}

// labelPicker is the button that opens the labels of the repository, to add
// to the pull request or take off it.
func (tab *repoTab) labelPicker(c *ui.Context, pv *prView) {
	t := c.Theme()
	b := ui.ButtonBase(c).Key("labels-add").Size(28, 24).Radius(6).Center().Shrink(0).Label("Edit labels")
	if b.Hovered() || pv.labelMenu {
		b.Background(hoverBg)
	}
	b.Children(func() { ui.Icon(c, iconTag).FontSize(14).TextColor(t.TextMuted) })
	if b.Clicked() {
		pv.labelMenu = !pv.labelMenu
		pv.labelQuery = ""
		if pv.labelMenu {
			tab.loadRepoLabels(pv)
		}
	}
	ui.Popover(c, b, &pv.labelMenu, func() {
		ui.Column(c).Width(360).Gap(4).Children(func() {
			ui.Row(c).Gap(8).Padding(6, 10).AlignItems(ui.Center).BorderWidth(0, 0, 2, 0).BorderColor(t.Accent).Children(func() {
				ui.Icon(c, iconSearch).FontSize(15).TextColor(t.TextMuted)
				ui.TextInputBase(c, &pv.labelQuery).Placeholder("Search labels").Label("Search labels").FontSize(14).Grow(1).MinWidth(0).AutoFocus()
			})
			if !pv.labelsLoaded {
				ui.Text(c, "Loading labels…").FontSize(12.5).TextColor(t.TextMuted).Padding(8, 10)
				return
			}
			q := strings.ToLower(strings.TrimSpace(pv.labelQuery))
			shown := 0
			ui.Scroll(c).MaxHeight(320).Children(func() {
				ui.Column(c).FillWidth().Children(func() {
					for _, l := range pv.repoLabels {
						if q != "" && !strings.Contains(strings.ToLower(l.Name+" "+l.Description), q) {
							continue
						}
						shown++
						on := slices.ContainsFunc(pv.pr.Labels, func(o github.Label) bool { return o.Name == l.Name })
						row := ui.ButtonBase(c).Key("label-opt-"+l.Name).FillWidth().Height(32).Padding(0, 10).Gap(10).Radius(7).AlignItems(ui.Center).Justify(ui.Start).Label(l.Name)
						if row.Hovered() {
							row.Background(hoverBg)
						}
						if row.Clicked() {
							tab.toggleLabel(pv, l)
						}
						row.Children(func() {
							ui.Box(c).Size(10, 10).Radius(5).Background(labelColor(l)).Shrink(0)
							ui.RichText(c,
								ui.Span{Text: l.Name, Weight: 500},
								ui.Span{Text: cmpPrefix(l.Description, " · "), Color: t.TextMuted},
							).FontSize(14).SingleLine().Grow(1).MinWidth(0)
							if on {
								ui.Icon(c, iconCheck).FontSize(14).Shrink(0)
							}
						})
					}
				})
			})
			if shown == 0 {
				ui.Text(c, "No labels match.").FontSize(12.5).TextColor(t.TextMuted).Padding(8, 10)
			}
		})
	})
}

func cmpPrefix(s, prefix string) string {
	if s == "" {
		return ""
	}
	return prefix + s
}

// loadRepoLabels reads the labels of the repository once, off the main
// thread.
func (t *repoTab) loadRepoLabels(pv *prView) {
	if pv.labelsLoaded || t.gh == nil {
		return
	}
	repo, a := *t.gh, t.app
	a.background(func() {
		var labels []github.Label
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cl, err := a.client(ctx, repo.Host)
		if err == nil {
			labels, err = cl.RepoLabels(ctx, repo)
		}
		a.update(func() {
			if err != nil && len(labels) == 0 {
				pv.labelMenu = false
				a.fail("Could not read the labels", err)
				return
			}
			pv.repoLabels, pv.labelsLoaded = labels, true
		})
	})
}

// toggleLabel adds a label to the pull request, or takes it off, showing the
// change at once and undoing it if GitHub refuses.
func (t *repoTab) toggleLabel(pv *prView, l github.Label) {
	if t.gh == nil {
		return
	}
	before := slices.Clone(pv.pr.Labels)
	after := slices.DeleteFunc(slices.Clone(before), func(o github.Label) bool { return o.Name == l.Name })
	if len(after) == len(before) {
		after = append(after, l)
	}
	set := func(labels []github.Label) {
		pv.pr.Labels = labels
		for i := range t.prs {
			if t.prs[i].Number == pv.number {
				t.prs[i].Labels = labels
			}
		}
	}
	set(after)
	names := make([]string, len(after))
	for i, o := range after {
		names[i] = o.Name
	}
	repo, n, a := *t.gh, pv.number, t.app
	a.background(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cl, err := a.client(ctx, repo.Host)
		if err == nil {
			err = cl.SetLabels(ctx, repo, n, names)
		}
		a.update(func() {
			if err != nil && t.prTabOpen(pv) {
				set(before)
				a.fail("Could not change the labels", err)
			}
		})
	})
}
