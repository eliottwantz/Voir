package app

// The diff of Local Changes is the viewer of the file chosen, with Stage and
// Discard on the change under the pointer and on the lines chosen (see
// diffsurface.go).

import (
	"github.com/egoist/mygo/ui"
)

// workSurface is the diff of the file of Local Changes, whose hunks and
// lines are staged, unstaged or discarded from the diff itself.
func (tab *repoTab) workSurface(c *ui.Context) {
	rv := tab.wrv
	if rv == nil || (rv.loading && len(rv.files) == 0) {
		ui.Row(c).Padding(20).Justify(ui.Center).FillWidth().Children(func() { spinner(c, "Loading") })
		return
	}
	tab.reviewSurface(c, rv)
}
