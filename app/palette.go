package app

import (
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// palette is the command palette (⇧⌘P): a field over every action of the
// menu bar, which runs the one chosen as its menu item would.
type palette struct {
	open  bool
	query string
	list  ui.ListState
	sel   int
}

// paletteEntry is an action the palette lists, with the menu item it runs.
type paletteEntry struct {
	ac   bindable
	item *mygo.MenuItem
}

// paletteEntries are the actions that match the query, best first; with no
// query, all of them in the order of the keybinds. The actions read from the
// keyboard only where they act are left out: they work on what has the focus,
// which the palette has taken. The palette itself is left out too.
func (a *app) paletteEntries(query string) []paletteEntry {
	menu := a.menuTemplate()
	q := strings.TrimSpace(query)
	var out []paletteEntry
	scores := map[string]int{}
	for _, ac := range bindables {
		if ac.id == "palette" {
			continue
		}
		it := findOrNilItem(menu, ac.id)
		if it == nil || it.Click == nil {
			continue
		}
		s, ok := fuzzyBest(q, ac.label, ac.menuLabel(), ac.group+" "+ac.label)
		if !ok {
			continue
		}
		scores[ac.id] = s
		out = append(out, paletteEntry{ac, it})
	}
	if q != "" {
		slices.SortStableFunc(out, func(x, y paletteEntry) int { return scores[y.ac.id] - scores[x.ac.id] })
	}
	return out
}

// openPalette shows the palette, empty.
func (a *app) openPalette() {
	a.cmds = palette{open: true}
}

// runPaletteEntry closes the palette and runs the action, in the main window.
func (a *app) runPaletteEntry(e paletteEntry) {
	a.cmds.open, a.cmds.query = false, ""
	e.item.Click(e.item, a.win)
}

func (a *app) paletteView(c *ui.Context) {
	p := &a.cmds
	if !p.open {
		return
	}
	t := c.Theme()
	entries := a.paletteEntries(p.query)
	p.sel = min(max(p.sel, 0), max(len(entries)-1, 0))
	choose := func(i int) {
		if i >= 0 && i < len(entries) {
			a.runPaletteEntry(entries[i])
		}
	}
	step := func(d int) {
		if len(entries) > 0 {
			p.sel = (p.sel + d + len(entries)) % len(entries)
			p.list.ScrollIntoView(p.sel)
		}
	}
	ui.Modal(c, &p.open, func() {
		ui.Column(c).Width(560).Gap(8).Children(func() {
			in := ui.TextInput(c.Key("palette-query"), &p.query).Placeholder("Run an Action").AutoFocus().Label("Command palette").FontSize(15)
			if in.Changed() {
				p.sel = 0
				p.list.ScrollTo(0, ui.Start)
			}
			// The arrows choose in the list while the field keeps the focus.
			switch {
			case in.Shortcut(0, ui.KeyDown):
				step(1)
			case in.Shortcut(0, ui.KeyUp):
				step(-1)
			}
			if in.Submitted() {
				choose(p.sel)
			}
			p.list.Selected = &p.sel
			p.list.Key = func(i int) any { return entries[i].ac.id }
			l := ui.List(c.Key("palette-list"), &p.list, len(entries), func(i int) {
				e := entries[i]
				b := ui.ButtonBase(c.Key("run")).FillWidth().Gap(10).Padding(6, 8).Radius(6).AlignItems(ui.Center).
					Label(strings.TrimSuffix(e.ac.label, "…"))
				// The list paints the row chosen in the accent: its text follows.
				muted, check, border := t.TextMuted, t.Accent, hairline
				if i == p.sel {
					muted, check, border = t.AccentText.Alpha(0.75), t.AccentText, t.AccentText.Alpha(0.4)
				} else if b.Hovered() {
					b.Background(hoverBg)
				}
				if b.Clicked() {
					choose(i)
				}
				b.Children(func() {
					ui.Text(c, e.ac.label).FontSize(13).SingleLine().Grow(1).Shrink(1).MinWidth(0)
					if e.item.Checked {
						ui.Icon(c, iconCheck).FontSize(13).TextColor(check).Shrink(0)
					}
					ui.Text(c, e.ac.group).FontSize(11).TextColor(muted).SingleLine().Shrink(0)
					if k := a.keyText(e.ac.id); k != "" {
						ui.Text(c, k).FontSize(11).TextColor(muted).Padding(1, 6).Radius(4).Border(1, border).Shrink(0)
					}
				})
			}).MaxHeight(380)
			if l.Submitted() {
				choose(p.sel)
			}
			if len(entries) == 0 {
				ui.Text(c, "No action matches").TextColor(t.TextMuted).Padding(8)
			}
		})
	})
}
