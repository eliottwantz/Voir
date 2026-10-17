package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/eliott/myfork/internal/git"
)

// statusColor is the color of a change's letter.
func statusColor(c *ui.Context, s byte) ui.Color {
	t := c.Theme()
	switch s {
	case 'A', '?':
		return t.Success
	case 'D', 'U':
		return t.Danger
	case 'R', 'C':
		return ui.Hex("#8b5cf6")
	}
	return t.Warning
}

// statusBadge is the colored letter before a file.
func statusBadge(c *ui.Context, s byte) {
	letter, meaning := statusLetter(s)
	col := statusColor(c, s)
	ui.Text(c, letter).Size(16, 16).TextAlign(ui.Center).FontSize(10.5).FontWeight(700).TextColor(col).Radius(4).Background(col.Alpha(0.16)).Tooltip(meaning).Shrink(0)
}

// diffArea shows a pane's patch, or why there is none.
func (tab *repoTab) diffArea(c *ui.Context, p *diffPane) {
	t := c.Theme()
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
		switch {
		case p.key == "":
			ui.Text(c, "Select a file to see its changes").TextColor(t.TextMuted).Padding(20).TextAlign(ui.Center).FillWidth()
		case p.loading:
			ui.Row(c).Padding(20).Justify(ui.Center).FillWidth().Children(func() { ui.Spinner(c).Label("Loading") })
		case p.err != "":
			ui.Text(c, p.err).TextColor(t.Danger).Padding(20).Selectable()
		case p.binary:
			ui.Text(c, "Binary file: "+p.path).TextColor(t.TextMuted).Padding(20).TextAlign(ui.Center).FillWidth()
		case p.empty || p.state == nil:
			ui.Text(c, "No textual changes").TextColor(t.TextMuted).Padding(20).TextAlign(ui.Center).FillWidth()
		default:
			tab.fileHeader(c, p)
			if git.PatchHasLFSPointer(p.patch) {
				ui.Row(c).Gap(8).Padding(4, 12).Shrink(0).AlignItems(ui.Center).Background(t.Accent.Alpha(0.1)).Children(func() {
					ui.Icon(c, iconCloud).FontSize(13).TextColor(t.Accent)
					ui.Text(c, "This file is stored with Git LFS: the lines are its pointer, not its content.").FontSize(12)
				})
			}
			d := p.state
			editor.DiffView(c, d).Grow(1).FillWidth().MinHeight(0)
		}
	})
}

// fileHeader names the file above its diff, with the lines it changed.
func (tab *repoTab) fileHeader(c *ui.Context, p *diffPane) {
	t := c.Theme()
	added, deleted := p.state.Stats()
	dir, base := dirAndBase(p.path)
	ui.Row(c).Padding(4, 12).Gap(8).Shrink(0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
		ui.Text(c, dir).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
		ui.Text(c, base).FontSize(12.5).FontWeight(600).SingleLine().Shrink(0).Margin(0, 0, 0, -8)
		ui.Spacer(c)
		ui.Textf(c, "+%d", added).FontSize(12).TextColor(t.Success).Font("SF Mono, Menlo, monospace")
		ui.Textf(c, "−%d", deleted).FontSize(12).TextColor(t.Danger).Font("SF Mono, Menlo, monospace")
	})
}
