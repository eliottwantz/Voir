package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"github.com/eliott/myfork/internal/github"
)

var (
	white           = ui.Hex("#ffffff")
	mergeGreen      = ui.Hex("#1f883d")
	mergeGreenHover = ui.Hex("#1a7f37")
)

// roundBadge is a round icon in a color, which says at a glance how a
// review, the checks or the merge stand.
func roundBadge(c *ui.Context, svg *ui.SVG, col ui.Color, size float32) {
	ui.Row(c).Size(size, size).Radius(size / 2).Background(col).Center().Shrink(0).TextColor(white).Children(func() {
		ui.Icon(c, svg).FontSize(size * 0.55)
	})
}

// reviewBadge is the badge of a review that approved or asked for changes,
// and reports whether it was one.
func reviewBadge(c *ui.Context, state string, size float32) bool {
	t := c.Theme()
	switch state {
	case "APPROVED":
		roundBadge(c, iconCheck, mergeGreen, size)
	case "CHANGES_REQUESTED":
		roundBadge(c, iconFileDiff, t.Danger, size)
	default:
		return false
	}
	return true
}

// reviewVerdict reads the last approval or request for changes of each
// reviewer: how many approve, and how many ask for changes.
func (pv *prView) reviewVerdict() (approvals, changes int) {
	last := map[string]string{}
	for _, cm := range pv.convo {
		switch cm.State {
		case "APPROVED", "CHANGES_REQUESTED":
			last[cm.User.Login] = cm.State
		case "DISMISSED":
			delete(last, cm.User.Login)
		}
	}
	for _, s := range last {
		if s == "APPROVED" {
			approvals++
		} else {
			changes++
		}
	}
	return
}

// reviewer is someone involved in the review of a pull request, and where
// they stand: State is APPROVED or CHANGES_REQUESTED after their last
// verdict, or empty if none stands.
type reviewer struct {
	User  github.User
	State string
}

// reviewers lists the people who reviewed, in the order of their first
// review, then the ones asked to review who have not. GitHub drops a reviewer
// from the requested ones once they review, so both are needed.
func (pv *prView) reviewers() []reviewer {
	var out []reviewer
	at := map[string]int{}
	add := func(u github.User) int {
		i, ok := at[u.Login]
		if !ok {
			i = len(out)
			at[u.Login] = i
			out = append(out, reviewer{User: u})
		}
		return i
	}
	for _, cm := range pv.convo {
		if cm.State == "" || cm.User.Login == "" || cm.User.Login == pv.pr.User.Login {
			continue
		}
		i := add(cm.User)
		switch cm.State {
		case "APPROVED", "CHANGES_REQUESTED":
			out[i].State = cm.State
		case "DISMISSED":
			out[i].State = ""
		}
	}
	for _, u := range pv.pr.RequestedReviewers {
		add(u)
	}
	return out
}

// mergeReady reports whether GitHub says the pull request can be merged
// now: no conflicts, and nothing that blocks it.
func (pv *prView) mergeReady() bool {
	pr := pv.pr
	if pr.State != "open" || pr.Merged || pr.Draft || pr.Mergeable == nil || !*pr.Mergeable {
		return false
	}
	switch pr.MergeableState {
	case "clean", "has_hooks":
		return !pv.inStack() || pv.stackMergeIssue() == ""
	}
	return false
}

// prMergeStatus is the box that says if the pull request can be merged: the
// reviews, the checks and the branch, each with a big round icon.
func (tab *repoTab) prMergeStatus(c *ui.Context, pv *prView) {
	pr := pv.pr
	if pr.State != "open" || pr.Merged {
		return
	}
	t := c.Theme()
	row := func(key string, svg *ui.SVG, col ui.Color, title, sub string) {
		ui.Row(c).Key(key).FillWidth().Gap(14).Padding(12, 16).AlignItems(ui.Center).Children(func() {
			roundBadge(c, svg, col, 36)
			ui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, title).FontSize(14.5).FontWeight(600)
				if sub != "" {
					ui.Text(c, sub).FontSize(12.5).TextColor(t.TextMuted)
				}
			})
		})
	}
	line := func(key string) { ui.Box(c).Key(key).Height(1).Background(hairline).FillWidth().Shrink(0) }
	approvals, changes := pv.reviewVerdict()
	ready := pv.mergeReady()

	ui.Column(c).Key("merge-status").FillWidth().Radius(8).Border(1, hairline).Children(func() {
		switch {
		case changes > 0:
			row("review", iconFileDiff, t.Danger, "Changes requested", plural(changes, "reviewer has", "reviewers have")+" requested changes")
			line("l-review")
		case approvals > 0:
			row("review", iconCheck, mergeGreen, "Changes approved", plural(approvals, "approving review", "approving reviews"))
			line("l-review")
		case pr.MergeableState == "blocked" && !pr.Draft:
			row("review", iconClose, t.Danger, "Review required", "An approving review may be required before merging.")
			line("l-review")
		}
		switch pv.summary {
		case "success":
			row("checks", iconCheck, mergeGreen, "All checks have passed", plural(len(pv.checks), "successful check", "successful checks"))
			line("l-checks")
		case "failure":
			failed := 0
			for _, ch := range pv.checks {
				if ch.State == "failure" {
					failed++
				}
			}
			row("checks", iconClose, t.Danger, "Some checks were not successful", fmt.Sprintf("%d failing, %d in all", failed, len(pv.checks)))
			line("l-checks")
		case "pending":
			row("checks", iconRefresh, t.Warning, "Checks are running", "Merging may wait for them.")
			line("l-checks")
		}
		if n := pv.unresolved(); n > 0 {
			row("threads", iconComment, t.Warning, plural(n, "unresolved conversation", "unresolved conversations"), "Resolve them in the comments below, or in the Code tab.")
			line("l-threads")
		}
		switch {
		case pr.Draft:
			row("merge", iconDraft, muted, "This pull request is still a draft", "Mark it ready for review to merge it.")
		case pr.Mergeable != nil && !*pr.Mergeable:
			row("merge", iconWarning, t.Danger, "This branch has conflicts that must be resolved", "Conflicts with "+pr.Base.Ref+".")
		case pr.MergeableState == "behind":
			row("merge", iconWarning, t.Warning, "This branch is out of date", "It is behind "+pr.Base.Ref+".")
		case pr.MergeableState == "blocked":
			row("merge", iconWarning, t.Danger, "Merging is blocked", "Required reviews or checks have not passed.")
		case ready:
			row("merge", iconMerge, mergeGreen, "Ready to merge", "This branch has no conflicts with "+pr.Base.Ref+".")
		case pr.Mergeable != nil:
			row("merge", iconMerge, t.Warning, "Merging may be blocked", "GitHub reports "+pr.MergeableState+".")
		default:
			row("merge", iconRefresh, muted, "Checking if it can be merged", "")
		}
	})
}

// mergeButton is the button that merges, as GitHub draws it: green when the
// pull request can be merged, with the other ways to merge in its menu.
func (tab *repoTab) mergeButton(c *ui.Context, pv *prView, idle bool) {
	a := tab.app
	pr := pv.pr
	blocked, label := "", "Merge pull request"
	stack := pv.inStack()
	if stack {
		blocked, label = pv.stackMergeIssue(), "Merge stack"
	}
	off := !idle || pr.Draft || blocked != "" || pr.Mergeable != nil && !*pr.Mergeable
	ready := pv.mergeReady()
	bg, hover, fg := ui.RGBA(127, 127, 127, 0.12), ui.RGBA(127, 127, 127, 0.22), c.Theme().Text
	if ready && !off {
		bg, hover, fg = mergeGreen, mergeGreenHover, white
	}
	tip := ""
	switch {
	case blocked != "":
		tip = blocked
	case pr.Draft:
		tip = "A draft cannot be merged: mark it ready for review first"
	case pr.Mergeable != nil && !*pr.Mergeable:
		tip = "This pull request has conflicts with " + pr.Base.Ref
	case !ready:
		tip = "Merging is blocked or not checked yet: GitHub may still refuse it"
	}
	part := func(key string, radii [4]float32) ui.Element {
		b := ui.ButtonBase(c).Key(key).Height(32).Padding(0, 14).Gap(6).AlignItems(ui.Center).Disabled(off).Background(bg).TextColor(fg).Label(label)
		b.Radius(radii[0], radii[1], radii[2], radii[3])
		if off {
			b.Opacity(0.5)
		} else if b.Hovered() {
			b.Background(hover)
		}
		if tip != "" {
			b.Tooltip(tip)
		}
		return b
	}
	ui.Row(c).Key("merge-button").Gap(1).Shrink(0).Children(func() {
		round := float32(7)
		first := [4]float32{round, 0, 0, round}
		if stack {
			first = [4]float32{round, round, round, round}
		}
		m := part("merge-main", first)
		m.Children(func() { ui.Text(c, label).FontSize(13).FontWeight(600) })
		if m.Clicked() {
			a.mergePRDialog(tab, mergeMethods[0])
		}
		if stack {
			return
		}
		arrow := part("merge-more", [4]float32{0, round, round, 0}).Padding(0, 8).Label("Merge options")
		arrow.Children(func() { ui.Icon(c, iconChevronDown).FontSize(14) })
		if arrow.Clicked() {
			a.prMergeMenu = !a.prMergeMenu
		}
		rows := make([]menuRow, len(mergeMethods))
		for i, m := range mergeMethods {
			rows[i] = menuRow{icon: iconMerge, label: m, run: func() { a.mergePRDialog(tab, m) }}
		}
		popMenu(c, arrow, &a.prMergeMenu, rows)
	})
}
