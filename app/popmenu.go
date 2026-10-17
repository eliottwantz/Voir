package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// menuRow is an entry of a popMenu: an icon, a name, and the shortcut that
// does the same, if any.
type menuRow struct {
	icon     *ui.SVG
	label    string
	hint     string
	danger   bool
	disabled bool
	// tooltip says why a row is disabled, or what it does.
	tooltip string
	run     func()
}

// popMenu shows groups of rows in a panel below anchor while *open is true,
// as a menu that has icons, which the native ones cannot. Choosing a row
// closes it and runs the row's action. Groups are set apart by a line.
func popMenu(c *ui.Context, anchor ui.Element, open *bool, groups ...[]menuRow) {
	ui.Popover(c, anchor, open, func() {
		ui.Column(c).MinWidth(260).Gap(2).Children(func() {
			for gi, rows := range groups {
				if gi > 0 {
					ui.Box(c).Key(fmt.Sprint("sep", gi)).Height(1).Background(hairline).Margin(4, 4).Shrink(0)
				}
				for _, r := range rows {
					menuRowButton(c, open, r)
				}
			}
		})
	})
}

// menuRowButton is a row of a popMenu: choosing it closes the menu and runs
// the row's action.
func menuRowButton(c *ui.Context, open *bool, r menuRow) {
	t := c.Theme()
	fg := t.Text
	switch {
	case r.disabled:
		fg = t.TextMuted
	case r.danger:
		fg = t.Danger
	}
	b := ui.ButtonBase(c).Key(r.label).Height(32).Padding(0, 10).Gap(10).Radius(7).FillWidth().Justify(ui.Start).AlignItems(ui.Center).
		Label(r.label).Disabled(r.disabled).TextColor(fg)
	if r.tooltip != "" {
		b.Tooltip(r.tooltip)
	}
	if r.disabled {
		b.Opacity(0.5)
	} else if b.Hovered() {
		b.Background(hoverBg)
	}
	if b.Clicked() {
		*open = false
		r.run()
	}
	b.Children(func() {
		ui.Icon(c, r.icon).FontSize(15).TextColor(fg).Shrink(0)
		ui.Text(c, r.label).FontSize(13.5).TextColor(fg).Grow(1).SingleLine()
		if r.hint != "" {
			ui.Text(c, r.hint).FontSize(12).TextColor(t.TextMuted).Shrink(0)
		}
	})
}
