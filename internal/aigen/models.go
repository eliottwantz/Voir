package aigen

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
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

var modelLine = regexp.MustCompile(`^[\w.-]+/\S+$`)

// Models asks the program which models it has: Claude Code its own list, over
// the control protocol; Codex a catalog; OpenCode names them provider/model.
func Models(ctx context.Context, cfg Config) ([]ModelChoice, error) {
	switch cfg.Provider {
	case Claude:
		return claudeModels(ctx, cfg)
	case Codex:
		return codexModels(ctx, cfg)
	case OpenCode:
		return openCodeModels(ctx, cfg)
	}
	return nil, nil
}

// claudeModels asks Claude Code for the models its own picker offers, over the
// control protocol it reads on standard input: initialize, then list_models.
// No generation runs, and the list follows the installed version and the
// account, so it is what this Mac's Claude Code can actually run.
func claudeModels(ctx context.Context, cfg Config) ([]ModelChoice, error) {
	bin, err := Find(Claude, cfg.Binary)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin,
		"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--settings", `{"disableAllHooks":true}`, "--tools", "", "--disable-slash-commands",
		"--strict-mcp-config", "--permission-mode", "dontAsk")
	cmd.Env = environment()
	cmd.Stdin = strings.NewReader(
		"{\"type\":\"control_request\",\"request_id\":\"init\",\"request\":{\"subtype\":\"initialize\"}}\n" +
			"{\"type\":\"control_request\",\"request_id\":\"models\",\"request\":{\"subtype\":\"list_models\"}}\n")
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var models []ModelChoice
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		var msg struct {
			Type     string `json:"type"`
			Response struct {
				Subtype   string `json:"subtype"`
				RequestID string `json:"request_id"`
				Response  struct {
					Models []struct {
						Value       string `json:"value"`
						DisplayName string `json:"displayName"`
					} `json:"models"`
				} `json:"response"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(line), &msg) != nil || msg.Type != "control_response" ||
			msg.Response.RequestID != "models" || msg.Response.Subtype != "success" {
			continue
		}
		for _, m := range msg.Response.Response.Models {
			if m.Value != "" {
				models = append(models, ModelChoice{ID: m.Value, Name: cmp.Or(m.DisplayName, m.Value)})
			}
		}
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("Claude Code listed no models")
	}
	return models, nil
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
