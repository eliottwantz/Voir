package app

import "testing"

func TestFuzzyScore(t *testing.T) {
	for _, c := range []struct {
		query, text string
		want        bool
	}{
		{"", "anything", true},
		{"fb", "foo/bar.go", true},
		{"fbg", "foo/bar.go", true},
		{"gbf", "foo/bar.go", false},
		{"FB", "foo/bar.go", true},
		{"bar foo", "foo/bar.go", true},
		{"bar zzz", "foo/bar.go", false},
		{"rtab", "repoTab", true},
		{"xyz", "foo/bar.go", false},
	} {
		if _, ok := fuzzyScore(c.query, c.text); ok != c.want {
			t.Errorf("fuzzyScore(%q, %q) matched = %v, want %v", c.query, c.text, ok, c.want)
		}
	}
}

func TestFuzzyScoreRanksTighterMatchesFirst(t *testing.T) {
	for _, c := range []struct{ query, best, worse string }{
		{"tab", "app/tab.go", "app/tests_about.go"},
		{"fb", "foo_bar.go", "fabulous.go"},
		{"main", "main.go", "domain_name.go"},
	} {
		b, ok1 := fuzzyScore(c.query, c.best)
		w, ok2 := fuzzyScore(c.query, c.worse)
		if !ok1 || !ok2 || b <= w {
			t.Errorf("%q: %q scored %d, %q scored %d; want the first higher", c.query, c.best, b, c.worse, w)
		}
	}
}
