package app

import (
	"fmt"
	"strings"
	"testing"
)

func TestSideRowsGroupAndCompactFolders(t *testing.T) {
	g := func(path string) sideFile { return sideFile{path: path, group: kindOf(path, false)} }
	files := []sideFile{
		g("cmd/mayoi/tabs.go"), g("docs/FEATURES.md"), g("CHANGELOG.md"),
		g("cmd/mayoi/markdown_test.go"), g("cmd/mayoi/editor.go"),
	}
	show := func(closed map[string]bool, filter string) string {
		var out []string
		for _, r := range sideRows(files, filter, closed, true) {
			out = append(out, fmt.Sprintf("%s%s", strings.Repeat(" ", r.depth), r.name))
		}
		return strings.Join(out, "|")
	}
	want := "Docs|" + " docs|  FEATURES.md| CHANGELOG.md|" + "Code|" + " cmd/mayoi|  editor.go|  tabs.go|" + "Tests|" + " cmd/mayoi|  markdown_test.go"
	if got := show(map[string]bool{}, ""); got != want {
		t.Errorf("tree\n got %s\nwant %s", got, want)
	}
	if got, want := show(map[string]bool{"g:docs": true, "d:code:cmd/mayoi": true}, ""), "Docs|Code| cmd/mayoi|Tests| cmd/mayoi|  markdown_test.go"; got != want {
		t.Errorf("closed\n got %s\nwant %s", got, want)
	}
	// Filtering keeps the groups, so that the tree does not jump.
	if got, want := show(map[string]bool{}, "EDITOR"), "Code| cmd/mayoi|  editor.go"; got != want {
		t.Errorf("filtered\n got %s\nwant %s", got, want)
	}
}

// A tree that keeps no kinds, as the commit view's, lists the files as plain
// folders, in one order.
func TestSideRowsWithoutGroups(t *testing.T) {
	g := func(path string) sideFile { return sideFile{path: path, group: kindOf(path, false)} }
	files := []sideFile{g("cmd/mayoi/tabs.go"), g("docs/FEATURES.md"), g("CHANGELOG.md"), g("cmd/mayoi/markdown_test.go")}
	var out []string
	for _, r := range sideRows(files, "", map[string]bool{}, false) {
		out = append(out, fmt.Sprintf("%s%s", strings.Repeat(" ", r.depth), r.name))
	}
	want := "cmd/mayoi| markdown_test.go| tabs.go|docs| FEATURES.md|CHANGELOG.md"
	if got := strings.Join(out, "|"); got != want {
		t.Errorf("tree\n got %s\nwant %s", got, want)
	}
}

func TestSideRowsOfOneKindHaveNoGroups(t *testing.T) {
	files := []sideFile{{path: "b.go", group: kindIndex("code")}, {path: "A.go", group: kindIndex("code")}}
	rows := sideRows(files, "", map[string]bool{}, true)
	if len(rows) != 2 || rows[0].name != "A.go" || rows[0].depth != 0 {
		t.Errorf("rows %+v", rows)
	}
}
