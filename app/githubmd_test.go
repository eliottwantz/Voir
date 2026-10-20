package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

const codacyComment = "## Up to standards :white_check_mark:\n\n" +
	"<details><summary><strong>🟢 Issues</strong>  <code>0 issues</code></summary>\n\n" +
	"> <br/>\n>\n> **Results:** `0` new issues\n>\n> [View in Codacy](https://app.codacy.com/x)\n\n" +
	"</details>\n\n" +
	"<!-- a1b2c3d4-e5f6 -->\n"

func TestSplitDetailsFoldsTheSummary(t *testing.T) {
	parts := splitDetails(cleanGitHubMarkdown(codacyComment))
	if len(parts) != 2 || parts[0].details || !parts[1].details {
		t.Fatalf("parts %+v", parts)
	}
	if parts[1].summary != "<strong>🟢 Issues</strong> <code>0 issues</code>" {
		t.Errorf("summary %q", parts[1].summary)
	}
	if strings.Contains(parts[1].text, "<br") || !strings.Contains(parts[1].text, "**Results:**") {
		t.Errorf("body %q", parts[1].text)
	}
	if strings.Contains(parts[0].text, "<!--") {
		t.Errorf("hidden comment kept: %q", parts[0].text)
	}
}

func TestSplitDetailsNestsAndKeepsCode(t *testing.T) {
	src := "```html\n<details><summary>no</summary>\n```\n\n<details open>\n<summary>a</summary>\n\n<details>\n<summary>b</summary>\ninner\n</details>\n\nafter\n</details>\n"
	parts := splitDetails(cleanGitHubMarkdown(src))
	if len(parts) != 2 || parts[0].details || !parts[1].details || !parts[1].open || parts[1].summary != "a" {
		t.Fatalf("parts %+v", parts)
	}
	inner := splitDetails(parts[1].text)
	if len(inner) != 2 || !inner[0].details || inner[0].summary != "b" || strings.TrimSpace(inner[1].text) != "after" {
		t.Fatalf("inner %+v", inner)
	}
}

func TestSplitDetailsClosesWhatIsLeftOpen(t *testing.T) {
	parts := splitDetails("<details><summary>s</summary>text")
	if len(parts) != 1 || !parts[0].details || parts[0].text != "text" {
		t.Fatalf("parts %+v", parts)
	}
}

func TestGitHubMarkdownShowsDetailsAsGitHubDoes(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Padding(10).Children(func() { githubMarkdown(c, "md", codacyComment) })
	}, 700, 400)
	tt.Frame()
	for _, bad := range []string{"<details>", "<summary>", "<strong>", "<br/>", "<!--"} {
		if tt.HasText(bad) {
			t.Errorf("raw %q shows: %q", bad, tt.Texts())
		}
	}
	if !tt.HasText("Up to standards ✅") || !tt.HasText("🟢 Issues 0 issues") || tt.HasText("View in Codacy") {
		t.Fatalf("closed: %q", tt.Texts())
	}
	if err := tt.Click("🟢 Issues 0 issues"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("View in Codacy") || !tt.HasText("new issues") {
		t.Fatalf("open: %q", tt.Texts())
	}
	snapshot(t, tt, "github-details")
}

const codacyCoverage = "## Up to standards :white_check_mark:\n\n" +
	"<details><summary>🟢 <strong>Coverage</strong> <code>69.74% diff coverage</code></summary>\n\n" +
	"> | Metric | Results |\n> | --- | --- |\n> | Coverage variation | Report missing for 5846dd791b6b9b576303e3ec9c64f60464432590<sup>1</sup> |\n>\n" +
	"> [View coverage diff in Codacy](https://app.codacy.com/x)\n>\n" +
	"> <details><summary>Coverage variation details</summary>\n>\n" +
	"> | | Coverable lines |\n> | --- | --- |\n> | Head commit | 101323 |\n>\n" +
	"> **Coverage variation** is the difference\n> </details>\n>\n" +
	"> <details><summary>Diff coverage details</summary>\n>\n> Diff coverage is the percentage\n> </details>\n>\n" +
	"> <sup>1</sup> Codacy didn't receive coverage data.\n\n" +
	"</details>\n"

func TestGitHubMarkdownNestsDetailsInQuotes(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Padding(10).Children(func() { githubMarkdown(c, "md", codacyCoverage) })
	}, 900, 700)
	tt.Frame()
	if err := tt.Click("🟢 Coverage 69.74% diff coverage"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, bad := range []string{"<details>", "<summary>", "</details>"} {
		if tt.HasText(bad) {
			t.Errorf("raw %q shows: %q", bad, tt.Texts())
		}
	}
	for _, want := range []string{"Coverage variation details", "Diff coverage details", "View coverage diff in Codacy"} {
		if !tt.HasText(want) {
			t.Fatalf("no %q: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("Coverage variation details"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Coverage variation is the difference") {
		t.Errorf("the nested details did not open: %q", tt.Texts())
	}
	snapshot(t, tt, "github-nested-details")
}

func TestCleanGitHubMarkdownReadsTheHTMLOfBots(t *testing.T) {
	src := "- <picture><source media=\"(prefers-color-scheme: dark)\" srcset=\"a.svg\"><img src=\"b.png\" alt=\"Medium severity\" width=\"62\"></picture> <a href=\"https://x/y\">AGENTS.md uses imports</a> · New\n" +
		"<h3>Title</h3>\n<hr>\n<p align=\"center\">centred</p>\n" +
		"Head 81344f68048955c1b4ca6d978125c8709a3b7e99 and `81344f68048955c1b4ca6d978125c8709a3b7e99` and https://x/commit/81344f68048955c1b4ca6d978125c8709a3b7e99\n"
	got := cleanGitHubMarkdown(src)
	for _, want := range []string{"- `Medium severity` [AGENTS.md uses imports](https://x/y) · New", "### Title", "---", "\ncentred\n", "Head `81344f6` and `81344f68048955c1b4ca6d978125c8709a3b7e99` and https://x/commit/81344f68048955c1b4ca6d978125c8709a3b7e99"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in %q", want, got)
		}
	}
	if strings.Contains(got, "<") {
		t.Errorf("tags left: %q", got)
	}
}
