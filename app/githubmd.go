package app

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// GitHub renders the HTML in comments, which bots write: <details> that
// fold, <br> on a line of its own, and <!-- hidden --> notes. The markdown
// view shows those as text, so githubMarkdown reads them first.

var (
	detailsTag  = regexp.MustCompile(`(?im)(?:^[ \t]*<details(?:\s[^>]*)?>|</details\s*>)`)
	detailsOpen = regexp.MustCompile(`(?i)\sopen(?:[\s=/>])`)
	summaryTag  = regexp.MustCompile(`(?is)^\s*<summary(?:\s[^>]*)?>(.*?)</summary\s*>`)
	lonelyBreak = regexp.MustCompile(`(?im)^((?:[ \t]*>)*)[ \t]*<br\s*/?>[ \t]*$`)
	anyTag      = regexp.MustCompile(`<[^>]*>`)
	fenceLine   = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
)

// mdPart is a run of Markdown, or a <details> with its summary and the
// Markdown it folds.
type mdPart struct {
	text    string
	details bool
	open    bool
	summary string
}

// plainSpans returns the ranges of src outside fenced code blocks, which
// the HTML in them is left alone in.
func plainSpans(src string) [][2]int {
	var spans [][2]int
	start, pos := 0, 0
	fence := ""
	for pos < len(src) {
		end := strings.IndexByte(src[pos:], '\n')
		if end < 0 {
			end = len(src)
		} else {
			end += pos + 1
		}
		line := src[pos:end]
		if m := fenceLine.FindStringSubmatch(line); m != nil {
			switch {
			case fence == "":
				if pos > start {
					spans = append(spans, [2]int{start, pos})
				}
				fence, start = m[1], pos
			case m[1][0] == fence[0] && len(m[1]) >= len(fence) && strings.TrimSpace(line[len(m[0]):]) == "":
				fence, start = "", end
			}
		}
		pos = end
	}
	if fence == "" && start < len(src) {
		spans = append(spans, [2]int{start, len(src)})
	}
	return spans
}

// mapPlain applies f to the parts of src outside fenced code blocks.
func mapPlain(src string, f func(string) string) string {
	var sb strings.Builder
	last := 0
	for _, s := range plainSpans(src) {
		sb.WriteString(src[last:s[0]])
		sb.WriteString(f(src[s[0]:s[1]]))
		last = s[1]
	}
	sb.WriteString(src[last:])
	return sb.String()
}

// emojis are the shortcodes bots write that GitHub shows as emoji.
var emojis = map[string]string{
	"white_check_mark": "✅", "heavy_check_mark": "✔️", "x": "❌", "warning": "⚠️",
	"rotating_light": "🚨", "tada": "🎉", "rocket": "🚀", "sparkles": "✨", "bug": "🐛",
	"eyes": "👀", "memo": "📝", "construction": "🚧", "red_circle": "🔴", "large_orange_circle": "🟠",
	"large_yellow_circle": "🟡", "large_green_circle": "🟢", "green_circle": "🟢", "bulb": "💡",
	"information_source": "ℹ️", "no_entry": "⛔", "+1": "👍", "-1": "👎",
}

var emojiCode = regexp.MustCompile(`:([a-z0-9_+-]+):`)

// cleanGitHubMarkdown drops the hidden comments and the <br> that stand on
// a line of their own, which would show as text, and spells the emoji.
func cleanGitHubMarkdown(src string) string {
	return mapPlain(src, func(s string) string {
		s = htmlComment.ReplaceAllString(s, "")
		s = lonelyBreak.ReplaceAllString(s, "$1")
		return emojiCode.ReplaceAllStringFunc(s, func(m string) string {
			if e, ok := emojis[m[1:len(m)-1]]; ok {
				return e
			}
			return m
		})
	})
}

// splitDetails cuts src, cleaned, at its <details> blocks, which nest.
func splitDetails(src string) []mdPart {
	var parts []mdPart
	add := func(s string) {
		if strings.TrimSpace(s) != "" {
			parts = append(parts, mdPart{text: s})
		}
	}
	finish := func(openTag, body string) {
		p := mdPart{details: true, open: detailsOpen.MatchString(openTag + ">"), summary: "Details"}
		if m := summaryTag.FindStringSubmatchIndex(body); m != nil {
			if s := strings.Join(strings.Fields(body[m[2]:m[3]]), " "); s != "" {
				p.summary = s
			}
			body = body[m[1]:]
		}
		p.text = body
		parts = append(parts, p)
	}
	depth, pos, start := 0, 0, 0
	openTag := ""
	for _, span := range plainSpans(src) {
		for _, m := range detailsTag.FindAllStringIndex(src[span[0]:span[1]], -1) {
			from, to := span[0]+m[0], span[0]+m[1]
			tag := strings.TrimSpace(src[from:to])
			if !strings.HasPrefix(tag, "</") {
				if depth == 0 {
					add(src[pos:from])
					start, openTag = to, tag
				}
				depth++
			} else if depth > 0 {
				depth--
				if depth == 0 {
					finish(openTag, src[start:from])
					pos = to
				}
			}
		}
	}
	if depth > 0 {
		finish(openTag, src[start:])
	} else {
		add(src[pos:])
	}
	return parts
}

// githubMarkdown shows a comment, a description or a review as GitHub
// does: Markdown, with its <details> folding.
func githubMarkdown(c *ui.Context, key, src string) {
	parts := splitDetails(cleanGitHubMarkdown(src))
	if len(parts) == 0 {
		return
	}
	if len(parts) == 1 && !parts[0].details {
		markdown.View(c, parts[0].text, &markdown.Options{OnLink: openInBrowser}).FillWidth()
		return
	}
	t := c.Theme()
	ui.Column(c).Key(key).FillWidth().Gap(8).Children(func() {
		for i, p := range parts {
			k := fmt.Sprint(key, "-", i)
			if !p.details {
				c.Key(k)
				markdown.View(c, p.text, &markdown.Options{OnLink: openInBrowser}).FillWidth()
				continue
			}
			box := ui.Column(c).Key(k).FillWidth().Gap(4)
			open := ui.Local(box, "open", func() bool { return p.open })
			box.Children(func() {
				// The button lies over the summary, whose text would take
				// the click for a selection.
				ui.Row(c).Key(k+"-head").Gap(6).Padding(2, 4).AlignItems(ui.Center).AlignSelf(ui.Start).Children(func() {
					chevron := iconChevronRight
					if *open {
						chevron = iconChevronDown
					}
					ui.Icon(c, chevron).FontSize(13).TextColor(t.TextMuted)
					c.Key(k + "-summary")
					markdown.View(c, p.summary, &markdown.Options{OnLink: openInBrowser})
					b := ui.ButtonBase(c).Key(k + "-toggle").Absolute().Left(0).Top(0).Right(0).Bottom(0).Radius(5).
						Label(strings.Join(strings.Fields(anyTag.ReplaceAllString(p.summary, "")), " "))
					if b.Hovered() {
						b.Background(hoverBg.Alpha(0.5))
					}
					if b.Clicked() {
						*open = !*open
					}
				})
				if *open {
					ui.Column(c).Key(k + "-body").FillWidth().PaddingStart(18).Children(func() {
						githubMarkdown(c, k+"-in", p.text)
					})
				}
			})
		}
	})
}
