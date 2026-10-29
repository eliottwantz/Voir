package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/eliott/voir/internal/highlight"
)

// diffPalette is the colors of the files' cards and their diffs, in the
// light or the dark: those of codiff and of the diff renderer it uses.
type diffPalette struct {
	appBg      ui.Color // behind the cards
	code       ui.Color // the code's own color
	lineNumber ui.Color
	codeBg     ui.Color // the background of the cards
	addBg      ui.Color
	addGutter  ui.Color
	addWord    ui.Color
	addBar     ui.Color
	delBg      ui.Color
	delGutter  ui.Color
	delWord    ui.Color
	delBar     ui.Color
	emptySide  ui.Color // the side of a split row without a line
	gapBg      ui.Color // "N unmodified lines"
	gapText    ui.Color
	cardBorder ui.Color
	headerBg   ui.Color
	pill       ui.Color // the background of counts
	hover      ui.Color // what is under the pointer
	syntax     [highlight.NumClasses]ui.Color
}

var (
	lightDiff = diffPalette{
		appBg:      ui.Hex("#f8f8f6"),
		code:       ui.Hex("#111111"),
		lineNumber: ui.Hex("#9a9a9a"),
		codeBg:     ui.Hex("#ffffff"),
		addBg:      ui.Hex("#e2f7ea"),
		addGutter:  ui.Hex("#d3f3df"),
		addWord:    ui.RGBA(13, 190, 78, 0.20),
		addBar:     ui.Hex("#0dbe4e"),
		delBg:      ui.Hex("#ffe6e8"),
		delGutter:  ui.Hex("#ffd9dc"),
		delWord:    ui.RGBA(255, 46, 63, 0.17),
		delBar:     ui.Hex("#ff2e3f"),
		emptySide:  ui.Hex("#fafafa"),
		gapBg:      ui.Hex("#f4f4f4"),
		gapText:    ui.Hex("#8a8a8a"),
		cardBorder: ui.RGBA(17, 17, 17, 0.10),
		headerBg:   ui.Hex("#ffffff"),
		pill:       ui.RGBA(127, 127, 127, 0.11),
		hover:      ui.RGBA(127, 127, 127, 0.11),
	}
	darkDiff = diffPalette{
		appBg:      ui.Hex("#141414"),
		code:       ui.Hex("#c8c8c8"),
		lineNumber: ui.Hex("#6c6c6c"),
		codeBg:     ui.Hex("#1c1c1c"),
		addBg:      ui.Hex("#22382a"),
		addGutter:  ui.Hex("#2a4632"),
		addWord:    ui.RGBA(94, 204, 113, 0.22),
		addBar:     ui.Hex("#5ecc71"),
		delBg:      ui.Hex("#40262a"),
		delGutter:  ui.Hex("#52302f"),
		delWord:    ui.RGBA(255, 103, 98, 0.24),
		delBar:     ui.Hex("#ff6762"),
		emptySide:  ui.Hex("#202020"),
		gapBg:      ui.Hex("#262626"),
		gapText:    ui.Hex("#8a8a8a"),
		cardBorder: ui.RGBA(230, 230, 230, 0.10),
		headerBg:   ui.Hex("#232323"),
		pill:       ui.RGBA(127, 127, 127, 0.16),
		hover:      ui.RGBA(127, 127, 127, 0.16),
	}
)

func init() {
	// Licht and Dunkel, codiff's themes.
	light := map[highlight.Class]string{
		highlight.Comment: "#919191", highlight.Preproc: "#adadad", highlight.Keyword: "#352de3",
		highlight.Type: "#c56e0e", highlight.LangConst: "#626fc9", highlight.Function: "#284181",
		highlight.ClassName: "#bb28c7", highlight.Exception: "#f93232", highlight.Number: "#dd3c2f",
		highlight.String: "#00a33f", highlight.Escape: "#00a33f", highlight.Regexp: "#699d36",
		highlight.Tag: "#0072c8", highlight.Attribute: "#0072c8", highlight.Property: "#444444",
		highlight.Heading: "#111111", highlight.Inserted: "#1a7f37", highlight.Deleted: "#c4232e",
	}
	dark := map[highlight.Class]string{
		highlight.Comment: "#919191", highlight.Preproc: "#adadad", highlight.Keyword: "#6a93cf",
		highlight.Type: "#eb9a3d", highlight.LangConst: "#7b8cfd", highlight.Function: "#7b8cfd",
		highlight.ClassName: "#f06efb", highlight.Exception: "#f93232", highlight.Number: "#db584d",
		highlight.String: "#52ce81", highlight.Escape: "#52ce81", highlight.Regexp: "#699d36",
		highlight.Tag: "#6a93cf", highlight.Attribute: "#6a93cf", highlight.Property: "#c8c8c8",
		highlight.Heading: "#e8e8e8", highlight.Inserted: "#56d364", highlight.Deleted: "#ff7b72",
	}
	lightDiff.syntax[highlight.Plain] = lightDiff.code
	darkDiff.syntax[highlight.Plain] = darkDiff.code
	for k, v := range light {
		lightDiff.syntax[k] = ui.Hex(v)
	}
	for k, v := range dark {
		darkDiff.syntax[k] = ui.Hex(v)
	}
}

func diffPaletteFor(t *ui.Theme) *diffPalette {
	if t.Dark {
		return &darkDiff
	}
	return &lightDiff
}
