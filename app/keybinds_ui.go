package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// keybindsTab is the index of the Keybinds tab of the settings.
const keybindsTab = 4

// scopeNames say where a key acts, for the keys that act in one place.
var scopeNames = map[string]string{
	"commits": "commit panel",
	"changes": "Local Changes",
	"files":   "lists of files",
	"pulls":   "pull requests page",
	"comment": "comment boxes",
}

// startRecording waits for the key of an action. The menu drops its
// accelerators while it waits, so that it does not take the keys pressed.
func (a *app) startRecording(id string) {
	a.recording, a.keyNote, a.keyBad = id, "", false
	a.rebuildMenu()
}

// stopRecording stops waiting for a key, and gives the menu its accelerators
// back.
func (a *app) stopRecording() {
	if a.recording == "" {
		return
	}
	a.recording = ""
	a.rebuildMenu()
}

// recordKey takes the key pressed for the action being recorded: Escape stops,
// a chord that cannot be used says why and waits for another.
func (a *app) recordKey(c *ui.Context) {
	if c.Shortcut(0, ui.KeyEscape) {
		a.stopRecording()
		a.keyNote, a.keyBad = "", false
		return
	}
	ch, ok := pressedChord(c)
	if !ok {
		return
	}
	ac, _ := bindableByID(a.recording)
	if why := chordProblem(ac, ch); why != "" {
		a.keyNote, a.keyBad = why+". Try another key, or Escape to stop.", true
		return
	}
	lost := a.setKey(ac.id, ch, true)
	a.keyNote, a.keyBad = strings.TrimSuffix(ac.label, "…")+" is now "+ch.display()+".", false
	if len(lost) > 0 {
		for i := range lost {
			lost[i] = strings.TrimSuffix(lost[i], "…")
		}
		a.keyNote = strings.TrimSuffix(ac.label, "…") + " is now " + ch.display() + "; " + strings.Join(lost, ", ") + " lost it."
	}
	a.stopRecording()
}

// keybindsHeader is the field that narrows the actions, the button that gives
// every key back, and what the last change did.
func (a *app) keybindsHeader(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(8).Shrink(0).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("keybinds-filter"), &a.keyFilter).Placeholder("Filter the actions, or the keys").Grow(1).Label("Filter keybinds")
			if ui.Button(c.Key("keybinds-reset-all"), "Reset All").Tooltip("Give every action its own key back").Clicked() {
				a.stopRecording()
				a.resetKeys(func(bindable) bool { return true })
				a.keyNote, a.keyBad = "Every key is back to its default.", false
				a.keysChanged()
			}
		})
		switch {
		case a.recording != "":
			ac, _ := bindableByID(a.recording)
			ui.Text(c, "Press the keys for “"+strings.TrimSuffix(ac.label, "…")+"”. Escape stops.").FontSize(12).TextColor(t.Accent)
		case a.keyNote != "":
			col := t.TextMuted
			if a.keyBad {
				col = t.Danger
			}
			ui.Text(c, a.keyNote).FontSize(12).TextColor(col)
		default:
			ui.Text(c, "Click a key, then press the keys to use. Actions without a key have none until you set one.").FontSize(12).TextColor(t.TextMuted)
		}
	})
}

// keysChanged draws the main window again: its tooltips name the keys.
func (a *app) keysChanged() {
	if a.win != nil {
		a.win.Invalidate()
	}
}

// keybindsPrefs lists the actions by group, each with its key.
func (a *app) keybindsPrefs(c *ui.Context) {
	t := c.Theme()
	over := a.store.settings().Keybinds
	q := strings.ToLower(strings.TrimSpace(a.keyFilter))
	shown := 0
	for _, group := range actionGroups {
		var rows []bindable
		for _, ac := range bindables {
			if ac.group != group {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(ac.label+" "+group+" "+displayAccel(accelOf(ac, over))), q) {
				continue
			}
			rows = append(rows, ac)
		}
		if len(rows) == 0 {
			continue
		}
		shown += len(rows)
		ui.Text(c.Key("kg-"+group), group).FontSize(12).FontWeight(600).TextColor(t.TextMuted).Padding(8, 0, 0)
		for _, ac := range rows {
			a.keybindRow(c.Key("kb-"+ac.id), ac, over)
		}
	}
	if shown == 0 {
		ui.Text(c, "No action matches.").TextColor(t.TextMuted)
	}
	ui.Text(c, "The arrows, Enter, Escape and Tab inside lists, dialogs and text fields, and ⌘A, ⌘C, ⌘V, ⌘X, ⌘Z, ⌘H, ⌘M and ⌘Q, are not remappable.").
		FontSize(11).TextColor(t.TextMuted).Padding(10, 0, 0)
}

// keybindRow is an action, the button that records its key, and the buttons
// that give it back its own or take it off.
func (a *app) keybindRow(c *ui.Context, ac bindable, over map[string]string) {
	t := c.Theme()
	accel := accelOf(ac, over)
	_, changed := over[ac.id]
	recording := a.recording == ac.id
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).MinHeight(28).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, strings.TrimSuffix(ac.label, "…")).FontSize(13).Shrink(1).MinWidth(0)
			if n := scopeNames[ac.scope]; n != "" {
				ui.Text(c, n).FontSize(11).TextColor(t.TextMuted).Shrink(0)
			}
		})
		key := ui.ButtonBase(c.Key("key")).Width(150).Height(26).Radius(6).Center().Shrink(0).Border(1, hairline).
			Label(strings.TrimSuffix(ac.label, "…") + " key").Tooltip("Click, then press the keys to use")
		text, col := displayAccel(accel), t.Text
		switch {
		case recording:
			key.Border(1, t.Accent)
			text, col = "Press keys…", t.Accent
		case text == "":
			text, col = "Not set", t.TextMuted
			if key.Hovered() {
				key.Background(hoverBg)
			}
		default:
			if key.Hovered() {
				key.Background(hoverBg)
			}
		}
		key.TextColor(col)
		if key.Clicked() {
			if recording {
				a.stopRecording()
			} else {
				a.startRecording(ac.id)
			}
		}
		key.Children(func() { ui.Text(c, text).FontSize(13).FontWeight(500) })

		// Both buttons keep their place when they are not needed, so that the
		// keys stay in a column.
		if changed {
			if iconButton(c.Key("reset"), iconUndo, "Give “"+strings.TrimSuffix(ac.label, "…")+"” its own key back").Size(26, 26).Clicked() {
				a.stopRecording()
				a.resetKey(ac.id)
				a.keyNote, a.keyBad = "", false
				a.keysChanged()
			}
		} else {
			ui.Box(c.Key("reset")).Size(26, 26).Shrink(0)
		}
		if accel != "" {
			if iconButton(c.Key("clear"), iconClose, "Take the key off “"+strings.TrimSuffix(ac.label, "…")+"”").Size(26, 26).Clicked() {
				a.stopRecording()
				a.setKey(ac.id, chord{}, false)
				a.keyNote, a.keyBad = "", false
				a.keysChanged()
			}
		} else {
			ui.Box(c.Key("clear")).Size(26, 26).Shrink(0)
		}
	})
}
