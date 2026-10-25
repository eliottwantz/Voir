package app

import (
	"fmt"
	"image/color"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSideRowsGroupAndNestFolders(t *testing.T) {
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
	want := "Docs|" + " docs|  FEATURES.md| CHANGELOG.md|" + "Code|" + " cmd|  mayoi|   editor.go|   tabs.go|" + "Tests|" + " cmd|  mayoi|   markdown_test.go"
	if got := show(map[string]bool{}, ""); got != want {
		t.Errorf("tree\n got %s\nwant %s", got, want)
	}
	if got, want := show(map[string]bool{"g:docs": true, "d:code:cmd/mayoi": true}, ""), "Docs|Code| cmd|  mayoi|Tests| cmd|  mayoi|   markdown_test.go"; got != want {
		t.Errorf("closed\n got %s\nwant %s", got, want)
	}
	// Filtering keeps the groups, so that the tree does not jump.
	if got, want := show(map[string]bool{}, "EDITOR"), "Code| cmd|  mayoi|   editor.go"; got != want {
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
	want := "cmd| mayoi|  markdown_test.go|  tabs.go|docs| FEATURES.md|CHANGELOG.md"
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

// The file chosen in a tree is in the accent colour while the tree has the
// focus, and in gray when it is elsewhere.
func TestChosenFileIsAccentOnlyWhileTheTreeHasFocus(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	// The commit that changes main.go, then its Changes.
	if err := tt.Click("Greet the world"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	at := func() color.RGBA {
		r, ok := tt.Find("main.go")
		if !ok {
			t.Fatalf("no main.go in %q", tt.Texts())
		}
		// The middle of the row, clear of its text.
		return tt.Image().RGBAAt(int(r.X+r.W/2), int(r.Y+r.H/2))
	}
	r, _ := tt.Find("main.go")
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	snapshot(t, tt, "changes-tree-focused")
	acc := ui.LightTheme().Accent
	if got := at(); got.R != acc.R || got.G != acc.G || got.B != acc.B {
		t.Errorf("chosen file with focus = %v, want the accent %v", got, acc)
	}
	// Focus moves to the graph.
	if err := tt.Click("Greet the world"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	snapshot(t, tt, "changes-tree-unfocused")
	if got := at(); got.R == acc.R && got.G == acc.G && got.B == acc.B {
		t.Errorf("chosen file without focus is still the accent: %v", got)
	}
}

func TestCompareTreeOrderPutsFoldersBeforeFiles(t *testing.T) {
	paths := []string{"Sources/NodeArchitecture/IO/Output.swift", "Package.swift", "Distributions/Package-Public.swift", "Package.resolved", "Sources/A.swift", "Distributions/Package-Internal.swift", "Sources/NodeArchitectureTesting/TestGraph.swift"}
	slices.SortFunc(paths, compareTreeOrder)
	want := []string{"Distributions/Package-Internal.swift", "Distributions/Package-Public.swift", "Sources/NodeArchitecture/IO/Output.swift", "Sources/NodeArchitectureTesting/TestGraph.swift", "Sources/A.swift", "Package.resolved", "Package.swift"}
	if !slices.Equal(paths, want) {
		t.Errorf("got %q, want %q", paths, want)
	}
}
