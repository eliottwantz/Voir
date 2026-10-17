package main

import (
	"fmt"
	"math"
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/highlight"
	"github.com/eliott/myfork/internal/worddiff"
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
	return float32(math.Round(float64(t.codeSize()) * 20 / 13))
}

// charWidth is the width of a column of code.
func (t *repoTab) charWidth(c *ui.Context, rv *review) float32 {
	key := fmt.Sprint(t.codeFont(), t.codeSize())
	if rv.charKey != key {
		width, _ := c.MeasureText(0, ui.Span{Text: "0000000000000000000", Font: t.codeFont(), Size: t.codeSize()})
		rv.charW, rv.charKey = width/19, key
	}
	return rv.charW
}

// gutterWidth is the width of a column of line numbers.
func (t *repoTab) gutterWidth(c *ui.Context, rv *review, f *reviewFile) float32 {
	return float32(f.metrics().digits)*t.charWidth(c, rv) + 18
}

// cardRow is a row inside a file's card, between its sides.
func cardRow(c *ui.Context, pal *diffPalette) ui.Element {
	return ui.Row(c).Background(pal.codeBg).BorderWidth(0, 1, 0, 1).BorderColor(pal.cardBorder)
}

// cardEnd is the bottom of a file's card.
func (tab *repoTab) cardEnd(c *ui.Context, f *reviewFile) {
	pal := diffPaletteFor(c.Theme())
	if f.collapsed {
		ui.Box(c).Height(0)
		return
	}
	ui.Box(c).Height(4).Background(pal.codeBg).
		BorderWidth(0, 1, 1, 1).BorderColor(pal.cardBorder).Radius(0, 0, cardRadius, cardRadius)
}

// fileNote says why a file's lines do not show.
func (tab *repoTab) fileNote(c *ui.Context, rv *review, f *reviewFile, kind int32) {
	t := c.Theme()
	pal := diffPaletteFor(t)
	note := func(text string, col ui.Color) {
		cardRow(c, pal).Padding(18, 16).Justify(ui.Center).Children(func() {
			ui.Text(c, text).FontSize(12.5).TextColor(col)
		})
	}
	switch kind {
	case noteBinary:
		note("Binary file not shown", t.TextMuted)
	case noteNoText:
		note("No textual changes", t.TextMuted)
	case noteUnreadable:
		note("The patch could not be read", t.Danger)
	case noteBig:
		cardRow(c, pal).Gap(10).Padding(16).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("Large diff: %s changed lines", plural(f.added+f.deleted, "line", "lines"))).TextColor(t.TextMuted)
			if ui.Button(c, "Load Diff").Key("load" + f.path).Clicked() {
				f.open = true
				rv.dirty = true
			}
		})
	case noteLFS:
		cardRow(c, pal).Gap(8).Padding(5, 12).AlignItems(ui.Center).Background(t.Accent.Alpha(0.1)).Children(func() {
			ui.Icon(c, iconCloud).FontSize(13).TextColor(t.Accent)
			ui.Text(c, "This file is stored with Git LFS: the lines are its pointer, not its content.").FontSize(12)
		})
	}
}

// gapRow is the unchanged lines not shown: the arrows that show them, how
// many there are, and the function or heading they are in.
func (tab *repoTab) gapRow(c *ui.Context, rv *review, f *reviewFile, r reviewRow) {
	t := c.Theme()
	pal := diffPaletteFor(t)
	gutter := tab.gutterWidth(c, rv, f)
	if !rv.split && !f.oneSided() {
		// The numbers of both sides.
		gutter = 2*gutter - 4
	}
	first, last := r.gap == 0, int(r.gap) == len(f.hunks)
	n := int(r.count)
	expand := func(top, bottom int) {
		if f.expanded == nil {
			f.expanded = map[int]gapShown{}
		}
		s := f.expanded[int(r.gap)]
		s.top += top
		s.bottom += bottom
		f.expanded[int(r.gap)] = s
		f.metricsDone = false
		rv.dirty = true
	}
	all := func() { expand(n, 0) }
	can := f.canExpand()
	cardRow(c, pal).Height(30).Background(pal.gapBg).Children(func() {
		ui.Row(c).Width(gutter + 4).Height(30).Shrink(0).Children(func() {
			button := func(svg *ui.SVG, tip string, action func()) {
				b := ui.ButtonBase(c).Grow(1).Height(30).Center().Label(tip).Tooltip(tip).TextColor(pal.gapText).Disabled(!can)
				b.BorderWidth(0, 2, 0, 0).BorderColor(pal.codeBg)
				if b.Hovered() {
					b.TextColor(t.Text).Background(pal.hover)
				}
				b.Children(func() { ui.Icon(c, svg).FontSize(14) })
				if b.Clicked() {
					action()
				}
			}
			switch {
			case n <= expandStep && first:
				button(iconArrowUp, "Show the lines above", all)
			case n <= expandStep && last:
				button(iconArrowDown, "Show the lines below", all)
			case n <= expandStep:
				button(iconExpand, "Show the lines", all)
			default:
				if !first {
					button(iconArrowDown, fmt.Sprintf("Show %d more lines", expandStep), func() { expand(expandStep, 0) })
				}
				if !last {
					button(iconArrowUp, fmt.Sprintf("Show %d more lines", expandStep), func() { expand(0, expandStep) })
				}
			}
		})
		label := ui.ButtonBase(c).Padding(0, 12).Height(30).FocusRing(false).Disabled(!can).Children(func() {
			text := fmt.Sprintf("%d unmodified lines", n)
			if n == 1 {
				text = "1 unmodified line"
			}
			ui.Text(c, text).FontSize(12).TextColor(pal.gapText)
		})
		if label.Hovered() && can {
			label.Underline()
		}
		if label.Clicked() {
			switch {
			case n <= expandStep:
				all()
			case first:
				expand(0, expandStep)
			case last:
				expand(expandStep, 0)
			default:
				expand(expandStep, expandStep)
			}
		}
		if n > expandStep {
			b := ui.ButtonBase(c).Padding(3, 8).Radius(6).Disabled(!can).Children(func() {
				ui.Text(c, "Expand all").FontSize(12).FontWeight(600).TextColor(pal.gapText)
			})
			if b.Hovered() {
				b.Background(pal.hover)
			}
			if b.Clicked() {
				all()
			}
		}
		if h := int(r.gap); h < len(f.hunks) && f.hunks[h].Section != "" {
			ui.Text(c, f.hunks[h].Section).Font(tab.codeFont()).FontSize(12).TextColor(pal.gapText).SingleLine().Grow(1).Shrink(1).MinWidth(0)
		}
	})
}

// lineSide is what one side of a line row shows.
type lineSide struct {
	present bool
	kind    byte // ' ', '+' or '-'
	num     int
	text    string
	segs    []highlight.Seg
	words   []worddiff.Range
	hunk    int32
	index   int32 // the line's index in its hunk, -1 for expanded context
}

// sideOf returns a line of a row for the old or the new side.
func sideOf(f *reviewFile, split bool, r reviewRow, s diffSide) lineSide {
	if r.hunk < 0 {
		// Expanded context.
		ls := lineSide{present: true, kind: ' ', hunk: -1, index: -1}
		ls.text = f.contextText(r.old, r.new)
		if s == sideOld {
			ls.num = int(r.old)
			if int(r.old) <= len(f.oldHL) {
				ls.segs = f.oldHL[r.old-1]
			} else if int(r.new) <= len(f.newHL) {
				ls.segs = f.newHL[r.new-1]
			}
		} else {
			ls.num = int(r.new)
			if int(r.new) <= len(f.newHL) {
				ls.segs = f.newHL[r.new-1]
			}
		}
		return ls
	}
	idx := r.a
	if s == sideNew && split {
		idx = r.b
	}
	l := f.lineAt(r.hunk, idx)
	if l == nil {
		return lineSide{}
	}
	ls := lineSide{present: true, kind: l.Kind, text: l.Text, hunk: r.hunk, index: idx}
	if l.Kind == '-' {
		ls.num = l.Old
		if l.Old > 0 && l.Old <= len(f.oldHL) {
			ls.segs = f.oldHL[l.Old-1]
		}
	} else {
		ls.num = l.New
		if s == sideOld && l.Kind == ' ' {
			ls.num = l.Old
		}
		if l.New > 0 && l.New <= len(f.newHL) {
			ls.segs = f.newHL[l.New-1]
		}
	}
	if ls.segs == nil {
		ls.segs = f.hunkHL[[2]int{int(r.hunk), int(idx)}]
	}
	ls.words = f.words[[2]int{int(r.hunk), int(idx)}]
	return ls
}

// lineRow is a line of a hunk, or a pair of lines side by side.
func (tab *repoTab) lineRow(c *ui.Context, rv *review, f *reviewFile, r reviewRow) {
	pal := diffPaletteFor(c.Theme())
	gutter := tab.gutterWidth(c, rv, f)
	lh := tab.lineHeight()
	split := rv.split && !f.oneSided()
	wrap := tab.app.store.settings().WrapLines
	e := cardRow(c, pal).AlignItems(ui.Stretch).MinHeight(lh)
	if !wrap {
		handle := ui.Local(e, "input-handle", func() ui.Handle { return ui.Handle{} })
		e.Bind(handle)
		charWidth := tab.charWidth(c, rv)
		e.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputScroll || math.Abs(float64(ev.DX)) <= math.Abs(float64(ev.DY)) {
				return false
			}
			b := handle.Bounds(c)
			code := b.W - 2*gutter - 40
			if split {
				code = b.W/2 - gutter - 30
			}
			limit := max(float32(f.metrics().maxCols)*charWidth-code, 0)
			f.hscroll = min(max(f.hscroll+ev.DX, 0), limit)
			if tab.app.win != nil {
				tab.app.win.Invalidate()
			}
			return true
		})
	}
	if !split {
		s := sideOf(f, false, r, sideNew)
		sd := sideNew
		if s.kind == '-' {
			sd = sideOld
		}
		e.Children(func() {
			tab.lineCell(c, rv, f, r, s, sd, gutter, !f.oneSided(), wrap)
		})
		return
	}
	e.Children(func() {
		left := sideOf(f, true, r, sideOld)
		right := sideOf(f, true, r, sideNew)
		ui.Row(c).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Stretch).Children(func() {
			tab.lineCell(c, rv, f, r, left, sideOld, gutter, false, wrap)
		})
		ui.Box(c).Width(1).Shrink(0).Background(pal.cardBorder)
		ui.Row(c).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Stretch).Children(func() {
			tab.lineCell(c, rv, f, r, right, sideNew, gutter, false, wrap)
		})
	})
}

// lineCell shows a line: its numbers and its code.
func (tab *repoTab) lineCell(c *ui.Context, rv *review, f *reviewFile, r reviewRow, s lineSide, sd diffSide, gutter float32, unified, wrap bool) {
	pal := diffPaletteFor(c.Theme())
	lh := tab.lineHeight()
	if !s.present {
		ui.Box(c).Grow(1).Background(pal.emptySide).MinHeight(lh)
		return
	}
	bg, gutterBg, bar, numColor := pal.codeBg, pal.codeBg, ui.Transparent, pal.lineNumber
	var wordColor ui.Color
	switch s.kind {
	case '+':
		bg, gutterBg, bar, numColor, wordColor = pal.addBg, pal.addGutter, pal.addBar, pal.addBar, pal.addWord
	case '-':
		bg, gutterBg, bar, numColor, wordColor = pal.delBg, pal.delGutter, pal.delBar, pal.delBar, pal.delWord
	}
	cell := ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Stretch).Background(bg)
	if cell.Hovered() {
		cell.Background(blend(bg, pal.hover.Alpha(0.5)))
		gutterBg = blend(gutterBg, pal.hover.Alpha(0.5))
	}
	cell.ContextMenu(func(m *ui.Menu) {
		if m.Item("Copy Line").Chosen() {
			copyText(s.text)
		}
		if m.Item("Copy Path").Chosen() {
			copyText(f.path)
		}
	})
	cell.Children(func() {
		num := func(n int, width float32) {
			txt := ""
			if n > 0 {
				txt = strconv.Itoa(n)
			}
			ui.Text(c, txt).Font(tab.codeFont()).FontSize(tab.codeSize()-1).FixedLineHeight(lh).TextColor(numColor).
				Width(width).TextAlign(ui.End).Padding(0, 8, 0, 0).Shrink(0)
		}
		ui.Row(c).Shrink(0).AlignItems(ui.Start).Background(gutterBg).Children(func() {
			ui.Box(c).Width(4).AlignSelf(ui.Stretch).Background(bar)
			if unified {
				old, new := 0, 0
				if r.hunk < 0 {
					old, new = int(r.old), int(r.new)
				} else if l := f.lineAt(r.hunk, s.index); l != nil {
					old, new = l.Old, l.New
				}
				num(old, gutter-4)
				num(new, gutter-4)
			} else {
				num(s.num, gutter-4)
			}
		})
		spans := tab.lineSpans(c, f, s, sd, pal, wordColor)
		// The margin keeps the clipped code clear of the line numbers.
		code := ui.Box(c).Grow(1).Basis(0).MinWidth(0).ClipX().Margin(0, 10)
		code.Children(func() {
			text := ui.RichText(c, spans...).Font(tab.codeFont()).
				FontSize(tab.codeSize()).FixedLineHeight(lh).TextColor(pal.code)
			if !wrap {
				text.NoWrap().AlignSelf(ui.Start).Left(-f.hscroll)
			}
		})
	})
}

// lineSpans styles the code of a line, kept from frame to frame.
func (tab *repoTab) lineSpans(c *ui.Context, f *reviewFile, s lineSide, sd diffSide, pal *diffPalette, wordColor ui.Color) []ui.Span {
	key := spanKey{hunk: s.hunk, index: s.index, num: int32(s.num), side: sd, dark: c.Theme().Dark}
	if spans, ok := f.spans[key]; ok {
		return spans
	}
	var marks []mark
	for _, rg := range s.words {
		marks = append(marks, mark{Range: rg, color: wordColor})
	}
	spans := codeSpans(s.text, s.segs, marks, pal)
	if f.spans == nil {
		f.spans = map[spanKey][]ui.Span{}
	}
	f.spans[key] = spans
	return spans
}

// blend lays a translucent color over another.
func blend(base, over ui.Color) ui.Color { return over.Over(base) }
