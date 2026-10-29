package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/eliott/voir/internal/git"
)

func revealInFinder(path string) {
	switch runtime.GOOS {
	case "darwin":
		exec.Command("open", "-R", path).Start()
	case "windows":
		exec.Command("explorer", "/select,", path).Start()
	default:
		exec.Command("xdg-open", filepath.Dir(path)).Start()
	}
}

func openInTerminal(dir string) {
	switch runtime.GOOS {
	case "darwin":
		exec.Command("open", "-a", "Terminal", dir).Start()
	case "windows":
		exec.Command("cmd", "/c", "start", "cmd", "/K", "cd /d "+dir).Start()
	default:
		exec.Command("x-terminal-emulator", "--working-directory="+dir).Start()
	}
}

func validName(label, s string) string {
	if s == "" {
		return "Enter a " + label
	}
	if strings.ContainsAny(s, " ~^:?*[\\") || strings.HasPrefix(s, "-") || strings.Contains(s, "..") || strings.HasSuffix(s, "/") || strings.HasSuffix(s, ".lock") {
		return "That is not a valid " + label
	}
	return ""
}

// branchNames lists the local branches, the current one first.
func (tab *repoTab) branchNames() []string {
	var names []string
	for _, r := range tab.refs {
		if r.Kind == git.LocalBranch {
			if r.Head {
				names = append([]string{r.Name}, names...)
			} else {
				names = append(names, r.Name)
			}
		}
	}
	return names
}

func (tab *repoTab) remoteNames() []string {
	var names []string
	for _, r := range tab.remotes {
		names = append(names, r.Name)
	}
	return names
}

// newBranchDialog is Create Branch.
func (a *app) newBranchDialog(tab *repoTab, at string) {
	if at == "" {
		at = "HEAD"
	}
	label := at
	if len(at) == 40 {
		label = at[:7]
	}
	dirty := tab.hasChanges()
	fields := []*field{
		text("name", "Branch name:", "Enter branch name", ""),
		text("at", "Create at:", "", at),
		check("checkout", "Check out after create", true),
	}
	if dirty {
		fields = append(fields, choice("lc", "Local changes:", "Don't change", "Don't change", "Stash and reapply", "Discard"))
	}
	a.showDialog(&dialog{
		title:    "Create branch",
		message:  "Create a new branch at " + label,
		ok:       "Create Branch",
		fields:   fields,
		saveWork: func(d *dialog) bool { return d.field("lc").text == "Discard" },
		validate: func(d *dialog) string {
			if r := validName("branch name", d.str("name")); r != "" {
				return r
			}
			if d.str("at") == "" {
				return "Enter a revision"
			}
			return ""
		},
		run: func(d *dialog) error {
			lc := git.KeepChanges
			switch d.field("lc").text {
			case "Stash and reapply":
				lc = git.StashChanges
			case "Discard":
				lc = git.DiscardChanges
			}
			return tab.repo.CreateBranch(d.str("name"), d.str("at"), d.bool("checkout"), lc)
		},
	})
}

// newTagDialog is Create Tag.
func (a *app) newTagDialog(tab *repoTab, at string) {
	if at == "" {
		at = "HEAD"
	}
	a.showDialog(&dialog{
		title:  "Create tag",
		ok:     "Create Tag",
		fields: []*field{text("name", "Tag name:", "v1.0.0", ""), text("at", "Create at:", "", at), &field{kind: fieldArea, key: "msg", label: "Message (optional — makes an annotated tag)"}, check("push", "Push to origin after creating", false)},
		validate: func(d *dialog) string {
			return validName("tag name", d.str("name"))
		},
		run: func(d *dialog) error {
			if err := tab.repo.CreateTag(d.str("name"), d.str("at"), d.str("msg")); err != nil {
				return err
			}
			if d.bool("push") && len(tab.remotes) > 0 {
				return tab.repo.PushTag(context.Background(), tab.remotes[0].Name, d.str("name"))
			}
			return nil
		},
	})
}

// pushTagDialog is Push Tag.
func (a *app) pushTagDialog(tab *repoTab, name string) {
	a.showDialog(&dialog{
		title:  "Push tag “" + name + "”",
		ok:     "Push Tag",
		fields: []*field{choice("remote", "Remote:", tab.remoteNames()[0], tab.remoteNames()...)},
		run: func(d *dialog) error {
			return tab.repo.PushTag(context.Background(), d.field("remote").text, name)
		},
	})
}

// stashDialog is Save Stash.
func (a *app) stashDialog(tab *repoTab) {
	hasNew := false
	if tab.status != nil {
		for _, e := range tab.status.Entries {
			hasNew = hasNew || e.Untracked
		}
	}
	a.showDialog(&dialog{
		title:   "Save stash",
		message: "Save your local changes to a new stash",
		ok:      "Save Stash",
		fields:  []*field{text("msg", "Message:", "Stash message (optional)", ""), check("untracked", "Include untracked files", hasNew)},
		run: func(d *dialog) error {
			return tab.repo.StashPush(d.str("msg"), d.bool("untracked"))
		},
	})
}

// quickStash saves a stash at once.
func (a *app) quickStash(tab *repoTab) {
	tab.do("Stash", func(r *git.Repo) error { return r.StashPush("", true) })
}

// fetchDialog is Fetch….
func (a *app) fetchDialog(tab *repoTab) {
	remotes := append([]string{"All remotes"}, tab.remoteNames()...)
	s := a.store.settings()
	a.showDialog(&dialog{
		title:   "Fetch",
		message: "Fetch latest changes from remote repositories",
		ok:      "Fetch",
		fields:  []*field{choice("remote", "Remote:", remotes[0], remotes...), check("tags", "Fetch all tags", s.AlwaysFetchTags), check("prune", "Prune deleted remote branches", true)},
		run: func(d *dialog) error {
			remote := d.field("remote").text
			if remote == "All remotes" {
				remote = ""
			}
			return tab.repo.Fetch(context.Background(), remote, d.bool("tags"), d.bool("prune"))
		},
	})
}

// pullDialog is Pull….
func (a *app) pullDialog(tab *repoTab) {
	if len(tab.remotes) == 0 {
		a.fail("Pull", fmt.Errorf("This repository has no remote."))
		return
	}
	cur := tab.currentBranch()
	if cur == nil {
		a.fail("Pull", fmt.Errorf("HEAD is detached: check out a branch to pull into it."))
		return
	}
	remote, branch := tab.remoteNames()[0], cur.Name
	if cur.Upstream != "" {
		if r, b, ok := strings.Cut(cur.Upstream, "/"); ok {
			remote, branch = r, b
		}
	}
	a.showDialog(&dialog{
		title:   "Pull",
		message: "Pull remote changes and merge them into “" + cur.Name + "”",
		ok:      "Pull",
		fields: []*field{
			choice("remote", "Remote:", remote, tab.remoteNames()...),
			text("branch", "Branch:", "", branch),
			&field{kind: fieldNote, label: "Into: " + cur.Name},
			check("rebase", "Rebase instead of merge", false),
			check("stash", "Stash and reapply local changes", tab.hasChanges()),
		},
		validate: func(d *dialog) string {
			if d.str("branch") == "" {
				return "Enter a branch"
			}
			return ""
		},
		run: func(d *dialog) error {
			r := tab.repo
			stash := d.bool("stash") && tab.hasChanges()
			if stash {
				if err := r.StashPush("Fork: before pull", true); err != nil {
					return err
				}
			}
			err := r.Pull(context.Background(), d.field("remote").text, d.str("branch"), d.bool("rebase"))
			if stash {
				if perr := r.StashPop("stash@{0}"); perr != nil && err == nil {
					err = perr
				}
			}
			return err
		},
	})
}

// pushDialog is Push….
func (a *app) pushDialog(tab *repoTab, branch string) {
	if len(tab.remotes) == 0 {
		a.fail("Push", fmt.Errorf("This repository has no remote. Add one in Repository ▸ Settings."))
		return
	}
	if branch == "" {
		if cur := tab.currentBranch(); cur != nil {
			branch = cur.Name
		}
	}
	remote := tab.remoteNames()[0]
	remoteBranch := branch
	if tab.status != nil && branch == tab.status.Branch && tab.status.Upstream != "" {
		if r, b, ok := strings.Cut(tab.status.Upstream, "/"); ok {
			remote, remoteBranch = r, b
		}
	}
	a.showDialog(&dialog{
		title:   "Push",
		message: "Push your local changes to a remote repository",
		ok:      "Push",
		fields: []*field{
			choice("branch", "Branch:", branch, tab.branchNames()...),
			choice("remote", "Remote:", remote, tab.remoteNames()...),
			text("to", "To:", "", remoteBranch),
			check("track", "Create tracking reference", true),
			check("tags", "Push all tags", false),
			check("force", "Force push (with lease)", false),
		},
		validate: func(d *dialog) string { return validName("branch", d.str("to")) },
		run: func(d *dialog) error {
			return tab.repo.Push(context.Background(), git.PushOptions{
				Remote: d.field("remote").text, Local: d.field("branch").text, RemoteBranch: d.str("to"),
				SetUpstream: d.bool("track"), Tags: d.bool("tags"), Force: d.bool("force"),
			})
		},
	})
}

// mergeDialog is Merge.
func (a *app) mergeDialog(tab *repoTab, rev string) {
	cur := tab.currentBranch()
	if cur == nil {
		a.fail("Merge", fmt.Errorf("HEAD is detached: check out a branch to merge into it."))
		return
	}
	a.showDialog(&dialog{
		title:   fmt.Sprintf("Merge into “%s”", cur.Name),
		message: "Merge " + shortRev(rev),
		ok:      "Merge",
		fields:  []*field{choice("how", "Merge:", "Fast-forward if possible", "Fast-forward if possible", "Always create a merge commit", "Fast-forward only", "Squash")},
		run: func(d *dialog) error {
			o := git.MergeOptions{}
			switch d.field("how").text {
			case "Always create a merge commit":
				o.NoFastForward = true
			case "Fast-forward only":
				o.FastForwardOnly = true
			case "Squash":
				o.Squash = true
			}
			return tab.repo.Merge(rev, o)
		},
	})
}

func shortRev(rev string) string {
	if len(rev) == 40 {
		return rev[:7]
	}
	return rev
}

// renameBranchDialog is Rename Branch.
func (a *app) renameBranchDialog(tab *repoTab, old string) {
	a.showDialog(&dialog{
		title:   "Rename branch",
		message: "Rename “" + old + "”",
		ok:      "Rename",
		fields:  []*field{text("name", "New name:", "", old)},
		validate: func(d *dialog) string {
			if d.str("name") == old {
				return "Enter a different name"
			}
			return validName("branch name", d.str("name"))
		},
		run: func(d *dialog) error { return tab.repo.RenameBranch(old, d.str("name")) },
	})
}

// resetDialog is Reset Branch.
func (a *app) resetDialog(tab *repoTab, cm *git.Commit) {
	cur := tab.currentBranch()
	name := "HEAD"
	if cur != nil {
		name = cur.Name
	}
	a.showDialog(&dialog{
		title:   fmt.Sprintf("Reset “%s” to %s", name, cm.Short()),
		message: cm.Subject,
		ok:      "Reset",
		danger:  true,
		fields: []*field{
			choice("mode", "Mode:", "Mixed — keep changes, unstaged", "Soft — keep changes, staged", "Mixed — keep changes, unstaged", "Hard — discard all changes"),
			note("A hard reset throws away every local change; Undo" + a.hint("undo") + " brings them back."),
		},
		saveWork: func(d *dialog) bool { return strings.HasPrefix(d.field("mode").text, "Hard") },
		run: func(d *dialog) error {
			mode := git.ResetMixed
			switch {
			case strings.HasPrefix(d.field("mode").text, "Soft"):
				mode = git.ResetSoft
			case strings.HasPrefix(d.field("mode").text, "Hard"):
				mode = git.ResetHard
			}
			return tab.repo.Reset(cm.Hash, mode)
		},
	})
}

// checkoutCommitDialog is Checkout Revision: a commit becomes HEAD, detached.
func (a *app) checkoutCommitDialog(tab *repoTab, cm *git.Commit) {
	var branches []string
	for _, r := range tab.refsAt[cm.Hash] {
		if r.Kind == git.LocalBranch && !r.Head {
			branches = append(branches, r.Name)
		}
	}
	if len(branches) == 1 {
		tab.checkout(branches[0])
		return
	}
	a.showDialog(&dialog{
		title:   "Checkout commit " + cm.Short(),
		message: "HEAD will be detached at this commit.",
		ok:      "Checkout",
		run:     func(*dialog) error { return tab.repo.Checkout(cm.Hash, git.KeepChanges) },
	})
}

// checkoutBranchDialog is Checkout Branch….
func (a *app) checkoutBranchDialog(tab *repoTab) {
	var names []string
	for _, r := range tab.refs {
		switch r.Kind {
		case git.LocalBranch:
			if !r.Head {
				names = append(names, r.Name)
			}
		case git.RemoteBranch:
			names = append(names, r.Name)
		}
	}
	if len(names) == 0 {
		a.say("There is no other branch to check out")
		return
	}
	a.showDialog(&dialog{
		title:  "Checkout branch",
		ok:     "Checkout",
		fields: []*field{choice("branch", "Branch:", names[0], names...)},
		run: func(d *dialog) error {
			name := d.field("branch").text
			if strings.Contains(name, "/") {
				for _, r := range tab.refs {
					if r.Kind == git.RemoteBranch && r.Name == name {
						_, local, _ := strings.Cut(name, "/")
						if tab.hasLocal(local) {
							return tab.repo.Checkout(local, git.KeepChanges)
						}
						return tab.repo.CheckoutRemote(name, local)
					}
				}
			}
			return tab.repo.Checkout(name, git.KeepChanges)
		},
	})
}

func (tab *repoTab) hasLocal(name string) bool {
	for _, r := range tab.refs {
		if r.Kind == git.LocalBranch && r.Name == name {
			return true
		}
	}
	return false
}

// worktreeDialog is New Worktree….
func (a *app) worktreeDialog(tab *repoTab) {
	def := filepath.Dir(tab.repo.Root) + "/" + tab.name + "-worktree"
	d := &dialog{
		title:  "New worktree",
		ok:     "Create",
		fields: []*field{text("path", "Folder:", def, def), text("branch", "Branch:", "branch name", ""), check("new", "Create a new branch", true), check("open", "Open in a new tab", true)},
		validate: func(d *dialog) string {
			if d.str("path") == "" {
				return "Choose a folder"
			}
			return validName("branch name", d.str("branch"))
		},
		run: func(d *dialog) error {
			return tab.repo.AddWorktree(d.str("path"), d.str("branch"), d.bool("new"))
		},
	}
	d.done = func() {
		if d.bool("open") {
			a.openRepoReporting(d.str("path"))
		}
	}
	a.showDialog(d)
}

// ignoreDialog is Add to .gitignore.
func (a *app) ignoreDialog(tab *repoTab, p string) {
	ext := filepath.Ext(p)
	opts := []string{p}
	if ext != "" {
		opts = append(opts, "*"+ext)
	}
	if d := filepath.Dir(p); d != "." {
		opts = append(opts, d+"/")
	}
	a.showDialog(&dialog{
		title:  "Ignore",
		ok:     "Add to .gitignore",
		fields: []*field{choice("pattern", "Pattern:", opts[0], opts...)},
		run: func(d *dialog) error {
			pattern := d.field("pattern").text
			if !strings.HasPrefix(pattern, "*") {
				pattern = "/" + pattern
			}
			return tab.repo.AddToGitignore(pattern)
		},
	})
}

// cloneDialog is Clone….
func (a *app) cloneDialog() {
	home, _ := os.UserHomeDir()
	where := a.store.data.SourceDir
	if where == "" {
		where = filepath.Join(home, "Developer")
		if _, err := os.Stat(where); err != nil {
			where = home
		}
	}
	d := &dialog{
		title:   "Clone",
		message: "Clone a remote repository into a local directory",
		ok:      "Clone",
		fields: []*field{
			text("url", "URL:", "Git repository URL", ""),
			text("dir", "Location:", "", where),
			text("name", "Name:", "", ""),
		},
		validate: func(d *dialog) string {
			if d.str("url") == "" {
				return "Enter the repository URL"
			}
			return ""
		},
	}
	d.run = func(d *dialog) error {
		url := d.str("url")
		name := d.str("name")
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(strings.TrimRight(url, "/")), ".git")
			name = name[strings.LastIndexAny(name, ":")+1:]
		}
		dest := filepath.Join(d.str("dir"), name)
		if err := git.Clone(context.Background(), url, dest, nil); err != nil {
			return err
		}
		a.store.mu.Lock()
		a.store.data.SourceDir = d.str("dir")
		a.store.mu.Unlock()
		a.store.save()
		a.update(func() { a.openRepoReporting(dest) })
		return nil
	}
	a.showDialog(d)
}

// initDialog is Create New Local Repository….
func (a *app) initDialog() {
	home, _ := os.UserHomeDir()
	a.showDialog(&dialog{
		title:  "Create new local repository",
		ok:     "Create",
		fields: []*field{text("dir", "Folder:", "", filepath.Join(home, "Developer", "new-repository"))},
		validate: func(d *dialog) string {
			if d.str("dir") == "" {
				return "Choose a folder"
			}
			return ""
		},
		run: func(d *dialog) error {
			r, err := git.Init(d.str("dir"))
			if err != nil {
				return err
			}
			a.update(func() { a.openRepoReporting(r.Root) })
			return nil
		},
	})
}

// remotesDialog is the repository's remotes, to add and edit.
func (a *app) addRemoteDialog(tab *repoTab) {
	a.showDialog(&dialog{
		title:  "Add remote",
		ok:     "Add",
		fields: []*field{text("name", "Name:", "origin", ""), text("url", "URL:", "Git repository URL", "")},
		validate: func(d *dialog) string {
			if d.str("name") == "" || d.str("url") == "" {
				return "Enter a name and a URL"
			}
			return ""
		},
		run: func(d *dialog) error { return tab.repo.AddRemote(d.str("name"), d.str("url")) },
	})
}

func (a *app) savePatch(tab *repoTab, cm *git.Commit) {
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Title: "Save as Patch", DefaultPath: cm.Short() + ".patch"})
		if err != nil || path == "" {
			return
		}
		if err := tab.repo.SavePatch(cm.Hash, path); err != nil {
			a.update(func() { a.fail("Save as patch failed", err) })
			return
		}
		a.say("Patch saved")
	}()
}

func (a *app) applyPatchFile(tab *repoTab) {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "Apply Patch"})
		if err != nil || len(paths) == 0 {
			return
		}
		a.update(func() {
			tab.do("Apply patch", func(r *git.Repo) error { return r.ApplyPatchFile(paths[0]) })
		})
	}()
}

// repoSettingsDialog is Settings for This Repository: user and remotes.
func (a *app) repoSettingsDialog(tab *repoTab) {
	r := tab.repo
	a.showDialog(&dialog{
		title:   "Settings for “" + tab.name + "”",
		message: "Leave a field empty to use the global configuration.",
		ok:      "Save",
		fields: []*field{
			text("name", "User name:", git.GlobalConfig("user.name"), r.Config("user.name")),
			text("email", "Email:", git.GlobalConfig("user.email"), r.Config("user.email")),
			check("nff", "No fast-forward on merge", r.Config("merge.ff") == "false"),
			note("Remotes: " + remoteSummary(tab)),
		},
		run: func(d *dialog) error {
			for _, kv := range [][2]string{{"user.name", d.str("name")}, {"user.email", d.str("email")}} {
				if kv[1] == "" {
					r.Do("config", "--unset", kv[0])
				} else if err := r.SetConfig(kv[0], kv[1]); err != nil {
					return err
				}
			}
			if d.bool("nff") {
				return r.SetConfig("merge.ff", "false")
			}
			r.Do("config", "--unset", "merge.ff")
			return nil
		},
	})
}

func remoteSummary(tab *repoTab) string {
	if len(tab.remotes) == 0 {
		return "none"
	}
	var parts []string
	for _, r := range tab.remotes {
		parts = append(parts, r.Name+" ("+r.FetchURL+")")
	}
	return strings.Join(parts, ", ")
}

// openPrefs shows the Preferences window, or brings forward the one open.
func (a *app) openPrefs() {
	if a.win == nil {
		return
	}
	if a.prefsWin != nil && !a.prefsWin.IsDestroyed() {
		a.prefsWin.Restore()
		a.prefsWin.Focus()
		return
	}
	w := mygo.NewWindow(mygo.WindowOptions{
		Title:             "Settings",
		Width:             620,
		Height:            560,
		MinWidth:          540,
		MinHeight:         360,
		DisableMaximize:   true,
		DisableFullScreen: true,
		Content:           ui.View(a.prefsView),
		Page:              mygo.PageOptions{DevTools: devTools()},
	})
	a.prefsWin = w
	w.OnClosed(func() {
		a.update(func() {
			if a.prefsWin == w {
				a.prefsWin = nil
			}
			a.stopRecording()
		})
	})
	// A key pressed elsewhere is not for the binding being recorded.
	w.OnBlur(func() { a.update(a.stopRecording) })
}

// prefsView is the content of the Preferences window.
func (a *app) prefsView(c *ui.Context) {
	a.applyThemeChoice(c)
	if a.recording != "" {
		a.recordKey(c)
	} else if c.Shortcut(0, ui.KeyEscape) && a.prefsWin != nil {
		a.prefsWin.Close()
	}
	t := c.Theme()
	s := a.store.settings()
	changed := false
	set := func(fn func(*Settings)) { a.store.update(fn); changed = true }
	ui.Column(c).Fill().Padding(20).Gap(14).Background(t.Background).Children(func() {
		ui.Segmented(c, &a.prefsTab, "General", "Diff", "Git", "AI", "Keybinds").Label("Preferences")
		if a.prefsTab == keybindsTab {
			a.keybindsHeader(c)
		}
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(10).Children(func() {
				switch a.prefsTab {
				case 0:
					ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "Appearance:").Width(130).TextColor(t.TextMuted).TextAlign(ui.End)
						v := map[string]string{"system": "System", "light": "Light", "dark": "Dark"}[s.Theme]
						if ui.Select(c, &v, []string{"System", "Light", "Dark"}).Changed() {
							set(func(s *Settings) { s.Theme = strings.ToLower(v) })
							a.themeDirty = true
						}
					})
					ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "Sort commits:").Width(130).TextColor(t.TextMuted).TextAlign(ui.End)
						v := "Topologically"
						if s.SortByDate {
							v = "By date"
						}
						if ui.Select(c, &v, []string{"Topologically", "By date"}).Changed() {
							set(func(s *Settings) { s.SortByDate = v == "By date" })
						}
					})
					ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "Graph lanes:").Width(130).TextColor(t.TextMuted).TextAlign(ui.End)
						lanes := float64(s.GraphLanes)
						if ui.NumberInput(c, &lanes, 1, 100, 1).Changed() {
							a.store.update(func(s *Settings) { s.GraphLanes = int(lanes) })
						}
					})
					ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "Merge message:").Width(130).TextColor(t.TextMuted).TextAlign(ui.End)
						v := squashMessageLabel(s.MergeMessage)
						if ui.Select(c.Key("merge-message"), &v, squashMessageOptions).Changed() {
							set(func(s *Settings) { s.MergeMessage = squashMessageValue(v) })
						}
					})
					ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "Squash message:").Width(130).TextColor(t.TextMuted).TextAlign(ui.End)
						v := squashMessageLabel(s.SquashMessage)
						if ui.Select(c.Key("squash-message"), &v, squashMessageOptions).Changed() {
							set(func(s *Settings) { s.SquashMessage = squashMessageValue(v) })
						}
					})
					a.prefCheck(c, "Fetch remotes automatically", s.AutoFetch, func(s *Settings, on bool) { s.AutoFetch = on }, set)
					if s.AutoFetch {
						ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
							ui.Text(c, "Fetch every:").Width(130).TextColor(t.TextMuted).TextAlign(ui.End)
							mins := float64(s.fetchMinutes())
							if ui.NumberInput(c.Key("fetch-minutes"), &mins, 1, 1440, 1).Changed() {
								a.store.update(func(s *Settings) { s.AutoFetchMinutes = int(mins) })
							}
							ui.Text(c, "minutes").TextColor(t.TextMuted)
						})
					}
					a.prefCheck(c, "Always include tags into fetch", s.AlwaysFetchTags, func(s *Settings, on bool) { s.AlwaysFetchTags = on }, set)
					a.prefCheck(c, "Automatically push on commit", s.PushOnCommit, func(s *Settings, on bool) { s.PushOnCommit = on }, set)
				case 1:
					ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "Layout:").Width(130).TextColor(t.TextMuted).TextAlign(ui.End)
						v := map[string]string{"split": "Side by side", "unified": "Unified"}[s.DiffLayout]
						if ui.Select(c, &v, []string{"Side by side", "Unified"}).Changed() {
							set(func(s *Settings) { s.DiffLayout = map[string]string{"Side by side": "split", "Unified": "unified"}[v] })
						}
					})
					ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "Context lines:").Width(130).TextColor(t.TextMuted).TextAlign(ui.End)
						v := fmt.Sprint(max(s.ContextLines, 1))
						if ui.Select(c, &v, []string{"1", "3", "5", "10", "25"}).Changed() {
							set(func(s *Settings) { fmt.Sscan(v, &s.ContextLines) })
						}
					})
					a.prefCheck(c, "Ignore whitespace changes", s.IgnoreWhitespace, func(s *Settings, on bool) { s.IgnoreWhitespace = on }, set)
					a.prefCheck(c, "Wrap long lines", s.WrapLines, func(s *Settings, on bool) { s.WrapLines = on }, set)
				case 3:
					a.aiPrefs(c, s.AI)
				case keybindsTab:
					a.keybindsPrefs(c)
				default:
					a.prefCheck(c, "First-parent history only", s.FirstParent, func(s *Settings, on bool) { s.FirstParent = on }, set)
					a.prefCheck(c, "Hide tags in the commit list", s.HideTags, func(s *Settings, on bool) { s.HideTags = on }, set)
					a.prefCheck(c, "Hide stashes in the commit list", s.HideStashes, func(s *Settings, on bool) { s.HideStashes = on }, set)
					ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "Global user:").Width(130).TextColor(t.TextMuted).TextAlign(ui.End)
						ui.Text(c, git.GlobalConfig("user.name")+"  <"+git.GlobalConfig("user.email")+">").FontSize(13).Selectable()
					})
				}
			})
		})
	})
	if changed {
		// The window is drawn on its own: the repositories change under the
		// main one, which is drawn again when they are.
		for _, tb := range a.tabs {
			tb.resetDiffs()
			tb.reload()
		}
		if a.win != nil {
			a.win.Invalidate()
		}
	}
}

func (a *app) prefCheck(c *ui.Context, label string, on bool, apply func(*Settings, bool), set func(func(*Settings))) {
	v := on
	row := ui.Row(c).Gap(10).Children(func() {
		ui.Box(c).Width(130)
		if ui.Checkbox(c, &v, label).Changed() {
			set(func(s *Settings) { apply(s, v) })
		}
	})
	_ = row
}
