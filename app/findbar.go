package app

import (
	"github.com/egoist/mygo/ui"
)

// findAction is what the buttons and keys of a find bar ask for.
type findAction int

const (
	findNone findAction = iota
	findPrev
	findNext
	findDone
)

// findBar is the bar of every search in the app: the graph's, a commit's
// changes and a pull request's code. It has the field, how many results there
// are, the buttons that go to the previous and the next, and Done, which
// closes it. focus is set to give the field the keys, and is cleared once it
// has them.
func findBar(c *ui.Context, q *string, placeholder, status string, canStep bool, focus *bool) findAction {
	t := c.Theme()
	act := findNone
	ui.Row(c.Key("find-bar")).Padding(6, 10).Gap(6).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
		ui.Row(c).Height(26).Padding(0, 8).Gap(8).Radius(6).Border(1, hairline).Grow(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
			in := ui.TextInputBase(c, q).Placeholder(placeholder).Label(placeholder).FontSize(13).Grow(1).MinWidth(0)
			if *focus {
				in.Focus()
				*focus = false
			}
			switch {
			case in.Shortcut(0, ui.KeyEscape):
				act = findDone
			case in.Shortcut(ui.Shift, ui.KeyEnter):
				act = findPrev
			case in.Shortcut(0, ui.KeyEnter):
				act = findNext
			}
			if status != "" {
				ui.Text(c, status).FontSize(12).TextColor(t.TextMuted).Shrink(0).SingleLine()
			}
		})
		ui.Row(c).Gap(2).Shrink(0).Children(func() {
			if iconButton(c, iconChevronUp, "Previous result (⇧↩)").Size(26, 26).Disabled(!canStep).Clicked() {
				act = findPrev
			}
			if iconButton(c, iconChevronDown, "Next result (↩)").Size(26, 26).Disabled(!canStep).Clicked() {
				act = findNext
			}
		})
		done := ui.ButtonBase(c).Height(26).Padding(0, 12).Radius(6).Border(1, hairline).Center().Shrink(0).Label("Done").Tooltip("Close the search (⎋)")
		if done.Hovered() {
			done.Background(hoverBg)
		}
		done.Children(func() { ui.Text(c, "Done").FontSize(13) })
		if done.Clicked() {
			act = findDone
		}
	})
	return act
}
