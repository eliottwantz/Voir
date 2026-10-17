package git

import (
	"fmt"
	"strings"
)

// PatchLine is a line of a hunk.
type PatchLine struct {
	// Kind is ' ' for a line both sides have, '-' for one deleted, '+' for
	// one added, '\\' for the "No newline at end of file" marker.
	Kind byte
	Text string
	// Old and New are the line's numbers in the old and the new file, 0
	// where it has none.
	Old, New int
}

// PatchHunk is a hunk with its lines.
type PatchHunk struct {
	OldStart, NewStart int
	Section            string
	Lines              []PatchLine
}

// Patch is the patch of one file, read line by line.
type Patch struct {
	Header string
	Hunks  []PatchHunk
}

// ParsePatchLines reads the patch of one file.
func ParsePatchLines(patch string) (*Patch, error) {
	head, raw := SplitPatch(patch)
	p := &Patch{Header: head}
	for _, h := range raw {
		lines := strings.Split(strings.TrimSuffix(h, "\n"), "\n")
		var hunk PatchHunk
		var oldN, newN int
		if _, err := fmt.Sscanf(lines[0], "@@ -%d,%d +%d,%d @@", &hunk.OldStart, new(int), &hunk.NewStart, new(int)); err != nil {
			// A count of 1 is left out: "@@ -3 +3,2 @@".
			if !parseShortHeader(lines[0], &hunk) {
				return nil, fmt.Errorf("bad hunk header %q", lines[0])
			}
		}
		if i := strings.Index(lines[0][2:], "@@"); i >= 0 {
			hunk.Section = strings.TrimSpace(lines[0][2+i+2:])
		}
		oldN, newN = hunk.OldStart, hunk.NewStart
		for _, l := range lines[1:] {
			if l == "" {
				// A blank context line whose space was trimmed.
				l = " "
			}
			pl := PatchLine{Kind: l[0], Text: l[1:]}
			switch l[0] {
			case ' ':
				pl.Old, pl.New = oldN, newN
				oldN++
				newN++
			case '-':
				pl.Old = oldN
				oldN++
			case '+':
				pl.New = newN
				newN++
			case '\\':
			default:
				return nil, fmt.Errorf("bad patch line %q", l)
			}
			hunk.Lines = append(hunk.Lines, pl)
		}
		p.Hunks = append(p.Hunks, hunk)
	}
	return p, nil
}

func parseShortHeader(line string, h *PatchHunk) bool {
	rest, ok := strings.CutPrefix(line, "@@ -")
	if !ok {
		return false
	}
	parts := strings.Fields(rest)
	if len(parts) < 2 {
		return false
	}
	num := func(s string) (int, bool) {
		s = strings.TrimLeft(s, "+-")
		a, _, _ := strings.Cut(s, ",")
		var n int
		_, err := fmt.Sscanf(a, "%d", &n)
		return n, err == nil
	}
	o, ok1 := num(parts[0])
	n, ok2 := num(parts[1])
	h.OldStart, h.NewStart = o, n
	return ok1 && ok2
}

// LineID names a line of a patch: a hunk, and a line of it.
type LineID struct{ Hunk, Line int }

// Changed reports whether a line adds or deletes.
func (l PatchLine) Changed() bool { return l.Kind == '+' || l.Kind == '-' }

// ChangedLines lists the lines of a patch that add or delete.
func (p *Patch) ChangedLines() []LineID {
	var ids []LineID
	for hi, h := range p.Hunks {
		for li, l := range h.Lines {
			if l.Changed() {
				ids = append(ids, LineID{hi, li})
			}
		}
	}
	return ids
}

// Partial writes a patch holding only the selected changes. To stage lines,
// apply it to the index; to unstage or discard them, the patch of the
// index or of the work tree is cut with reverse set, and applied with -R.
//
// A change left out stays as it is in the file the patch applies to: when
// applying forward, an unselected deletion is context, an unselected
// addition is dropped; when reversing, an unselected addition is context
// (the file has it), an unselected deletion is dropped.
func (p *Patch) Partial(selected map[LineID]bool, reverse bool) (string, bool) {
	var b strings.Builder
	b.WriteString(p.Header)
	any := false
	for hi, h := range p.Hunks {
		var body strings.Builder
		oldN, newN := 0, 0
		changed := false
		prev := byte(0)
		for li, l := range h.Lines {
			switch l.Kind {
			case ' ':
				body.WriteString(" " + l.Text + "\n")
				oldN++
				newN++
				prev = ' '
			case '\\':
				// The marker belongs to the line before it: keep it when
				// that line is in the patch.
				if prev != 0 {
					body.WriteString("\\" + l.Text + "\n")
				}
			case '-', '+':
				sel := selected[LineID{hi, li}]
				switch {
				case sel:
					body.WriteString(string(l.Kind) + l.Text + "\n")
					if l.Kind == '-' {
						oldN++
					} else {
						newN++
					}
					changed = true
					prev = l.Kind
				case l.Kind == '-' && !reverse, l.Kind == '+' && reverse:
					// Not changed after all: the line stays as context.
					body.WriteString(" " + l.Text + "\n")
					oldN++
					newN++
					prev = ' '
				default:
					prev = 0
				}
			}
		}
		if !changed {
			continue
		}
		any = true
		oldStart, newStart := h.OldStart, h.NewStart
		if oldN == 0 {
			oldStart = max(oldStart-1, 0)
		}
		if newN == 0 {
			newStart = max(newStart-1, 0)
		}
		sec := ""
		if h.Section != "" {
			sec = " " + h.Section
		}
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@%s\n%s", oldStart, oldN, newStart, newN, sec, body.String())
	}
	return b.String(), any
}

// StageLines stages the selected changes of a file's work tree patch.
func (r *Repo) StageLines(patch string, selected map[LineID]bool) error {
	p, err := ParsePatchLines(patch)
	if err != nil {
		return err
	}
	part, ok := p.Partial(selected, false)
	if !ok {
		return fmt.Errorf("no line selected")
	}
	return r.ApplyPatch(part, true, false)
}

// UnstageLines takes the selected changes of a file's staged patch out of
// the index.
func (r *Repo) UnstageLines(patch string, selected map[LineID]bool) error {
	p, err := ParsePatchLines(patch)
	if err != nil {
		return err
	}
	part, ok := p.Partial(selected, true)
	if !ok {
		return fmt.Errorf("no line selected")
	}
	return r.ApplyPatch(part, true, true)
}

// DiscardLines throws away the selected changes of a file's work tree patch.
func (r *Repo) DiscardLines(patch string, selected map[LineID]bool) error {
	p, err := ParsePatchLines(patch)
	if err != nil {
		return err
	}
	part, ok := p.Partial(selected, true)
	if !ok {
		return fmt.Errorf("no line selected")
	}
	return r.ApplyPatch(part, false, true)
}

// IntentToAdd records a new file in the index with no content, so that its
// lines show as a patch that can be staged in parts.
func (r *Repo) IntentToAdd(path string) error {
	_, err := r.Do("add", "-N", "--", path)
	return err
}
