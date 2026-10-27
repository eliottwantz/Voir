package app

// The things the actions that are not just a dialog do. Each does nothing
// where it makes no sense, as a key can be pressed anywhere.

// commitFromMenu commits the staged changes, with Local Changes shown first:
// the message is there to be read.
func (a *app) commitFromMenu(t *repoTab, push, pr bool) {
	if !t.onWip {
		t.showChanges()
		return
	}
	if t.busy == "" {
		t.commitAndShip(push, pr)
	}
}

// stageFromMenu stages every change, or unstages every one.
func (a *app) stageFromMenu(t *repoTab, stage bool) {
	if t.busy == "" && t.hasChanges() {
		t.toggleStaged(nil, stage)
	}
}

// amendFromMenu turns Amend on or off.
func (a *app) amendFromMenu(t *repoTab) {
	if !t.onWip {
		t.showChanges()
		return
	}
	t.setAmend(!t.amend)
}

// aiMessageFromMenu writes the message of the commit about to be made.
func (a *app) aiMessageFromMenu(t *repoTab) {
	if !t.onWip {
		t.showChanges()
		return
	}
	if t.indexChanged() > 0 && !t.aiWriting {
		t.writeCommitMessage()
	}
}

// stepCommit chooses the commit d rows from the one chosen, in the graph.
func (a *app) stepCommit(t *repoTab, d int) {
	a.pulls = false
	if t.onWip {
		t.showHistory()
	}
	i := t.sel + d
	if t.sel < 0 {
		i = 0
	}
	t.selectRow(i)
	t.scrollSel = true
}

// copyCommitHash copies the hash of the commit chosen.
func (a *app) copyCommitHash(t *repoTab) {
	if t.onWip || t.commit == nil {
		return
	}
	a.copy(t.commit.Hash)
	a.say("Copied %s", t.commit.Short())
}

// stepTab goes to the next tab d along: of the pull requests on their page,
// of the repositories elsewhere.
func (a *app) stepTab(d int) {
	if a.pulls {
		a.nextPR(d)
		return
	}
	a.nextTab(d)
}

// goTab goes to the tab with a number, counted from 0; -1 is the last one.
func (a *app) goTab(n int) {
	if a.pulls {
		list, _ := a.openedPRs()
		if n < 0 {
			n = len(list) - 1
		}
		if n >= 0 && n < len(list) {
			a.prTab, list[n].tab.pr = list[n].tab, list[n].pv
		}
		return
	}
	if n < 0 {
		n = len(a.tabs) - 1
	}
	if n >= 0 && n < len(a.tabs) && n != a.cur {
		a.cur = n
		a.saveTabs()
	}
}

// onPR runs fn on the pull request shown on the pull requests page.
func (a *app) onPR(fn func(t *repoTab)) {
	if t := a.activePR(); a.pulls && t != nil {
		fn(t)
	}
}

// pickPRTab shows a tab of the pull request shown.
func (a *app) pickPRTab(tab int) { a.onPR(func(t *repoTab) { t.pr.tab = tab }) }

// stepPR shows the pull request d along; on the pull requests page only.
func (a *app) stepPR(d int) {
	if a.pulls {
		a.nextPR(d)
	}
}

// togglePRDraft marks a draft ready for review, and converts what is ready to a
// draft.
func togglePRDraft(t *repoTab) {
	pv := t.pr
	if pv.busy != "" || pv.pr.State != "open" || pv.pr.Merged {
		return
	}
	if pv.pr.Draft {
		t.markPRReady()
	} else {
		t.convertPRToDraft()
	}
}
