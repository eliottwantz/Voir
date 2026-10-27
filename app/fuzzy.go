package app

import (
	"strings"
	"unicode"
)

const (
	fuzzyHit      = 10 // every matched character
	fuzzyRun      = 12 // a character right after the previous match
	fuzzyBoundary = 8  // a match at the start of a word, or a camelCase hump
	fuzzyStart    = 6  // a match at the very start of the text
	fuzzyNone     = -1 << 30
)

// fuzzyScore says whether text contains the query's characters in order, and
// how well: runs, word starts and early matches score higher. A query of
// several words needs every word to match, in any order. An empty query
// matches everything.
func fuzzyScore(query, text string) (int, bool) {
	total := 0
	var hay, folded []rune
	for _, word := range strings.Fields(query) {
		if hay == nil {
			hay = []rune(text)
			folded = []rune(strings.ToLower(text))
			if len(folded) != len(hay) { // case folding changed the length
				folded = hay
			}
		}
		s, ok := fuzzyWord([]rune(strings.ToLower(word)), hay, folded)
		if !ok {
			return 0, false
		}
		total += s
	}
	return total, true
}

// fuzzyBest is the best fuzzyScore of the query against any one of the texts.
func fuzzyBest(query string, texts ...string) (int, bool) {
	best, found := 0, false
	for _, t := range texts {
		if s, ok := fuzzyScore(query, t); ok && (!found || s > best) {
			best, found = s, true
		}
	}
	return best, found
}

// fuzzyMatch reports whether the query fuzzily matches any of the texts.
func fuzzyMatch(query string, texts ...string) bool {
	_, ok := fuzzyBest(query, texts...)
	return ok
}

// fuzzyWord scores one word against the text, picking the best alignment.
func fuzzyWord(q, hay, folded []rune) (int, bool) {
	if len(q) == 0 {
		return 0, true
	}
	// Cheap subsequence test first: most rows fail here.
	qi := 0
	for _, r := range folded {
		if r == q[qi] {
			if qi++; qi == len(q) {
				break
			}
		}
	}
	if qi < len(q) {
		return 0, false
	}
	n := len(folded)
	// match[j]: best score with the current query char matched at j;
	// reach[j]: best score with it matched at or before j, less a point per
	// character skipped since.
	prevMatch := make([]int, n)
	prevReach := make([]int, n)
	match := make([]int, n)
	reach := make([]int, n)
	for i, qc := range q {
		for j := range n {
			match[j] = fuzzyNone
			if folded[j] != qc {
				continue
			}
			gain := fuzzyHit
			if boundary(hay, j) {
				gain += fuzzyBoundary
			}
			if j == 0 {
				gain += fuzzyStart
			}
			switch {
			case i == 0:
				match[j] = gain - min(j, 8) // a late start costs a little
			case j > 0:
				best := fuzzyNone
				if prevMatch[j-1] > fuzzyNone {
					best = prevMatch[j-1] + fuzzyRun
				}
				if j > 1 && prevReach[j-2] > fuzzyNone {
					best = max(best, prevReach[j-2])
				}
				if best > fuzzyNone {
					match[j] = best + gain
				}
			}
		}
		run := fuzzyNone
		for j := range n {
			if run > fuzzyNone {
				run--
			}
			run = max(run, match[j])
			reach[j] = run
		}
		prevMatch, match = match, prevMatch
		prevReach, reach = reach, prevReach
	}
	best := fuzzyNone
	for _, s := range prevMatch {
		best = max(best, s)
	}
	return best, best > fuzzyNone
}

// boundary reports whether the character at j starts a word: after a
// separator, or a capital after a lowercase letter.
func boundary(s []rune, j int) bool {
	if j == 0 {
		return true
	}
	prev, cur := s[j-1], s[j]
	if !unicode.IsLetter(prev) && !unicode.IsDigit(prev) {
		return true
	}
	return unicode.IsLower(prev) && unicode.IsUpper(cur)
}
