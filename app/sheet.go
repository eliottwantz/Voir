package app

import "github.com/egoist/mygo/ui"

// sheet is a large window-sized panel over the repository: Git LFS, the merge
// editor and the interactive rebase.
type sheet struct {
	open  bool
	title string
	body  func(c *ui.Context)
	// state is what the sheet shows, for tests to look into.
	state any
	// wide makes the sheet as large as the window allows.
	wide bool
}

func (a *app) sheetView(c *ui.Context) {
	s := a.sheet
	if s == nil {
		return
	}
	if !s.open {
		a.sheet = nil
		return
	}
	t := c.Theme()
	w, h := c.Size()
	ui.Modal(c, &s.open, func() {
		width, height := min(w-80, 1040), min(h-80, 700)
		if s.wide {
			width, height = w-60, h-60
		}
		ui.Column(c).Width(width).Height(height).Gap(8).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, s.title).FontSize(15).Bold().SingleLine().Grow(1).MinWidth(0)
				if ui.Button(c, "Close").Clicked() {
					s.open = false
				}
			})
			ui.Box(c).Height(1).Background(t.Border).Shrink(0)
			s.body(c)
		})
	})
}
