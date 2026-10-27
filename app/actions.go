package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

// A bindable is an action: something a key can do. Most are items of the menu bar, which
// carries their binding as the item's accelerator; those with key set are
// read from the keyboard where they act, and scope says where: "" is
// everywhere, else the area ("commits", "changes", "files", "pulls",
// "comment"), so that ⌘↩ can maximize the commit panel, commit, and expand a
// pull request without any of them clashing.
type bindable struct {
	id    string
	group string
	label string
	// menu is the label of the menu item, when it differs from label.
	menu string
	// def is the binding the action has until the user changes it, as an
	// accelerator; the actions added with the keybinds have none.
	def   string
	scope string
	key   bool
}

// Groups of the Keybinds settings, in the order they are shown.
const (
	groupApp     = "App"
	groupTabs    = "Tabs and Windows"
	groupRepo    = "Repository"
	groupChanges = "Local Changes"
	groupCommits = "Commits"
	groupView    = "View"
	groupDiff    = "Diff"
	groupPulls   = "Pull Requests"
)

// bindables are all the things a key can be bound to.
var bindables = []bindable{
	{id: "prefs", group: groupApp, label: "Open Settings", menu: "Preferences…", def: "CmdOrCtrl+,"},
	{id: "install-cli", group: groupApp, label: "Install the myfork Command", menu: "Install `myfork` CLI…"},
	{id: "quick-launch", group: groupApp, label: "Quick Launch…"},
	{id: "refresh", group: groupApp, label: "Refresh", def: "CmdOrCtrl+R"},
	{id: "find", group: groupApp, label: "Find…", def: "CmdOrCtrl+F"},

	{id: "new-tab", group: groupTabs, label: "New Tab", def: "CmdOrCtrl+T"},
	{id: "open-repo", group: groupTabs, label: "Open Repository…", menu: "Open…", def: "CmdOrCtrl+O"},
	{id: "clone", group: groupTabs, label: "Clone…", def: "CmdOrCtrl+Shift+N"},
	{id: "init-repo", group: groupTabs, label: "Create New Local Repository…"},
	{id: "close-tab", group: groupTabs, label: "Close Tab", def: "CmdOrCtrl+W"},
	{id: "close-window", group: groupTabs, label: "Close Window", def: "CmdOrCtrl+Shift+W"},
	{id: "restore-tab", group: groupTabs, label: "Restore Closed Tab", def: "CmdOrCtrl+Shift+T"},
	{id: "next-tab", group: groupTabs, label: "Next Tab", def: "CmdOrCtrl+Shift+]", key: true},
	{id: "prev-tab", group: groupTabs, label: "Previous Tab", def: "CmdOrCtrl+Shift+[", key: true},
	{id: "next-tab-alt", group: groupTabs, label: "Next Tab (Second Key)", def: "Ctrl+Tab", key: true},
	{id: "prev-tab-alt", group: groupTabs, label: "Previous Tab (Second Key)", def: "Ctrl+Shift+Tab", key: true},
	{id: "go-tab-1", group: groupTabs, label: "Go to Tab 1", menu: "Tab 1"},
	{id: "go-tab-2", group: groupTabs, label: "Go to Tab 2", menu: "Tab 2"},
	{id: "go-tab-3", group: groupTabs, label: "Go to Tab 3", menu: "Tab 3"},
	{id: "go-tab-4", group: groupTabs, label: "Go to Tab 4", menu: "Tab 4"},
	{id: "go-tab-5", group: groupTabs, label: "Go to Tab 5", menu: "Tab 5"},
	{id: "go-tab-6", group: groupTabs, label: "Go to Tab 6", menu: "Tab 6"},
	{id: "go-tab-7", group: groupTabs, label: "Go to Tab 7", menu: "Tab 7"},
	{id: "go-tab-8", group: groupTabs, label: "Go to Tab 8", menu: "Tab 8"},
	{id: "go-tab-9", group: groupTabs, label: "Go to Tab 9", menu: "Tab 9"},
	{id: "last-tab", group: groupTabs, label: "Go to Last Tab", menu: "Last Tab"},

	{id: "fetch", group: groupRepo, label: "Fetch…", def: "CmdOrCtrl+Shift+F"},
	{id: "quick-fetch", group: groupRepo, label: "Quick Fetch"},
	{id: "pull", group: groupRepo, label: "Pull…", def: "CmdOrCtrl+Shift+L"},
	{id: "quick-pull", group: groupRepo, label: "Quick Pull"},
	{id: "quick-pull-rebase", group: groupRepo, label: "Quick Pull with Rebase"},
	{id: "push", group: groupRepo, label: "Push…", def: "CmdOrCtrl+Shift+P"},
	{id: "quick-push", group: groupRepo, label: "Quick Push"},
	{id: "stash", group: groupRepo, label: "Save Stash…"},
	{id: "quick-stash", group: groupRepo, label: "Quick Stash"},
	{id: "checkout-branch", group: groupRepo, label: "Checkout Branch…", def: "CmdOrCtrl+Shift+B"},
	{id: "new-branch", group: groupRepo, label: "New Branch…", def: "CmdOrCtrl+Alt+B"},
	{id: "new-tag", group: groupRepo, label: "New Tag…"},
	{id: "new-worktree", group: groupRepo, label: "New Worktree…"},
	{id: "create-pr", group: groupRepo, label: "Create Pull Request…"},
	{id: "open-pr", group: groupRepo, label: "Open Pull Request…"},
	{id: "lfs", group: groupRepo, label: "Git LFS…"},
	{id: "add-remote", group: groupRepo, label: "Add Remote…"},
	{id: "apply-patch", group: groupRepo, label: "Apply Patch…"},
	{id: "reveal", group: groupRepo, label: "Show in Finder"},
	{id: "terminal", group: groupRepo, label: "Open in Terminal"},
	{id: "repo-settings", group: groupRepo, label: "Settings for This Repository…"},

	{id: "commit", group: groupChanges, label: "Commit Staged Changes"},
	{id: "commit-push", group: groupChanges, label: "Commit and Push"},
	{id: "commit-pr", group: groupChanges, label: "Commit, Push and Create Pull Request"},
	{id: "stage-all", group: groupChanges, label: "Stage All Changes"},
	{id: "unstage-all", group: groupChanges, label: "Unstage All Changes"},
	{id: "amend", group: groupChanges, label: "Amend the Last Commit"},
	{id: "ai-message", group: groupChanges, label: "Write the Commit Message with AI"},
	{id: "commit-box", group: groupChanges, label: "Commit (in the Message and the File Lists)", def: "CmdOrCtrl+Enter", scope: "changes", key: true},
	{id: "stage-toggle", group: groupChanges, label: "Stage or Unstage the Files Chosen", def: "CmdOrCtrl+S", scope: "changes", key: true},
	{id: "stage-all-toggle", group: groupChanges, label: "Stage All, or Unstage All when All Are Staged", def: "CmdOrCtrl+Shift+S", scope: "changes", key: true},
	{id: "discard-selected", group: groupChanges, label: "Unstage or Discard the Files Chosen", def: "Delete", scope: "changes", key: true},
	{id: "open-selected-file", group: groupChanges, label: "Open Selected File", def: "CmdOrCtrl+Shift+O"},
	{id: "copy-path", group: groupChanges, label: "Copy Path of the File Chosen", def: "CmdOrCtrl+C", scope: "files", key: true},
	{id: "copy-full-path", group: groupChanges, label: "Copy Path on Disk of the File Chosen", def: "CmdOrCtrl+Shift+C", scope: "files", key: true},

	{id: "view-changes", group: groupCommits, label: "Show Local Changes", menu: "Local Changes", def: "CmdOrCtrl+1"},
	{id: "view-commits", group: groupCommits, label: "Show All Commits", menu: "All Commits", def: "CmdOrCtrl+2"},
	{id: "side-pulls", group: groupCommits, label: "Show Pull Requests", menu: "Pull Requests", def: "CmdOrCtrl+3"},
	{id: "show-head", group: groupCommits, label: "Show HEAD", def: "CmdOrCtrl+Shift+H"},
	{id: "prev-commit", group: groupCommits, label: "Select Previous Commit"},
	{id: "next-commit", group: groupCommits, label: "Select Next Commit"},
	{id: "copy-hash", group: groupCommits, label: "Copy Hash of the Commit Chosen"},
	{id: "pane-commit", group: groupCommits, label: "Summary Tab (of a Commit or a Pull Request)", menu: "Summary", def: "Alt+1"},
	{id: "pane-changes", group: groupCommits, label: "Changes Tab (Timeline of a Pull Request)", menu: "Changes", def: "Alt+2"},
	{id: "pane-tree", group: groupCommits, label: "File Tree Tab (Code of a Pull Request)", menu: "File Tree", def: "Alt+3"},
	{id: "toggle-panel", group: groupCommits, label: "Toggle Commit Panel", def: "CmdOrCtrl+J"},
	{id: "maximize-panel", group: groupCommits, label: "Maximize the Commit Panel", def: "CmdOrCtrl+Enter", scope: "commits", key: true},
	{id: "open-commit-window", group: groupCommits, label: "Open Commit in New Window"},
	{id: "hide-tags", group: groupCommits, label: "Hide Tags"},
	{id: "hide-stashes", group: groupCommits, label: "Hide Stashes in Commit List"},
	{id: "first-parent", group: groupCommits, label: "Collapse All Merges (First Parent only)"},
	{id: "filter-current-branch", group: groupCommits, label: "Filter Current Branch"},

	{id: "toggle-sidebar", group: groupView, label: "Toggle Sidebar", def: "CmdOrCtrl+B"},
	{id: "code-bigger", group: groupView, label: "Increase Code Font Size", menu: "Bigger"},
	{id: "code-smaller", group: groupView, label: "Decrease Code Font Size", menu: "Smaller"},
	{id: "code-reset", group: groupView, label: "Reset Code Font Size", menu: "Actual Size"},

	{id: "diff-split", group: groupDiff, label: "Diff Side by Side", menu: "Side by Side"},
	{id: "diff-unified", group: groupDiff, label: "Diff Unified", menu: "Unified"},
	{id: "diff-toggle", group: groupDiff, label: "Switch Between Side by Side and Unified", menu: "Switch Layout"},
	{id: "ignore-whitespace", group: groupDiff, label: "Ignore Whitespace"},
	{id: "wrap-lines", group: groupDiff, label: "Wrap Lines"},
	{id: "submit-comment", group: groupDiff, label: "Submit a Comment or a Reply", def: "CmdOrCtrl+Enter", scope: "comment", key: true},

	{id: "pr-tab-summary", group: groupPulls, label: "Pull Request: Summary Tab", menu: "Summary"},
	{id: "pr-tab-timeline", group: groupPulls, label: "Pull Request: Timeline Tab", menu: "Timeline"},
	{id: "pr-tab-code", group: groupPulls, label: "Pull Request: Code Tab", menu: "Code"},
	{id: "pr-tab-guide", group: groupPulls, label: "Pull Request: Guide Tab", menu: "Guide", def: "Alt+4"},
	{id: "pr-next", group: groupPulls, label: "Next Pull Request", menu: "Next Pull Request"},
	{id: "pr-prev", group: groupPulls, label: "Previous Pull Request", menu: "Previous Pull Request"},
	{id: "pr-reload", group: groupPulls, label: "Read the Pull Request Again", menu: "Read Again"},
	{id: "pr-checkout", group: groupPulls, label: "Check Out the Pull Request", menu: "Check Out"},
	{id: "pr-web", group: groupPulls, label: "Open the Pull Request on GitHub", menu: "Open on GitHub"},
	{id: "pr-ready", group: groupPulls, label: "Mark Ready for Review, or Convert to Draft", menu: "Ready for Review / Convert to Draft"},
	{id: "pr-tree", group: groupPulls, label: "Toggle the Files Tree of the Code", menu: "Files Tree"},
	{id: "pr-copy-link", group: groupPulls, label: "Copy the Link to the Pull Request", def: "CmdOrCtrl+Shift+C", scope: "pulls", key: true},
	{id: "pr-copy-number", group: groupPulls, label: "Copy the Number of the Pull Request", def: "CmdOrCtrl+Shift+K", scope: "pulls", key: true},
	{id: "pr-expand", group: groupPulls, label: "Expand the Pull Request, or Show the List Again", def: "CmdOrCtrl+Enter", scope: "pulls", key: true},
	{id: "pr-leave", group: groupPulls, label: "Leave the Pull Requests Page", def: "Escape", scope: "pulls", key: true},
}

// actionGroups are the groups of the actions, in order.
var actionGroups = []string{groupApp, groupTabs, groupRepo, groupChanges, groupCommits, groupView, groupDiff, groupPulls}

// bindableByID is the action with an ID.
func bindableByID(id string) (bindable, bool) {
	i := slices.IndexFunc(bindables, func(a bindable) bool { return a.id == id })
	if i < 0 {
		return bindable{}, false
	}
	return bindables[i], true
}

// menuLabel is what the menu item of the action says.
func (ac bindable) menuLabel() string {
	if ac.menu != "" {
		return ac.menu
	}
	return ac.label
}

// unboundMark stores that a binding was cleared, as against never changed.
const unboundMark = "none"

// accel is the accelerator of an action: what the user chose, else the
// default; "" for none.
func (a *app) accel(id string) string {
	ac, ok := bindableByID(id)
	if !ok {
		panic("app: no action " + id)
	}
	return accelOf(ac, a.store.settings().Keybinds)
}

func accelOf(ac bindable, overrides map[string]string) string {
	if v, ok := overrides[ac.id]; ok {
		if v == unboundMark {
			return ""
		}
		return v
	}
	return ac.def
}

// chord is the key of an bindable, if it has one.
func (a *app) chord(id string) (chord, bool) { return parseAccel(a.accel(id)) }

// A shortcutter is something a key can be pressed in: the window's context, or
// a focused element.
type shortcutter interface {
	Shortcut(mods ui.Modifiers, key ui.Key) bool
}

// pressed reports whether the key of an action was pressed in s.
func (a *app) pressed(s shortcutter, id string) bool {
	ch, ok := a.chord(id)
	return ok && s.Shortcut(ch.mods, ch.key)
}

// menuAccel is the accelerator a menu item carries: none while a binding is
// being recorded, so that the menu does not take the keys pressed for it.
func (a *app) menuAccel(id string) string {
	if a.recording != "" {
		return ""
	}
	return a.accel(id)
}

// overlap reports whether two actions can be pressed in the same place, so
// that they cannot share a chord.
func overlap(x, y bindable) bool {
	return x.scope == "" || y.scope == "" || x.scope == y.scope
}

// holders are the other actions that have a chord where action ac acts.
func holders(ac bindable, ch chord, overrides map[string]string) []bindable {
	var held []bindable
	for _, o := range bindables {
		if o.id == ac.id || !overlap(ac, o) {
			continue
		}
		if oc, ok := parseAccel(accelOf(o, overrides)); ok && oc == ch {
			held = append(held, o)
		}
	}
	return held
}

// chordProblem says why a chord cannot be an action's key, "" when it can.
func chordProblem(ac bindable, ch chord) string {
	if ch.key == ui.KeyEscape && ch.mods == 0 && ac.scope == "" {
		return "Escape is for leaving things"
	}
	if def, _ := parseAccel(ac.def); ch != def {
		if why := ch.reservedFor(); why != "" {
			return fmt.Sprintf("%s is kept for %s", ch.display(), why)
		}
	}
	if ch.typing() {
		return fmt.Sprintf("%s would be typed into the fields", ch.display())
	}
	// The menu takes a key before the text fields do: without a modifier, it
	// would eat Delete or the arrows while one is edited.
	if !ac.key && ch.mods&^ui.Shift == 0 && !strings.HasPrefix(nameOfKey(ch.key), "F") {
		return "Use a modifier key, ⌘, ⌃ or ⌥, with it"
	}
	return ""
}

// setKey binds an action to a chord, or to none, and says which other actions
// lost the chord. The menu follows.
func (a *app) setKey(id string, ch chord, bound bool) (lost []string) {
	ac, ok := bindableByID(id)
	if !ok {
		return nil
	}
	a.store.update(func(s *Settings) {
		over := cloneBinds(s.Keybinds)
		if bound {
			for _, o := range holders(ac, ch, over) {
				over[o.id] = unboundMark
				if o.def == "" {
					delete(over, o.id)
				}
				lost = append(lost, o.label)
			}
			over[id] = ch.accel()
			if over[id] == ac.def {
				delete(over, id)
			}
		} else {
			over[id] = unboundMark
			if ac.def == "" {
				delete(over, id)
			}
		}
		s.Keybinds = over
	})
	a.rebuildMenu()
	return lost
}

// resetKey gives an action its own binding back.
func (a *app) resetKey(id string) { a.resetKeys(func(ac bindable) bool { return ac.id == id }) }

// resetKeys gives the bindings of the actions that match theirs back.
func (a *app) resetKeys(match func(bindable) bool) {
	a.store.update(func(s *Settings) {
		over := cloneBinds(s.Keybinds)
		for _, ac := range bindables {
			if match(ac) {
				delete(over, ac.id)
			}
		}
		if len(over) == 0 {
			over = nil
		}
		s.Keybinds = over
	})
	a.rebuildMenu()
}

func cloneBinds(m map[string]string) map[string]string {
	out := make(map[string]string, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// keyText is the binding of an action as the interface shows it, "" for none.
func (a *app) keyText(id string) string { return displayAccel(a.accel(id)) }

// hint is the binding of an action in brackets, for a tooltip: " (⌘B)".
func (a *app) hint(id string) string {
	if k := a.keyText(id); k != "" {
		return " (" + k + ")"
	}
	return ""
}

// keyHint is a line of help that starts with the binding of an action; it is
// empty when the action has none.
func (a *app) keyHint(id, does string) string {
	if k := a.keyText(id); k != "" {
		return k + " " + does
	}
	return ""
}

// pressedDelete reports whether the key that unstages or discards the files
// chosen was pressed in s: Backspace does it too while Delete is its key.
func (a *app) pressedDelete(s shortcutter) bool {
	if a.pressed(s, "discard-selected") {
		return true
	}
	ac, _ := bindableByID("discard-selected")
	return a.accel("discard-selected") == ac.def && s.Shortcut(0, ui.KeyBackspace)
}

// keyed shows the binding of an action beside a menu item, and has the item
// answer to it.
func (a *app) keyed(it *ui.MenuItem, id string) *ui.MenuItem {
	if ch, ok := a.chord(id); ok {
		return it.Shortcut(ch.mods, ch.key)
	}
	return it
}
