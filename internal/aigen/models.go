package aigen

import (
	"cmp"
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Efforts are the reasoning efforts a provider takes, "" being its own choice;
// none for a provider that has no such option.
func Efforts(p Provider) []string {
	switch p {
	case Claude:
		return []string{"", "low", "medium", "high", "xhigh", "max"}
	case Codex:
		return []string{"low", "medium", "high"}
	}
	return nil
}

// ModelChoice is a model to offer: what --model is given, and its name.
type ModelChoice struct{ ID, Name string }

// claudeModels are the Claude models to offer, with the alias Claude Code
// takes for the latest of each family.
var claudeModels = []struct {
	ModelChoice
	Alias string
}{
	{ModelChoice{"claude-fable-5-1", "Fable 5.1"}, ""},
	{ModelChoice{"claude-opus-5-5", "Opus 5.5"}, "opus"},
	{ModelChoice{"claude-sonnet-5-5", "Sonnet 5.5"}, "sonnet"},
	{ModelChoice{"claude-haiku-5-5", "Haiku 5.5"}, "haiku"},
}

// SuggestedModels are models worth offering without asking the program: the
// current Claude models. Codex and OpenCode name theirs (see Models).
func SuggestedModels(p Provider) []ModelChoice {
	if p != Claude {
		return nil
	}
	out := make([]ModelChoice, len(claudeModels))
	for i, m := range claudeModels {
		out[i] = m.ModelChoice
	}
	return out
}

// ModelName is the name of a model that is known, by its id or its alias, "" for
// any other: "sonnet" is "Sonnet 5.5".
func ModelName(p Provider, model string) string {
	if p != Claude {
		return ""
	}
	for _, m := range claudeModels {
		if model == m.ID || (m.Alias != "" && model == m.Alias) {
			return m.Name
		}
	}
	return ""
}

var modelLine = regexp.MustCompile(`^[\w.-]+/\S+$`)

// Models asks the program which models it has. OpenCode names them
// provider/model; Codex has a catalog. Claude's are known.
func Models(ctx context.Context, cfg Config) ([]ModelChoice, error) {
	switch cfg.Provider {
	case Codex:
		return codexModels(ctx, cfg)
	case OpenCode:
		return openCodeModels(ctx, cfg)
	}
	return nil, nil
}

// codexCatalog is the shape of `codex debug models`: every model the program
// knows, most of which it does not offer.
type codexCatalog struct {
	Models []struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"display_name"`
		Visibility  string `json:"visibility"`
	} `json:"models"`
}

// codexModels are the models Codex lists to choose from, in its own order.
func codexModels(ctx context.Context, cfg Config) ([]ModelChoice, error) {
	bin, err := Find(Codex, cfg.Binary)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "debug", "models")
	cmd.Env = environment()
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var catalog codexCatalog
	if err := json.Unmarshal(out, &catalog); err != nil {
		return nil, err
	}
	var models []ModelChoice
	for _, m := range catalog.Models {
		if m.Slug == "" || m.Visibility == "hide" {
			continue
		}
		models = append(models, ModelChoice{ID: m.Slug, Name: cmp.Or(m.DisplayName, m.Slug)})
	}
	return models, nil
}

// openCodeModels are the models OpenCode has, as provider/model.
func openCodeModels(ctx context.Context, cfg Config) ([]ModelChoice, error) {
	bin, err := Find(OpenCode, cfg.Binary)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "models")
	cmd.Env = environment()
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var models []ModelChoice
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); modelLine.MatchString(l) {
			models = append(models, ModelChoice{ID: l, Name: l})
		}
	}
	slices.SortFunc(models, func(a, b ModelChoice) int { return strings.Compare(a.ID, b.ID) })
	return models, nil
}
