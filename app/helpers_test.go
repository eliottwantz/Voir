package app

import (
	"fmt"
	"strings"
)

// longFile is a document of n lines, the ones numbered in changed edited.
func longFile(n int, edited ...int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		if slicesContains(edited, i) {
			fmt.Fprintf(&b, "- Line %d, **edited** and `code`\n", i)
			continue
		}
		fmt.Fprintf(&b, "- Line %d of the document\n", i)
	}
	return b.String()
}

func slicesContains(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
