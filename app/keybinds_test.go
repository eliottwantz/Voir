package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestAcceleratorsRoundTrip(t *testing.T) {
	for _, accel := range []string{"CmdOrCtrl+Shift+F", "Alt+1", "CmdOrCtrl+Alt+B", "Ctrl+Shift+Tab", "CmdOrCtrl+Shift+]", "F5", "Delete", "CmdOrCtrl+,", "CmdOrCtrl+Enter"} {
		ch, ok := parseAccel(accel)
		if !ok {
			t.Errorf("%q is not a chord", accel)
			continue
		}
		if got := ch.accel(); !strings.EqualFold(got, accel) {
			t.Errorf("%q came back as %q", accel, got)
		}
	}
	for _, bad := range []string{"", "CmdOrCtrl+", "Hyper+A", "CmdOrCtrl+Shift+Nope"} {
		if _, ok := parseAccel(bad); ok {
			t.Errorf("%q is a chord", bad)
		}
	}
	if ch, _ := parseAccel("cmd+shift+f"); ch != (chord{ui.Super | ui.Shift, ui.KeyF}) {
		t.Errorf("cmd+shift+f is %+v", ch)
	}
	if got := displayAccel("CmdOrCtrl+Shift+F"); got != "⇧⌘F" && got != "Ctrl+Shift+F" {
		t.Errorf("shown as %q", got)
	}
}

func TestEveryActionIsInTheMenuOrReadFromTheKeyboard(t *testing.T) {
	a, _ := testApp(t)
	menu := a.menuTemplate()
	ids := map[string]bool{}
	for _, ac := range bindables {
		if ids[ac.id] {
			t.Errorf("%s is twice in the actions", ac.id)
		}
		ids[ac.id] = true
		if ac.label == "" || !slices_contains(actionGroups, ac.group) {
			t.Errorf("%s has label %q and group %q", ac.id, ac.label, ac.group)
		}
		if _, ok := parseAccel(ac.def); ac.def != "" && !ok {
			t.Errorf("%s has default %q, which is no key", ac.id, ac.def)
		}
		item := findOrNil(menu, ac.id)
		switch {
		case ac.key && item != nil && ac.scope != "":
			t.Errorf("%s is read from the keyboard where it acts, but is in the menu", ac.id)
		case !ac.key && item == nil:
			t.Errorf("%s is no item of the menu", ac.id)
		case item != nil && item.Label != ac.menuLabel():
			t.Errorf("%s is %q in the menu, %q in the actions", ac.id, item.Label, ac.menuLabel())
		case item != nil && !ac.key && item.Accelerator != wantAccel(ac):
			t.Errorf("%s has %q in the menu, %q by default", ac.id, item.Accelerator, ac.def)
		}
	}
	walk(menu, func(it *mygo.MenuItem) {
		if it.ID != "" && !ids[it.ID] {
			t.Errorf("menu item %q (%s) is no action", it.ID, it.Label)
		}
	})
}

// wantAccel is what the menu item of an action carries by default: the keys
// with Option alone are read from the keyboard instead.
func wantAccel(ac bindable) string {
	if ch, ok := parseAccel(ac.def); ok && routed(ch) {
		return ""
	}
	return ac.def
}

func slices_contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func findOrNil(items []*mygo.MenuItem, id string) *mygo.MenuItem {
	var found *mygo.MenuItem
	walk(items, func(it *mygo.MenuItem) {
		if it.ID == id {
			found = it
		}
	})
	return found
}

func TestNoTwoActionsStartWithTheSameKeyWhereTheyAct(t *testing.T) {
	for i, x := range bindables {
		xc, ok := parseAccel(x.def)
		if !ok {
			continue
		}
		for _, y := range bindables[i+1:] {
			if yc, ok := parseAccel(y.def); ok && yc == xc && overlap(x, y) {
				t.Errorf("%s and %s both start with %s", x.id, y.id, xc.display())
			}
		}
	}
}

func TestNewActionsHaveNoKeyUntilOneIsSet(t *testing.T) {
	a, _ := testApp(t)
	for _, id := range []string{"quick-fetch", "commit", "commit-push", "stage-all", "amend", "ai-message", "next-commit", "pr-next", "code-bigger", "diff-toggle", "go-tab-1"} {
		if got := a.accel(id); got != "" {
			t.Errorf("%s has %q", id, got)
		}
		if item := find(t, a.menuTemplate(), id); item.Accelerator != "" {
			t.Errorf("the menu item of %s has %q", id, item.Accelerator)
		}
	}
}

func TestSettingAKeyMovesTheMenuAndTakesItFromTheActionThatHadIt(t *testing.T) {
	a, _ := testApp(t)
	ch := chord{ui.Cmd | ui.Shift, ui.KeyF}
	// ⇧⌘F is Fetch…: giving it to Quick Fetch takes it from Fetch.
	lost := a.setKey("quick-fetch", ch, true)
	if len(lost) != 1 || lost[0] != "Fetch…" {
		t.Errorf("lost %q", lost)
	}
	menu := a.menuTemplate()
	if got := find(t, menu, "quick-fetch").Accelerator; got != "CmdOrCtrl+Shift+F" {
		t.Errorf("Quick Fetch has %q", got)
	}
	if got := find(t, menu, "fetch").Accelerator; got != "" {
		t.Errorf("Fetch has %q", got)
	}
	// Reset gives Fetch its key back, and the menu follows.
	a.resetKey("fetch")
	if got := a.accel("fetch"); got != "CmdOrCtrl+Shift+F" {
		t.Errorf("Fetch has %q after reset", got)
	}
	// Taking a key off an action with a default keeps it off.
	a.setKey("new-tab", chord{}, false)
	if got := find(t, a.menuTemplate(), "new-tab").Accelerator; got != "" {
		t.Errorf("New Tab has %q", got)
	}
	a.resetKeys(func(bindable) bool { return true })
	if got := a.accel("new-tab"); got != "CmdOrCtrl+T" {
		t.Errorf("New Tab has %q after Reset All", got)
	}
	if over := a.store.settings().Keybinds; len(over) != 0 {
		t.Errorf("Reset All left %v", over)
	}
}

func TestKeysInDifferentPlacesCanBeTheSame(t *testing.T) {
	a, _ := testApp(t)
	// ⌘↩ commits in Local Changes, maximizes the commit panel and expands a
	// pull request: three places, one key.
	for _, id := range []string{"commit-box", "maximize-panel", "pr-expand", "submit-comment"} {
		if got := a.accel(id); got != "CmdOrCtrl+Enter" {
			t.Errorf("%s has %q", id, got)
		}
	}
	// A key of the menu is everywhere, so it takes the key from them all.
	lost := a.setKey("commit", chord{ui.Cmd, ui.KeyEnter}, true)
	if len(lost) != 4 {
		t.Errorf("lost %q", lost)
	}
	if got := a.accel("pr-expand"); got != "" {
		t.Errorf("pr-expand has %q", got)
	}
}

func TestKeysThatCannotBeUsed(t *testing.T) {
	quickFetch, _ := bindableByID("quick-fetch")
	nextTab, _ := bindableByID("next-tab")
	discard, _ := bindableByID("discard-selected")
	for _, c := range []struct {
		ac   bindable
		ch   chord
		bad  bool
		name string
	}{
		{quickFetch, chord{0, ui.KeyA}, true, "a letter alone"},
		{quickFetch, chord{ui.Shift, ui.KeyA}, true, "a capital"},
		{quickFetch, chord{0, ui.KeyDelete}, true, "Delete would be eaten by the menu"},
		{quickFetch, chord{0, ui.KeyF5}, false, "a function key"},
		{quickFetch, chord{ui.Cmd, ui.KeyQ}, true, "Quit"},
		{quickFetch, chord{ui.Cmd, ui.KeyC}, true, "Copy"},
		{quickFetch, chord{ui.Cmd | ui.Alt, ui.KeyF}, false, "a free key"},
		{quickFetch, chord{0, ui.KeyEscape}, true, "Escape"},
		{nextTab, chord{0, ui.KeyTab}, false, "a key read where it acts may be bare when it makes no text"},
		{nextTab, chord{0, ui.KeyN}, true, "n alone is typing"},
		{discard, chord{ui.Cmd, ui.KeyBackspace}, false, "Cmd+Backspace"},
	} {
		if got := chordProblem(c.ac, c.ch) != ""; got != c.bad {
			t.Errorf("%s: problem %q", c.name, chordProblem(c.ac, c.ch))
		}
	}
	// An action may keep a default that is kept for the system elsewhere.
	copyPath, _ := bindableByID("copy-path")
	if why := chordProblem(copyPath, chord{ui.Cmd, ui.KeyC}); why != "" {
		t.Errorf("copy-path cannot keep ⌘C: %s", why)
	}
}

func TestKeybindsAreKept(t *testing.T) {
	a, _ := testApp(t)
	a.setKey("quick-fetch", chord{ui.Cmd | ui.Alt, ui.KeyF}, true)
	a.setKey("new-tab", chord{}, false)
	b, err := json.Marshal(a.store.data)
	if err != nil {
		t.Fatal(err)
	}
	var back stored
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	b2 := newApp(&store{data: back})
	if got := b2.accel("quick-fetch"); got != "CmdOrCtrl+Alt+F" {
		t.Errorf("Quick Fetch has %q after a restart", got)
	}
	if got := b2.accel("new-tab"); got != "" {
		t.Errorf("New Tab has %q after a restart", got)
	}
	if got := b2.accel("close-tab"); got != "CmdOrCtrl+W" {
		t.Errorf("Close Tab has %q after a restart", got)
	}
}

func TestKeyboardActionsFollowTheirKeys(t *testing.T) {
	a, _, _ := prApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tab.showChanges()
	a.showPulls()
	tt.Frame()
	tab.openPR(3)
	tt.Frame()
	tab.pr.tab = prTabSummary

	// Escape leaves the pull requests page ... until the key is taken off.
	a.setKey("pr-leave", chord{}, false)
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if !a.pulls {
		t.Fatalf("Escape left the page with its key taken off")
	}
	// ... and the key can be another.
	a.setKey("pr-leave", chord{ui.Cmd, ui.KeyL}, true)
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if !a.pulls {
		t.Fatalf("Escape left the page, which now answers to ⌘L")
	}
	tt.Key(ui.Cmd, ui.KeyL)
	tt.Frame()
	if a.pulls {
		t.Fatalf("⌘L did not leave the page")
	}
}

func TestKeybindsSettingsRecordKeys(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.prefsView, 620, 560)
	a.prefsTab = keybindsTab
	tt.Frame()
	for _, want := range []string{"Keybinds", "Reset All", "Quick Fetch", "New Tab", "Local Changes", "Not set"} {
		if !tt.HasText(want) {
			t.Errorf("no %q: %q", want, tt.Texts())
		}
	}

	snapshot(t, tt, "keybinds")

	// Narrow the list to Quick Fetch, and give it a key.
	a.keyFilter = "Quick Fetch"
	tt.Frame()
	if tt.HasText("New Tab") {
		t.Errorf("the filter left New Tab: %q", tt.Texts())
	}
	if err := tt.Click("Not set"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.recording != "quick-fetch" || !tt.HasText("Press keys…") {
		t.Fatalf("recording %q: %q", a.recording, tt.Texts())
	}
	// The menu leaves its keys alone while one is being recorded.
	if got := find(t, a.menuTemplate(), "new-tab").Accelerator; got != "" {
		t.Errorf("New Tab has %q while recording", got)
	}
	// A key that cannot be used says why, and keeps waiting.
	tt.Key(0, ui.KeyA)
	tt.Frame()
	if a.recording != "quick-fetch" || !a.keyBad {
		t.Fatalf("recording %q, note %q", a.recording, a.keyNote)
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyF)
	tt.Frame()
	if a.recording != "" || a.accel("quick-fetch") != "CmdOrCtrl+Alt+F" {
		t.Fatalf("recording %q, key %q, note %q", a.recording, a.accel("quick-fetch"), a.keyNote)
	}
	if got := find(t, a.menuTemplate(), "new-tab").Accelerator; got != "CmdOrCtrl+T" {
		t.Errorf("New Tab has %q after recording", got)
	}

	// Escape stops without changing anything, and does not close the window.
	if err := tt.Click(displayAccel("CmdOrCtrl+Alt+F")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.recording != "" || a.accel("quick-fetch") != "CmdOrCtrl+Alt+F" {
		t.Errorf("recording %q, key %q", a.recording, a.accel("quick-fetch"))
	}

	// The buttons beside the key take it off, and give back the default.
	a.keyFilter = "New Tab"
	tt.Frame()
	if err := tt.Click("Take the key off “New Tab”"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.accel("new-tab") != "" || !tt.HasText("Not set") {
		t.Fatalf("New Tab has %q: %q", a.accel("new-tab"), tt.Texts())
	}
	if err := tt.Click("Give “New Tab” its own key back"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.accel("new-tab") != "CmdOrCtrl+T" {
		t.Fatalf("New Tab has %q", a.accel("new-tab"))
	}
}

// run chooses the menu item of an action, as its key does.
func run(t *testing.T, a *app, id string) {
	t.Helper()
	it := find(t, a.menuTemplate(), id)
	if it.Click == nil {
		t.Fatalf("%s does nothing", id)
	}
	it.Click(it, nil)
}

func TestNewActionsDoTheirThing(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tt.Frame()

	// Select Next and Previous Commit walk the graph.
	tab.selectRow(0)
	run(t, a, "next-commit")
	if tab.sel != 1 {
		t.Errorf("next commit: row %d", tab.sel)
	}
	run(t, a, "prev-commit")
	run(t, a, "prev-commit")
	if tab.sel != 0 {
		t.Errorf("previous commit stays on the first: row %d", tab.sel)
	}
	run(t, a, "copy-hash")
	if tab.commit == nil || a.copied != tab.commit.Hash {
		t.Errorf("copied %q", a.copied)
	}

	// The font size has limits, and a way back.
	size := a.store.settings().CodeFontSize
	run(t, a, "code-bigger")
	if got := a.store.settings().CodeFontSize; got != size+1 {
		t.Errorf("bigger: %v from %v", got, size)
	}
	run(t, a, "code-smaller")
	run(t, a, "code-smaller")
	run(t, a, "code-reset")
	if got := a.store.settings().CodeFontSize; got != defaultSettings().CodeFontSize {
		t.Errorf("reset: %v", got)
	}
	for range 60 {
		run(t, a, "code-smaller")
	}
	if got := a.store.settings().CodeFontSize; got < 6 {
		t.Errorf("smaller went to %v", got)
	}

	// The layout of the diffs switches between the two.
	run(t, a, "diff-toggle")
	if got := a.store.settings().DiffLayout; got != "unified" {
		t.Errorf("layout %q", got)
	}
	run(t, a, "diff-toggle")
	if got := a.store.settings().DiffLayout; got != "split" {
		t.Errorf("layout %q", got)
	}

	// Committing shows Local Changes first, where the message is.
	run(t, a, "commit")
	if !tab.onWip {
		t.Errorf("commit did not show Local Changes")
	}
}

func TestGoToTabChoosesTheTabByItsNumber(t *testing.T) {
	a, _ := testApp(t)
	second := demoRepo(t)
	if err := a.openRepo(second.Root); err != nil {
		t.Fatal(err)
	}
	third := demoRepo(t)
	if err := a.openRepo(third.Root); err != nil {
		t.Fatal(err)
	}
	run(t, a, "go-tab-1")
	if a.cur != 0 {
		t.Errorf("tab %d", a.cur)
	}
	run(t, a, "go-tab-2")
	if a.cur != 1 {
		t.Errorf("tab %d", a.cur)
	}
	run(t, a, "go-tab-9") // there is none
	if a.cur != 1 {
		t.Errorf("a tab that is not there moved to %d", a.cur)
	}
	run(t, a, "last-tab")
	if a.cur != 2 {
		t.Errorf("last tab is %d", a.cur)
	}
	a.stepTab(1)
	if a.cur != 0 {
		t.Errorf("next tab is %d", a.cur)
	}
}

func TestPullRequestActionsActOnTheOneShown(t *testing.T) {
	a, _, _ := prApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	a.showPulls()
	tt.Frame()
	// Without a pull request, they do nothing.
	run(t, a, "pr-tab-code")
	run(t, a, "pr-tree")
	tab.openPR(3)
	tt.Frame()
	for id, want := range map[string]int{"pr-tab-code": prTabCode, "pr-tab-timeline": prTabTimeline, "pr-tab-guide": prTabGuide, "pr-tab-summary": prTabSummary} {
		run(t, a, id)
		if tab.pr.tab != want {
			t.Errorf("%s: tab %d, want %d", id, tab.pr.tab, want)
		}
	}
	tree := a.prTree
	run(t, a, "pr-tree")
	if a.prTree == tree {
		t.Errorf("the files tree did not switch")
	}
	// Off the pull requests page they do not change the pull request.
	a.pulls = false
	tab.pr.tab = prTabSummary
	run(t, a, "pr-tab-code")
	if tab.pr.tab == prTabCode {
		t.Errorf("a pull request tab was chosen off the page")
	}
}

// ⌥ with a digit is read from the keyboard: the menu of macOS does not take
// keys that have Option but no Command or Control.
func TestOptionDigitsChooseTheTabsOfACommit(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tt.Frame()
	tab.selectRow(0)
	tt.Frame()
	for _, c := range []struct {
		key  ui.Key
		pane panelTab
	}{{ui.Key3, paneTree}, {ui.Key2, paneChanges}, {ui.Key1, paneCommit}} {
		tab.cv.pane = -1
		tt.Key(ui.Alt, c.key)
		tt.Frame()
		if tab.cv.pane != c.pane {
			t.Errorf("⌥%d chose pane %d, want %d", c.key-ui.Key0, tab.cv.pane, c.pane)
		}
	}

	// In a window of its own, the same keys choose its tabs.
	open := find(t, a.menuTemplate(), "open-commit-window")
	open.Click(open, nil)
	cw := a.cwins[0]
	cw.win = &mygo.Window{}
	wt := ui.NewTester(cw.view, 1120, 760)
	wt.Frame()
	cw.cv.pane = -1
	wt.Key(ui.Alt, ui.Key3)
	wt.Frame()
	if cw.cv.pane != paneTree {
		t.Errorf("⌥3 chose pane %d in the commit's window", cw.cv.pane)
	}
}

func TestOptionDigitsChooseTheTabsOfAPullRequest(t *testing.T) {
	a, _, _ := prApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	a.showPulls()
	tt.Frame()
	tab.openPR(3)
	tt.Frame()
	for _, c := range []struct {
		key ui.Key
		tab int
	}{{ui.Key2, prTabTimeline}, {ui.Key3, prTabCode}, {ui.Key4, prTabGuide}, {ui.Key1, prTabSummary}} {
		tt.Key(ui.Alt, c.key)
		tt.Frame()
		if tab.pr.tab != c.tab {
			t.Errorf("⌥%d chose tab %d, want %d", c.key-ui.Key0, tab.pr.tab, c.tab)
		}
	}
	// A key the user moved elsewhere follows.
	a.setKey("pr-tab-code", chord{ui.Alt | ui.Shift, ui.Key9}, true)
	tt.Key(ui.Alt|ui.Shift, ui.Key9)
	tt.Frame()
	if tab.pr.tab != prTabCode {
		t.Errorf("⌥⇧9 chose tab %d", tab.pr.tab)
	}
}

func TestTheSameKeyActsInTheCommitPanelAndOnAPullRequest(t *testing.T) {
	a, _, _ := prApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tab := a.tab()
	tab.showHistory()
	tab.selectRow(0)
	tt.Frame()
	// Both start with ⌥1 to ⌥3, and setting one does not take it from the other.
	for _, pair := range [][2]string{{"pane-commit", "pr-tab-summary"}, {"pane-changes", "pr-tab-timeline"}, {"pane-tree", "pr-tab-code"}} {
		if a.accel(pair[0]) == "" || a.accel(pair[0]) != a.accel(pair[1]) {
			t.Errorf("%s has %q, %s has %q", pair[0], a.accel(pair[0]), pair[1], a.accel(pair[1]))
		}
	}
	if lost := a.setKey("pr-tab-code", chord{ui.Alt, ui.Key3}, true); len(lost) != 0 {
		t.Errorf("lost %q", lost)
	}
	// On the commits, the key chooses the tab of the commit and leaves the pull
	// request alone ...
	tab.cv.pane, tab.pr = -1, nil
	tt.Key(ui.Alt, ui.Key3)
	tt.Frame()
	if tab.cv.pane != paneTree {
		t.Errorf("⌥3 chose pane %d", tab.cv.pane)
	}
	// ... and on the pull request, the other way round.
	a.showPulls()
	tt.Frame()
	tab.openPR(3)
	tt.Frame()
	tab.pr.tab, tab.cv.pane = prTabSummary, paneCommit
	tt.Key(ui.Alt, ui.Key3)
	tt.Frame()
	if tab.pr.tab != prTabCode || tab.cv.pane != paneCommit {
		t.Errorf("⌥3 chose tab %d of the pull request and pane %d of the commit", tab.pr.tab, tab.cv.pane)
	}
	// A key of the menu shared by two places is not the menu's, which cannot
	// tell them apart.
	for _, id := range []string{"pane-tree", "pr-tab-code"} {
		if it := find(t, a.menuTemplate(), id); it.Accelerator != "" {
			t.Errorf("%s has %q in the menu", id, it.Accelerator)
		}
	}
}

func TestTooltipsEndWithTheKeyOfTheirAction(t *testing.T) {
	a, _ := testApp(t)
	want := " (" + displayAccel("CmdOrCtrl+T") + ")"
	if got := a.tip("New tab", "new-tab"); got != "New tab"+want {
		t.Errorf("tip is %q", got)
	}
	// An action with no key adds nothing, and one that is given a key shows it.
	if got := a.tip("Stash", "stash"); got != "Stash" {
		t.Errorf("tip is %q", got)
	}
	a.setKey("stash", chord{ui.Cmd | ui.Alt, ui.KeyS}, true)
	if got := a.tip("Stash", "stash"); got != "Stash ("+displayAccel("CmdOrCtrl+Alt+S")+")" {
		t.Errorf("tip is %q", got)
	}
	// Every action the tooltips name exists.
	for _, id := range []string{"quick-fetch", "quick-pull", "quick-push", "new-branch", "stash", "new-tag", "create-pr", "diff-split", "diff-unified", "close-tab", "side-pulls", "view-changes", "view-commits", "pane-commit", "pane-changes", "pane-tree", "open-commit-window", "copy-hash", "ignore-whitespace", "wrap-lines", "copy-path", "pr-tab-summary", "pr-tab-timeline", "pr-tab-code", "pr-tab-guide", "pr-web", "pr-tree", "pr-reload", "pr-checkout", "pr-ready", "pr-leave", "stage-toggle", "amend", "ai-message", "commit-box", "commit-pr", "commit-push", "open-selected-file"} {
		if _, ok := bindableByID(id); !ok {
			t.Errorf("no action %s", id)
		}
	}
}

func TestRemoteButtonsShowTheKeyOfTheirCommand(t *testing.T) {
	a, _ := testApp(t)
	// Fetch… is ⇧⌘F; the button is Quick Fetch, which has no key yet.
	want := "Fetch (" + displayAccel("CmdOrCtrl+Shift+F") + ", with options)"
	if got := a.remoteTip("Fetch", "quick-fetch", "fetch"); got != want {
		t.Errorf("tooltip %q, want %q", got, want)
	}
	a.setKey("quick-fetch", chord{ui.Cmd | ui.Alt, ui.KeyF}, true)
	if got, want := a.remoteTip("Fetch", "quick-fetch", "fetch"), "Fetch ("+displayAccel("CmdOrCtrl+Alt+F")+")"; got != want {
		t.Errorf("tooltip %q, want %q", got, want)
	}
	a.setKey("quick-fetch", chord{}, false)
	a.setKey("fetch", chord{}, false)
	if got := a.remoteTip("Fetch", "quick-fetch", "fetch"); got != "Fetch" {
		t.Errorf("tooltip %q", got)
	}
}

func TestStageAllButtonsShowTheirKey(t *testing.T) {
	a, _ := testApp(t)
	// ⇧⌘S moves every file across; until Stage All has a key of its own, the
	// button shows that one.
	want := "Stage every file (" + displayAccel("CmdOrCtrl+Shift+S") + ")"
	if got := a.tipAny("Stage every file", "stage-all", "stage-all-toggle"); got != want {
		t.Errorf("tooltip %q, want %q", got, want)
	}
	a.setKey("stage-all", chord{ui.Cmd | ui.Alt, ui.KeyA}, true)
	if got, want := a.tipAny("Stage every file", "stage-all", "stage-all-toggle"), "Stage every file ("+displayAccel("CmdOrCtrl+Alt+A")+")"; got != want {
		t.Errorf("tooltip %q, want %q", got, want)
	}
}
