package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
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

// prTabStrip is the pull requests opened, of every repository, as tabs:
// choosing one shows it, and its close button, shown over the one chosen or
// pointed at, closes it. Each tab shows its number; the tooltip names the
// repository.
func (a *app) prTabStrip(c *ui.Context) {
	t := c.Theme()
	type opened struct {
		tab *repoTab
		pv  *prView
	}
	var list []opened
	repos := 0
	for _, tab := range a.prRepos() {
		for _, pv := range tab.prTabs {
			list = append(list, opened{tab, pv})
		}
		if len(tab.prTabs) > 0 {
			repos++
		}
	}
	ui.ScrollHorizontal(c).Gap(2).Padding(4, 0, 4, 0).Grow(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
		for _, o := range list {
			tab, pv := o.tab, o.pv
			active := tab == a.prTab && pv == tab.pr
			item := ui.Row(c).Key(fmt.Sprint("prtab", tab.gh, pv.number)).Gap(2).Radius(8).AlignItems(ui.Center).Shrink(0)
			hovered := item.Hovered()
			if active {
				item.Background(subtleBg)
			} else if hovered {
				item.Background(hoverBg)
			}
			item.Children(func() {
				// The tab shows only the number; the accessible name and the
				// close button name the repository too when there are several.
				label := fmt.Sprintf("#%d", pv.number)
				what := label
				if repos > 1 {
					what = tab.gh.Name + " " + label
				}
				tabBtn := ui.ButtonBase(c).Key("select").Gap(6).Padding(5, 8).Radius(8).AlignItems(ui.Center).Shrink(0).Label("Pull request " + what).Tooltip(tab.gh.String() + ": " + pv.pr.Title)
				if tabBtn.Clicked() {
					a.prTab, tab.pr = tab, pv
				}
				tabBtn.Children(func() {
					ui.Icon(c, prStateIcon(pv.pr)).FontSize(14).TextColor(prStateColor(c, pv.pr)).Shrink(0)
					fg := t.TextMuted
					if active {
						fg = t.Text
					}
					ui.Text(c, label).FontSize(13.5).TextColor(fg).SingleLine().Shrink(0)
				})
				if active || hovered {
					if iconButton(c.Key("close"), iconClose, "Close "+what).Size(20, 20).Clicked() {
						tab.closePR(pv)
					}
				}
			})
		}
		if a.prExpanded {
			if iconButton(c, iconPlus, "Show the list, to open another pull request").Clicked() {
				a.prExpanded = false
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
			a.prTabStrip(c)
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
			ui.Column(c).Grow(1).Center().Children(func() { spinner(c, "Loading") })
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
				spinner(c, pv.busy)
			} else if pv.reloading {
				spinner(c, "Reading the pull request again")
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
				tab.mergeButton(c, pv, idle)
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
	blocked := ""
	if pv.inStack() {
		blocked = pv.stackMergeIssue()
	}
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
	// A pull request in a stack is merged with the stack, by the button.
	var mergeRows []menuRow
	if !pv.inStack() {
		mergeRows = []menuRow{
			{icon: iconMerge, label: "Merge", disabled: cannotMerge, tooltip: blocked, run: func() { a.mergePRDialog(tab, mergeMethods[0]) }},
			{icon: iconMerge, label: "Squash and merge", disabled: cannotMerge, tooltip: blocked, run: func() { a.mergePRDialog(tab, mergeMethods[1]) }},
			{icon: iconMerge, label: "Rebase and merge", disabled: cannotMerge, tooltip: blocked, run: func() { a.mergePRDialog(tab, mergeMethods[2]) }},
		}
	}
	popMenu(c, anchor, &a.prMore,
		[]menuRow{{icon: iconRefresh, label: "Read again", run: tab.reloadPR}},
		[]menuRow{{icon: iconBranch, label: "Check Out", disabled: tab.busy != "", run: func() { tab.checkoutPR(pv.number) }}},
		[]menuRow{{icon: iconEdit, label: "Edit title and description", disabled: !idle, run: func() { a.editPRDialog(tab) }}},
		[]menuRow{draft, auto},
		mergeRows,
		[]menuRow{
			{icon: iconExternal, label: "Open on GitHub", run: func() { openInBrowser(pr.HTMLURL) }},
			{icon: iconLink, label: "Copy link", hint: "⇧⌘C", run: tab.copyPRLink},
			{icon: iconCopy, label: "Copy PR number", hint: "⇧⌘K", run: tab.copyPRNumber},
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

// prCard is a comment or a review: who wrote it, when, and what it says, then
// the threads of inline comments of the review, to answer and resolve here.
func (tab *repoTab) prCard(c *ui.Context, key string, who github.User, when string, state string, body string, avatar bool, threads []*github.Thread) {
	t := c.Theme()
	ui.Column(c).Key(key).FillWidth().Radius(8).Border(1, hairline).Children(func() {
		ui.Row(c).Gap(8).Padding(6, 12).AlignItems(ui.Center).Background(subtleBg).Children(func() {
			reviewBadge(c, state, 22)
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
			case "DISMISSED":
				pill(c, "dismissed", t.TextMuted)
			}
		})
		rv := tab.pr.rv
		if rv == nil {
			threads = nil
		}
		if strings.TrimSpace(body) != "" {
			ui.Column(c).Padding(4, 12, 8).FillWidth().Children(func() {
				githubMarkdown(c, "body", body)
			})
		} else if state == "DISMISSED" {
			ui.Text(c, "This review was dismissed.").FontSize(12.5).Italic().TextColor(t.TextMuted).Padding(8, 12)
		} else if len(threads) == 0 {
			ui.Text(c, "No description provided.").FontSize(12.5).Italic().TextColor(t.TextMuted).Padding(8, 12)
		}
		if len(threads) > 0 {
			ui.Column(c).FillWidth().Gap(10).Padding(8, 12, 12).Children(func() {
				for _, th := range threads {
					tab.threadBox(c, rv, th, true)
				}
			})
		}
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
			tab.prMergeStatus(c, pv)
			meta(iconBlame, "Reviewers", func() {
				reviewers := pv.reviewers()
				if len(reviewers) == 0 {
					ui.Text(c, "None").FontSize(12.5).TextColor(t.TextMuted)
				}
				for _, r := range reviewers {
					// The avatar alone, ringed green once it approved and red
					// when it asked for changes.
					ring, tip := ui.Color{}, r.User.Login
					switch r.State {
					case "APPROVED":
						ring, tip = mergeGreen, tip+" approved"
					case "CHANGES_REQUESTED":
						ring, tip = t.Danger, tip+" requested changes"
					}
					ui.Row(c).Key("reviewer-"+r.User.Login).Radius(16).Border(2, ring).Shrink(0).Tooltip(tip).Children(func() {
						ui.Avatar(c, r.User.Login, tab.app.avatars.image(r.User.Login, r.User.AvatarURL)).FontSize(10)
					})
				}
			})
			meta(iconTag, "Labels", func() {
				if len(pr.Labels) == 0 {
					ui.Text(c, "None").FontSize(12.5).TextColor(t.TextMuted)
				}
				for _, l := range pr.Labels {
					col := labelColor(l)
					ui.Text(c, l.Name).Key("label-"+l.Name).FontSize(11.5).FontWeight(600).Padding(1, 8).Radius(10).Background(col.Alpha(0.18)).Border(1, col.Alpha(0.5))
				}
				if pv.loaded {
					tab.labelPicker(c, pv)
				}
			})

			if sectionHeading(c, "description", "Description", !pv.descClosed, func() {
				ui.Spacer(c)
				if iconButton(c.Key("edit-description"), iconPencil, "Edit description").Size(22, 22).Disabled(pv.busy != "").Clicked() {
					tab.app.editPRDialog(tab)
				}
			}) {
				pv.descClosed = !pv.descClosed
			}
			if !pv.descClosed {
				if strings.TrimSpace(pr.Body) == "" {
					ui.Text(c, "No description provided.").FontSize(12.5).Italic().TextColor(t.TextMuted)
				} else {
					githubMarkdown(c, "pr-body", pr.Body)
				}
			}

			if sectionHeading(c, "checks", "Checks", pv.checksOpen, func() { tab.prChecksNote(c, pv) }) {
				pv.checksOpen = !pv.checksOpen
			}
			if pv.checksOpen {
				tab.prChecks(c, pv)
			}

			newest := !pv.commentsOldest
			convo := pv.conversation()
			if sectionHeading(c, "comments", fmt.Sprintf("Comments (%d)", len(convo)), true, func() {
				ui.Spacer(c)
				orderButton(c, "comments-order", &newest)
			}) {
				// Always open: the composer is under them.
			}
			pv.commentsOldest = !newest
			if len(convo) == 0 {
				ui.Text(c, "No comments yet.").FontSize(12.5).TextColor(t.TextMuted)
			}
			if newest {
				slices.Reverse(convo)
			}
			for i, it := range convo {
				tab.prCard(c, fmt.Sprint("comment-", it.id, "-", i), it.user, relativeTime(now, it.when), it.state(), it.body(), true, it.threads)
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
const timelineWidth = 1000

// prTimeline is what happened to the pull request, in order: it was opened,
// commits were pushed, people commented and reviewed. Each event has its
// avatar or icon on a line that joins them.
func (tab *repoTab) prTimeline(c *ui.Context, pv *prView) {
	t := c.Theme()
	now := c.Now()
	pr := pv.pr
	type event struct {
		when   time.Time
		commit *github.Commit
		item   *convoItem
	}
	events := []event{{when: pr.CreatedAt}}
	for i := range pv.commits {
		events = append(events, event{when: pv.commits[i].Commit.Author.Date, commit: &pv.commits[i]})
	}
	convo := pv.conversation()
	for i := range convo {
		events = append(events, event{when: convo[i].when, item: &convo[i]})
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
					case e.item != nil:
						it := e.item
						entry(fmt.Sprint("event-", it.id, "-", i), first, last, func() { avatar(&it.user, it.user.Login) }, func() {
							tab.prCard(c, fmt.Sprint("card-", it.id, "-", i), it.user, relativeTime(now, e.when), it.state(), it.body(), false, it.threads)
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
	if pv.commitSel != "" && !slices.ContainsFunc(pv.commits, func(cm github.Commit) bool { return cm.SHA == pv.commitSel }) {
		tab.showPRCommit(pv, "")
	}
	rv := pv.rv
	if pv.commitSel != "" {
		rv = pv.commitRV
	}
	if rv == nil {
		ui.Column(c).Grow(1).Center().Children(func() { spinner(c, "Loading") })
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
		tab.prCommitPicker(c, pv)
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
		if toggle(iconSearch, "Find in the code (⌘F)", false) {
			rv.toggleFind()
		}
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
		ui.Column(c).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
			switch {
			case rv.loading:
				ui.Column(c).Grow(1).Center().Children(func() { spinner(c, "Loading") })
			case rv.err != "":
				ui.Column(c).Grow(1).Center().Padding(24).Children(func() {
					ui.Text(c, rv.err).FontSize(13).TextColor(t.Danger).Selectable()
				})
			default:
				if i := slices.IndexFunc(pv.commits, func(cm github.Commit) bool { return cm.SHA == pv.commitSel }); i >= 0 {
					tab.prCommitMessage(c, &pv.commits[i])
				}
				tab.reviewSurface(c, rv)
			}
		})
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

// prCommitMessage is the message of the commit the Code tab shows: its title
// and description, and who committed it when.
func (tab *repoTab) prCommitMessage(c *ui.Context, cm *github.Commit) {
	t := c.Theme()
	subject, body, _ := strings.Cut(cm.Commit.Message, "\n")
	body = strings.TrimSpace(body)
	ui.Column(c).Key("commit-message").Shrink(0).Padding(12, 18).Gap(6).Background(headerBg(t)).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
		ui.Text(c, subject).FontSize(14).FontWeight(600).Selectable()
		if body != "" {
			ui.Text(c, body).Font(mono).FontSize(12.5).Selectable().MaxWidth(760)
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if cm.Author != nil {
				ui.Avatar(c, cm.Author.Login, tab.app.avatars.image(cm.Author.Login, cm.Author.AvatarURL)).Size(20, 20)
			} else {
				ui.Avatar(c, cm.Commit.Author.Name, nil).Size(20, 20)
			}
			ui.Textf(c, "%s committed %s", cm.Commit.Author.Name, relativeTime(c.Now(), cm.Commit.Author.Date)).FontSize(12).TextColor(t.TextMuted)
			ui.Spacer(c)
			ui.Textf(c, "commit %s", cm.SHA[:min(7, len(cm.SHA))]).Font(mono).FontSize(12).TextColor(refColor).Selectable()
		})
	})
}

// prCommitPicker chooses what the Code tab shows: the changes of the whole
// pull request, or those of one of its commits.
func (tab *repoTab) prCommitPicker(c *ui.Context, pv *prView) {
	t := c.Theme()
	label := "All changes"
	rows := []menuRow{{icon: iconPR, label: "All changes", hint: plural(len(pv.commits), "commit", "commits"), run: func() { tab.showPRCommit(pv, "") }}}
	var commits []menuRow
	for _, cm := range pv.commits {
		short := cm.SHA[:min(7, len(cm.SHA))]
		if cm.SHA == pv.commitSel {
			label = short + " " + cm.Subject()
		}
		commits = append(commits, menuRow{icon: iconCommit, label: short + " " + cm.Subject(), run: func() { tab.showPRCommit(pv, cm.SHA) }})
	}
	btn := ui.ButtonBase(c).Key("commit-picker").Height(28).Padding(0, 8).Gap(6).Radius(7).AlignItems(ui.Center).MaxWidth(320).Shrink(1).MinWidth(0).
		Label("Changes shown: " + label).Tooltip("Show the changes of one commit")
	if btn.Hovered() {
		btn.Background(hoverBg)
	}
	if btn.Clicked() {
		pv.commitMenu = !pv.commitMenu
	}
	btn.Children(func() {
		ui.Icon(c, iconCommit).FontSize(14).TextColor(t.TextMuted).Shrink(0)
		ui.Text(c, label).FontSize(12.5).SingleLine().MinWidth(0).Shrink(1)
		ui.Icon(c, iconChevronDown).FontSize(12).TextColor(t.TextMuted).Shrink(0)
	})
	popMenu(c, btn, &pv.commitMenu, rows, commits)
}

// ---- dialogs ----

// reviewPRDialog finishes a review of the pull request shown: a comment, and
// whether it approves or asks for changes.
// editPRDialog changes the title and the description of the pull request.
func (a *app) editPRDialog(tab *repoTab) {
	pv := tab.pr
	if pv == nil || tab.gh == nil {
		return
	}
	repo, n := *tab.gh, pv.number
	body := area("body", "Description", "What does this change, and why?", pv.pr.Body)
	body.lines, body.preview = 12, true
	a.showDialog(&dialog{
		width:   720,
		stacked: true,
		title:   "Edit pull request",
		message: fmt.Sprintf("Change the title and the description of #%d", n),
		ok:      "Save",
		fields:  []*field{text("title", "Title:", "", pv.pr.Title), body},
		validate: func(d *dialog) string {
			if d.str("title") == "" {
				return "Enter a title"
			}
			return ""
		},
		run: func(d *dialog) error {
			ctx := context.Background()
			cl, err := a.client(ctx, repo.Host)
			if err != nil {
				return err
			}
			return cl.EditPR(ctx, repo, n, d.str("title"), d.raw("body"))
		},
		done: func() {
			tab.prsLoaded = false
			tab.reloadPRView(pv)
		},
	})
}

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
			}
			tab.prsLoaded = false
			tab.reloadPR()
			a.say("Review submitted on #%d", n)
		},
	})
}

// mergeMethods are the ways to merge a pull request, as GitHub says them.
var mergeMethods = []string{"Create a merge commit", "Squash and merge", "Rebase and merge"}

// mergeAPIMethod is the method the API calls a way to merge.
func mergeAPIMethod(method string) string {
	return map[string]string{"Create a merge commit": "merge", "Squash and merge": "squash", "Rebase and merge": "rebase"}[method]
}

// mergeLabel is what the merge button says for a way to merge: GitHub calls the
// plain merge "Merge pull request" there.
func mergeLabel(method string) string {
	if method == mergeMethods[0] {
		return "Merge pull request"
	}
	return method
}

// The ways to fill the description of a squash commit, as GitHub's repository
// setting words them.
var squashMessageOptions = []string{"Default message", "Pull request title", "Pull request title and description"}

func squashMessageLabel(v string) string {
	switch v {
	case "title":
		return squashMessageOptions[1]
	case "title_description":
		return squashMessageOptions[2]
	}
	return squashMessageOptions[0]
}

func squashMessageValue(label string) string {
	switch label {
	case squashMessageOptions[1]:
		return "title"
	case squashMessageOptions[2]:
		return "title_description"
	}
	return ""
}

// squashBody is the description a squash commit starts with: by default the
// message of each commit, as GitHub lists them.
func squashBody(setting string, pv *prView) string {
	switch setting {
	case "title":
		return ""
	case "title_description":
		return pv.pr.Body
	}
	var parts []string
	for _, cm := range pv.commits {
		subject, body, _ := strings.Cut(cm.Commit.Message, "\n")
		part := "* " + strings.TrimSpace(subject)
		if body = strings.TrimSpace(body); body != "" {
			part += "\n\n" + body
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "\n\n")
}

// mergeBody is the description a merge commit starts with: by default the
// title of the pull request, as GitHub says it.
func mergeBody(setting string, pv *prView) string {
	switch setting {
	case "title":
		return ""
	case "title_description":
		return pv.pr.Body
	}
	return pv.pr.Title
}

// lastMergeMethod is the way the last pull request was merged, which the merge
// button offers again, as GitHub does.
func (a *app) lastMergeMethod() string {
	api := a.store.settings().MergeMethod
	for _, m := range mergeMethods {
		if mergeAPIMethod(m) == api {
			return m
		}
	}
	return mergeMethods[0]
}

// mergePRDialog confirms the merge of the pull request shown by a way to
// merge, and remembers the way for the next one.
func (a *app) mergePRDialog(tab *repoTab, method string) {
	pv := tab.pr
	if pv == nil || tab.gh == nil {
		return
	}
	api := mergeAPIMethod(method)
	a.store.update(func(s *Settings) { s.MergeMethod = api })
	// GitHub refuses the plain merge of a pull request in a stack, even the
	// first: the stack is merged up to it instead.
	if pv.inStack() {
		a.mergeStackDialog(tab, pv, method)
		return
	}
	repo, n := *tab.gh, pv.number
	d := &dialog{
		width:   640,
		stacked: true,
		title:   fmt.Sprintf("Merge pull request #%d", n),
		message: pv.pr.Title,
		ok:      "Confirm " + strings.ToLower(mergeLabel(method)[:1]) + mergeLabel(method)[1:],
		run: func(d *dialog) error {
			ctx := context.Background()
			cl, err := a.client(ctx, repo.Host)
			if err != nil {
				return err
			}
			title, message := "", ""
			if api != "rebase" {
				title, message = d.str("title"), strings.TrimSpace(d.field("message").text)
			}
			return cl.Merge(ctx, repo, n, api, title, message)
		},
		done: func() {
			tab.prsLoaded = false
			tab.reloadPR()
			a.say("Merged #%d", n)
		},
	}
	if method == mergeMethods[0] {
		d.ok = "Confirm merge"
	}
	if api == "rebase" {
		d.message = "The commits of " + pv.pr.Title + " will be rebased and added to the base branch."
	} else {
		set := a.store.settings()
		body := ""
		if api == "squash" {
			body = squashBody(set.SquashMessage, pv)
		}
		title := fmt.Sprintf("%s (#%d)", pv.pr.Title, n)
		if api == "merge" {
			body = mergeBody(set.MergeMessage, pv)
			if set.MergeMessage == "" {
				from := pv.pr.Head.Ref
				if r := pv.pr.Head.Repo; r != nil {
					from = r.FullName + "/" + from
				}
				title = fmt.Sprintf("Merge pull request #%d from %s", n, from)
			}
		}
		d.fields = []*field{
			text("title", "Commit message", "", title),
			area("message", "Extended description", "Add an optional extended description…", body),
		}
	}
	a.showDialog(d)
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
	// The pull requests come as read, even late: a tab can open the dialog
	// without having shown them once, as a worktree's does, so ask for them
	// now, and the stack fields read them as they draw, so they turn up when
	// the list does.
	if !tab.prsLoaded && !tab.prsLoading {
		tab.loadPRs()
	}
	// below is the open pull requests of this repository that a new one can be
	// stacked on, by the branch they are of; read as drawn, not kept here.
	below := func() map[string]github.PR {
		out := map[string]github.PR{}
		for _, p := range tab.prs {
			if p.State == "open" && !p.Merged && (p.Head.Repo == nil || strings.EqualFold(p.Head.Repo.FullName, repo.String())) {
				out[p.Head.Ref] = p
			}
		}
		return out
	}
	// builtOn is the pull request a branch is built on, by branch: the open one
	// whose head is the nearest below it. Found once for each branch asked,
	// again when the pull requests it reads arrive.
	builtOn := map[string]struct {
		p      *github.PR
		loaded bool // the tab's pull requests were loaded when it was read
	}{}
	builtOnFor := func(head string) *github.PR {
		if c, ok := builtOn[head]; ok && (c.loaded || !tab.prsLoaded) {
			return c.p
		}
		p := tab.prBuiltOn(head)
		builtOn[head] = struct {
			p      *github.PR
			loaded bool
		}{p, tab.prsLoaded}
		return p
	}
	// target is the pull request the new one would be stacked on: the open one
	// of the branch it is into.
	target := func(d *dialog) (github.PR, bool) {
		if tab.stacksOff {
			return github.PR{}, false
		}
		p, ok := below()[strings.TrimSpace(d.field("base").text)]
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
