package aigen

import (
	"context"
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

// SuggestedModels are models worth offering without asking the program: the
// aliases Claude Code takes. Codex and OpenCode name theirs (see Models).
func SuggestedModels(p Provider) []string {
	if p == Claude {
		return []string{"haiku", "sonnet", "opus"}
	}
	return nil
}

var modelLine = regexp.MustCompile(`^[\w.-]+/\S+$`)

// Models asks OpenCode which models it has, as provider/model; the others
// have no such list.
func Models(ctx context.Context, cfg Config) ([]string, error) {
	if cfg.Provider != OpenCode {
		return SuggestedModels(cfg.Provider), nil
	}
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
	var models []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); modelLine.MatchString(l) {
			models = append(models, l)
		}
	}
	slices.Sort(models)
	return models, nil
}
