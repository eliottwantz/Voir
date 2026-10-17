package main

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/fileicon"
)

// sideFile is a file of a change, as the sidebar lists it.
type sideFile struct {
	path           string
	status         byte
	added, deleted int
	binary, viewed bool
	// group is the file's index in kinds.
	group int
}

type sideRowKind uint8

const (
	sideGroup sideRowKind = iota
	sideDir
	sideLeaf
)

// sideRow is a row of the files tree: a group, a folder, or a file.
type sideRow struct {
	kind  sideRowKind
	key   string // what closes a group or a folder
	name  string
	depth int
	file  int // a file's index
	count int // the files of a group
}

type sideDirNode struct {
	name  string
	dirs  []*sideDirNode
	files []int
}

func (n *sideDirNode) dir(name string) *sideDirNode {
	for _, d := range n.dirs {
		if d.name == name {
			return d
		}
	}
	d := &sideDirNode{name: name}
	n.dirs = append(n.dirs, d)
	return d
}

// sideRows lays out the files that match the filter as a tree: under their
// groups when the files are of several kinds, the folders holding only a
// folder joined into one, as cmd/app, the folders before the files, and
// what is closed left out.
func sideRows(files []sideFile, filter string, closed map[string]bool) []sideRow {
	q := strings.ToLower(strings.TrimSpace(filter))
	var kindsSeen []int
	for _, f := range files {
		if !slices.Contains(kindsSeen, f.group) {
			kindsSeen = append(kindsSeen, f.group)
		}
	}
	grouped := len(kindsSeen) > 1
	slices.Sort(kindsSeen)
	lower := func(i int) string { return strings.ToLower(files[i].path) }

	var rows []sideRow
	var emit func(n *sideDirNode, prefix string, depth int)
	emit = func(n *sideDirNode, prefix string, depth int) {
		slices.SortFunc(n.dirs, func(a, b *sideDirNode) int { return strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name)) })
		slices.SortFunc(n.files, func(a, b int) int { return strings.Compare(lower(a), lower(b)) })
		for _, d := range n.dirs {
			name := d.name
			for len(d.dirs) == 1 && len(d.files) == 0 {
				d = d.dirs[0]
				name += "/" + d.name
			}
			key := "d:" + prefix + name
			rows = append(rows, sideRow{kind: sideDir, key: key, name: name, depth: depth})
			if !closed[key] {
				emit(d, prefix+name+"/", depth+1)
			}
		}
		for _, i := range n.files {
			_, base := dirAndBase(files[i].path)
			rows = append(rows, sideRow{kind: sideLeaf, name: base, depth: depth, file: i})
		}
	}
	for _, g := range kindsSeen {
		root := &sideDirNode{}
		count := 0
		for i, f := range files {
			if f.group != g || q != "" && !strings.Contains(strings.ToLower(f.path), q) {
				continue
			}
			count++
			parts := strings.Split(f.path, "/")
			n := root
			for _, p := range parts[:len(parts)-1] {
				n = n.dir(p)
			}
			n.files = append(n.files, i)
		}
		if count == 0 {
			continue
		}
		depth, prefix := 0, ""
		if grouped {
			key := "g:" + kinds[g].key
			rows = append(rows, sideRow{kind: sideGroup, key: key, name: kinds[g].label, count: count})
			if closed[key] {
				continue
			}
			depth, prefix = 1, kinds[g].key+":"
		}
		emit(root, prefix, depth)
	}
	return rows
}

// filesTree is the files of a change as a tree, with the total of their lines
// under it. Choosing a file calls choose with its index in files.
func (tab *repoTab) filesTree(c *ui.Context, list *ui.ListState, filter *string, closed map[string]bool, files []sideFile, current int, choose func(int)) {
	t := c.Theme()
	filterField(c, filter, "Filter files")
	rows := sideRows(files, *filter, closed)
	if len(rows) == 0 {
		ui.Text(c, "No matching files").FontSize(12).TextColor(t.TextMuted).Padding(16).TextAlign(ui.Center).FillWidth()
		return
	}
	added, deleted := 0, 0
	for _, r := range rows {
		if r.kind == sideLeaf && !files[r.file].binary {
			added += files[r.file].added
			deleted += files[r.file].deleted
		}
	}
	list.Key = func(i int) any {
		if i < 0 || i >= len(rows) {
			return i
		}
		if r := rows[i]; r.kind == sideLeaf {
			return "f:" + files[r.file].path
		} else {
			return r.key
		}
	}
	ui.List(c, list, len(rows), func(i int) {
		if i >= len(rows) {
			return
		}
		r := rows[i]
		row := ui.Row(c).Height(30).Padding(0, 8, 0, 8+float32(r.depth)*14).Gap(6).Radius(6).Margin(0, 6).AlignItems(ui.Center).MinWidth(0)
		switch {
		case r.kind == sideLeaf && r.file == current:
			row.Background(hairline)
		case row.Hovered():
			row.Background(hoverBg)
		}
		row.Role(ui.RoleButton).Label(r.name)
		if row.Clicked() {
			if r.kind == sideLeaf {
				choose(r.file)
			} else {
				closed[r.key] = !closed[r.key]
			}
		}
		row.Children(func() {
			if r.kind != sideLeaf {
				chevron := iconChevronDown
				if closed[r.key] {
					chevron = iconChevronRight
				}
				ui.Icon(c, chevron).FontSize(12).TextColor(t.TextMuted).Shrink(0)
			} else {
				ui.Box(c).Width(12).Shrink(0)
			}
			switch r.kind {
			case sideGroup:
				ui.Icon(c, iconLayers).FontSize(14).TextColor(t.TextMuted).Shrink(0)
				ui.Text(c, r.name).FontSize(13).FontWeight(600).SingleLine().Grow(1).MinWidth(0)
				ui.Textf(c, "%d", r.count).Font(mono).FontSize(10).FontWeight(600).TextColor(t.TextMuted).Shrink(0)
			case sideDir:
				ui.Icon(c, iconFolder).FontSize(14).TextColor(t.TextMuted).Shrink(0)
				ui.Text(c, r.name).FontSize(13).SingleLine().Grow(1).MinWidth(0)
			default:
				f := files[r.file]
				ic := fileicon.For(f.path)
				ui.Icon(c, ic.SVG).FontSize(14).TextColor(ic.Color(t.Dark)).Shrink(0)
				name := ui.Text(c, r.name).FontSize(13).SingleLine().Grow(1).MinWidth(0).Tooltip(f.path)
				if f.viewed {
					name.TextColor(t.TextMuted)
				}
				if !f.binary {
					ui.Textf(c, "+%d −%d", f.added, f.deleted).Font(mono).FontSize(10).FontWeight(600).TextColor(t.TextMuted).Shrink(0).
						Tooltip(plural(f.added, "line", "lines") + " added, " + plural(f.deleted, "line", "lines") + " removed")
				}
				letter, meaning := statusLetter(f.status)
				ui.Text(c, letter).Font(mono).FontSize(11).FontWeight(700).TextColor(statusColor(c, f.status)).
					Width(12).TextAlign(ui.Center).Shrink(0).Tooltip(meaning)
			}
		})
	}).Grow(1).MinHeight(0).Label("Files")
	ui.Row(c).MinHeight(40).Padding(6, 14).Gap(6).Shrink(0).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(hairline).Children(func() {
		ui.Text(c, "Total:").FontSize(11).FontWeight(600).TextColor(t.TextMuted)
		ui.Textf(c, "+%d", added).Font(mono).FontSize(11).FontWeight(600).TextColor(t.Success)
		ui.Textf(c, "−%d", deleted).Font(mono).FontSize(11).FontWeight(600).TextColor(t.Danger)
	})
}
