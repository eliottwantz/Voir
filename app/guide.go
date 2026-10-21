package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/aigen"
	"github.com/eliott/myfork/internal/github"
)

// guideFile is where the guide of a pull request is kept: in the cache, which
// the system may empty, as a guide can be written again.
func (a *app) guideFile(repo github.Repo, n int) string {
	dir := a.guideDir
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(base, "MyFork", "guides")
	}
	name := strings.NewReplacer("/", "_", ":", "_").Replace(fmt.Sprintf("%s_%s_%s_%d.json", repo.Host, repo.Owner, repo.Name, n))
	return filepath.Join(dir, name)
}

// loadGuide reads the guide kept for a pull request, nil when there is none.
func (a *app) loadGuide(repo github.Repo, n int) *aigen.Guide {
	path := a.guideFile(repo, n)
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var g aigen.Guide
	if json.Unmarshal(b, &g) != nil || g.Number != n {
		return nil
	}
	return &g
}

func (a *app) saveGuide(repo github.Repo, g *aigen.Guide) {
	path := a.guideFile(repo, g.Number)
	if path == "" {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	if b, err := json.Marshal(g); err == nil {
		os.WriteFile(path, b, 0o644)
	}
}

var htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

// settledReview is a line for each review thread that is resolved or outdated:
// what the guide is told not to raise again.
func settledReview(rv *github.Reviews) string {
	if rv == nil {
		return ""
	}
	var lines []string
	for _, th := range rv.Threads {
		if (!th.Resolved && !th.Outdated) || len(th.Comments) == 0 {
			continue
		}
		who := th.Comments[0].Author.Login
		if who == "" {
			who = "ghost"
		}
		state := "outdated"
		if th.Resolved {
			state = "resolved"
		}
		body := strings.Join(strings.Fields(htmlComment.ReplaceAllString(th.Comments[0].Body, "")), " ")
		if r := []rune(body); len(r) > 200 {
			body = string(r[:200]) + "…"
		}
		lines = append(lines, fmt.Sprintf("- %s (%s, %s): %s", th.Path, state, who, body))
	}
	return strings.Join(lines, "\n")
}

// writeGuide writes the review guide of the pull request, off the main thread:
// it takes minutes, as the model reads the change.
func (t *repoTab) writeGuide(pv *prView) {
	if pv.guideRunning || !pv.loaded || t.gh == nil {
		return
	}
	pv.guideRunning, pv.guideErr = true, ""
	a, repo, remote, ghRepo, pr := t.app, t.repo, t.ghRemote, *t.gh, pv.pr
	cfg, _ := a.aiConfig()
	notes := a.store.settings().AI.GuideNotes
	var stack []aigen.GuideLayer
	if pv.stack != nil {
		for _, e := range pv.stack.Entries {
			state := e.State
			if e.Merged() {
				state = "merged"
			}
			stack = append(stack, aigen.GuideLayer{Number: e.Number, Title: e.Title, State: state, Head: e.Head.SHA})
		}
	}
	settled := settledReview(pv.reviews)
	a.background(func() {
		ctx := context.Background()
		// The commits of the pull request have to be here: the model reads them.
		base, err := a.fetchPR(ctx, repo, remote, pr.Number, pr)
		var g *aigen.Guide
		if err == nil {
			g, err = aigen.WriteGuide(ctx, cfg, repo, remote, pr.Head.SHA, aigen.GuideInput{
				Number: pr.Number, Title: pr.Title, Description: pr.Body, BaseBranch: pr.Base.Ref,
				BaseCommit: base, Settled: settled, Stack: stack, Instructions: notes,
			})
		}
		if g != nil {
			a.saveGuide(ghRepo, g)
		}
		a.update(func() {
			pv.guideRunning = false
			if err != nil {
				pv.guideErr = errorText(err)
				return
			}
			pv.guide = g
		})
	})
}

// prGuide is the guide of the pull request: a tour of its changes in stops.
func (tab *repoTab) prGuide(c *ui.Context, pv *prView) {
	t := c.Theme()
	cfg, _ := tab.app.aiConfig()
	write := func(label string, primary bool) {
		var b ui.Element
		if primary {
			b = ui.PrimaryButton(c.Key("write-guide"), label)
		} else {
			b = ui.Button(c.Key("write-guide"), label)
		}
		if b.Disabled(pv.guideRunning || !pv.loaded).Clicked() {
			tab.writeGuide(pv)
		}
	}
	switch {
	case pv.guideRunning:
		ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
			ui.Spinner(c).Label("Writing the guide")
			ui.Textf(c, "%s is reading the changes. This can take a few minutes.", cfg.Provider.Label()).FontSize(13).TextColor(t.TextMuted)
		})
		return
	case pv.guide == nil:
		emptyPanel(c, "No review guide yet", "A guide walks through the changes in the order to read them, with what to judge on the way. "+cfg.Provider.Label()+" writes it from a checkout of the pull request, which it reads and does not change.", func() {
			write("Write the Guide", true)
		})
		if pv.guideErr != "" {
			ui.Text(c, pv.guideErr).FontSize(12).TextColor(t.Danger).Selectable().Padding(0, 24)
		}
		return
	}
	g := pv.guide
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Padding(14, 18, 24).Gap(14).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconSparkles).FontSize(14).TextColor(t.TextMuted)
				ui.Textf(c, "Written by %s · %s", aigen.Provider(g.Provider).Label(), relativeTime(c.Now(), g.Written)).FontSize(12.5).TextColor(t.TextMuted)
				ui.Spacer(c)
				write("Write it Again", false)
			})
			if g.Head != pv.pr.Head.SHA && pv.pr.Head.SHA != "" {
				ui.Text(c, "The pull request changed since this guide was written.").FontSize(12.5).Padding(8, 10).Radius(8).Background(t.Warning.Alpha(0.15)).Border(1, t.Warning.Alpha(0.4))
			}
			if pv.guideErr != "" {
				ui.Text(c, pv.guideErr).FontSize(12).TextColor(t.Danger).Selectable()
			}
			for i, s := range g.Stops {
				guideStop(c, i, s)
			}
		})
	})
}

// attentionLabel is how a kind of attention is called.
var attentionLabel = map[string]string{"tradeoff": "Tradeoff", "contract": "Contract", "product": "Product call", "risk": "Risk", "accessibility": "Accessibility"}

func guideStop(c *ui.Context, i int, s aigen.Stop) {
	t := c.Theme()
	ui.Column(c).Key(fmt.Sprint("stop", i)).Gap(8).Padding(12, 14).Radius(10).Border(1, hairline).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Textf(c, "%d", i+1).FontSize(12).FontWeight(700).Size(22, 22).Radius(11).Center().Background(subtleBg).Shrink(0)
			ui.Text(c, s.Title).FontSize(15).FontWeight(600).Selectable()
		})
		if s.Summary != "" {
			ui.Text(c, s.Summary).FontSize(13.5).Selectable()
		}
		if s.Attention != nil {
			col := t.Warning
			ui.Column(c).Gap(2).Padding(8, 10).Radius(8).Background(col.Alpha(0.12)).Border(1, col.Alpha(0.4)).Children(func() {
				ui.Text(c, attentionLabel[s.Attention.Kind]).FontSize(11.5).FontWeight(700).TextColor(col)
				ui.Text(c, s.Attention.Note).FontSize(13).Selectable()
			})
		}
		for j, e := range s.Excerpts {
			guideExcerpt(c, fmt.Sprint(i, "-", j), e, s.Notes)
		}
		if len(s.Files) > 0 {
			ui.Column(c).Gap(2).Children(func() {
				for _, f := range s.Files {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, iconFile).FontSize(13).TextColor(t.TextMuted).Shrink(0)
						ui.Text(c, f.Path).Font(mono).FontSize(12).SingleLine().Shrink(1).MinWidth(0)
						if f.Note != "" {
							ui.Text(c, f.Note).FontSize(12).TextColor(t.TextMuted).SingleLine().MinWidth(0)
						}
					})
				}
			})
		}
		for _, q := range s.Questions {
			ui.Text(c, "? "+q).FontSize(13).TextColor(t.TextMuted).Selectable()
		}
	})
}

// guideExcerpt is lines of a file, as they are on a side of the diff, with the
// notes on them.
func guideExcerpt(c *ui.Context, key string, e aigen.Excerpt, notes []aigen.LineNote) {
	t := c.Theme()
	ui.Column(c).Key("ex"+key).Gap(0).Radius(8).Border(1, hairline).Children(func() {
		ui.Row(c).Gap(8).Padding(5, 10).Background(subtleBg).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("%s:%d–%d", e.Path, e.Start, e.End)).Font(mono).FontSize(12).SingleLine().MinWidth(0).Shrink(1)
			if e.Side == "old" {
				ui.Text(c, "removed").FontSize(11).TextColor(t.Danger).Shrink(0)
			}
			if e.Caption != "" {
				ui.Text(c, e.Caption).FontSize(12).TextColor(t.TextMuted).SingleLine().MinWidth(0)
			}
		})
		for k, line := range e.Lines {
			n := e.Start + k
			bg := t.Success.Alpha(0.08)
			if e.Side == "old" {
				bg = t.Danger.Alpha(0.08)
			}
			ui.Row(c).Gap(10).Padding(0, 10).Background(bg).Children(func() {
				ui.Textf(c, "%d", n).Font(mono).FontSize(12).TextColor(t.TextMuted).Width(36).TextAlign(ui.End).Shrink(0)
				ui.Text(c, line).Font(mono).FontSize(12).Selectable()
			})
			for _, note := range notes {
				if note.Path == e.Path && note.Line == n {
					ui.Column(c).Gap(2).Padding(6, 10).Background(t.Accent.Alpha(0.1)).Children(func() {
						ui.Text(c, note.Title).FontSize(12.5).FontWeight(600)
						if note.Body != "" {
							ui.Text(c, note.Body).FontSize(12.5).Selectable()
						}
					})
				}
			}
		}
	})
}
