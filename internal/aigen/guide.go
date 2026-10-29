package aigen

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/eliott/voir/internal/git"
)

// Guide is a tour of a pull request: stops through its diff, in the order to
// read them.
type Guide struct {
	Number     int
	Head       string // the commit it was written for
	BaseBranch string
	Stops      []Stop
	Provider   string
	Model      string
	Written    time.Time
}

// Stop is a step of a guide.
type Stop struct {
	Title     string     `json:"title"`
	Summary   string     `json:"summary"`
	Attention *Attention `json:"attention"`
	Excerpts  []Excerpt  `json:"excerpts"`
	Files     []FileNote `json:"files"`
	Notes     []LineNote `json:"notes"`
	Questions []string   `json:"questions"`
}

// Attention is the one thing to judge at a stop.
type Attention struct {
	Kind string `json:"kind"` // tradeoff, contract, product, risk, accessibility
	Note string `json:"note"`
}

// Excerpt is code worth showing: the lines Start to End of a file on a side of
// the diff, with the text they have.
type Excerpt struct {
	Path    string   `json:"path"`
	Side    string   `json:"side"` // new, old
	Start   int      `json:"start"`
	End     int      `json:"end"`
	Caption string   `json:"caption"`
	Lines   []string `json:"lines,omitempty"`
}

// FileNote is a changed file a stop owns.
type FileNote struct {
	Path string `json:"path"`
	Note string `json:"note"`
}

// LineNote explains a line of an excerpt.
type LineNote struct {
	Path  string `json:"path"`
	Line  int    `json:"line"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

const maxExcerptLines = 25

// hunkRange is the lines a hunk header gives, or ok false for none.
type hunkRange struct{ start, end int }

// change is what the diff of a pull request touches: the files, and for each
// the lines of its hunks on each side.
type change struct {
	files []string
	hunks map[string]map[string][]hunkRange // path -> side -> ranges
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func rangeOf(start, count string) (hunkRange, bool) {
	s, _ := strconv.Atoi(start)
	n := 1
	if count != "" {
		n, _ = strconv.Atoi(count)
	}
	if n == 0 {
		return hunkRange{}, false
	}
	return hunkRange{s, s + n - 1}, true
}

// parseChange reads the names of the changed files (NUL separated) and a
// zero-context patch.
func parseChange(nameOnly, patch string) change {
	ch := change{hunks: map[string]map[string][]hunkRange{}}
	for _, f := range strings.Split(nameOnly, "\x00") {
		if f != "" {
			ch.files = append(ch.files, f)
		}
	}
	var oldPath string
	var cur map[string][]hunkRange
	inHunks := false
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			oldPath, cur, inHunks = "", nil, false
		case !inHunks && strings.HasPrefix(line, "--- "):
			oldPath = ""
			if strings.HasPrefix(line, "--- a/") {
				oldPath = line[6:]
			}
		case !inHunks && strings.HasPrefix(line, "+++ "):
			path := oldPath
			if strings.HasPrefix(line, "+++ b/") {
				path = line[6:]
			}
			if path == "" {
				continue
			}
			if ch.hunks[path] == nil {
				ch.hunks[path] = map[string][]hunkRange{}
			}
			cur = ch.hunks[path]
		case cur != nil:
			m := hunkHeader.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			inHunks = true
			if r, ok := rangeOf(m[1], m[2]); ok {
				cur["old"] = append(cur["old"], r)
			}
			if r, ok := rangeOf(m[3], m[4]); ok {
				cur["new"] = append(cur["new"], r)
			}
		}
	}
	return ch
}

// clamp cuts an excerpt to the hunks it overlaps; ok is false when it overlaps
// none.
func (ch change) clamp(e Excerpt) (Excerpt, bool) {
	start, end := min(e.Start, e.End), max(e.Start, e.End)
	var over []hunkRange
	for _, h := range ch.hunks[e.Path][e.Side] {
		if h.start <= end && start <= h.end {
			over = append(over, h)
		}
	}
	if len(over) == 0 {
		return e, false
	}
	e.Start, e.End = max(start, over[0].start), min(end, over[len(over)-1].end)
	return e, true
}

// mergeExcerpts joins excerpts of one file and side that overlap or touch,
// and keeps none over 25 lines.
func mergeExcerpts(in []Excerpt) []Excerpt {
	var out []Excerpt
	for _, e := range in {
		i := slices.IndexFunc(out, func(o Excerpt) bool {
			return o.Path == e.Path && o.Side == e.Side && o.Start <= e.End+1 && e.Start <= o.End+1
		})
		if i < 0 {
			out = append(out, e)
			continue
		}
		o := &out[i]
		o.Start, o.End = min(o.Start, e.Start), max(o.End, e.End)
		if o.Caption == "" {
			o.Caption = e.Caption
		}
	}
	for i := range out {
		out[i].End = min(out[i].End, out[i].Start+maxExcerptLines-1)
	}
	return out
}

// normalize makes a model's guide safe to show: excerpts clamped to the diff,
// notes on lines that are shown, every changed file placed once.
func normalize(stops []Stop, ch change) []Stop {
	changed := map[string]bool{}
	for _, f := range ch.files {
		changed[f] = true
	}
	var shaped []Stop
	for _, s := range stops {
		if strings.TrimSpace(s.Title) == "" {
			continue
		}
		var ex []Excerpt
		for _, e := range s.Excerpts {
			if !changed[e.Path] || (e.Side != "new" && e.Side != "old") {
				continue
			}
			if c, ok := ch.clamp(e); ok {
				ex = append(ex, c)
			}
		}
		s.Excerpts = mergeExcerpts(ex)
		if s.Attention != nil && strings.TrimSpace(s.Attention.Note) == "" {
			s.Attention = nil
		}
		var files []FileNote
		for _, f := range s.Files {
			if changed[f.Path] {
				files = append(files, FileNote{Path: f.Path, Note: strings.TrimSpace(f.Note)})
			}
		}
		s.Files = files
		var notes []LineNote
		for _, n := range s.Notes {
			if strings.TrimSpace(n.Title) == "" {
				continue
			}
			if slices.ContainsFunc(s.Excerpts, func(e Excerpt) bool { return e.Path == n.Path && e.Start <= n.Line && n.Line <= e.End }) {
				notes = append(notes, n)
			}
		}
		s.Notes = notes
		var qs []string
		for _, q := range s.Questions {
			if strings.TrimSpace(q) != "" {
				qs = append(qs, q)
			}
		}
		s.Questions = qs
		shaped = append(shaped, s)
	}
	placed := map[string]bool{}
	for _, s := range shaped {
		for _, e := range s.Excerpts {
			placed[e.Path] = true
		}
	}
	for i := range shaped {
		var files []FileNote
		for _, f := range shaped[i].Files {
			if !placed[f.Path] {
				placed[f.Path] = true
				files = append(files, f)
			}
		}
		shaped[i].Files = files
	}
	var rest []FileNote
	for _, f := range ch.files {
		if !placed[f] {
			rest = append(rest, FileNote{Path: f})
		}
	}
	if len(rest) > 0 {
		shaped = append(shaped, Stop{Title: "Other files", Summary: "Changed files the tour does not otherwise cover.", Files: rest})
	}
	return shaped
}

// WriteGuide writes the guide of a pull request whose head is the commit head.
// The head is checked out in a worktree of its own, off in a temporary
// directory, which the model may read but not change. layers are filled with
// the heads that can be read here.
func WriteGuide(ctx context.Context, cfg Config, r *git.Repo, remote, head string, in GuideInput) (*Guide, error) {
	if !r.HasCommit(head) {
		return nil, fmt.Errorf("the commits of #%d are not in this repository", in.Number)
	}
	stack := slices.Clone(in.Stack)
	for i, l := range stack {
		switch {
		case l.Number == in.Number:
			stack[i].Head = head
		case l.Head != "" && !r.HasCommit(l.Head) && remote != "":
			r.FetchRef(ctx, remote, fmt.Sprintf("pull/%d/head", l.Number))
			if !r.HasCommit(l.Head) {
				stack[i].Head = ""
			}
		}
	}
	in.Stack = stack

	tmp, err := os.MkdirTemp("", "voir-guide-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	path := filepath.Join(tmp, "checkout")
	if _, err := r.DoContext(ctx, "worktree", "add", "--detach", "--quiet", path, head); err != nil {
		return nil, err
	}
	defer r.Do("worktree", "remove", "--force", path)
	w, err := git.Open(path)
	if err != nil {
		return nil, err
	}

	req := GuidePrompt(in)
	req.Dir = path
	raw, err := Generate(ctx, cfg, req)
	if err != nil {
		return nil, err
	}
	var out struct {
		Stops []Stop `json:"stops"`
	}
	if err := decode(cfg, req, raw, &out); err != nil {
		return nil, err
	}
	rng := in.BaseCommit + "...HEAD"
	names, err := w.Read("-c", "core.quotePath=false", "diff", "--no-ext-diff", "--name-only", "-z", rng)
	if err != nil {
		return nil, err
	}
	patch, err := w.Read("-c", "core.quotePath=false", "diff", "--no-ext-diff", "--unified=0", "--no-color", rng)
	if err != nil {
		return nil, err
	}
	stops := normalize(out.Stops, parseChange(string(names), string(patch)))
	// The text of the excerpts is kept with the guide: the worktree is gone.
	cache := map[string][]string{}
	lines := func(rev, file string) []string {
		key := rev + ":" + file
		if l, ok := cache[key]; ok {
			return l
		}
		b, err := w.Read("show", key)
		var l []string
		if err == nil {
			l = strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
		}
		cache[key] = l
		return l
	}
	for i := range stops {
		for j, e := range stops[i].Excerpts {
			src := lines("HEAD", e.Path)
			if e.Side == "old" {
				src = lines(in.BaseCommit, e.Path)
			}
			if e.Start >= 1 && e.End <= len(src) {
				stops[i].Excerpts[j].Lines = slices.Clone(src[e.Start-1 : e.End])
			}
		}
	}
	return &Guide{
		Number: in.Number, Head: head, BaseBranch: in.BaseBranch, Stops: stops,
		Provider: string(cfg.Provider), Model: cfg.Model, Written: time.Now(),
	}, nil
}
