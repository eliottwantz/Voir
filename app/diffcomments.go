package app

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
	who, login := "You", ""
	if pv != nil && pv.viewer != "" {
		who, login = pv.viewer, pv.viewer
	}
	cardRow(c, pal).Padding(8, 16).Gap(10).AlignItems(ui.Start).Children(func() {
		ui.Avatar(c, who, tab.app.avatars.image(login, "")).Size(28, 28).Shrink(0)
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

// threadRow is a review thread of the pull request, under its line: its
// comments, and what can be done to them.
func (tab *repoTab) threadRow(c *ui.Context, rv *review, th *github.Thread) {
	t := c.Theme()
	pal := diffPaletteFor(t)
	pv := tab.pr
	can := pv.canComment()
	folded := (th.Resolved || th.Outdated) && !rv.opened[th.RootID]
	cardRow(c, pal).Padding(8, 16).Children(func() {
		box := ui.Column(c).Grow(1).MinWidth(0).Radius(12).Border(1, pal.cardBorder).Background(pal.headerBg).Clip()
		if th.Pending {
			box.Border(1, refColor.Alpha(0.45))
		}
		box.Children(func() {
			head := ui.Row(c).Height(34).Padding(0, 4, 0, 10).Gap(8).AlignItems(ui.Center)
			if !folded {
				head.BorderWidth(0, 0, 1, 0).BorderColor(pal.cardBorder)
			}
			head.Children(func() {
				ui.Icon(c, iconComment).FontSize(13).TextColor(t.TextMuted)
				ui.Text(c, threadLabel(th)).FontSize(11).FontWeight(600).TextColor(t.TextMuted).Shrink(0)
				badge := func(text string, color ui.Color) {
					ui.Text(c, text).FontSize(10).FontWeight(700).Padding(1, 6).Radius(8).Background(color.Alpha(0.14)).TextColor(color).Shrink(0)
				}
				if th.Pending {
					badge("Pending", refColor)
				}
				if th.Resolved {
					badge("Resolved", t.Success)
				}
				if th.Outdated {
					badge("Outdated", t.TextMuted)
				}
				if folded && len(th.Comments) > 0 {
					first := th.Comments[0]
					ui.RichText(c,
						ui.Span{Text: first.Author.Login + " ", Weight: 600},
						ui.Span{Text: firstLine(first.Body), Color: t.TextMuted},
					).FontSize(12).SingleLine().Grow(1).Shrink(1).MinWidth(0)
				} else {
					ui.Spacer(c)
				}
				if th.Resolved || th.Outdated {
					label := "Show"
					if !folded {
						label = "Hide"
					}
					b := ui.ButtonBase(c).Padding(3, 8).Radius(6).Label(label + " thread").Children(func() {
						ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(t.Accent)
					})
					if b.Clicked() {
						rv.opened[th.RootID] = folded
					}
				}
				if can && th.NodeID != "" && !th.Pending {
					label := "Resolve"
					if th.Resolved {
						label = "Unresolve"
					}
					if ui.Button(c, label).Height(24).Disabled(rv.busy > 0).Clicked() {
						resolved, node := !th.Resolved, th.NodeID
						tab.writePR("Resolve thread", func(ctx context.Context, cl *github.Client, _ github.Repo, _ int) error {
							return cl.Resolve(ctx, node, resolved)
						}, nil)
					}
				}
				if len(th.Comments) > 0 && th.Comments[0].URL != "" {
					if iconButton(c, iconOpen, "Open on GitHub").Size(24, 24).Clicked() {
						openInBrowser(th.Comments[0].URL)
					}
				}
			})
			if folded {
				return
			}
			for _, cm := range th.Comments {
				tab.threadComment(c, rv, cm)
			}
			if can && !th.Pending {
				tab.replyBox(c, rv, th)
			}
		})
	})
}

// threadComment is a comment of a thread: the viewer's own can be edited and
// deleted.
func (tab *repoTab) threadComment(c *ui.Context, rv *review, cm *github.ThreadComment) {
	t := c.Theme()
	pal := diffPaletteFor(t)
	pv := tab.pr
	now := c.Now()
	own := pv.canComment() && cm.Author.Login == pv.viewer
	ui.Row(c.Key(cm.ID)).Gap(10).Padding(10, 12).AlignItems(ui.Start).Children(func() {
		ui.Avatar(c, cm.Author.Login, tab.app.avatars.image(cm.Author.Login, cm.Author.AvatarURL)).Size(24, 24).Shrink(0)
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, cm.Author.Login).FontSize(12).Bold()
				ui.Text(c, relativeTime(now, cm.CreatedAt)).FontSize(11).TextColor(t.TextMuted).
					Tooltip(cm.CreatedAt.Local().Format("Mon Jan 2 15:04 2006"))
				ui.Spacer(c)
				if own && rv.editing != cm.ID {
					if iconButton(c, iconPencil, "Edit comment").Size(22, 22).Clicked() {
						rv.editing, rv.editText = cm.ID, cm.Body
					}
					if iconButton(c, iconTrash, "Delete comment").Size(22, 22).Disabled(rv.busy > 0).Clicked() {
						tab.deleteThreadComment(cm.ID)
					}
				}
			})
			if rv.editing != cm.ID {
				markdown.View(c, cm.Body, &markdown.Options{OnLink: openInBrowser}).FillWidth()
				return
			}
			area := ui.TextAreaBase(c, &rv.editText).MinHeight(60).Padding(8, 10).FontSize(13).Radius(8).
				Background(pal.codeBg).Border(1, t.Accent.Alpha(0.5)).Label("Edit comment").AutoFocus()
			save := func() {
				id, body := cm.ID, strings.TrimSpace(rv.editText)
				if body == "" || rv.busy > 0 {
					return
				}
				tab.writePR("Edit comment", func(ctx context.Context, cl *github.Client, repo github.Repo, _ int) error {
					return cl.EditComment(ctx, repo, id, body)
				}, func(err error) {
					if err == nil {
						rv.editing = 0
					}
				})
			}
			if area.Shortcut(ui.Cmd, ui.KeyEnter) {
				save()
			}
			if area.Shortcut(0, ui.KeyEscape) {
				rv.editing = 0
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					rv.editing = 0
				}
				if ui.PrimaryButton(c, "Save").Disabled(rv.busy > 0 || strings.TrimSpace(rv.editText) == "").Clicked() {
					save()
				}
			})
		})
	})
}

// replyBox is the field answering a thread.
func (tab *repoTab) replyBox(c *ui.Context, rv *review, th *github.Thread) {
	t := c.Theme()
	pal := diffPaletteFor(t)
	text := rv.replies[th.RootID]
	if text == nil {
		text = new(string)
		rv.replies[th.RootID] = text
	}
	ui.Column(c).Gap(8).Padding(8, 12, 10).BorderWidth(1, 0, 0, 0).BorderColor(pal.cardBorder).Children(func() {
		area := ui.TextAreaBase(c, text).Placeholder("Reply…").MinHeight(34).Padding(7, 10).FontSize(13).Radius(8).
			Background(pal.codeBg).Border(1, pal.cardBorder).Label("Reply to the thread")
		if area.Focused() {
			area.Border(1, t.Accent.Alpha(0.6))
		}
		send := func() {
			body := strings.TrimSpace(*text)
			if body == "" || rv.busy > 0 {
				return
			}
			root := th.RootID
			tab.writePR("Reply", func(ctx context.Context, cl *github.Client, repo github.Repo, n int) error {
				return cl.Reply(ctx, repo, n, root, body)
			}, func(err error) {
				if err == nil {
					*text = ""
				}
			})
		}
		if area.Shortcut(ui.Cmd, ui.KeyEnter) {
			send()
		}
		if strings.TrimSpace(*text) != "" {
			ui.Row(c).Gap(8).Justify(ui.End).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "⌘↩ replies").FontSize(11).TextColor(t.TextMuted)
				if ui.PrimaryButton(c, "Reply").Disabled(rv.busy > 0).Clicked() {
					send()
				}
			})
		}
	})
}

// deleteThreadComment deletes a comment of the viewer's, once they agree.
func (tab *repoTab) deleteThreadComment(id int64) {
	pv := tab.pr
	if pv == nil || tab.gh == nil {
		return
	}
	repo, a := *tab.gh, tab.app
	a.confirm("Delete this comment?", "It is deleted on GitHub, for everyone.", "Delete", func() error {
		ctx := context.Background()
		cl, err := a.client(ctx, repo.Host)
		if err != nil {
			return err
		}
		return cl.DeleteComment(ctx, repo, id)
	}, func() { tab.reloadReviews(pv) })
}

// reloadReviews reads the review threads again, off the main thread.
func (t *repoTab) reloadReviews(pv *prView) {
	if t.gh == nil || pv.rv == nil {
		return
	}
	repo, n, a := *t.gh, pv.number, t.app
	a.background(func() {
		ctx := context.Background()
		var reviews *github.Reviews
		if cl, err := a.client(ctx, repo.Host); err == nil {
			reviews, _ = cl.Reviews(ctx, repo, n)
		}
		a.update(func() {
			if t.pr == pv && reviews != nil {
				pv.reviews = reviews
				pv.rv.threads = reviews.Threads
				pv.rv.dirty = true
			}
		})
	})
}

// writePR runs a change on the pull request's review comments, then reads the
// threads again. An error shows in a dialog; done is told it.
func (t *repoTab) writePR(title string, fn func(ctx context.Context, cl *github.Client, repo github.Repo, n int) error, done func(err error)) {
	pv := t.pr
	if pv == nil || pv.rv == nil || t.gh == nil {
		return
	}
	rv := pv.rv
	rv.busy++
	repo, n, a := *t.gh, pv.number, t.app
	a.background(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cl, err := a.client(ctx, repo.Host)
		if err == nil {
			err = fn(ctx, cl, repo, n)
		}
		var reviews *github.Reviews
		if err == nil {
			reviews, _ = cl.Reviews(ctx, repo, n)
		}
		a.update(func() {
			rv.busy--
			if err != nil {
				a.fail(title+" failed", err)
			} else if t.pr == pv && reviews != nil {
				pv.reviews = reviews
				rv.threads = reviews.Threads
				rv.dirty = true
			}
			if done != nil {
				done(err)
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
