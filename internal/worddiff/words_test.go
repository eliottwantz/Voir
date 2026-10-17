package worddiff

import (
	"testing"

	"github.com/eliott/myfork/internal/git"
)

func TestWordDiffMarksTheWordsChanged(t *testing.T) {
	a, b := "println(\"hello\")", "println(\"hello, world\")"
	ra, rb := WordDiff(a, b)
	if len(ra) != 0 && a[ra[0].Start:ra[0].End] == "" {
		t.Errorf("old ranges %v", ra)
	}
	if len(rb) == 0 {
		t.Fatalf("nothing marked in the new line")
	}
	if got := b[rb[0].Start:rb[len(rb)-1].End]; got != ", world" {
		t.Errorf("marked %q in %q", got, b)
	}
	if ra, rb := WordDiff("same", "same"); ra != nil || rb != nil {
		t.Errorf("equal lines marked: %v %v", ra, rb)
	}
}

func TestPairsMatchDeletedLinesWithTheirReplacements(t *testing.T) {
	lines := []git.PatchLine{{Kind: ' '}, {Kind: '-'}, {Kind: '-'}, {Kind: '+'}, {Kind: ' '}}
	got := Pairs(lines)
	if len(got) != 1 || got[0] != (Pair{Del: 1, Add: 3}) {
		t.Errorf("pairs %v", got)
	}
}
