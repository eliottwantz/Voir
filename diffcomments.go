package main

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/eliott/myfork/internal/github"
)

// commentRow is a comment being written, under the line it is on.
func (tab *repoTab) commentRow(c *ui.Context, rv *review, cm *comment) {
	t := c.Theme()
	pal := diffPaletteFor(t)
	pv := tab.pr
	who := "You"
	if pv != nil && pv.viewer != "" {
		who = pv.viewer
	}
	cardRow(c, pal).Padding(8, 16).Gap(10).AlignItems(ui.Start).Children(func() {
		ui.Avatar(c, who, nil).Size(28, 28).Shrink(0)
		box := ui.Column(c).Grow(1).MinWidth(0).Radius(12).Border(1, pal.cardBorder).Background(pal.headerBg).Clip()
		box.Children(func() {
			ui.Row(c).Height(32).Padding(0, 4, 0, 10).Gap(8).BorderWidth(0, 0, 1, 0).BorderColor(pal.cardBorder).Children(func() {
				ui.Text(c, who).FontSize(12).Bold()
				ui.Text(c, cm.label()).FontSize(11).TextColor(t.TextMuted)
				ui.Spacer(c)
				if iconButton(c, iconClose, "Delete comment").Size(24, 24).Clicked() {
					rv.deleteComment(cm)
				}
			})
			area := ui.TextAreaBase(c, &cm.text).Placeholder("Write a review comment…").MinHeight(64).Padding(8, 10).
				FontSize(13).Label("Review comment")
			if cm.focus {
				area.Focus()
				cm.focus = false
			}
			if area.Shortcut(ui.Cmd, ui.KeyEnter) {
				tab.postPRComment(cm, true)
			}
			if area.Shortcut(0, ui.KeyEscape) && !cm.pending() {
				rv.deleteComment(cm)
			}
			focused := area.Focused()
			if cm.wasFocused && !focused && !cm.pending() && !cm.posting {
				// An empty comment the user left goes.
				rv.deleteComment(cm)
			}
			cm.wasFocused = focused
			if focused {
				box.Border(1, t.Accent.Alpha(0.6))
			}
			ui.Row(c).Gap(8).Padding(8, 10).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(pal.cardBorder).Children(func() {
				if cm.postErr != "" {
					ui.Text(c, cm.postErr).FontSize(12).TextColor(t.Danger).Grow(1).Shrink(1).MinWidth(0).MaxLines(3).Selectable()
				} else {
					ui.Text(c, "⌘↩ adds it to your review").FontSize(11).TextColor(t.TextMuted).Grow(1).Shrink(1).MinWidth(0)
				}
				can := cm.pending() && !cm.posting
				if ui.Button(c, "Add Single Comment").Disabled(!can).Clicked() {
					tab.postPRComment(cm, false)
				}
				label := "Start a Review"
				if pv != nil && pv.reviews != nil && pv.reviews.Pending != nil {
					label = "Add Review Comment"
				}
				if cm.posting {
					label = "Posting…"
				}
				if ui.PrimaryButton(c, label).Disabled(!can).Clicked() {
					tab.postPRComment(cm, true)
				}
			})
		})
	})
}

// threadLabel says where a thread is: "New line 12", "Old lines 3-5".
func threadLabel(th *github.Thread) string {
	cm := comment{side: sideNew, line: th.Line, start: th.StartLine}
	if th.Side == github.Left {
		cm.side = sideOld
	}
	if th.Outdated {
		cm.line, cm.start = th.OriginalLine, 0
	}
	return cm.label()
}

// threadRow is a review thread of the pull request, under its line.
func (tab *repoTab) threadRow(c *ui.Context, rv *review, th *github.Thread) {
	t := c.Theme()
	pal := diffPaletteFor(t)
	now := c.Now()
	cardRow(c, pal).Padding(8, 16).Children(func() {
		box := ui.Column(c).Grow(1).MinWidth(0).Radius(12).Border(1, pal.cardBorder).Background(pal.headerBg).Clip()
		if th.Pending {
			box.Border(1, refColor.Alpha(0.45))
		}
		box.Children(func() {
			ui.Row(c).Height(34).Padding(0, 4, 0, 10).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(pal.cardBorder).Children(func() {
				ui.Icon(c, iconComment).FontSize(13).TextColor(t.TextMuted)
				ui.Text(c, threadLabel(th)).FontSize(11).FontWeight(600).TextColor(t.TextMuted).Shrink(0)
				badge := func(text string, color ui.Color) {
					ui.Text(c, text).FontSize(10).FontWeight(700).Padding(1, 6).Radius(8).Background(color.Alpha(0.14)).TextColor(color).Shrink(0)
				}
				if th.Pending {
					badge("Pending", refColor)
				}
				if th.Outdated {
					badge("Outdated", t.TextMuted)
				}
				ui.Spacer(c)
				if len(th.Comments) > 0 && th.Comments[0].URL != "" {
					if iconButton(c, iconOpen, "Open on GitHub").Size(24, 24).Clicked() {
						openInBrowser(th.Comments[0].URL)
					}
				}
			})
			for _, cm := range th.Comments {
				ui.Row(c.Key(cm.ID)).Gap(10).Padding(10, 12).AlignItems(ui.Start).Children(func() {
					ui.Avatar(c, cm.Author.Login, nil).Size(24, 24).Shrink(0)
					ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
						ui.Row(c).Gap(6).Children(func() {
							ui.Text(c, cm.Author.Login).FontSize(12).Bold()
							ui.Text(c, relativeTime(now, cm.CreatedAt)).FontSize(11).TextColor(t.TextMuted).
								Tooltip(cm.CreatedAt.Local().Format("Mon Jan 2 15:04 2006"))
						})
						markdown.View(c, cm.Body, &markdown.Options{OnLink: openInBrowser}).FillWidth()
					})
				})
			}
		})
	})
}

// postPRComment posts a review comment of the pull request shown: alone, or
// into the viewer's pending review.
func (t *repoTab) postPRComment(cm *comment, review bool) {
	pv := t.pr
	if pv == nil || pv.rv == nil || t.gh == nil || !pv.canComment() || !cm.pending() || cm.posting {
		return
	}
	rv := pv.rv
	nc := github.NewComment{Body: strings.TrimSpace(cm.text), CommitID: pv.pr.Head.SHA, Path: cm.path, Side: github.Right, Line: cm.line}
	if cm.side == sideOld {
		nc.Side = github.Left
	}
	if cm.start != 0 && cm.start != cm.line {
		nc.StartLine, nc.Line = min(cm.start, cm.line), max(cm.start, cm.line)
	}
	var pending *github.PendingReview
	if pv.reviews != nil {
		pending = pv.reviews.Pending
	}
	repo, n, a := *t.gh, pv.number, t.app
	cm.posting, cm.postErr = true, ""
	a.background(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cl, err := a.client(ctx, repo.Host)
		if err == nil {
			if review {
				err = cl.AddToReview(ctx, repo, n, pending, nc)
			} else {
				err = cl.Comment(ctx, repo, n, nc)
			}
		}
		var reviews *github.Reviews
		if err == nil {
			reviews, _ = cl.Reviews(ctx, repo, n)
		}
		a.update(func() {
			cm.posting = false
			if err != nil {
				cm.postErr = errorText(err)
				return
			}
			rv.deleteComment(cm)
			if t.pr == pv && reviews != nil {
				pv.reviews = reviews
				rv.threads = reviews.Threads
			}
			rv.dirty = true
		})
	})
}
