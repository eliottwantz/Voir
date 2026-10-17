package app

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
	"github.com/eliott/myfork/internal/github"
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

// stackButton is the pull request's place in its stack, which opens the stack:
// its pull requests, to open each, and what can be done to all of them.
func (tab *repoTab) stackButton(c *ui.Context, pv *prView) {
	t := c.Theme()
	a := tab.app
	pos, size := pv.stackPos()+1, len(pv.stack.Entries)
	b := ui.ButtonBase(c).Key("stack").Gap(5).Padding(4, 10).Radius(8).Background(subtleBg).Shrink(0).AlignItems(ui.Center).
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
	open := pv.stackOpen()
	ui.Popover(c, anchor, &a.prStack, func() {
		ui.Column(c).MinWidth(300).MaxWidth(420).Gap(2).Children(func() {
			ui.Text(c, fmt.Sprintf("Stack #%d", st.Number)).FontSize(13).FontWeight(600).TextColor(t.TextMuted).Padding(6, 10, 4)
			// The top of the stack is first, as it is built.
			for i := len(st.Entries) - 1; i >= 0; i-- {
				e := st.Entries[i]
				pr := entryPR(e)
				row := ui.ButtonBase(c).Key(fmt.Sprint("entry", e.Number)).Padding(6, 10).Gap(10).Radius(7).FillWidth().Justify(ui.Start).AlignItems(ui.Center).
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
			ui.Box(c).Key("stack-sep").Height(1).Background(hairline).Margin(4, 4).Shrink(0)
			menuRowButton(c, &a.prStack, menuRow{
				icon: iconMerge, label: fmt.Sprintf("Merge stack (%d)", merging),
				disabled: !idle || merging == 0,
				run:      func() { a.mergeStackDialog(tab, pv) },
			})
			menuRowButton(c, &a.prStack, menuRow{
				icon: iconRefresh, label: "Rebase stack",
				disabled: !idle || len(open) == 0 || tab.ghRemote == "",
				run:      func() { a.rebaseStackDialog(tab, pv) },
			})
		})
	})
}

// mergeStackDialog merges the stack up to the pull request shown, all of it
// or none.
func (a *app) mergeStackDialog(tab *repoTab, pv *prView) {
	if tab.gh == nil || pv.stack == nil {
		return
	}
	repo, n, base := *tab.gh, pv.number, pv.stack.Base.Ref
	count := pv.stackMergeable()
	a.showDialog(&dialog{
		title:   fmt.Sprintf("Merge stack #%d", pv.stack.Number),
		message: fmt.Sprintf("Merges %s into %s up to #%d, all together: if one cannot be merged, none is.", plural(count, "pull request", "pull requests"), base, n),
		ok:      "Merge",
		fields:  []*field{choice("method", "Method:", mergeMethods[0], mergeMethods...)},
		run: func(d *dialog) error {
			ctx := context.Background()
			cl, err := a.client(ctx, repo.Host)
			if err != nil {
				return err
			}
			method := map[string]string{"Create a merge commit": "merge", "Squash and merge": "squash", "Rebase and merge": "rebase"}[d.field("method").text]
			res, err := cl.MergeStack(ctx, repo, n, method)
			// GitHub merges in the background: ask until it has.
			for i := 0; err == nil && res.Status == "pending" && i < 120; i++ {
				time.Sleep(time.Second)
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
			return nil
		},
		done: func() {
			tab.prsLoaded = false
			for _, v := range tab.prTabs {
				tab.reloadPRView(v)
			}
			a.say("Merged the stack up to #%d", n)
		},
	})
}

// rebaseStackDialog rebases the open pull requests of the stack, each onto the
// one before, and pushes them.
func (a *app) rebaseStackDialog(tab *repoTab, pv *prView) {
	if tab.gh == nil || pv.stack == nil {
		return
	}
	st := pv.stack
	open := pv.stackOpen()
	if len(open) == 0 {
		return
	}
	// The first branch was built on the pull request under it, which may be
	// merged by now: only its own commits go onto the base branch.
	parent := ""
	if i := slices.IndexFunc(st.Entries, func(e github.StackEntry) bool { return e.Number == open[0].Number }); i > 0 {
		parent = st.Entries[i-1].Head.SHA
	}
	branches := make([]string, len(open))
	for i, e := range open {
		branches[i] = e.Head.Ref
	}
	remote, base := tab.ghRemote, st.Base.Ref
	a.showDialog(&dialog{
		title:   fmt.Sprintf("Rebase stack #%d", st.Number),
		message: fmt.Sprintf("Fetches %s, rebases %s one onto the next, the first onto %s, and pushes those that moved with a lease. The branches are checked out in turn, so the changes of the work tree have to be committed or stashed. A conflict stops it before anything is pushed.", remote, plural(len(open), "pull request", "pull requests"), base),
		ok:      "Rebase",
		run:     func(d *dialog) error { return nil },
		done: func() {
			var pushed []string
			tab.doThen("Rebase stack", func(r *git.Repo) (err error) {
				pushed, err = r.RebaseStack(context.Background(), remote, base, parent, branches)
				return
			}, func() {
				tab.prsLoaded = false
				for _, v := range tab.prTabs {
					tab.reloadPRView(v)
				}
				if len(pushed) == 0 {
					a.say("The stack is already up to date")
				} else {
					a.say("Rebased %s", plural(len(pushed), "branch", "branches"))
				}
			})
		},
	})
}
