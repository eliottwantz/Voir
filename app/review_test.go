package app

import "testing"

func TestScrollingPastExpandedFilesViewsThem(t *testing.T) {
	tab := &repoTab{}
	rv := &review{hash: "abc", pr: true}
	for _, p := range []string{"a", "b", "c", "d"} {
		rv.files = append(rv.files, &reviewFile{path: p})
	}
	rv.files[1].collapsed = true
	viewed := func() (got []string) {
		for _, f := range rv.files {
			if f.viewed {
				got = append(got, f.path)
			}
		}
		return got
	}
	same := func(got []string, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("viewed %v, want %v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("viewed %v, want %v", got, want)
			}
		}
	}

	rv.jumped = true
	tab.autoView(rv, 0)
	tab.autoView(rv, 2) // a is passed; b is folded, so it is not
	same(viewed(), "a")
	if rv.files[0].collapsed {
		t.Fatal("a file viewed by scrolling stays open")
	}

	// Scrolling back and down again does not mark again a file unmarked.
	tab.markViewed(rv, rv.files[0], false)
	tab.autoView(rv, 0)
	tab.autoView(rv, 2)
	same(viewed())

	// A jump forward does not view the files it skips.
	rv.jumped = true
	tab.autoView(rv, 3)
	same(viewed())
	tab.autoView(rv, 4)
	same(viewed(), "d")
}
