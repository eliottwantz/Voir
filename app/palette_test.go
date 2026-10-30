package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestThePaletteListsTheMenuActionsBestFirst(t *testing.T) {
	a, _ := testApp(t)
	all := a.paletteEntries("")
	ids := map[string]bool{}
	for _, e := range all {
		ids[e.ac.id] = true
	}
	for _, id := range []string{"push", "quick-fetch", "toggle-sidebar", "pr-web", "hide-tags", "go-tab-1"} {
		if !ids[id] {
			t.Errorf("%s is not in the palette", id)
		}
	}
	// What acts on the focus, and the palette itself, are not.
	for _, id := range []string{"palette", "stage-toggle", "copy-path", "submit-comment", "pr-expand"} {
		if ids[id] {
			t.Errorf("%s is in the palette", id)
		}
	}
	if got := a.paletteEntries("qpush"); len(got) == 0 || got[0].ac.id != "quick-push" {
		t.Errorf("qpush: %v", paletteIDs(got))
	}
	if got := a.paletteEntries("tog side"); len(got) == 0 || got[0].ac.id != "toggle-sidebar" {
		t.Errorf("tog side: %v", paletteIDs(got))
	}
	if got := a.paletteEntries("zzzz"); len(got) != 0 {
		t.Errorf("zzzz: %v", paletteIDs(got))
	}
}

func TestThePaletteRunsTheActionChosen(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	shown := a.sidebarShown
	run(t, a, "palette")
	tt.Frame()
	if !a.cmds.open || !tt.HasText("Command palette") {
		t.Fatalf("the palette is not open: %q", tt.Texts())
	}
	snapshot(t, tt, "palette")
	tt.Type("toggle sidebar")
	tt.Frame()
	if !tt.HasText("Toggle Sidebar") || tt.HasText("Quick Fetch") {
		t.Errorf("filtered: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.cmds.open {
		t.Error("the palette is still open")
	}
	if a.sidebarShown == shown {
		t.Error("the sidebar was not toggled")
	}

	// The arrows choose in the list while the field keeps the focus.
	a.openPalette()
	tt.Frame()
	tt.Type("diff")
	tt.Frame()
	want := a.paletteEntries("diff")[1].ac.id
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if got := a.paletteEntries("diff")[a.cmds.sel].ac.id; got != want {
		t.Errorf("down chose %s, not %s", got, want)
	}
}

func TestThePaletteHasShiftCmdPAndPushMoved(t *testing.T) {
	a, _ := testApp(t)
	if got := a.accel("palette"); got != "CmdOrCtrl+Shift+P" {
		t.Errorf("palette: %q", got)
	}
	if got := a.accel("push"); got != "CmdOrCtrl+Alt+P" {
		t.Errorf("push: %q", got)
	}
	ch, _ := parseAccel("CmdOrCtrl+Shift+P")
	if lost := a.setKey("palette", ch, true); len(lost) != 0 {
		t.Errorf("⇧⌘P is shared with %v", lost)
	}
}

func paletteIDs(es []paletteEntry) []string {
	var ids []string
	for _, e := range es {
		ids = append(ids, e.ac.id)
	}
	return ids
}
