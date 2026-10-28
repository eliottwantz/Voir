package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

var rebaseActions = []string{"Pick", "Reword", "Edit", "Squash", "Fixup", "Fixup -C", "Drop"}

var rebaseHints = map[string]string{
	"Pick":     "Use the commit as it is",
	"Reword":   "Use the commit, and edit its message",
	"Edit":     "Use the commit, then stop to amend it",
	"Squash":   "Meld into the commit below, and keep both messages",
	"Fixup":    "Meld into the commit below, and discard this message",
	"Fixup -C": "Meld into the commit below, and use this message instead of theirs",
	"Drop":     "Remove the commit",
}

type rebaseRow struct {
	hash    string
	short   string
	subject string
	author  string
	message string
	action  string
}

// rebaseState is the sheet that edits the todo list of an interactive rebase.
type rebaseState struct {
	tab        *repoTab
	onto       string
	label      string
	rows       []*rebaseRow // newest first, as the history shows them
	loading    bool
	running    bool
	err        string
	backup     bool
	autostash  bool
	updateRefs bool
}

// rebaseDialog opens the interactive rebase of the branch checked out onto a
// revision: the commits after it can be reordered, reworded, melded or
// dropped.
func (a *app) rebaseDialog(tab *repoTab, onto, label string) {
	if tab.currentBranch() == nil {
		a.fail("Interactive rebase", errString("HEAD is detached: check out a branch to rebase it."))
		return
	}
	if tab.opState != "" {
		a.fail("Interactive rebase", errString("A "+tab.opState+" is in progress: finish or abort it first."))
		return
	}
	st := &rebaseState{tab: tab, onto: onto, label: label, loading: true, backup: true, autostash: tab.hasChanges()}
	a.sheet = &sheet{open: true, title: "Interactive rebase onto " + label, body: st.view, state: st}
	repo := tab.repo
	a.background(func() {
		commits, err := repo.RebaseCommits(onto)
		a.update(func() {
			st.loading = false
			if err != nil {
				st.err = errorText(err)
				return
			}
			for i := len(commits) - 1; i >= 0; i-- {
				c := commits[i]
				msg := c.Subject
				if c.Body != "" {
					msg += "\n\n" + c.Body
				}
				st.rows = append(st.rows, &rebaseRow{hash: c.Hash, short: c.Short(), subject: c.Subject, author: c.Author, message: msg, action: "Pick"})
			}
		})
	})
}

type errString string

func (e errString) Error() string { return string(e) }

func (st *rebaseState) move(i, d int) {
	j := i + d
	if j < 0 || j >= len(st.rows) {
		return
	}
	st.rows[i], st.rows[j] = st.rows[j], st.rows[i]
}

// problem says why the rebase cannot start, "" when it can.
func (st *rebaseState) problem() string {
	if len(st.rows) == 0 {
		return "There is no commit to rebase"
	}
	// The oldest commit has nothing below it to meld into, once the ones
	// dropped are gone.
	for i := len(st.rows) - 1; i >= 0; i-- {
		switch st.rows[i].action {
		case "Drop":
			continue
		case "Squash", "Fixup", "Fixup -C":
			return "The oldest commit cannot be squashed or fixed up: there is nothing below it"
		}
		break
	}
	for _, r := range st.rows {
		if r.action == "Reword" && strings.TrimSpace(r.message) == "" {
			return "A reworded commit needs a message"
		}
	}
	return ""
}

// todo is the list git replays: oldest first.
func (st *rebaseState) todo() []git.TodoItem {
	var items []git.TodoItem
	for i := len(st.rows) - 1; i >= 0; i-- {
		r := st.rows[i]
		it := git.TodoItem{Hash: r.hash, Subject: r.subject}
		switch r.action {
		case "Reword":
			it.Action, it.Message = git.ActionReword, strings.TrimSpace(r.message)+"\n"
		case "Edit":
			it.Action = git.ActionEdit
		case "Squash":
			it.Action = git.ActionSquash
		case "Fixup":
			it.Action = git.ActionFixup
		case "Fixup -C":
			it.Action = git.ActionFixupMessage
		case "Drop":
			it.Action = git.ActionDrop
		default:
			it.Action = git.ActionPick
		}
		items = append(items, it)
	}
	return items
}

func (st *rebaseState) start() {
	a := st.tab.app
	if st.running || st.problem() != "" {
		return
	}
	st.running, st.err = true, ""
	repo, todo := st.tab.repo, st.todo()
	opts := git.RebaseOptions{Backup: st.backup, Autostash: st.autostash, UpdateRefs: st.updateRefs}
	onto := st.onto
	a.background(func() {
		backup, err := repo.RebaseInteractive(onto, todo, opts)
		stopped := repo.State() == "rebase"
		a.update(func() {
			st.running = false
			defer st.tab.reload()
			switch {
			case err == nil:
				a.sheet = nil
				msg := "Rebased"
				if backup != "" {
					msg += ", backup: " + backup
				}
				a.say("%s", msg)
			case stopped:
				// Conflicts: the repository is mid-rebase, and the banner
				// says so, with Continue and Abort.
				a.sheet = nil
				a.fail("The rebase stopped", errString("Resolve the conflicts in Local Changes, then continue the rebase.\n\n"+errorText(err)))
			default:
				st.err = errorText(err)
			}
		})
	})
}

func (st *rebaseState) view(c *ui.Context) {
	t := c.Theme()
	switch {
	case st.loading:
		spinner(c, "Loading").Margin(20)
		return
	case len(st.rows) == 0 && st.err == "":
		ui.Text(c, "There is no commit after "+st.label+" on this branch to rebase.").TextColor(t.TextMuted).Padding(12)
		return
	}
	ui.Text(c, "Choose what happens to each commit, and use the arrows to reorder. The oldest commit is at the bottom, and is replayed first.").FontSize(12).TextColor(t.TextMuted).Shrink(0)
	moveFrom, moveBy := -1, 0
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(2).Children(func() {
			for i, r := range st.rows {
				row := r
				idx := i
				ui.Column(c.Key(row.hash)).Padding(5, 8).Gap(4).Radius(7).Background(subtleBg).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Select(c, &row.action, rebaseActions).Width(120).Label("Action for " + row.short).Tooltip(rebaseHints[row.action])
						ui.Text(c, row.short).Font("SF Mono, Menlo, monospace").FontSize(11.5).TextColor(t.TextMuted).Width(58)
						sub := ui.Text(c, row.subject).SingleLine().Grow(1).MinWidth(0).FontSize(13)
						if row.action == "Drop" {
							sub.Strikethrough().Opacity(0.5)
						}
						ui.Text(c, row.author).FontSize(11.5).TextColor(t.TextMuted).SingleLine().Width(120)
						if ui.ButtonBase(c).Size(24, 24).Radius(5).Center().Label("Move up " + row.short).Tooltip("Move up").Disabled(idx == 0).Children(func() { ui.Icon(c, iconChevronUp).FontSize(13) }).Clicked() {
							moveFrom, moveBy = idx, -1
						}
						if ui.ButtonBase(c).Size(24, 24).Radius(5).Center().Label("Move down " + row.short).Tooltip("Move down").Disabled(idx == len(st.rows)-1).Children(func() { ui.Icon(c, iconChevronDown).FontSize(13) }).Clicked() {
							moveFrom, moveBy = idx, 1
						}
					})
					if row.action == "Reword" {
						ui.TextArea(c, &row.message).Lines(2, 8).Label("Message of "+row.short).Margin(0, 0, 0, 128)
					}
				})
			}
		})
	})
	if moveFrom >= 0 {
		st.move(moveFrom, moveBy)
	}
	ui.Column(c).Gap(6).Shrink(0).Children(func() {
		ui.Row(c).Gap(16).Children(func() {
			ui.Checkbox(c, &st.backup, "Backup current state with temporary branch")
			ui.Checkbox(c, &st.autostash, "Stash and reapply local changes")
			ui.Checkbox(c, &st.updateRefs, "Update dependent branches")
		})
		if st.err != "" {
			ui.Text(c, st.err).FontSize(12).TextColor(t.Danger).Selectable()
		}
		reason := st.problem()
		ui.Row(c).Gap(8).Justify(ui.End).AlignItems(ui.Center).Children(func() {
			if st.running {
				spinner(c, "Rebasing")
			}
			start := ui.PrimaryButton(c, "Start Rebase").Disabled(st.running || reason != "")
			if reason != "" {
				start.Tooltip(reason)
			}
			if start.Clicked() {
				st.start()
			}
		})
	})
}
