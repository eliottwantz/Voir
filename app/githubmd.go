package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/eliott/voir/internal/github"
)

// GitHub renders the HTML in comments, which bots write: <details> that
// fold, <br> on a line of its own, and <!-- hidden --> notes. The markdown
// view shows those as text, so githubMarkdown reads them first.

var (
	detailsTag  = regexp.MustCompile(`(?im)(?:^[ \t]*(?:>[ \t]*)*<details(?:\s[^>]*)?>|</details\s*>)`)
	detailsOpen = regexp.MustCompile(`(?i)\sopen(?:[\s=/>])`)
	summaryTag  = regexp.MustCompile(`(?is)^[\s>]*<summary(?:\s[^>]*)?>(.*?)</summary\s*>`)
	lonelyBreak = regexp.MustCompile(`(?im)^((?:[ \t]*>)*)[ \t]*<br\s*/?>[ \t]*$`)
	anyTag      = regexp.MustCompile(`<[^>]*>`)

	quotedDetails = regexp.MustCompile(`(?im)^[ \t]*>[ \t]*<details`)
	pictureTag    = regexp.MustCompile(`(?is)<picture[^>]*>.*?</picture\s*>`)
	imgTag        = regexp.MustCompile(`(?i)<img\s[^>]*>`)
	linkTag       = regexp.MustCompile(`(?is)<a\s[^>]*?href\s*=\s*"([^"]*)"[^>]*>(.*?)</a\s*>`)
	headingTag    = regexp.MustCompile(`(?im)^[ \t]*<h([1-6])(?:\s[^>]*)?>(.*?)</h[1-6]\s*>[ \t]*$`)
	ruleTag       = regexp.MustCompile(`(?im)^[ \t]*<hr\s*/?>[ \t]*$`)
	paragraphTag  = regexp.MustCompile(`(?im)^[ \t]*<p(?:\s[^>]*)?>(.*?)(?:</p\s*>)?[ \t]*$|^[ \t]*</p\s*>[ \t]*$`)
	prNumber      = regexp.MustCompile(`(^|\s)(\(?)#(\d+)\b`)
	bareSHA       = regexp.MustCompile(`(^|[\s(\[])([0-9a-f]{40})($|[\s).,;<\]|])`)
	fenceLine     = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
)

// githubMarkdownOptions is shared by every view of GitHub's text, so that an
// image loads once and stays loaded.
var githubMarkdownOptions = &markdown.Options{OnLink: followLink, LoadImage: loadGitHubImage}

// fetchImages is off in tests, which have no network.
var fetchImages = false

var imageClient = &http.Client{Timeout: 30 * time.Second}

var imageTokens sync.Map // host → token, "" for none

// imageToken is the GitHub CLI's token for host, read once.
func imageToken(host string) string {
	if t, ok := imageTokens.Load(host); ok {
		return t.(string)
	}
	tok := github.Token(context.Background(), host)
	imageTokens.Store(host, tok)
	return tok
}

// loadGitHubImage downloads a picture of a comment: PNG, JPEG, GIF, WebP,
// BMP or SVG.
func loadGitHubImage(url string) (ui.ImageSource, error) {
	if !fetchImages || !(strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://")) {
		return nil, os.ErrNotExist
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	// Pictures attached to a private repository answer only to the signed-in
	// account; they redirect to a signed address, to which Go does not
	// forward the token.
	if tok := imageToken(req.URL.Host); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	res, err := imageClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image: %s", res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 20<<20))
	if err != nil {
		return nil, err
	}
	if strings.Contains(res.Header.Get("Content-Type"), "svg") || bytes.HasPrefix(bytes.TrimSpace(data), []byte("<svg")) {
		return ui.ParseSVG(data)
	}
	return ui.DecodeBitmap(data)
}

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

// htmlAttr is the value of attribute name in tag.
func htmlAttr(tag, name string) string {
	m := regexp.MustCompile(`(?i)\s` + name + `\s*=\s*"([^"]*)"`).FindStringSubmatch(tag)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// cleanGitHubMarkdown drops the hidden comments and the <br> that stand on
// a line of their own, which would show as text, turns the <picture>, <img>,
// <a>, <h1> and <hr> bots write into Markdown, shortens the commit hashes as
// GitHub does, and spells the emoji.
func cleanGitHubMarkdown(src string) string {
	return mapPlain(src, func(s string) string {
		s = htmlComment.ReplaceAllString(s, "")
		s = lonelyBreak.ReplaceAllString(s, "$1")
		// A picture is an icon, as a badge of the severity: its alt.
		s = pictureTag.ReplaceAllStringFunc(s, func(m string) string {
			if alt := htmlAttr(imgTag.FindString(m), "alt"); alt != "" {
				return "`" + alt + "`"
			}
			return ""
		})
		s = imgTag.ReplaceAllStringFunc(s, func(m string) string {
			if src := htmlAttr(m, "src"); src != "" {
				return "![" + htmlAttr(m, "alt") + "](" + src + ")"
			}
			return m
		})
		s = linkTag.ReplaceAllString(s, "[$2]($1)")
		s = headingTag.ReplaceAllStringFunc(s, func(m string) string {
			sub := headingTag.FindStringSubmatch(m)
			return strings.Repeat("#", int(sub[1][0]-'0')) + " " + strings.TrimSpace(sub[2])
		})
		s = ruleTag.ReplaceAllString(s, "---")
		s = paragraphTag.ReplaceAllString(s, "$1")
		s = prNumber.ReplaceAllString(s, "$1$2[#$3](#$3)")
		s = bareSHA.ReplaceAllStringFunc(s, func(m string) string {
			sub := bareSHA.FindStringSubmatch(m)
			return sub[1] + "`" + sub[2][:7] + "`" + sub[3]
		})
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

type quoteRun struct {
	text   string
	quoted bool
}

// quoteRuns cuts text at the lines quoted with >, which a blank line stays
// with the lines before.
func quoteRuns(text string) []quoteRun {
	var runs []quoteRun
	for _, line := range strings.SplitAfter(text, "\n") {
		if line == "" {
			continue
		}
		trim := strings.TrimSpace(line)
		quoted := strings.HasPrefix(strings.TrimLeft(line, " \t"), ">")
		if n := len(runs); n > 0 && (trim == "" || runs[n-1].quoted == quoted) {
			runs[n-1].text += line
			continue
		}
		runs = append(runs, quoteRun{line, quoted})
	}
	return runs
}

// unquote takes the first > off the lines of text.
func unquote(text string) string {
	lines := strings.SplitAfter(text, "\n")
	for i, line := range lines {
		l := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(l, ">") {
			l = strings.TrimPrefix(l[1:], " ")
		}
		lines[i] = l
	}
	return strings.Join(lines, "")
}

// githubText shows Markdown, whose quotes may hold <details>: those are cut
// out of the quotes, which show around what they fold.
func githubText(c *ui.Context, key, text string) {
	if !quotedDetails.MatchString(text) {
		githubRuns(c, key, text)
		return
	}
	ui.Column(c.Key(key)).FillWidth().Gap(8).Children(func() {
		for i, r := range quoteRuns(text) {
			k := fmt.Sprint(key, "-", i)
			switch {
			case r.quoted && quotedDetails.MatchString(r.text):
				ui.Column(c.Key(k)).FillWidth().BorderStartWidth(3).BorderColor(c.Theme().TextMuted.Alpha(0.35)).PaddingStart(12).Children(func() {
					githubMarkdown(c, k+"-q", unquote(r.text))
				})
			case strings.TrimSpace(r.text) != "":
				githubRuns(c, k, r.text)
			}
		}
	})
}

// githubMarkdown shows a comment, a description or a review as GitHub
// does: Markdown, with its <details> folding.
func githubMarkdown(c *ui.Context, key, src string) {
	parts := splitDetails(cleanGitHubMarkdown(src))
	if len(parts) == 0 {
		return
	}
	if len(parts) == 1 && !parts[0].details {
		githubText(c, key, parts[0].text)
		return
	}
	t := c.Theme()
	ui.Column(c.Key(key)).FillWidth().Gap(8).Children(func() {
		for i, p := range parts {
			k := fmt.Sprint(key, "-", i)
			if !p.details {
				githubText(c, k, p.text)
				continue
			}
			box := ui.Column(c.Key(k)).FillWidth().Gap(4)
			open := ui.Local(box, "open", func() bool { return p.open })
			box.Children(func() {
				// The button lies over the summary, whose text would take
				// the click for a selection.
				ui.Row(c.Key(k+"-head")).Gap(6).Padding(2, 4).AlignItems(ui.Center).AlignSelf(ui.Start).Children(func() {
					chevron := iconChevronRight
					if *open {
						chevron = iconChevronDown
					}
					ui.Icon(c, chevron).FontSize(13).TextColor(t.TextMuted)
					c.Key(k + "-summary")
					markdown.View(c, p.summary, githubMarkdownOptions)
					b := ui.ButtonBase(c.Key(k + "-toggle")).Absolute().Left(0).Top(0).Right(0).Bottom(0).Radius(5).
						Label(strings.Join(strings.Fields(anyTag.ReplaceAllString(p.summary, "")), " "))
					if b.Hovered() {
						b.Background(hoverBg.Alpha(0.5))
					}
					if b.Clicked() {
						*open = !*open
					}
				})
				if *open {
					ui.Column(c.Key(k + "-body")).FillWidth().PaddingStart(18).Children(func() {
						githubMarkdown(c, k+"-in", p.text)
					})
				}
			})
		}
	})
}

// mdTable is a pipe table of images, which the markdown view would show as
// their descriptions: its header's cells, its alignments and its rows.
type mdTable struct {
	head  []string
	align []ui.Align
	left  []bool // aligned left with ":-"
	rows  [][]string
}

// mdRun is a run of Markdown, or a table of images (table set).
type mdRun struct {
	text  string
	table *mdTable
}

var (
	tableDelim = regexp.MustCompile(`^[ \t]*\|?[ \t]*:?-+:?[ \t]*(\|[ \t]*:?-+:?[ \t]*)*\|?[ \t]*$`)
	mdImage    = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
)

// tableCells cuts a table's line at its pipes, but those escaped or in
// code.
func tableCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	if strings.HasSuffix(line, "|") && !strings.HasSuffix(line, `\|`) {
		line = line[:len(line)-1]
	}
	var cells []string
	var cur strings.Builder
	code := false
	for i := 0; i < len(line); i++ {
		switch ch := line[i]; {
		case ch == '\\' && i+1 < len(line) && line[i+1] == '|':
			if !code {
				cur.WriteByte('|')
			} else {
				cur.WriteString(`\|`)
			}
			i++
		case ch == '`':
			code = !code
			cur.WriteByte(ch)
		case ch == '|' && !code:
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

// splitImageTables cuts text, outside its fenced code, at the pipe tables
// whose cells hold images, to show them as GitHub does.
func splitImageTables(text string) []mdRun {
	if !strings.Contains(text, "![") || !strings.Contains(text, "|") {
		return []mdRun{{text: text}}
	}
	var runs []mdRun
	last := 0
	for _, span := range plainSpans(text) {
		lines := strings.SplitAfter(text[span[0]:span[1]], "\n")
		off := span[0]
		at := make([]int, len(lines)+1) // the offset of each line
		for i, l := range lines {
			at[i] = off
			off += len(l)
		}
		at[len(lines)] = off
		for i := 0; i+1 < len(lines); i++ {
			head := lines[i]
			if !strings.Contains(head, "|") || !tableDelim.MatchString(strings.TrimRight(lines[i+1], "\r\n")) ||
				strings.HasPrefix(strings.TrimLeft(head, " \t"), ">") {
				continue
			}
			t := &mdTable{head: tableCells(head)}
			delim := tableCells(lines[i+1])
			if len(delim) != len(t.head) {
				continue
			}
			for _, d := range delim {
				switch {
				case strings.HasPrefix(d, ":") && strings.HasSuffix(d, ":"):
					t.align = append(t.align, ui.Center)
				case strings.HasSuffix(d, ":"):
					t.align = append(t.align, ui.End)
				default:
					t.align = append(t.align, ui.Start)
				}
				t.left = append(t.left, strings.HasPrefix(d, ":"))
			}
			end := i + 2
			for ; end < len(lines); end++ {
				l := strings.TrimSpace(lines[end])
				if l == "" || !strings.Contains(l, "|") {
					break
				}
				row := tableCells(l)
				row = append(row, make([]string, max(0, len(t.head)-len(row)))...)
				t.rows = append(t.rows, row[:len(t.head)])
			}
			if !mdImage.MatchString(strings.Join(lines[i:end], "")) {
				i = end - 1
				continue
			}
			if s := text[last:at[i]]; strings.TrimSpace(s) != "" {
				runs = append(runs, mdRun{text: s})
			}
			runs = append(runs, mdRun{table: t})
			last = at[end]
			i = end - 1
		}
	}
	if s := text[last:]; strings.TrimSpace(s) != "" || len(runs) == 0 {
		runs = append(runs, mdRun{text: s})
	}
	return runs
}

// githubRuns shows Markdown, with its tables of images as GitHub shows them.
func githubRuns(c *ui.Context, key, text string) {
	runs := splitImageTables(text)
	if len(runs) == 1 && runs[0].table == nil {
		c.Key(key)
		markdown.View(c, text, githubMarkdownOptions).FillWidth()
		return
	}
	ui.Column(c.Key(key)).FillWidth().Gap(12).Children(func() {
		for i, r := range runs {
			k := fmt.Sprint(key, "-t", i)
			if r.table == nil {
				c.Key(k)
				markdown.View(c, r.text, githubMarkdownOptions).FillWidth()
				continue
			}
			imageTable(c, k, r.table)
		}
	})
}

// imageTable shows a table whose cells hold images: its columns share the
// width, and each cell is Markdown, so an image alone in one shows as the
// image, as wide as its column.
func imageTable(c *ui.Context, key string, t *mdTable) {
	th := markdown.DefaultTheme(c.Theme())
	grid := ui.Column(c.Key(key)).FillWidth().Border(1, th.TableBorder).Radius(5).Clip().Role(ui.RoleTable)
	grid.Children(func() {
		row := func(n int, cells []string, header bool) {
			r := ui.Row(c.Key(fmt.Sprint(key, "-r", n))).FillWidth().AlignItems(ui.Stretch).Role(ui.RoleRow)
			if header {
				r.Background(th.TableHeader)
			}
			if n > 0 {
				r.BorderWidth(1, 0, 0, 0).BorderColor(th.TableBorder)
			}
			r.Children(func() {
				for k, cell := range cells {
					// A header's cells center, as a browser's do, unless aligned.
					align := t.align[k]
					if header && align == ui.Start && !t.left[k] {
						align = ui.Center
					}
					box := ui.Column(c.Key(fmt.Sprint(key, "-c", n, "-", k))).Grow(1).Basis(0).MinWidth(0).
						Padding(6, 10).AlignItems(align)
					if k > 0 {
						box.BorderWidth(0, 0, 0, 1).BorderColor(th.TableBorder)
					}
					if header {
						box.Role(ui.RoleColumnHeader)
						if cell != "" && !mdImage.MatchString(cell) {
							cell = "**" + cell + "**"
						}
					} else {
						box.Role(ui.RoleCell)
					}
					box.Children(func() {
						if cell != "" {
							c.Key(fmt.Sprint(key, "-m", n, "-", k))
							// A cell's images fit its column; a header's text
							// sits where it is aligned.
							v := markdown.View(c, cell, githubMarkdownOptions)
							if !header {
								v.FillWidth()
							}
						}
					})
				}
			})
		}
		row(0, t.head, true)
		for i, cells := range t.rows {
			row(i+1, cells, false)
		}
	})
}
