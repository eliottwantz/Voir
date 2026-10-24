package app

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/highlight"
)

// codeFont is the family of the code.
func (t *repoTab) codeFont() string {
	if f := t.app.store.settings().CodeFont; f != "" {
		return f + ", monospace"
	}
	return mono
}

func (t *repoTab) codeSize() float32 {
	if s := t.app.store.settings().CodeFontSize; s > 0 {
		return s
	}
	return 13
}

// lineHeight is the height of a line of code: 20 at 13.
func (t *repoTab) lineHeight() float32 {
	return float32(int(t.codeSize()*20/13 + 0.5))
}

// tabWidth is the columns a tab takes.
const tabWidth = 4

// styleCode styles a line of code by the colors of its tokens. Tabs become
// spaces up to the next tab stop.
func styleCode(text string, segs []highlight.Seg, pal *diffPalette) []ui.Span {
	if text == "" {
		return []ui.Span{{Text: " "}}
	}
	// The places where the style may change.
	cuts := []int{0, len(text)}
	for _, s := range segs {
		cuts = append(cuts, int(s.Start), int(s.End))
	}
	slices.Sort(cuts)
	cuts = slices.Compact(cuts)

	var spans []ui.Span
	col := 0
	si := 0
	for i := 0; i+1 < len(cuts); i++ {
		start, end := cuts[i], cuts[i+1]
		if start >= len(text) || start < 0 {
			break
		}
		end = min(end, len(text))
		if end <= start {
			continue
		}
		for si < len(segs) && int(segs[si].End) <= start {
			si++
		}
		color := pal.code
		if si < len(segs) && int(segs[si].Start) <= start {
			color = pal.syntax[segs[si].Class]
		}
		part := text[start:end]
		if strings.IndexByte(part, '\t') >= 0 {
			part, col = expandTabs(part, col)
		} else {
			col += len(part)
		}
		if n := len(spans); n > 0 && spans[n-1].Color == color {
			spans[n-1].Text += part
			continue
		}
		spans = append(spans, ui.Span{Text: part, Color: color})
	}
	return spans
}

// expandTabs replaces the tabs of s, which starts at column col, and
// returns the column after it.
func expandTabs(s string, col int) (string, int) {
	var b strings.Builder
	for _, r := range s {
		if r == '\t' {
			n := tabWidth - col%tabWidth
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		col++
	}
	return b.String(), col
}
