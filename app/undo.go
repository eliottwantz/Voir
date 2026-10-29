package app

import (
	"errors"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/voir/internal/git"
)

// undoLimit is how many operations a tab can undo.
const undoLimit = 50

// undoStep is an operation done in the app, as the repository was before and
// after it: Undo (⌘Z) takes it from after back to before, and Redo (⇧⌘Z)
// forward again. The steps of a tab last as long as the tab.
type undoStep struct {
	title         string
	before, after git.Snapshot
	// soft is for a commit: undoing it leaves its changes staged.
	soft bool
}

// recording runs fn, an operation on the repository off the main thread, and
// keeps it to undo when it changed a branch, a tag or HEAD, or, with work, the
// files or the index, which it saves first: that is for the operations that
// throw changes away. An operation that stops in the middle (a rebase on a
// conflict) is not kept: Abort is its undo.
func (t *repoTab) recording(title string, work bool, fn func() error) error {
	repo := t.repo
	if repo == nil || repo.State() != "" {
		return fn()
	}
	before, err := repo.Snapshot(work)
	if err != nil {
		return fn()
	}
	// What failed after a part of it was done, as a commit whose push
	// failed, keeps that part to undo.
	ferr := fn()
	if repo.State() != "" {
		return ferr
	}
	after, err := repo.Snapshot(work)
	if err != nil || after.Same(before) && after.Index == before.Index && after.Work == before.Work {
		return ferr
	}
	step := undoStep{title: title, before: before, after: after}
	if after.HeadHash != before.HeadHash && after.Head == before.Head {
		if es, _ := repo.Reflog(1); len(es) == 1 && strings.HasPrefix(es[0].Action, "commit") {
			step.soft = true
		}
	}
	t.app.update(func() { t.pushUndo(step) })
	return ferr
}

// pushUndo keeps a step to undo; what was undone can no longer be redone.
func (t *repoTab) pushUndo(s undoStep) {
	t.undos = append(t.undos, s)
	if len(t.undos) > undoLimit {
		t.undos = t.undos[len(t.undos)-undoLimit:]
	}
	t.redos = nil
}

// undoTitle is the name of an operation, from the question that asked for
// it: "Delete branch “x”?" is undone as “Delete branch “x””.
func undoTitle(question string) string {
	return strings.TrimSuffix(strings.TrimSpace(question), "?")
}

// undoButton is Undo in the bar along the top, in every view of the tab: it
// names the operation it takes back, and its context menu has Redo.
func (t *repoTab) undoButton(c *ui.Context) {
	a := t.app
	label := "Nothing to undo"
	if n := len(t.undos); n > 0 {
		label = "Undo “" + t.undos[n-1].title + "”"
	}
	b := iconButton(c, iconUndo, label).Tooltip(a.tip(label, "undo")).Disabled(t.busy != "" || len(t.undos)+len(t.redos) == 0)
	if b.Clicked() && len(t.undos) > 0 {
		t.undo()
	}
	b.ContextMenu(func(m *ui.Menu) {
		undo, redo := "Undo", "Redo"
		if n := len(t.undos); n > 0 {
			undo += " “" + t.undos[n-1].title + "”"
		}
		if n := len(t.redos); n > 0 {
			redo += " “" + t.redos[n-1].title + "”"
		}
		if a.keyed(m.Item(undo), "undo").Disabled(len(t.undos) == 0).Chosen() {
			t.undo()
		}
		if a.keyed(m.Item(redo), "redo").Disabled(len(t.redos) == 0).Chosen() {
			t.redo()
		}
	})
}

// undo takes back the last operation of the tab.
func (t *repoTab) undo() { t.undoRedo(false) }

// redo does again the last operation undone.
func (t *repoTab) redo() { t.undoRedo(true) }

func (t *repoTab) undoRedo(redo bool) {
	verb, stack := "Undo", &t.undos
	if redo {
		verb, stack = "Redo", &t.redos
	}
	if t.busy != "" {
		t.app.say("%s is still running", t.busy)
		return
	}
	if len(*stack) == 0 {
		t.app.say("Nothing to %s", strings.ToLower(verb))
		return
	}
	s := (*stack)[len(*stack)-1]
	to, from := s.before, s.after
	if redo {
		to, from = from, to
	}
	t.busy = verb + " " + s.title
	repo := t.repo
	t.app.background(func() {
		err := repo.Restore(to, from, s.soft, "voir: "+strings.ToLower(verb)+" "+s.title)
		t.app.update(func() {
			t.busy, t.workStale = "", true
			switch {
			case errors.Is(err, git.ErrMovedOn):
				// What came since would be lost: the step goes.
				*stack = (*stack)[:len(*stack)-1]
				t.app.say("Can’t %s “%s”: the repository changed since", strings.ToLower(verb), s.title)
			case err != nil:
				t.app.fail(verb+" “"+s.title+"” failed", err)
			default:
				*stack = (*stack)[:len(*stack)-1]
				if redo {
					t.undos = append(t.undos, s)
					t.app.post(message{text: "Redid “" + s.title + "”", label: "Undo", action: t.undo})
				} else {
					t.redos = append(t.redos, s)
					t.app.post(message{text: "Undid “" + s.title + "”", label: "Redo", action: t.redo})
				}
			}
			t.reload()
			if t.onReflog {
				t.loadReflog()
			}
		})
	})
}
