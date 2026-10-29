package app

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/voir/internal/git"
	"github.com/eliott/voir/internal/github"
)

// entryPR is a pull request of a stack as the pull request the state helpers
// read.
func entryPR(e github.StackEntry) github.PR {
	return github.PR{State: e.State, Draft: e.Draft, Merged: e.Merged()}
}

// stackPos is the index of the pull request in the stack, -1 when it is not
// there.
func (pv *prView) stackPos() int {
	if pv.stack == nil {
		return -1
	}
	return slices.IndexFunc(pv.stack.Entries, func(e github.StackEntry) bool { return e.Number == pv.number })
}

// stackMergeable counts the pull requests that merging the stack up to the
// one shown would merge: those below it, and it, that are still open.
func (pv *prView) stackMergeable() int {
	n := 0
	for i, e := range pv.stack.Entries {
		if i <= pv.stackPos() && e.State == "open" && !e.Merged() {
			n++
		}
	}
	return n
}

// stackOpen are the pull requests of the stack that are still open, from the
// one nearest the base branch.
func (pv *prView) stackOpen() []github.StackEntry {
	return slices.DeleteFunc(slices.Clone(pv.stack.Entries), func(e github.StackEntry) bool { return e.State != "open" || e.Merged() })
}

// stackMergeIssue says why the stack cannot be merged up to the pull request
// shown, "" when it can: GitHub merges all of them or none, so one that is
// closed or a draft stops the lot.
func (pv *prView) stackMergeIssue() string {
	for i, e := range pv.stack.Entries {
		if i > pv.stackPos() {
			break
		}
		switch {
		case e.Merged():
		case e.State != "open":
			return fmt.Sprintf("#%d is closed without being merged", e.Number)
		case e.Draft:
			return fmt.Sprintf("#%d is a draft: mark it ready for review first", e.Number)
		}
	}
	return ""
}

// inStack reports whether the pull request is in a stack that was read: GitHub
// merges those with the stack, not alone.
func (pv *prView) inStack() bool { return pv.stack != nil && pv.stackPos() >= 0 }

// stackLayers are the pull requests of a stack that an action concerns: those
// not merged, up to and including through, or all of them when through is 0.
func stackLayers(s github.Stack, through int) []github.StackEntry {
	var out []github.StackEntry
	for _, e := range s.Entries {
		if !e.Merged() {
			out = append(out, e)
		}
		if through != 0 && e.Number == through {
			break
		}
	}
	return out
}

// stackChanged reports whether the layers an action concerns are not what they
// were when the stack was read: one was added, closed or pushed to.
func stackChanged(was, now github.Stack, through int) bool {
	a, b := stackLayers(was, through), stackLayers(now, through)
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i].Number != b[i].Number || a[i].Head.SHA != b[i].Head.SHA || a[i].State != b[i].State || a[i].Draft != b[i].Draft {
			return true
		}
	}
	return false
}

var errStackChanged = errString("The stack changed since it was read: close this and read it again.")

// stackButton is the pull request's place in its stack, which opens the stack:
// its pull requests, to open each, and what can be done to all of them.
func (tab *repoTab) stackButton(c *ui.Context, pv *prView) {
	t := c.Theme()
	a := tab.app
	pos, size := pv.stackPos()+1, len(pv.stack.Entries)
	b := ui.ButtonBase(c.Key("stack")).Gap(5).Padding(4, 10).Radius(8).Background(subtleBg).Shrink(0).AlignItems(ui.Center).
		Label(fmt.Sprintf("Stack, %d of %d", pos, size)).Tooltip("Stack #" + fmt.Sprint(pv.stack.Number))
	if b.Hovered() {
		b.Background(hoverBg)
	}
	if b.Clicked() {
		a.prStack = !a.prStack
	}
	b.Children(func() {
		ui.Icon(c, iconLayers).FontSize(14).TextColor(t.TextMuted)
		ui.Textf(c, "%d/%d", pos, size).FontSize(13).FontWeight(500)
	})
	tab.stackMenu(c, b, pv)
}

func (tab *repoTab) stackMenu(c *ui.Context, anchor ui.Element, pv *prView) {
	t := c.Theme()
	a := tab.app
	st := pv.stack
	idle := pv.busy == "" && tab.busy == ""
	merging := pv.stackMergeable()
	issue := pv.stackMergeIssue()
	open := pv.stackOpen()
	ui.Popover(c, anchor, &a.prStack, func() {
		ui.Column(c).MinWidth(300).MaxWidth(420).Gap(2).Children(func() {
			ui.Text(c, fmt.Sprintf("Stack #%d", st.Number)).FontSize(13).FontWeight(600).TextColor(t.TextMuted).Padding(6, 10, 4)
			// The top of the stack is first, as it is built.
			for i := len(st.Entries) - 1; i >= 0; i-- {
				e := st.Entries[i]
				pr := entryPR(e)
				row := ui.ButtonBase(c.Key(fmt.Sprint("entry", e.Number))).Padding(6, 10).Gap(10).Radius(7).FillWidth().Justify(ui.Start).AlignItems(ui.Center).
					Label(fmt.Sprintf("#%d %s", e.Number, e.Title))
				if row.Hovered() {
					row.Background(hoverBg)
				}
				if row.Clicked() {
					a.prStack = false
					tab.openPR(e.Number)
				}
				row.Children(func() {
					ui.Icon(c, prStateIcon(pr)).FontSize(16).TextColor(prStateColor(c, pr)).Shrink(0)
					ui.Column(c).Grow(1).MinWidth(0).Children(func() {
						ui.Text(c, e.Title).FontSize(14).SingleLine().MinWidth(0)
						ui.Textf(c, "#%d · %s · %s", e.Number, e.Head.Ref, e.StateLabel()).FontSize(12).TextColor(t.TextMuted).SingleLine().MinWidth(0)
					})
					if e.Number == pv.number {
						ui.Icon(c, iconCheck).FontSize(14).TextColor(t.TextMuted).Shrink(0)
					}
				})
			}
			ui.Row(c).Gap(6).Padding(4, 12, 6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "↳").FontSize(13).TextColor(t.TextMuted)
				ui.Text(c, st.Base.Ref).Font(mono).FontSize(13).TextColor(t.TextMuted).SingleLine()
			})
			ui.Box(c.Key("stack-sep")).Height(1).Background(hairline).Margin(4, 4).Shrink(0)
			merge := menuRow{
				icon: iconMerge, label: fmt.Sprintf("Merge stack (%d)", merging),
				disabled: !idle || merging == 0 || issue != "",
				tooltip:  issue,
				run:      func() { a.mergeStackDialog(tab, pv, mergeMethods[0]) },
			}
			rebase := menuRow{
				icon: iconRefresh, label: "Rebase stack",
				disabled: !idle || len(open) == 0 || tab.ghRemote == "",
				run:      func() { a.rebaseStackDialog(tab, pv) },
			}
			switch {
			case len(open) == 0:
				rebase.tooltip = "Every pull request of the stack is merged or closed"
			case tab.ghRemote == "":
				rebase.tooltip = "The repository has no remote on GitHub"
			}
			menuRowButton(c, &a.prStack, merge)
			menuRowButton(c, &a.prStack, rebase)
			ui.Box(c.Key("stack-sep2")).Height(1).Background(hairline).Margin(4, 4).Shrink(0)
			menuRowButton(c, &a.prStack, menuRow{
				icon: iconClose, label: "Unstack", danger: true, disabled: !idle || len(open) == 0,
				tooltip: "Make the pull requests separate again",
				run:     func() { a.unstackDialog(tab, pv) },
			})
		})
	})
}

// mergeStackDialog merges the stack up to the pull request shown, all of it
// or none.
func (a *app) mergeStackDialog(tab *repoTab, pv *prView, method string) {
	if tab.gh == nil || pv.stack == nil {
		return
	}
	repo, n, was := *tab.gh, pv.number, *pv.stack
	base := was.Base.Ref
	count := pv.stackMergeable()
	queued := false
	a.showDialog(&dialog{
		title:   fmt.Sprintf("Merge stack #%d", was.Number),
		message: fmt.Sprintf("Merges %s into %s up to #%d, all together: if one cannot be merged, none is.", plural(count, "pull request", "pull requests"), base, n),
		ok:      "Merge",
		fields:  []*field{choice("method", "Method:", method, mergeMethods...)},
		run: func(d *dialog) error {
			ctx := context.Background()
			cl, err := a.client(ctx, repo.Host)
			if err != nil {
				return err
			}
			// The stack is read again, and the merge is for the heads that were
			// shown: not for what was pushed since.
			now, err := cl.Stack(ctx, repo, was.Number)
			if err != nil {
				return err
			}
			if stackChanged(was, now, n) {
				return errStackChanged
			}
			sha := ""
			for _, e := range now.Entries {
				if e.Number == n {
					sha = e.Head.SHA
				}
			}
			method := mergeAPIMethod(d.field("method").text)
			res, err := cl.MergeStack(ctx, repo, n, method, sha)
			// GitHub merges in the background: ask until it has.
			deadline := time.Now().Add(5 * time.Minute)
			for attempt := 0; err == nil && res.Status == "pending" && time.Now().Before(deadline); attempt++ {
				if res.Details.UUID == "" {
					return errString("GitHub did not say how to follow the merge: read the pull requests again in a moment")
				}
				time.Sleep(a.mergePoll(attempt))
				res, err = cl.MergeStackStatus(ctx, repo, n, res.Details.UUID)
			}
			switch {
			case err != nil:
				return err
			case res.Status == "failed":
				return errString(cmp.Or(res.Details.Message, "The stack could not be merged"))
			case res.Status == "pending":
				return errString("GitHub is still merging the stack: read the pull requests again in a moment")
			}
			queued = res.Status == "enqueued"
			return nil
		},
		done: func() {
			tab.reloadStackTabs()
			if queued {
				a.say("Added the stack up to #%d to the merge queue", n)
			} else {
				a.say("Merged the stack up to #%d", n)
			}
		},
	})
}

// reloadStackTabs reads the pull request list and the tabs of the pull
// requests again, after something was done to a stack.
func (t *repoTab) reloadStackTabs() {
	t.prsLoaded = false
	for _, v := range t.prTabs {
		t.reloadPRView(v)
	}
}

// rebaseStackDialog rebases the open pull requests of the stack, each onto the
// one before, and pushes them.
func (a *app) rebaseStackDialog(tab *repoTab, pv *prView) {
	if tab.gh == nil || pv.stack == nil || len(pv.stackOpen()) == 0 {
		return
	}
	repo, was := *tab.gh, *pv.stack
	remote, base := tab.ghRemote, was.Base.Ref
	var (
		branches []git.StackBranch
		parent   string
	)
	a.showDialog(&dialog{
		title:   fmt.Sprintf("Rebase stack #%d", was.Number),
		message: fmt.Sprintf("Rebases %s one onto the next, the first onto %s, and pushes those that moved, with a lease on what they were. It works in a copy of the branches off to the side: your checkout and your branches stay as they are. A conflict stops it before anything is pushed.", plural(len(pv.stackOpen()), "pull request", "pull requests"), base),
		ok:      "Rebase",
		run: func(d *dialog) error {
			ctx := context.Background()
			cl, err := a.client(ctx, repo.Host)
			if err != nil {
				return err
			}
			now, err := cl.Stack(ctx, repo, was.Number)
			if err != nil {
				return err
			}
			if stackChanged(was, now, 0) {
				return errStackChanged
			}
			layers := stackLayers(now, 0)
			if len(layers) == 0 {
				return errString("Every pull request of the stack is merged")
			}
			numbers := make([]int, len(layers))
			for i, e := range layers {
				if e.State != "open" {
					return errString(fmt.Sprintf("#%d is closed without being merged", e.Number))
				}
				numbers[i] = e.Number
			}
			// The branches are pushed to: they must be in this repository, and
			// the account must be able to write to it.
			access, err := cl.BranchAccess(ctx, repo, numbers)
			if err != nil {
				return err
			}
			for _, n := range numbers {
				acc := access[n]
				switch {
				case !strings.EqualFold(acc.HeadRepo, repo.String()):
					return errString(fmt.Sprintf("The branch of #%d is not in %s: a stack from a fork cannot be rebased here", n, repo))
				case !acc.CanPush():
					return errString(fmt.Sprintf("You cannot push to the branch of #%d", n))
				}
			}
			branches = branches[:0]
			for _, e := range layers {
				branches = append(branches, git.StackBranch{Name: e.Head.Ref, Head: e.Head.SHA})
			}
			// The first branch was built on the pull request under it, which may
			// be merged by now: only its own commits go onto the base branch.
			parent = ""
			if i := slices.IndexFunc(now.Entries, func(e github.StackEntry) bool { return e.Number == layers[0].Number }); i > 0 {
				parent = now.Entries[i-1].Head.SHA
			}
			return nil
		},
		done: func() {
			var pushed []string
			tab.doThen("Rebase stack", func(r *git.Repo) (err error) {
				pushed, err = r.RebaseStack(context.Background(), remote, base, parent, branches)
				return
			}, func() {
				tab.reloadStackTabs()
				if len(pushed) == 0 {
					a.say("The stack is already up to date")
				} else {
					a.say("Rebased %s", plural(len(pushed), "branch", "branches"))
				}
			})
		},
	})
}

// unstackDialog makes the pull requests of a stack separate again.
func (a *app) unstackDialog(tab *repoTab, pv *prView) {
	if tab.gh == nil || pv.stack == nil {
		return
	}
	repo, number := *tab.gh, pv.stack.Number
	remaining := 0
	a.showDialog(&dialog{
		title:   fmt.Sprintf("Unstack #%d", number),
		message: "The pull requests stay open as separate ones, each into the branch it is now into, and no branch changes. Those queued for merge or with auto-merge on stay stacked.",
		ok:      "Unstack",
		danger:  true,
		run: func(d *dialog) error {
			ctx := context.Background()
			cl, err := a.client(ctx, repo.Host)
			if err != nil {
				return err
			}
			remaining, err = cl.Unstack(ctx, repo, number)
			return err
		},
		done: func() {
			tab.reloadStackTabs()
			if remaining > 0 {
				a.say("Unstacked, but %d stay stacked: they are queued or have auto-merge on", remaining)
			} else {
				a.say("Unstacked #%d", number)
			}
		},
	})
}

// stackOnBase puts a pull request just made on top of the pull request its
// base branch is the branch of, starting a stack with it or adding to the one
// it is the top of. It says what it did.
func stackOnBase(ctx context.Context, cl *github.Client, repo github.Repo, below, created int) (string, error) {
	st, ok, err := cl.StackOf(ctx, repo, below)
	if err != nil {
		return "", err
	}
	if !ok {
		s, err := cl.CreateStack(ctx, repo, []int{below, created})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("stack #%d with #%d", s.Number, below), nil
	}
	if n := len(st.Entries); n == 0 || st.Entries[n-1].Number != below {
		return "", fmt.Errorf("#%d is not the top of its stack", below)
	}
	if _, err := cl.AddToStack(ctx, repo, st.Number, []int{created}); err != nil {
		return "", err
	}
	return fmt.Sprintf("stack #%d", st.Number), nil
}
