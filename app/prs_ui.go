package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/eliott/myfork/internal/aigen"
	"github.com/eliott/myfork/internal/git"
	"github.com/eliott/myfork/internal/github"
)

// stateColor is the color of a pull request's state.
func prStateColor(c *ui.Context, pr github.PR) ui.Color {
	t := c.Theme()
	switch pr.StateLabel() {
	case "Open":
		return t.Success
	case "Merged":
		return ui.Hex("#8b5cf6")
	case "Closed":
		return t.Danger
	}
	return t.TextMuted
}

// prStateIcon is the pull request glyph for its state: merge when merged,
// the crossed one when closed.
func prStateIcon(pr github.PR) *ui.SVG {
	switch pr.StateLabel() {
	case "Merged":
		return iconMerge
	case "Closed":
		return iconPRClosed
	}
	return iconPR
}

func pill(c *ui.Context, label string, col ui.Color) {
	ui.Text(c, label).FontSize(11.5).FontWeight(600).TextColor(col).Padding(2, 9).Radius(10).Background(col.Alpha(0.15)).Border(1, col.Alpha(0.4)).Shrink(0)
}

// prTabStrip is the pull requests opened, as tabs: choosing one shows it, and
// its close button, shown over the one chosen or pointed at, closes it.
func (tab *repoTab) prTabStrip(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Gap(2).Grow(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
		for _, pv := range slices.Clone(tab.prTabs) {
			active := pv == tab.pr
			item := ui.Row(c).Key(fmt.Sprint("prtab", pv.number)).Gap(2).Radius(8).AlignItems(ui.Center).Shrink(1).MinWidth(0)
			hovered := item.Hovered()
			if active {
				item.Background(subtleBg)
			} else if hovered {
				item.Background(hoverBg)
			}
			item.Children(func() {
				label := fmt.Sprintf("#%d", pv.number)
				tabBtn := ui.ButtonBase(c).Key("select").Gap(6).Padding(5, 8).Radius(8).AlignItems(ui.Center).Shrink(1).MinWidth(0).Label("Pull request " + label).Tooltip(pv.pr.Title)
				if tabBtn.Clicked() {
					tab.pr = pv
				}
				tabBtn.Children(func() {
					ui.Icon(c, prStateIcon(pv.pr)).FontSize(14).TextColor(prStateColor(c, pv.pr)).Shrink(0)
					fg := t.TextMuted
					if active {
						fg = t.Text
					}
					ui.Text(c, label).FontSize(13.5).TextColor(fg).SingleLine().MinWidth(0)
				})
				if active || hovered {
					if iconButton(c, iconClose, "Close "+label).Key("close").Size(20, 20).Clicked() {
						tab.closePR(pv)
					}
				}
			})
		}
		if tab.app.prExpanded {
			if iconButton(c, iconPlus, "Show the list, to open another pull request").Clicked() {
				tab.app.prExpanded = false
			}
		}
	})
}

// prPanel is the pull request chosen, beside the list or, expanded, in its
// place.
func (tab *repoTab) prPanel(c *ui.Context) {
	t := c.Theme()
	a := tab.app
	pv := tab.pr
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Background(t.Background).Children(func() {
		ui.Row(c).Height(44).Padding(0, 12, 0, 16).Gap(6).Shrink(0).AlignItems(ui.Center).Children(func() {
			tab.prTabStrip(c)
			ui.Spacer(c)
			if a.prExpanded {
				if iconButton(c, iconMinimize, "Show the list again").Clicked() {
					a.prExpanded = false
				}
			} else if iconButton(c, iconMaximize, "Expand the pull request").Clicked() {
				a.prExpanded = true
			}
			if iconButton(c, iconClose, "Close the pull request").Clicked() {
				tab.closePR(pv)
			}
		})
		switch {
		case pv.err != "":
			emptyPanel(c, "Could not load pull request #"+strconv.Itoa(pv.number), pv.err, func() {
				if ui.Button(c, "Try Again").Clicked() {
					tab.reloadPR()
				}
			})
		case !pv.loaded && pv.pr.Number == 0:
			ui.Column(c).Grow(1).Center().Children(func() { ui.Spinner(c).Label("Loading") })
		default:
			tab.prDetail(c, pv)
		}
	})
}

// prDetail is a pull request: its header and actions, then its summary,
// timeline or code.
func (tab *repoTab) prDetail(c *ui.Context, pv *prView) {
	t := c.Theme()
	pr := pv.pr
	now := c.Now()
	idle := pv.busy == ""
	ui.Column(c).Padding(0, 18, 12).Gap(8).Shrink(0).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			link := ui.ButtonBase(c).Gap(5).Padding(2, 4).Radius(5).Shrink(1).MinWidth(0).Label("Open on GitHub").Tooltip(pr.HTMLURL)
			if link.Hovered() {
				link.Background(hoverBg)
			}
			if link.Clicked() {
				openInBrowser(pr.HTMLURL)
			}
			link.Children(func() {
				ui.Text(c, tab.gh.String()).FontSize(12.5).TextColor(t.TextMuted).SingleLine().MinWidth(0)
				ui.Textf(c, "#%d", pr.Number).FontSize(12.5).FontWeight(600).TextColor(prStateColor(c, pr)).Shrink(0)
				ui.Icon(c, iconOpen).FontSize(11).TextColor(prStateColor(c, pr)).Shrink(0)
			})
			if pv.busy != "" {
				ui.Spinner(c).Label(pv.busy)
			}
			ui.Spacer(c)
			if pv.stack != nil && pv.stackPos() >= 0 {
				tab.stackButton(c, pv)
			}
			if n := len(pv.convo); n > 0 {
				ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Tooltip(plural(n, "comment", "comments")).Children(func() {
					ui.Icon(c, iconComment).FontSize(14).TextColor(t.TextMuted)
					ui.Textf(c, "%d", n).FontSize(12.5).TextColor(t.TextMuted)
				})
			}
			if pr.State == "open" && !pr.Merged {
				review := ui.Button(c, "").Label("Review").Tooltip("Comment, approve or request changes").Disabled(!idle)
				review.Children(func() {
					ui.Icon(c, iconReview).FontSize(14)
					ui.Text(c, "Review")
					if n := pv.draftComments(); n > 0 {
						ui.Textf(c, "%d", n).FontSize(11).FontWeight(700).Padding(0, 6).Radius(8).Background(refColor.Alpha(0.2)).TextColor(refColor)
					}
				})
				if review.Clicked() {
					tab.app.reviewPRDialog(tab)
				}
				blocked := pv.stackBlocksMerge()
				merge := ui.PrimaryButton(c, "Merge…").Disabled(!idle || pr.Draft || blocked != "" || pr.Mergeable != nil && !*pr.Mergeable)
				switch {
				case blocked != "":
					merge.Tooltip(blocked)
				case pr.Draft:
					merge.Tooltip("A draft cannot be merged: mark it ready for review first")
				case pr.Mergeable != nil && !*pr.Mergeable:
					merge.Tooltip("This pull request has conflicts with " + pr.Base.Ref)
				}
				if merge.Clicked() {
					tab.app.mergePRDialog(tab, mergeMethods[0])
				}
			}
			more := iconButton(c, iconMore, "More")
			if more.Clicked() {
				tab.app.prMore = !tab.app.prMore
			}
			tab.prMoreMenu(c, more, pv)
		})
		ui.Text(c, pr.Title).FontSize(20).Bold().Selectable()
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Avatar(c, pr.User.Login, tab.app.avatars.image(pr.User.Login, pr.User.AvatarURL)).FontSize(9)
			ui.Text(c, pr.User.Login).FontSize(13).FontWeight(600).SingleLine().Shrink(1).MinWidth(0)
			if !pr.UpdatedAt.IsZero() {
				ui.Text(c, "· updated "+relativeTime(now, pr.UpdatedAt)).FontSize(12.5).TextColor(t.TextMuted).SingleLine().Shrink(0)
			}
			ui.Spacer(c)
			cmd := fmt.Sprintf("gh pr checkout %d", pr.Number)
			cp := ui.ButtonBase(c).Padding(2, 5).Radius(5).Shrink(1).MinWidth(0).Label("Copy " + cmd).Tooltip("Copy " + cmd)
			if cp.Hovered() {
				cp.Background(hoverBg)
			}
			if cp.Clicked() {
				copyText(cmd)
				tab.app.say("Copied “%s”", cmd)
			}
			cp.Children(func() { ui.Text(c, cmd).Font(mono).FontSize(12).TextColor(t.TextMuted).SingleLine().MinWidth(0) })
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			pill(c, pr.StateLabel(), prStateColor(c, pr))
			ui.Text(c, pr.Base.Ref).Font(mono).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
			ui.Icon(c, iconArrowLeft).FontSize(13).TextColor(t.TextMuted).Shrink(0)
			ui.Text(c, pr.Head.Ref).Font(mono).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(2).MinWidth(0).Tooltip(prSummary(pr))
			ui.Spacer(c)
			ui.Icon(c, iconFile).FontSize(13).TextColor(t.TextMuted).Shrink(0)
			ui.Text(c, plural(max(pr.ChangedFiles, len(pv.files)), "file", "files")).FontSize(12.5).TextColor(t.TextMuted).Shrink(0)
			if pr.Additions+pr.Deletions > 0 {
				ui.Textf(c, "+%d", pr.Additions).Font(mono).FontSize(12).FontWeight(600).TextColor(t.Success).Shrink(0)
				ui.Textf(c, "−%d", pr.Deletions).Font(mono).FontSize(12).FontWeight(600).TextColor(t.Danger).Shrink(0)
			}
		})
	})
	ui.Row(c).Padding(8, 18).Gap(8).Shrink(0).AlignItems(ui.Center).BorderWidth(1, 0, 1, 0).BorderColor(hairline).Children(func() {
		ui.Segmented(c, &pv.tab, "Summary", "Guide", "Timeline", "Code").Label("Pull request")
		ui.Spacer(c)
		if pv.tab == 2 {
			tab.timelineControls(c, pv)
		} else {
			tab.prStatusPills(c, pv)
		}
	})
	switch pv.tab {
	case 1:
		tab.prGuide(c, pv)
	case 2:
		tab.prTimeline(c, pv)
	case 3:
		tab.prCode(c, pv)
	default:
		tab.prSummary(c, pv)
	}
}

// draftComments counts the comments of the review not yet submitted: those
// on GitHub, in the viewer's pending review, and those written here.
func (pv *prView) draftComments() int {
	n := pv.reviews.PendingComments()
	if pv.rv != nil {
		n += pv.rv.draftCount()
	}
	return n
}

// prMoreMenu is the menu of the More button: what else can be done to the
// pull request.
func (tab *repoTab) prMoreMenu(c *ui.Context, anchor ui.Element, pv *prView) {
	pr := pv.pr
	idle := pv.busy == ""
	open := pr.State == "open" && !pr.Merged
	blocked := pv.stackBlocksMerge()
	cannotMerge := !idle || !open || pr.Draft || blocked != "" || pr.Mergeable != nil && !*pr.Mergeable
	a := tab.app
	draft := menuRow{icon: iconDraft, label: "Convert to draft", disabled: !idle || !open, run: tab.convertPRToDraft}
	if pr.Draft {
		draft = menuRow{icon: iconPR, label: "Ready for Review", disabled: !idle || !open, run: tab.markPRReady}
	}
	auto := menuRow{icon: iconMerge, label: "Enable auto-merge", disabled: !idle || !open || pr.Draft, run: func() { tab.setPRAutoMerge(true) }}
	if pr.AutoMerge != nil {
		auto = menuRow{icon: iconMerge, label: "Disable auto-merge", disabled: !idle || !open, run: func() { tab.setPRAutoMerge(false) }}
	}
	closing := menuRow{icon: iconPRClosed, label: "Close pull request", danger: true, disabled: !idle, run: func() { tab.setPRState("closed") }}
	if !open {
		closing = menuRow{icon: iconPR, label: "Reopen pull request", disabled: !idle || pr.Merged, run: func() { tab.setPRState("open") }}
	}
	popMenu(c, anchor, &a.prMore,
		[]menuRow{{icon: iconBranch, label: "Check Out", disabled: tab.busy != "", run: func() { tab.checkoutPR(pv.number) }}},
		[]menuRow{draft, auto},
		[]menuRow{
			{icon: iconMerge, label: "Merge", disabled: cannotMerge, tooltip: blocked, run: func() { a.mergePRDialog(tab, mergeMethods[0]) }},
			{icon: iconMerge, label: "Squash and merge", disabled: cannotMerge, tooltip: blocked, run: func() { a.mergePRDialog(tab, mergeMethods[1]) }},
			{icon: iconMerge, label: "Rebase and merge", disabled: cannotMerge, tooltip: blocked, run: func() { a.mergePRDialog(tab, mergeMethods[2]) }},
		},
		[]menuRow{
			{icon: iconExternal, label: "Open on GitHub", run: func() { openInBrowser(pr.HTMLURL) }},
			{icon: iconLink, label: "Copy link", hint: "⇧⌘C", run: tab.copyPRLink},
			{icon: iconCopy, label: "Copy PR number", hint: "⇧⌘K", run: tab.copyPRNumber},
			{icon: iconRefresh, label: "Read again", run: tab.reloadPR},
		},
		[]menuRow{closing},
	)
}

func (tab *repoTab) copyPRLink() {
	if pv := tab.pr; pv != nil {
		tab.app.copy(pv.pr.HTMLURL)
		tab.app.say("Copied the link to #%d", pv.number)
	}
}

func (tab *repoTab) copyPRNumber() {
	if pv := tab.pr; pv != nil {
		tab.app.copy(strconv.Itoa(pv.number))
		tab.app.say("Copied #%d", pv.number)
	}
}

// prStatusPills say whether the checks pass and the branch merges.
func (tab *repoTab) prStatusPills(c *ui.Context, pv *prView) {
	t := c.Theme()
	pr := pv.pr
	switch pv.summary {
	case "success":
		pill(c, "Checks passed", t.Success)
	case "failure":
		failed := 0
		for _, ch := range pv.checks {
			if ch.State == "failure" {
				failed++
			}
		}
		pill(c, fmt.Sprintf("%d failing", failed), t.Danger)
	case "pending":
		pill(c, "Checks running", t.Warning)
	}
	if pr.State != "open" {
		return
	}
	switch {
	case pr.Mergeable != nil && !*pr.Mergeable:
		pill(c, "Conflicts", t.Danger)
	case pr.MergeableState == "behind":
		pill(c, "Behind "+pr.Base.Ref, t.Warning)
	case pr.Mergeable != nil && *pr.Mergeable:
		pill(c, "Can be merged", t.Success)
	}
}

// prCard is a comment or a review: who wrote it, when, and what it says.
func (tab *repoTab) prCard(c *ui.Context, key string, who github.User, when string, state string, body string, avatar bool) {
	t := c.Theme()
	ui.Column(c).Key(key).FillWidth().Radius(8).Border(1, hairline).Children(func() {
		ui.Row(c).Gap(8).Padding(6, 12).AlignItems(ui.Center).Background(subtleBg).Children(func() {
			if avatar {
				ui.Avatar(c, who.Login, tab.app.avatars.image(who.Login, who.AvatarURL)).FontSize(10)
			}
			ui.Text(c, who.Login).FontSize(12.5).FontWeight(600)
			ui.Text(c, when).FontSize(12).TextColor(t.TextMuted)
			switch state {
			case "APPROVED":
				pill(c, "approved", t.Success)
			case "CHANGES_REQUESTED":
				pill(c, "requested changes", t.Danger)
			}
		})
		if strings.TrimSpace(body) == "" {
			ui.Text(c, "No description provided.").FontSize(12.5).Italic().TextColor(t.TextMuted).Padding(8, 12)
			return
		}
		ui.Column(c).Padding(4, 12, 8).FillWidth().Children(func() {
			markdown.View(c, body, &markdown.Options{OnLink: openInBrowser}).FillWidth()
		})
	})
}

// sectionHeading is the title of a part of the summary, which opens and
// closes it; it reports a click.
func sectionHeading(c *ui.Context, key, title string, open bool, trailing func()) bool {
	t := c.Theme()
	clicked := false
	ui.Row(c).Key(key).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		b := ui.ButtonBase(c).Key(key+"-toggle").Gap(6).Padding(2, 4).Radius(5).Label(title).TextColor(t.TextMuted)
		if b.Hovered() {
			b.Background(hoverBg)
		}
		clicked = b.Clicked()
		b.Children(func() {
			ui.Text(c, title).FontSize(13).FontWeight(600)
			chevron := iconChevronRight
			if open {
				chevron = iconChevronDown
			}
			ui.Icon(c, chevron).FontSize(13)
		})
		if trailing != nil {
			trailing()
		}
	})
	return clicked
}

// orderButton flips an order: newest first or oldest first.
func orderButton(c *ui.Context, key string, newestFirst *bool) {
	t := c.Theme()
	label := "Oldest first"
	icon := iconArrowUp
	if *newestFirst {
		label, icon = "Newest first", iconArrowDown
	}
	b := ui.ButtonBase(c).Key(key).Gap(4).Padding(2, 6).Radius(5).Label(label).TextColor(t.TextMuted)
	if b.Hovered() {
		b.Background(hoverBg)
	}
	if b.Clicked() {
		*newestFirst = !*newestFirst
	}
	b.Children(func() {
		ui.Icon(c, icon).FontSize(12)
		ui.Text(c, label).FontSize(12)
	})
}

// prSummary is the pull request's people and labels, its description, its
// checks and its comments.
func (tab *repoTab) prSummary(c *ui.Context, pv *prView) {
	t := c.Theme()
	now := c.Now()
	pr := pv.pr
	meta := func(icon *ui.SVG, name string, body func()) {
		ui.Row(c).Key("meta-" + name).Gap(8).AlignItems(ui.Center).MinHeight(24).Children(func() {
			ui.Row(c).Gap(8).Width(112).Shrink(0).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icon).FontSize(14).TextColor(t.TextMuted)
				ui.Text(c, name).FontSize(12.5).TextColor(t.TextMuted)
			})
			ui.Row(c).Gap(6).Wrap().AlignItems(ui.Center).Grow(1).MinWidth(0).Children(body)
		})
	}
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Padding(14, 18, 24).Gap(12).Children(func() {
			meta(iconBlame, "Reviewers", func() {
				if len(pr.RequestedReviewers) == 0 {
					ui.Text(c, "None").FontSize(12.5).TextColor(t.TextMuted)
				}
				for _, r := range pr.RequestedReviewers {
					ui.Row(c).Key("reviewer-" + r.Login).Gap(5).AlignItems(ui.Center).Children(func() {
						ui.Avatar(c, r.Login, tab.app.avatars.image(r.Login, r.AvatarURL)).FontSize(8)
						ui.Text(c, r.Login).FontSize(12.5)
					})
				}
			})
			meta(iconTag, "Labels", func() {
				if len(pr.Labels) == 0 {
					ui.Text(c, "None").FontSize(12.5).TextColor(t.TextMuted)
				}
				for _, l := range pr.Labels {
					col := ui.Hex("#" + strings.TrimPrefix(cmp.Or(l.Color, "888888"), "#"))
					ui.Text(c, l.Name).Key("label-"+l.Name).FontSize(11.5).FontWeight(600).Padding(1, 8).Radius(10).Background(col.Alpha(0.18)).Border(1, col.Alpha(0.5))
				}
			})

			if sectionHeading(c, "description", "Description", !pv.descClosed, nil) {
				pv.descClosed = !pv.descClosed
			}
			if !pv.descClosed {
				if strings.TrimSpace(pr.Body) == "" {
					ui.Text(c, "No description provided.").FontSize(12.5).Italic().TextColor(t.TextMuted)
				} else {
					markdown.View(c, pr.Body, &markdown.Options{OnLink: openInBrowser}).FillWidth()
				}
			}

			if sectionHeading(c, "checks", "Checks", pv.checksOpen, func() { tab.prChecksNote(c, pv) }) {
				pv.checksOpen = !pv.checksOpen
			}
			if pv.checksOpen {
				tab.prChecks(c, pv)
			}

			newest := !pv.commentsOldest
			if sectionHeading(c, "comments", fmt.Sprintf("Comments (%d)", len(pv.convo)), true, func() {
				ui.Spacer(c)
				orderButton(c, "comments-order", &newest)
			}) {
				// Always open: the composer is under them.
			}
			pv.commentsOldest = !newest
			if len(pv.convo) == 0 {
				ui.Text(c, "No comments yet.").FontSize(12.5).TextColor(t.TextMuted)
			}
			convo := slices.Clone(pv.convo)
			if newest {
				slices.Reverse(convo)
			}
			for i, cm := range convo {
				tab.prCard(c, fmt.Sprint("comment-", cm.ID, "-", i), cm.User, relativeTime(now, cm.CreatedAt), cm.State, cm.Body, true)
			}
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				ui.TextArea(c, &pv.draft).Lines(3, 10).Placeholder("Leave a comment").Label("Write a comment")
				ui.Row(c).Justify(ui.End).Children(func() {
					if ui.PrimaryButton(c, "Comment").Disabled(strings.TrimSpace(pv.draft) == "" || pv.busy != "").Clicked() {
						tab.commentOnPR()
					}
				})
			})
		})
	})
}

// prChecksNote says how the checks fared, beside their heading.
func (tab *repoTab) prChecksNote(c *ui.Context, pv *prView) {
	t := c.Theme()
	switch {
	case len(pv.checks) == 0:
		ui.Text(c, "No checks reported").FontSize(12).TextColor(t.TextMuted)
	case pv.summary == "success":
		ui.Textf(c, "%d passed", len(pv.checks)).FontSize(12).TextColor(t.Success)
	case pv.summary == "failure":
		ui.Textf(c, "%d of %d failing", pv.failing(), len(pv.checks)).FontSize(12).TextColor(t.Danger)
	default:
		ui.Textf(c, "%d running", len(pv.checks)).FontSize(12).TextColor(t.Warning)
	}
}

// failing counts the checks that failed.
func (pv *prView) failing() int {
	n := 0
	for _, ch := range pv.checks {
		if ch.State == "failure" {
			n++
		}
	}
	return n
}

func (tab *repoTab) prChecks(c *ui.Context, pv *prView) {
	t := c.Theme()
	if len(pv.checks) == 0 {
		return
	}
	ui.Column(c).FillWidth().Radius(8).Border(1, hairline).Children(func() {
		for i, ch := range pv.checks {
			chk := ch
			col, icon := t.TextMuted, iconRefresh
			switch chk.State {
			case "success":
				col, icon = t.Success, iconCheck
			case "failure":
				col, icon = t.Danger, iconClose
			case "neutral":
				icon = iconMinus
			}
			row := ui.Row(c).Key(fmt.Sprint("check", i)).Gap(8).Padding(4, 12).AlignItems(ui.Center)
			row.Children(func() {
				ui.Icon(c, icon).FontSize(13).TextColor(col)
				ui.Text(c, chk.Name).FontSize(12.5).Grow(1).SingleLine()
				ui.Text(c, chk.State).FontSize(11.5).TextColor(col)
				if chk.URL != "" {
					if ui.Button(c, "Details").Key(fmt.Sprint("details", i)).Clicked() {
						openInBrowser(chk.URL)
					}
				}
			})
		}
	})
}

// timelineWidth is how wide the timeline gets: its lines are read, so they
// are not stretched across the page.
const timelineWidth = 760

// prTimeline is what happened to the pull request, in order: it was opened,
// commits were pushed, people commented and reviewed. Each event has its
// avatar or icon on a line that joins them.
func (tab *repoTab) prTimeline(c *ui.Context, pv *prView) {
	t := c.Theme()
	now := c.Now()
	pr := pv.pr
	type event struct {
		when    time.Time
		commit  *github.Commit
		comment *github.Comment
	}
	events := []event{{when: pr.CreatedAt}}
	for i := range pv.commits {
		events = append(events, event{when: pv.commits[i].Commit.Author.Date, commit: &pv.commits[i]})
	}
	for i := range pv.convo {
		events = append(events, event{when: pv.convo[i].CreatedAt, comment: &pv.convo[i]})
	}
	slices.SortStableFunc(events, func(a, b event) int { return a.when.Compare(b.when) })
	if !pv.timelineOldest {
		slices.Reverse(events)
	}
	const rail = 36
	// entry lays an event out: its mark on the line, and what it says.
	entry := func(key string, first, last bool, mark func(), body func()) {
		ui.Row(c).Key(key).FillWidth().Gap(14).AlignItems(ui.Start).Padding(0, 0, 0).Children(func() {
			ui.Column(c).Width(rail).Shrink(0).AlignSelf(ui.Stretch).AlignItems(ui.Center).Children(func() {
				line := ui.Box(c).Absolute().Left(rail/2 - 0.5).Width(1).Background(hairline)
				switch {
				case first && last:
					line.Height(0)
				case first:
					line.Top(18).Bottom(0)
				case last:
					line.Top(0).Height(18)
				default:
					line.Top(0).Bottom(0)
				}
				ui.Column(c).Padding(0, 0, 0).Background(t.Background).Children(mark)
			})
			ui.Column(c).Grow(1).MinWidth(0).Padding(0, 0, 18).Gap(2).Children(body)
		})
	}
	title := func(who, what string) {
		ui.RichText(c, ui.Span{Text: who + "  ", Weight: 600}, ui.Span{Text: what, Weight: 500}).FontSize(14).SingleLine().MinWidth(0)
	}
	avatar := func(u *github.User, name string) {
		if u == nil {
			ui.Avatar(c, name, nil).Size(36, 36)
			return
		}
		ui.Avatar(c, u.Login, tab.app.avatars.image(u.Login, u.AvatarURL)).Size(36, 36)
	}
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(timelineWidth).Padding(24, 18, 24).Children(func() {
				for i, e := range events {
					first, last := i == 0, i == len(events)-1
					switch {
					case e.commit != nil:
						cm := e.commit
						entry("commit-"+cm.SHA, first, last, func() { avatar(cm.Author, cm.Commit.Author.Name) }, func() {
							ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
								ui.Text(c, cm.Subject()).FontSize(14).FontWeight(600).SingleLine().Grow(1).MinWidth(0).Tooltip(cm.Commit.Message)
							})
							ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
								ui.Text(c, cm.SHA[:min(7, len(cm.SHA))]).Font(mono).FontSize(12).TextColor(t.TextMuted)
								ui.Text(c, cm.Commit.Author.Name+"  "+relativeTime(now, e.when)).FontSize(12.5).TextColor(t.TextMuted).SingleLine().MinWidth(0)
							})
						})
					case e.comment != nil:
						cm := e.comment
						entry(fmt.Sprint("event-", cm.ID, "-", i), first, last, func() { avatar(&cm.User, cm.User.Login) }, func() {
							tab.prCard(c, fmt.Sprint("card-", cm.ID, "-", i), cm.User, relativeTime(now, e.when), cm.State, cm.Body, false)
						})
					default:
						entry("opened", first, last, func() {
							ui.Column(c).Size(36, 36).Center().Children(func() { ui.Icon(c, iconPR).FontSize(18).TextColor(t.TextMuted) })
						}, func() {
							title(pr.User.Login, "Pull request opened")
							ui.Text(c, relativeTime(now, e.when)).FontSize(12.5).TextColor(t.TextMuted)
						})
					}
				}
			})
		})
	})
}

// timelineControls say how many comments and commits the timeline holds, and
// flip its order.
func (tab *repoTab) timelineControls(c *ui.Context, pv *prView) {
	t := c.Theme()
	ui.Row(c).Gap(5).AlignItems(ui.Center).Shrink(0).Tooltip(plural(len(pv.convo), "comment", "comments")).Children(func() {
		ui.Icon(c, iconComment).FontSize(14).TextColor(t.TextMuted)
		ui.Textf(c, "%d", len(pv.convo)).FontSize(12.5).TextColor(t.TextMuted)
	})
	ui.Text(c, "·").FontSize(12.5).TextColor(t.TextMuted)
	ui.Row(c).Gap(5).AlignItems(ui.Center).Shrink(0).Tooltip(plural(len(pv.commits), "commit", "commits")).Children(func() {
		ui.Icon(c, iconCommit).FontSize(14).TextColor(t.TextMuted)
		ui.Textf(c, "%d", len(pv.commits)).FontSize(12.5).TextColor(t.TextMuted)
	})
	newest := !pv.timelineOldest
	orderButton(c, "timeline-order", &newest)
	pv.timelineOldest = !newest
}

// prCode is the patches of the files, as cards in one surface, as a commit's,
// with the tree of the files beside it to show or hide.
func (tab *repoTab) prCode(c *ui.Context, pv *prView) {
	t := c.Theme()
	a := tab.app
	rv := pv.rv
	if rv == nil {
		ui.Column(c).Grow(1).Center().Children(func() { ui.Spinner(c).Label("Loading") })
		return
	}
	s := a.store.settings()
	viewed, collapsed := 0, 0
	for _, f := range rv.files {
		if f.viewed {
			viewed++
		}
		if f.collapsed {
			collapsed++
		}
	}
	allCollapsed := len(rv.files) > 0 && collapsed == len(rv.files)
	toggle := func(icon *ui.SVG, label string, on bool) bool {
		b := iconButton(c, icon, label)
		if on {
			b.Background(hairline).TextColor(t.Text)
		}
		return b.Clicked()
	}
	ui.Row(c).Height(42).Padding(0, 18).Gap(8).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
		ui.Text(c, plural(len(rv.files), "file", "files")).FontSize(12.5).TextColor(t.TextMuted)
		ui.Text(c, "·").FontSize(12.5).TextColor(t.TextMuted)
		ui.Textf(c, "%d / %d viewed", viewed, len(rv.files)).FontSize(12.5).TextColor(t.TextMuted)
		// How many comments there are, and the way from one to the next.
		if n := rv.totalComments(); n > 0 {
			ui.Text(c, "·").FontSize(12.5).TextColor(t.TextMuted)
			ui.Icon(c, iconComment).FontSize(14).TextColor(t.Accent)
			ui.Text(c, plural(n, "comment", "comments")).FontSize(12.5).TextColor(t.TextMuted)
			if iconButton(c, iconArrowUp, "Previous comment").Size(26, 26).Clicked() {
				rv.goToComment(-1)
			}
			if iconButton(c, iconArrowDown, "Next comment").Size(26, 26).Clicked() {
				rv.goToComment(1)
			}
		}
		ui.Spacer(c)
		if toggle(iconPilcrow, "Ignore whitespace changes", s.IgnoreWhitespace) {
			a.setSetting(func(s *Settings) { s.IgnoreWhitespace = !s.IgnoreWhitespace })
		}
		tab.layoutControl(c)
		if toggle(iconWrap, "Wrap long lines", s.WrapLines) {
			a.setSetting(func(s *Settings) { s.WrapLines = !s.WrapLines })
		}
		label := "Collapse all files"
		if allCollapsed {
			label = "Expand all files"
		}
		if iconButton(c, iconExpand, label).Clicked() {
			for _, f := range rv.files {
				f.collapsed = !allCollapsed
			}
			rv.dirty = true
		}
		label = "Show file tree"
		if a.prTree {
			label = "Hide file tree"
		}
		if toggle(iconTree, label, a.prTree) {
			a.prTree = !a.prTree
		}
	})
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).MinHeight(0).Children(func() { tab.selectSurface(c, rv) })
		if !a.prTree {
			return
		}
		ui.Column(c).Width(300).Shrink(0).MinHeight(0).BorderWidth(0, 0, 0, 1).BorderColor(hairline).Children(func() {
			ui.Row(c).Padding(10, 8, 6, 16).Gap(8).Shrink(0).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Files").FontSize(13).FontWeight(600)
				ui.Textf(c, "%d", len(rv.files)).FontSize(12).TextColor(t.TextMuted)
				ui.Spacer(c)
				if iconButton(c, iconClose, "Hide file tree").Clicked() {
					a.prTree = false
				}
			})
			tab.reviewFiles(c, rv)
		})
	})
}

// ---- dialogs ----

// reviewPRDialog finishes a review of the pull request shown: a comment, and
// whether it approves or asks for changes.
func (a *app) reviewPRDialog(tab *repoTab) {
	pv := tab.pr
	if pv == nil || tab.gh == nil {
		return
	}
	repo, n := *tab.gh, pv.number
	events := map[string]string{"Comment": "COMMENT", "Approve": "APPROVE", "Request changes": "REQUEST_CHANGES"}
	// The comments written on lines go with the review, whatever it says: those
	// on GitHub in a pending review, and those kept here until now.
	var pending *github.PendingReview
	var written []*comment
	var local []github.NewComment
	if pv.rv != nil {
		for _, cm := range pv.rv.comments {
			if !cm.pending() {
				continue
			}
			written = append(written, cm)
			nc := github.NewComment{Body: strings.TrimSpace(cm.text), CommitID: pv.pr.Head.SHA, Path: cm.path, Side: github.Right, Line: cm.line}
			if cm.side == sideOld {
				nc.Side = github.Left
			}
			if cm.start != 0 && cm.start != cm.line {
				nc.StartLine, nc.Line = min(cm.start, cm.line), max(cm.start, cm.line)
			}
			local = append(local, nc)
		}
	}
	body, drafts := "", pv.reviews.PendingComments()+len(local)
	if pv.reviews != nil && pv.reviews.Pending != nil {
		pending, body = pv.reviews.Pending, pv.reviews.Pending.Body
	}
	a.showDialog(&dialog{
		title:   "Finish Your Review",
		message: fmt.Sprintf("#%d %s", n, pv.pr.Title),
		ok:      "Submit Review",
		fields: append(draftNote(drafts),
			area("body", "", "Leave a comment", body),
			radio("event", "Comment",
				option{"Comment", "Submit general feedback without explicit approval."},
				option{"Approve", "Submit feedback and approve merging these changes."},
				option{"Request changes", "Submit feedback that must be addressed before merging."})),
		validate: func(d *dialog) string {
			if d.str("body") == "" && d.str("event") != "Approve" && drafts == 0 {
				return "Leave a comment"
			}
			return ""
		},
		run: func(d *dialog) error {
			ctx := context.Background()
			cl, err := a.client(ctx, repo.Host)
			if err != nil {
				return err
			}
			return cl.SubmitReview(ctx, repo, n, pending, events[d.str("event")], d.str("body"), local)
		},
		done: func() {
			// What was written is on GitHub now, and comes back as threads.
			if pv.rv != nil {
				pv.rv.comments = slices.DeleteFunc(pv.rv.comments, func(cm *comment) bool { return slices.Contains(written, cm) })
				pv.rv.dirty = true
			}
			tab.prsLoaded = false
			tab.reloadPR()
			a.say("Review submitted on #%d", n)
		},
	})
}

// mergeMethods are the ways to merge a pull request, as the dialog says them.
var mergeMethods = []string{"Create a merge commit", "Squash and merge", "Rebase and merge"}

// mergePRDialog merges the pull request shown, by the method the dialog starts
// with: one of the choices below.
func (a *app) mergePRDialog(tab *repoTab, method string) {
	pv := tab.pr
	if pv == nil || tab.gh == nil {
		return
	}
	repo, n := *tab.gh, pv.number
	title := fmt.Sprintf("%s (#%d)", pv.pr.Title, n)
	a.showDialog(&dialog{
		title:   fmt.Sprintf("Merge pull request #%d", n),
		message: pv.pr.Title,
		ok:      "Merge",
		fields: []*field{
			choice("method", "Method:", method, mergeMethods...),
			text("title", "Commit title:", "", title),
		},
		run: func(d *dialog) error {
			ctx := context.Background()
			cl, err := a.client(ctx, repo.Host)
			if err != nil {
				return err
			}
			method := map[string]string{"Create a merge commit": "merge", "Squash and merge": "squash", "Rebase and merge": "rebase"}[d.field("method").text]
			t := ""
			if method != "rebase" {
				t = d.str("title")
			}
			return cl.Merge(ctx, repo, n, method, t)
		},
		done: func() {
			tab.prsLoaded = false
			tab.reloadPR()
			a.say("Merged #%d", n)
		},
	})
}

// openPRDialog opens a pull request by its number.
func (a *app) openPRDialog(tab *repoTab) {
	if tab.gh == nil {
		a.fail("Open pull request", errString("This repository has no remote on GitHub."))
		return
	}
	a.showDialog(&dialog{
		title:  "Open pull request",
		ok:     "Open",
		fields: []*field{text("n", "Number:", "123", "")},
		validate: func(d *dialog) string {
			if n, err := strconv.Atoi(strings.TrimPrefix(d.str("n"), "#")); err != nil || n <= 0 {
				return "Enter a pull request number"
			}
			return ""
		},
		done: func() {},
		run:  func(d *dialog) error { return nil },
	})
	d := a.dialog
	d.done = func() {
		n, _ := strconv.Atoi(strings.TrimPrefix(d.str("n"), "#"))
		tab.openPR(n)
	}
}

// createPRDialog opens a pull request for a branch, the one checked out when
// none is given.
func (a *app) createPRDialog(tab *repoTab, branch string) {
	if branch == "" {
		if b := tab.currentBranch(); b != nil {
			branch = b.Name
		}
	}
	if branch == "" && tab.gh != nil {
		a.fail("Create pull request", errString("HEAD is detached: check out a branch first."))
		return
	}
	a.createPRDialogFrom(tab, branch)
}

// prBases are the branches a pull request can be into: those of the remote, the
// local ones that are not there, from the usual ones first.
func (tab *repoTab) prBases() []string {
	var bases []string
	prefix := tab.ghRemote + "/"
	for _, r := range tab.refs {
		if r.Kind == git.RemoteBranch && strings.HasPrefix(r.Name, prefix) {
			if n := strings.TrimPrefix(r.Name, prefix); n != "HEAD" {
				bases = append(bases, n)
			}
		}
	}
	for _, r := range tab.refs {
		if r.Kind == git.LocalBranch && !slices.Contains(bases, r.Name) {
			bases = append(bases, r.Name)
		}
	}
	rank := func(s string) int {
		switch s {
		case "main":
			return 0
		case "master":
			return 1
		case "develop":
			return 2
		}
		return 3
	}
	slices.SortStableFunc(bases, func(x, y string) int { return rank(x) - rank(y) })
	if len(bases) == 0 {
		bases = []string{"main"}
	}
	return bases
}

// defaultPRBase is the branch a pull request is into unless said otherwise: the
// default branch of the remote, or else the first usual one, and never the
// branch itself.
func (tab *repoTab) defaultPRBase(bases []string, head string) string {
	if out, err := tab.repo.Read("symbolic-ref", "--short", "refs/remotes/"+tab.ghRemote+"/HEAD"); err == nil {
		name := strings.TrimPrefix(strings.TrimSpace(string(out)), tab.ghRemote+"/")
		if name != head && slices.Contains(bases, name) {
			return name
		}
	}
	for _, b := range bases {
		if b != head {
			return b
		}
	}
	return bases[0]
}

// createPRDialogFrom opens a pull request for one of the branches, the first
// unless the dialog is told another; the destination is chosen among all the
// branches, by typing.
func (a *app) createPRDialogFrom(tab *repoTab, heads ...string) {
	if tab.gh == nil {
		a.fail("Create pull request", errString("This repository has no remote on GitHub."))
		return
	}
	branch := heads[0]
	bases := tab.prBases()
	var local *git.Ref
	for i := range tab.refs {
		if tab.refs[i].Kind == git.LocalBranch && tab.refs[i].Name == branch {
			local = &tab.refs[i]
		}
	}
	ahead := local == nil || local.Upstream == "" || local.Ahead > 0
	// head is the branch the pull request is of, as the dialog has it now.
	head := func(d *dialog) string {
		if len(heads) > 1 {
			return d.field("head").text
		}
		return branch
	}
	title := strings.ReplaceAll(strings.ReplaceAll(branch, "-", " "), "_", " ")
	if i := strings.LastIndex(title, "/"); i >= 0 {
		title = title[i+1:]
	}
	if out, err := tab.repo.Read("log", "-1", "--format=%s", branch); err == nil && strings.TrimSpace(string(out)) != "" {
		title = strings.TrimSpace(string(out))
	}
	repo, remote := *tab.gh, tab.ghRemote
	var created github.PR
	// The open pull requests of this repository that a new one can be stacked
	// on, by the branch they are of.
	below := map[string]github.PR{}
	for _, p := range tab.prs {
		if p.State == "open" && !p.Merged && (p.Head.Repo == nil || strings.EqualFold(p.Head.Repo.FullName, repo.String())) {
			below[p.Head.Ref] = p
		}
	}
	// builtOn is the pull request a branch is built on, by branch: the open one
	// whose head is the nearest below it. Found once for each branch asked.
	builtOn := map[string]*github.PR{}
	builtOnFor := func(head string) *github.PR {
		if p, ok := builtOn[head]; ok {
			return p
		}
		p := tab.prBuiltOn(head)
		builtOn[head] = p
		return p
	}
	// target is the pull request the new one would be stacked on: the open one
	// of the branch it is into.
	target := func(d *dialog) (github.PR, bool) {
		if tab.stacksOff {
			return github.PR{}, false
		}
		p, ok := below[strings.TrimSpace(d.field("base").text)]
		// Only the top of a stack can have another on it.
		if place := tab.prStacks[p.Number]; place.Size > 1 && place.Position != place.Size {
			return github.PR{}, false
		}
		return p, ok && p.Head.Ref != head(d)
	}
	var stacked string
	var stackErr error
	fields := []*field{}
	if len(heads) > 1 {
		fields = append(fields, choice("head", "From:", branch, heads...))
	}
	fields = append(fields,
		text("title", "Title:", "", title),
		&field{kind: fieldArea, key: "body", label: "Description", placeholder: "What does this change, and why?", lines: 8, preview: true},
		action("Write the title and description with AI", func(d *dialog) (func(), error) {
			cfg, style := a.aiConfig()
			pr, err := aigen.WritePR(context.Background(), cfg, style, tab.repo, remote, d.field("base").text, head(d))
			if err != nil {
				return nil, err
			}
			return func() {
				d.field("title").text = pr.Title
				d.field("body").text = pr.Body
			}, nil
		}),
		combo("base", "Into:", tab.defaultPRBase(bases, branch), bases...),
		check("draft", "Create as a draft", false),
		check("push", "Push the branch to "+remote+" first", ahead),
	)
	fields = append(fields,
		&field{kind: fieldCheck, key: "stack", checked: true,
			show: func(d *dialog) bool { _, ok := target(d); return ok },
			labelFn: func(d *dialog) string {
				p, _ := target(d)
				return fmt.Sprintf("Stack it on #%d “%s”, the pull request of %s", p.Number, ellipsis(p.Title, 48), p.Head.Ref)
			}},
		&field{kind: fieldNote, key: "stack-detail",
			show: func(d *dialog) bool { _, ok := target(d); return ok && d.bool("stack") },
			labelFn: func(d *dialog) string {
				p, _ := target(d)
				if place := tab.prStacks[p.Number]; place.Size > 1 {
					return fmt.Sprintf("It joins the stack of %d pull requests, on top of #%d, as number %d.", place.Size, p.Number, place.Size+1)
				}
				return fmt.Sprintf("It starts a stack of #%d and this pull request.", p.Number)
			}},
		&field{kind: fieldNote, key: "stack-hint",
			show: func(d *dialog) bool {
				_, ok := target(d)
				p := builtOnFor(head(d))
				return !ok && !tab.stacksOff && p != nil && p.Head.Ref != strings.TrimSpace(d.field("base").text)
			},
			labelFn: func(d *dialog) string {
				p := builtOnFor(head(d))
				return fmt.Sprintf("This branch is built on #%d “%s” (%s). Choose %s as the destination to stack on it.", p.Number, ellipsis(p.Title, 48), p.Head.Ref, p.Head.Ref)
			}},
	)
	a.showDialog(&dialog{
		width:   720,
		stacked: true,
		title:   "Create pull request",
		message: fmt.Sprintf("Open a pull request on %s for “%s”", repo, branch),
		ok:      "Create Pull Request",
		fields:  fields,
		validate: func(d *dialog) string {
			base := strings.TrimSpace(d.field("base").text)
			switch {
			case d.str("title") == "":
				return "Enter a title"
			case !slices.Contains(bases, base):
				return "Choose a branch to merge into from the list"
			case base == head(d):
				return "Choose a base other than the branch itself"
			}
			return ""
		},
		run: func(d *dialog) error {
			branch := head(d)
			ctx := context.Background()
			cl, err := a.client(ctx, repo.Host)
			if err != nil {
				return err
			}
			if d.bool("push") {
				if err := tab.repo.Push(ctx, git.PushOptions{Remote: remote, Local: branch, SetUpstream: true}); err != nil {
					return err
				}
			}
			base := d.field("base").text
			created, err = cl.Create(ctx, repo, github.NewPR{Title: d.str("title"), Body: d.raw("body"), Head: branch, Base: base, Draft: d.bool("draft")})
			if err != nil {
				return err
			}
			// The pull request exists now: failing to stack it is told, not
			// raised, or a second try would make another.
			if p, ok := target(d); ok && d.bool("stack") {
				stacked, stackErr = stackOnBase(ctx, cl, repo, p.Number, created.Number)
				if errors.Is(stackErr, github.ErrStacksUnsupported) {
					stackErr = nil
				}
			}
			return nil
		},
		done: func() {
			tab.prsLoaded = false
			tab.openPR(created.Number)
			switch {
			case stackErr != nil:
				a.say("Opened #%d, but could not stack it: %s", created.Number, errorText(stackErr))
			case stacked != "":
				a.say("Opened #%d in %s", created.Number, stacked)
			default:
				a.say("Opened #%d", created.Number)
			}
		},
	})
}

// draftNote says that the comments written on lines go with the review.
func draftNote(n int) []*field {
	if n == 0 {
		return nil
	}
	return []*field{note(plural(n, "comment", "comments") + " on lines will be submitted with this review.")}
}

// ellipsis cuts a text to n characters, with … when it was longer.
func ellipsis(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return strings.TrimRight(string(r[:n]), " ") + "…"
	}
	return s
}

// prBuiltOn is the open pull request that a branch is built on: the one whose
// head is the nearest commit below the branch's own, nil when it is built on
// none. A pull request of the branch itself is not one.
func (t *repoTab) prBuiltOn(head string) *github.PR {
	var best *github.PR
	bestCount := -1
	for i := range t.prs {
		p := t.prs[i]
		if p.State != "open" || p.Merged || p.Head.Ref == head || p.Head.SHA == "" || !t.repo.HasCommit(p.Head.SHA) {
			continue
		}
		if _, err := t.repo.Read("merge-base", "--is-ancestor", p.Head.SHA, head); err != nil {
			continue
		}
		out, err := t.repo.Read("rev-list", "--count", p.Head.SHA+".."+head)
		if err != nil {
			continue
		}
		n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
		if bestCount < 0 || n < bestCount {
			best, bestCount = &p, n
		}
	}
	return best
}
