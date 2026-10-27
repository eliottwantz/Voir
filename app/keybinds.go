package app

import (
	"runtime"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// A chord is a key with its modifiers, as a keybinding names it.
type chord struct {
	mods ui.Modifiers
	key  ui.Key
}

// keyDef names a key the way accelerators do ("a", "7", "F5", "Enter", "[").
type keyDef struct {
	key  ui.Key
	name string
}

// keyDefs are the keys a binding can use, in the order the recorder looks for
// them.
var keyDefs = func() []keyDef {
	var defs []keyDef
	for i := range 26 {
		defs = append(defs, keyDef{ui.KeyA + ui.Key(i), string(rune('a' + i))})
	}
	for i := range 10 {
		defs = append(defs, keyDef{ui.Key0 + ui.Key(i), string(rune('0' + i))})
	}
	for i := range 12 {
		defs = append(defs, keyDef{ui.KeyF1 + ui.Key(i), "F" + strconv.Itoa(i+1)})
	}
	return append(defs,
		keyDef{ui.KeyEnter, "Enter"}, keyDef{ui.KeyEscape, "Escape"}, keyDef{ui.KeyBackspace, "Backspace"},
		keyDef{ui.KeyTab, "Tab"}, keyDef{ui.KeySpace, "Space"}, keyDef{ui.KeyDelete, "Delete"},
		keyDef{ui.KeyInsert, "Insert"}, keyDef{ui.KeyHome, "Home"}, keyDef{ui.KeyEnd, "End"},
		keyDef{ui.KeyPageUp, "PageUp"}, keyDef{ui.KeyPageDown, "PageDown"},
		keyDef{ui.KeyLeft, "Left"}, keyDef{ui.KeyRight, "Right"}, keyDef{ui.KeyUp, "Up"}, keyDef{ui.KeyDown, "Down"},
		keyDef{ui.KeyMinus, "-"}, keyDef{ui.KeyEqual, "="}, keyDef{ui.KeyComma, ","}, keyDef{ui.KeyPeriod, "."},
		keyDef{ui.KeySlash, "/"}, keyDef{ui.KeySemicolon, ";"}, keyDef{ui.KeyQuote, "'"},
		keyDef{ui.KeyBracketLeft, "["}, keyDef{ui.KeyBracketRight, "]"}, keyDef{ui.KeyBackslash, "\\"},
		keyDef{ui.KeyBackquote, "`"},
	)
}()

// keyNamed is the key an accelerator names, in any case.
func keyNamed(name string) (ui.Key, bool) {
	for _, d := range keyDefs {
		if strings.EqualFold(d.name, name) {
			return d.key, true
		}
	}
	switch strings.ToLower(name) {
	case "esc":
		return ui.KeyEscape, true
	case "return":
		return ui.KeyEnter, true
	case "del":
		return ui.KeyDelete, true
	case "ins":
		return ui.KeyInsert, true
	}
	return 0, false
}

// nameOfKey is the name an accelerator gives a key.
func nameOfKey(k ui.Key) string {
	for _, d := range keyDefs {
		if d.key == k {
			return d.name
		}
	}
	return ""
}

// parseAccel reads an accelerator such as "CmdOrCtrl+Shift+F", the form menus
// and the settings use. "" and unknown names are not chords.
func parseAccel(s string) (chord, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return chord{}, false
	}
	parts := strings.Split(s, "+")
	var ch chord
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if i == len(parts)-1 {
			k, ok := keyNamed(p)
			if !ok {
				return chord{}, false
			}
			ch.key = k
			continue
		}
		switch strings.ToLower(p) {
		case "cmdorctrl", "commandorcontrol":
			ch.mods |= ui.Cmd
		case "cmd", "command", "super", "meta", "win":
			ch.mods |= ui.Super
		case "ctrl", "control":
			ch.mods |= ui.Ctrl
		case "alt", "option", "altgr":
			ch.mods |= ui.Alt
		case "shift":
			ch.mods |= ui.Shift
		default:
			return chord{}, false
		}
	}
	return ch, true
}

// accel writes a chord as an accelerator, as the menus take it.
func (ch chord) accel() string {
	var parts []string
	if ch.mods&ui.Cmd != 0 {
		parts = append(parts, "CmdOrCtrl")
	}
	// Where Command is Control, Cmd covers Ctrl, and Super is the other key.
	if runtime.GOOS == "darwin" {
		if ch.mods&ui.Ctrl != 0 {
			parts = append(parts, "Ctrl")
		}
	} else if ch.mods&ui.Super != 0 {
		parts = append(parts, "Super")
	}
	if ch.mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	if ch.mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	return strings.Join(append(parts, keyWord(nameOfKey(ch.key))), "+")
}

// display writes a chord as the interface shows it: ⇧⌘F on macOS, Ctrl+Shift+F
// elsewhere.
func (ch chord) display() string {
	name := nameOfKey(ch.key)
	if runtime.GOOS == "darwin" {
		var b strings.Builder
		for _, m := range []struct {
			mod    ui.Modifiers
			symbol string
		}{{ui.Ctrl, "⌃"}, {ui.Alt, "⌥"}, {ui.Shift, "⇧"}, {ui.Super, "⌘"}} {
			if ch.mods&m.mod != 0 {
				b.WriteString(m.symbol)
			}
		}
		return b.String() + keySymbol(name)
	}
	var parts []string
	for _, m := range []struct {
		mod  ui.Modifiers
		word string
	}{{ui.Ctrl, "Ctrl"}, {ui.Alt, "Alt"}, {ui.Shift, "Shift"}, {ui.Super, "Super"}} {
		if ch.mods&m.mod != 0 {
			parts = append(parts, m.word)
		}
	}
	return strings.Join(append(parts, keyWord(name)), "+")
}

// keySymbol is how a key is written on macOS.
func keySymbol(name string) string {
	switch name {
	case "Enter":
		return "↩"
	case "Escape":
		return "⎋"
	case "Backspace":
		return "⌫"
	case "Delete":
		return "⌦"
	case "Tab":
		return "⇥"
	case "Left":
		return "←"
	case "Right":
		return "→"
	case "Up":
		return "↑"
	case "Down":
		return "↓"
	case "Home":
		return "↖"
	case "End":
		return "↘"
	case "PageUp":
		return "⇞"
	case "PageDown":
		return "⇟"
	}
	return keyWord(name)
}

func keyWord(name string) string {
	if len(name) == 1 {
		return strings.ToUpper(name)
	}
	return name
}

// displayAccel is an accelerator as the interface shows it, "" for none.
func displayAccel(accel string) string {
	if ch, ok := parseAccel(accel); ok {
		return ch.display()
	}
	return ""
}

// typing reports whether pressing the chord could be typing: it has no
// modifier but Shift, and its key makes text.
func (ch chord) typing() bool {
	if ch.mods&^ui.Shift != 0 {
		return false
	}
	switch name := nameOfKey(ch.key); {
	case len(name) == 1, name == "Space":
		return true
	}
	return false
}

// reserved are the chords the Edit and App menus keep for themselves.
var reserved = map[string]string{
	"cmd+q":       "Quit",
	"cmd+h":       "Hide",
	"cmd+alt+h":   "Hide Others",
	"cmd+m":       "Minimize",
	"cmd+c":       "Copy",
	"cmd+v":       "Paste",
	"cmd+x":       "Cut",
	"cmd+a":       "Select All",
	"cmd+z":       "Undo",
	"cmd+shift+z": "Redo",
}

// reservedFor says what a chord is kept for, "" when it is free.
func (ch chord) reservedFor() string {
	var parts []string
	if ch.mods&ui.Cmd != 0 {
		parts = append(parts, "cmd")
	}
	if ch.mods&ui.Ctrl != 0 && ui.Cmd != ui.Ctrl {
		parts = append(parts, "ctrl")
	}
	if ch.mods&ui.Alt != 0 {
		parts = append(parts, "alt")
	}
	if ch.mods&ui.Shift != 0 {
		parts = append(parts, "shift")
	}
	return reserved[strings.Join(append(parts, nameOfKey(ch.key)), "+")]
}

// allMods are the combinations of modifiers a key can be pressed with.
var allMods = func() []ui.Modifiers {
	var all []ui.Modifiers
	for m := 0; m < 16; m++ {
		var mods ui.Modifiers
		for i, bit := range []ui.Modifiers{ui.Shift, ui.Ctrl, ui.Alt, ui.Super} {
			if m&(1<<i) != 0 {
				mods |= bit
			}
		}
		all = append(all, mods)
	}
	return all
}()

// pressedChord is the chord pressed in this frame, if any: the recorder of the
// Keybinds settings asks every key it knows. Modifiers alone are no chord.
func pressedChord(c *ui.Context) (chord, bool) {
	for _, d := range keyDefs {
		for _, m := range allMods {
			if c.Shortcut(m, d.key) {
				return chord{m, d.key}, true
			}
		}
	}
	return chord{}, false
}
