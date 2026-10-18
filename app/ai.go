package app

import (
	"cmp"
	"context"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/aigen"
)

// aiProvider is the provider chosen.
func (s AISettings) provider() aigen.Provider {
	switch aigen.Provider(s.Provider) {
	case aigen.Codex, aigen.OpenCode:
		return aigen.Provider(s.Provider)
	}
	return aigen.Claude
}

// config is how to run the provider chosen.
func (s AISettings) config() aigen.Config {
	p := s.provider()
	return aigen.Config{Provider: p, Binary: s.Binaries[string(p)], Model: s.Models[string(p)], Effort: s.Efforts[string(p)]}
}

func (s AISettings) style() aigen.Style {
	return aigen.Style{Mode: s.Style, Custom: s.Custom, FollowTemplates: !s.IgnoreTemplates}
}

// aiConfig is what the settings say to write with.
func (a *app) aiConfig() (aigen.Config, aigen.Style) {
	ai := a.store.settings().AI
	return ai.config(), ai.style()
}

// setAI changes the AI settings.
func (a *app) setAI(fn func(*AISettings)) {
	a.store.update(func(s *Settings) {
		fn(&s.AI)
	})
}

// setPerProvider sets what is kept for each provider.
func (s *AISettings) setPerProvider(kind, value string) {
	var m *map[string]string
	switch kind {
	case "model":
		m = &s.Models
	case "effort":
		m = &s.Efforts
	default:
		m = &s.Binaries
	}
	if *m == nil {
		*m = map[string]string{}
	}
	(*m)[string(s.provider())] = strings.TrimSpace(value)
}

// aiModels are the models to offer for the provider chosen, asked of the
// program once and kept: the id to give it, and the name to show.
func (a *app) aiModels() []aigen.ModelChoice {
	cfg, _ := a.aiConfig()
	a.mu.Lock()
	cached, ok := a.modelList[cfg.Provider]
	a.mu.Unlock()
	if ok {
		return cached
	}
	a.mu.Lock()
	if a.modelList == nil {
		a.modelList = map[aigen.Provider][]aigen.ModelChoice{}
	}
	a.modelList[cfg.Provider] = nil
	a.mu.Unlock()
	a.background(func() {
		models, _ := aigen.Models(context.Background(), cfg)
		a.mu.Lock()
		a.modelList[cfg.Provider] = models
		a.mu.Unlock()
		a.update(func() {})
	})
	return nil
}

// modelName is the name of the model chosen, as the program listed it; "" when
// the list is not there yet, or does not know the model.
func (a *app) modelName(p aigen.Provider, model string) string {
	if model == "" {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, m := range a.modelList[p] {
		if m.ID == model || m.Name == model {
			return m.Name
		}
	}
	return ""
}

// aiPrefs is the AI page of the preferences: who writes, with which model, and
// in what style.
func (a *app) aiPrefs(c *ui.Context, ai AISettings) {
	t := c.Theme()
	cfg := ai.config()
	row := func(label string, body func()) {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, label).Width(130).TextColor(t.TextMuted).TextAlign(ui.End).Shrink(0)
			body()
		})
	}
	ui.Text(c, "Commit messages, pull requests and review guides are written by a program on this Mac, which is run with the changes and nothing else.").FontSize(12).TextColor(t.TextMuted)
	row("Provider:", func() {
		names := make([]string, len(aigen.Providers))
		for i, p := range aigen.Providers {
			names[i] = p.Label()
		}
		v := cfg.Provider.Label()
		if ui.Select(c, &v, names).Changed() {
			a.setAI(func(s *AISettings) {
				for _, p := range aigen.Providers {
					if p.Label() == v {
						s.Provider = string(p)
					}
				}
			})
		}
	})
	row("Model:", func() {
		v := cfg.Model
		placeholder := map[aigen.Provider]string{aigen.Claude: "Claude Code's own choice", aigen.Codex: "Codex's own choice", aigen.OpenCode: "provider/model"}[cfg.Provider]
		in := ui.TextInput(c.Key("ai-model-"+string(cfg.Provider)), &v).Placeholder(placeholder).Grow(1).Label("Model")
		if in.Changed() {
			a.setAI(func(s *AISettings) { s.setPerProvider("model", v) })
		}
		if models := a.aiModels(); len(models) > 0 {
			// The names are shown, and the id is what is kept.
			names := []string{"Choose…"}
			for _, m := range models {
				names = append(names, m.Name)
			}
			pick := "Choose…"
			if cur := a.modelName(cfg.Provider, cfg.Model); cur != "" {
				pick = cur
			}
			if ui.Select(c, &pick, names).Changed() {
				for _, m := range models {
					if m.Name == pick {
						a.setAI(func(s *AISettings) { s.setPerProvider("model", m.ID) })
					}
				}
			}
		}
	})
	if name := a.modelName(cfg.Provider, cfg.Model); name != "" && name != cfg.Model {
		row("", func() {
			ui.Textf(c, "Runs %s (%s)", name, cfg.Model).FontSize(12).TextColor(t.TextMuted)
		})
	}
	if efforts := aigen.Efforts(cfg.Provider); len(efforts) > 0 {
		row("Effort:", func() {
			names := make([]string, len(efforts))
			for i, e := range efforts {
				names[i] = cmp.Or(e, "Default")
			}
			v := cmp.Or(cfg.Effort, efforts[0])
			if cfg.Effort == "" && efforts[0] != "" {
				v = efforts[0]
			}
			shown := cmp.Or(v, "Default")
			if ui.Select(c, &shown, names).Changed() {
				a.setAI(func(s *AISettings) {
					s.setPerProvider("effort", map[bool]string{true: "", false: shown}[shown == "Default"])
				})
			}
		})
	}
	row("Program:", func() {
		v := cfg.Binary
		path, err := aigen.Find(cfg.Provider, cfg.Binary)
		ph := string(cfg.Provider)
		if err == nil {
			ph = path
		}
		in := ui.TextInput(c.Key("ai-bin-"+string(cfg.Provider)), &v).Placeholder(ph).Grow(1).Label("Program")
		if in.Changed() {
			a.setAI(func(s *AISettings) { s.setPerProvider("binary", v) })
		}
	})
	if _, err := aigen.Find(cfg.Provider, cfg.Binary); err != nil {
		ui.Text(c, err.Error()).FontSize(12).TextColor(t.Danger)
	}
	row("Style:", func() {
		names := make([]string, len(aigen.Modes))
		cur := aigen.Modes[0].Label
		for i, m := range aigen.Modes {
			names[i] = m.Label
			if m.Mode == ai.Style {
				cur = m.Label
			}
		}
		if ui.Select(c, &cur, names).Changed() {
			a.setAI(func(s *AISettings) {
				for _, m := range aigen.Modes {
					if m.Label == cur {
						s.Style = m.Mode
					}
				}
			})
		}
	})
	if ai.Style == "custom" {
		v := ai.Custom
		if ui.TextArea(c, &v).Lines(3, 8).Placeholder("How commit messages and pull requests should be written").Label("Custom instructions").Changed() {
			a.setAI(func(s *AISettings) { s.Custom = v })
		}
	}
	a.prefCheck(c, "Follow the repository's pull request template", !ai.IgnoreTemplates, func(s *Settings, on bool) { s.AI.IgnoreTemplates = !on }, func(fn func(*Settings)) { a.store.update(fn) })
	ui.Text(c, "Preferences for review guides:").FontSize(12).TextColor(t.TextMuted)
	notes := ai.GuideNotes
	if ui.TextArea(c, &notes).Lines(2, 6).Placeholder("Tone, emphasis, what to dwell on or skip").Label("Review guide preferences").Changed() {
		a.setAI(func(s *AISettings) { s.GuideNotes = notes })
	}
}

// writeCommitMessage writes the message of the commit of what is staged, off
// the main thread, and puts it in the box. then, if any, runs once it is there.
func (t *repoTab) writeCommitMessage(then ...func()) {
	if t.aiWriting {
		return
	}
	t.aiWriting = true
	a, repo := t.app, t.repo
	cfg, style := a.aiConfig()
	a.background(func() {
		c, err := aigen.WriteCommit(context.Background(), cfg, style, repo)
		a.update(func() {
			t.aiWriting = false
			if err != nil {
				a.fail("Writing the commit message failed", err)
				return
			}
			t.msg = c.Message()
			for _, fn := range then {
				fn()
			}
		})
	})
}
