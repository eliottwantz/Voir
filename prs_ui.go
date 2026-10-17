package main

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/eliott/myfork/internal/git"
	"github.com/eliott/myfork/internal/github"
)

// prSidebar is the Pull Requests section of the sidebar.
func (tab *repoTab) prSidebar(c *ui.Context) {
	if tab.gh == nil {
		return
	}
	if !tab.prsLoaded && !tab.prsLoading {
		tab.loadPRs()
	}
	ui.SidebarSection(c, "Pull Requests", tab.open("prs"), func() {
		switch {
		case tab.prsLoading && len(tab.prs) == 0:
			ui.SidebarItem(c, "pr-loading", iconRefresh, "Loading…")
		case tab.prsErr != "":
			ui.SidebarItem(c, "pr-error", iconWarning, "Could not load: retry").Tooltip(tab.prsErr)
		case len(tab.prs) == 0:
			ui.SidebarItem(c, "pr-none", iconPR, "No open pull request")
		}
		for _, p := range tab.prs {
			tab.prItem(c, p)
		}
	})
}

func (tab *repoTab) prItem(c *ui.Context, p github.PR) {
	t := c.Theme()
	label := fmt.Sprintf("#%d %s", p.Number, p.Title)
	item := ui.SidebarItem(c, fmt.Sprintf("pr:%d", p.Number), iconPR, label).Tooltip(label + " — " + p.User.Login)
	item.Children(func() {
		if p.Draft {
			ui.Text(c, "draft").FontSize(10.5).TextColor(t.TextMuted)
		}
	})
	item.ContextMenu(func(m *ui.Menu) {
		if m.Item("Open").Chosen() {
			tab.openPR(p.Number)
		}
		if m.Item("Check Out").Chosen() {
			tab.checkoutPR(p.Number)
		}
		m.Separator()
		if m.Item("Open in Browser").Chosen() {
			openInBrowser(p.HTMLURL)
		}
		if m.Item("Copy Link").Chosen() {
			copyText(p.HTMLURL)
		}
	})
}

func (tab *repoTab) prSidebarChosen(id string) {
	switch id {
	case "pr-error":
		tab.prsLoaded = false
		tab.side = "history"
		if tab.mode == modePR {
			tab.showHistory()
		}
		return
	case "pr-loading", "pr-none":
		return
	}
	if n, err := strconv.Atoi(strings.TrimPrefix(id, "pr:")); err == nil {
		tab.openPR(n)
	}
}

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

func pill(c *ui.Context, label string, col ui.Color) {
	ui.Text(c, label).FontSize(11.5).FontWeight(600).TextColor(col).Padding(2, 9).Radius(10).Background(col.Alpha(0.15)).Border(1, col.Alpha(0.4)).Shrink(0)
}

// prViewUI is the pull request in the main area.
func (tab *repoTab) prViewUI(c *ui.Context) {
	t := c.Theme()
	pv := tab.pr
	if pv == nil {
		emptyPanel(c, "No pull request chosen", "", nil)
		return
	}
	if pv.err != "" {
		emptyPanel(c, "Could not load pull request #"+strconv.Itoa(pv.number), pv.err, func() {
			if ui.Button(c, "Try Again").Clicked() {
				tab.reloadPR()
			}
		})
		return
	}
	if !pv.loaded && pv.pr.Number == 0 {
		ui.Column(c).Grow(1).Center().Children(func() { ui.Spinner(c).Label("Loading") })
		return
	}
	pr := pv.pr
	now := c.Now()
	idle := pv.busy == ""

	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
		ui.Column(c).Padding(10, 16).Gap(6).Shrink(0).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, pr.Title).FontSize(17).Bold().SingleLine().Shrink(1).MinWidth(0)
				ui.Textf(c, "#%d", pr.Number).FontSize(17).TextColor(t.TextMuted).Shrink(0)
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				pill(c, pr.StateLabel(), prStateColor(c, pr))
				ui.Text(c, prSummary(pr)).FontSize(12.5).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
				if !pr.CreatedAt.IsZero() {
					ui.Text(c, "· "+relativeTime(now, pr.CreatedAt)).FontSize(12.5).TextColor(t.TextMuted).Shrink(0)
				}
				if pv.busy != "" {
					ui.Spinner(c).Label(pv.busy)
				}
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				tab.prStatusPills(c, pv)
				ui.Spacer(c)
				if ui.Button(c, "Check Out").Disabled(tab.busy != "").Clicked() {
					tab.checkoutPR(pv.number)
				}
				switch {
				case pr.Merged:
				case pr.State == "open":
					if pr.Draft {
						if ui.Button(c, "Ready for Review").Disabled(!idle).Clicked() {
							tab.markPRReady()
						}
					}
					review := ui.Button(c, "").Label("Review").Tooltip("Comment, approve or request changes").Disabled(!idle)
					review.Children(func() {
						ui.Icon(c, iconReview).FontSize(14)
						ui.Text(c, "Review")
					})
					if review.Clicked() {
						tab.app.reviewPRDialog(tab)
					}
					merge := ui.PrimaryButton(c, "Merge…").Disabled(!idle || pr.Draft || pr.Mergeable != nil && !*pr.Mergeable)
					switch {
					case pr.Draft:
						merge.Tooltip("A draft cannot be merged: mark it ready for review first")
					case pr.Mergeable != nil && !*pr.Mergeable:
						merge.Tooltip("This pull request has conflicts with " + pr.Base.Ref)
					}
					if merge.Clicked() {
						tab.app.mergePRDialog(tab)
					}
					if ui.Button(c, "Close").Disabled(!idle).Clicked() {
						tab.setPRState("closed")
					}
				default:
					if ui.Button(c, "Reopen").Disabled(!idle).Clicked() {
						tab.setPRState("open")
					}
				}
				if ui.Button(c, "Open in Browser").Clicked() {
					openInBrowser(pr.HTMLURL)
				}
				if ui.ButtonBase(c).Size(28, 28).Radius(6).Center().Label("Refresh pull request").Tooltip("Read it again").Children(func() { ui.Icon(c, iconRefresh).FontSize(14) }).Clicked() {
					tab.reloadPR()
				}
			})
		})
		ui.Row(c).Padding(5, 12).Shrink(0).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
			was := pv.tab
			ui.Segmented(c, &pv.tab, "Conversation", fmt.Sprintf("Commits (%d)", len(pv.commits)), fmt.Sprintf("Files changed (%d)", len(pv.files))).Label("Pull request")
			if pv.tab != was && pv.tab == 2 {
				// The files are in the sidebar, beside their patches.
				tab.app.showSide(sideFiles)
			}
		})
		switch pv.tab {
		case 1:
			tab.prCommits(c, pv)
		case 2:
			tab.prFiles(c, pv)
		default:
			tab.prConversation(c, pv)
		}
	})
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
	if pr.Additions+pr.Deletions > 0 {
		ui.Textf(c, "+%d", pr.Additions).FontSize(12).TextColor(t.Success).Font("SF Mono, Menlo, monospace")
		ui.Textf(c, "−%d", pr.Deletions).FontSize(12).TextColor(t.Danger).Font("SF Mono, Menlo, monospace")
	}
}

func (tab *repoTab) prConversation(c *ui.Context, pv *prView) {
	t := c.Theme()
	now := c.Now()
	pr := pv.pr
	card := func(who github.User, when string, state string, body string) {
		ui.Column(c).FillWidth().Radius(8).Border(1, hairline).Children(func() {
			ui.Row(c).Gap(8).Padding(6, 12).AlignItems(ui.Center).Background(subtleBg).Children(func() {
				ui.Avatar(c, who.Login, nil).FontSize(10)
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
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Padding(14, 16).Gap(12).Children(func() {
			card(pr.User, "opened "+relativeTime(now, pr.CreatedAt), "", pr.Body)
			if len(pv.checks) > 0 {
				ui.Column(c).FillWidth().Radius(8).Border(1, hairline).Children(func() {
					ui.Text(c, "Checks").FontSize(12.5).FontWeight(600).Padding(6, 12).FillWidth().Background(subtleBg)
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
						row := ui.Row(c).Key(fmt.Sprint("check", i)).Gap(8).Padding(3, 12).AlignItems(ui.Center)
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
			for _, cm := range pv.convo {
				card(cm.User, relativeTime(now, cm.CreatedAt), cm.State, cm.Body)
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

func (tab *repoTab) prCommits(c *ui.Context, pv *prView) {
	t := c.Theme()
	now := c.Now()
	if len(pv.commits) == 0 {
		ui.Text(c, "No commits").TextColor(t.TextMuted).Padding(20)
		return
	}
	ui.List(c, nil, len(pv.commits), func(i int) {
		cm := pv.commits[i]
		ui.Row(c).Gap(10).Padding(5, 14).AlignItems(ui.Center).Children(func() {
			ui.Text(c, cm.SHA[:7]).Font("SF Mono, Menlo, monospace").FontSize(11.5).TextColor(t.TextMuted).Width(58)
			ui.Text(c, cm.Subject()).FontSize(13).SingleLine().Grow(1).MinWidth(0)
			ui.Text(c, cm.Commit.Author.Name).FontSize(12).TextColor(t.TextMuted).SingleLine().Width(150)
			ui.Text(c, relativeTime(now, cm.Commit.Author.Date)).FontSize(12).TextColor(t.TextMuted).Width(110)
		})
	}).Grow(1).MinHeight(0).Label("Commits")
}

// prFiles is the patches of the files, as cards in one surface, as a
// commit's; the files are in the sidebar.
func (tab *repoTab) prFiles(c *ui.Context, pv *prView) {
	if pv.rv == nil {
		return
	}
	tab.reviewSurface(c, pv.rv)
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
	a.showDialog(&dialog{
		title:   "Finish Your Review",
		message: fmt.Sprintf("#%d %s", n, pv.pr.Title),
		ok:      "Submit Review",
		fields: []*field{
			area("body", "", "Leave a comment", ""),
			radio("event", "Comment",
				option{"Comment", "Submit general feedback without explicit approval."},
				option{"Approve", "Submit feedback and approve merging these changes."},
				option{"Request changes", "Submit feedback that must be addressed before merging."}),
		},
		validate: func(d *dialog) string {
			if d.str("body") == "" && d.str("event") != "Approve" {
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
			return cl.SubmitReview(ctx, repo, n, events[d.str("event")], d.str("body"))
		},
		done: func() {
			tab.prsLoaded = false
			tab.reloadPR()
			a.say("Review submitted on #%d", n)
		},
	})
}

// mergePRDialog merges the pull request shown.
func (a *app) mergePRDialog(tab *repoTab) {
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
			choice("method", "Method:", "Create a merge commit", "Create a merge commit", "Squash and merge", "Rebase and merge"),
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

// createPRDialog opens a pull request for the branch checked out.
func (a *app) createPRDialog(tab *repoTab, branch string) {
	if tab.gh == nil {
		a.fail("Create pull request", errString("This repository has no remote on GitHub."))
		return
	}
	if branch == "" {
		if b := tab.currentBranch(); b != nil {
			branch = b.Name
		}
	}
	if branch == "" {
		a.fail("Create pull request", errString("HEAD is detached: check out a branch first."))
		return
	}
	var bases []string
	prefix := tab.ghRemote + "/"
	for _, r := range tab.refs {
		if r.Kind == git.RemoteBranch && strings.HasPrefix(r.Name, prefix) {
			bases = append(bases, strings.TrimPrefix(r.Name, prefix))
		}
	}
	slices.SortStableFunc(bases, func(x, y string) int {
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
		return rank(x) - rank(y)
	})
	if len(bases) == 0 {
		bases = []string{"main"}
	}
	var ahead bool
	var local *git.Ref
	for i := range tab.refs {
		if tab.refs[i].Kind == git.LocalBranch && tab.refs[i].Name == branch {
			local = &tab.refs[i]
		}
	}
	ahead = local == nil || local.Upstream == "" || local.Ahead > 0
	title := strings.ReplaceAll(strings.ReplaceAll(branch, "-", " "), "_", " ")
	if i := strings.LastIndex(title, "/"); i >= 0 {
		title = title[i+1:]
	}
	if cs, err := tab.repo.Log(git.LogOptions{Limit: 1}); err == nil && len(cs) == 1 {
		title = cs[0].Subject
	}
	repo, remote := *tab.gh, tab.ghRemote
	var created github.PR
	a.showDialog(&dialog{
		title:   "Create pull request",
		message: fmt.Sprintf("Open a pull request on %s for “%s”", repo, branch),
		ok:      "Create Pull Request",
		fields: []*field{
			text("title", "Title:", "", title),
			{kind: fieldArea, key: "body", label: "Description", placeholder: "What does this change, and why?"},
			choice("base", "Into:", bases[0], bases...),
			check("draft", "Create as a draft", false),
			check("push", "Push “"+branch+"” to "+remote+" first", ahead),
		},
		validate: func(d *dialog) string {
			if d.str("title") == "" {
				return "Enter a title"
			}
			if d.field("base").text == branch {
				return "Choose a base other than the branch itself"
			}
			return ""
		},
		run: func(d *dialog) error {
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
			created, err = cl.Create(ctx, repo, github.NewPR{Title: d.str("title"), Body: d.raw("body"), Head: branch, Base: d.field("base").text, Draft: d.bool("draft")})
			return err
		},
		done: func() {
			tab.prsLoaded = false
			tab.openPR(created.Number)
			a.say("Opened #%d", created.Number)
		},
	})
}
