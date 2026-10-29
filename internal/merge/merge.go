// Package merge splits a 3-way merge into chunks that are resolved one by
// one, and writes the result: the engine of the merge editor.
package merge

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Chunk is a stretch of the merged file: lines both sides agree on, or a
// conflict between the two.
type Chunk struct {
	// Common holds the lines of a chunk that is not in conflict, each with
	// its newline. Git merged it already, taking whatever one side changed.
	Common []string
	// Conflict marks a chunk where both sides changed the same lines.
	Conflict bool
	// Ours, Base and Theirs are the lines of a conflict on each side, and
	// as they were before either changed.
	Ours, Base, Theirs []string
}

// Choice is how a conflict is resolved.
type Choice uint8

const (
	Unresolved   Choice = iota // keeps the conflict markers
	TakeOurs                   // the local side
	TakeTheirs                 // the incoming side
	TakeBoth                   // ours, then theirs
	TakeBothBack               // theirs, then ours
	TakeBase                   // what it was before
)

// markerSize makes the markers long enough not to be mistaken for the
// file's own text.
const markerSize = 13

// Merge3 merges two versions of a file with their common ancestor and
// returns the chunks, in order. base may be empty, as for a file both sides
// added.
func Merge3(ours, base, theirs string) ([]Chunk, error) {
	dir, err := os.MkdirTemp("", "voir-merge-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	files := map[string]string{"ours": ours, "base": base, "theirs": theirs}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			return nil, err
		}
	}
	cmd := exec.Command("git", "merge-file", "-p", "--diff3", fmt.Sprintf("--marker-size=%d", markerSize),
		"-L", "ours", "-L", "base", "-L", "theirs",
		filepath.Join(dir, "ours"), filepath.Join(dir, "base"), filepath.Join(dir, "theirs"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		// The exit status is the number of conflicts, up to 127.
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() < 0 || ee.ExitCode() > 127 {
			return nil, fmt.Errorf("git merge-file: %v: %s", err, strings.TrimSpace(stderr.String()))
		}
	}
	return parse(stdout.String())
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.SplitAfter(s, "\n")
}

func parse(out string) ([]Chunk, error) {
	mark := func(c byte) string { return strings.Repeat(string(c), markerSize) }
	start, sepBase, sepTheirs, end := mark('<'), mark('|'), mark('='), mark('>')
	var chunks []Chunk
	var common []string
	flush := func() {
		if len(common) > 0 {
			chunks = append(chunks, Chunk{Common: common})
			common = nil
		}
	}
	state := 0 // 0 common, 1 ours, 2 base, 3 theirs
	var cur Chunk
	for _, l := range lines(out) {
		bare := strings.TrimRight(l, "\r\n")
		switch {
		case state == 0 && strings.HasPrefix(bare, start):
			flush()
			cur = Chunk{Conflict: true}
			state = 1
		case state == 1 && strings.HasPrefix(bare, sepBase):
			state = 2
		case (state == 1 || state == 2) && bare == sepTheirs:
			state = 3
		case state == 3 && strings.HasPrefix(bare, end):
			chunks = append(chunks, cur)
			state = 0
		case state == 0:
			common = append(common, l)
		case state == 1:
			cur.Ours = append(cur.Ours, l)
		case state == 2:
			cur.Base = append(cur.Base, l)
		default:
			cur.Theirs = append(cur.Theirs, l)
		}
	}
	if state != 0 {
		return nil, errors.New("the merge ended inside a conflict")
	}
	flush()
	return chunks, nil
}

// Conflicts counts the chunks in conflict.
func Conflicts(chunks []Chunk) int {
	n := 0
	for _, c := range chunks {
		if c.Conflict {
			n++
		}
	}
	return n
}

// Resolve writes the merged file. choices holds one Choice per chunk (a
// shorter slice leaves the rest unresolved); a conflict left unresolved keeps
// its markers, as git writes them.
func Resolve(chunks []Chunk, choices []Choice) string {
	var b strings.Builder
	for i, c := range chunks {
		if !c.Conflict {
			for _, l := range c.Common {
				b.WriteString(l)
			}
			continue
		}
		choice := Unresolved
		if i < len(choices) {
			choice = choices[i]
		}
		write := func(ls []string) {
			for _, l := range ls {
				b.WriteString(l)
			}
		}
		switch choice {
		case TakeOurs:
			write(c.Ours)
		case TakeTheirs:
			write(c.Theirs)
		case TakeBoth:
			write(c.Ours)
			write(c.Theirs)
		case TakeBothBack:
			write(c.Theirs)
			write(c.Ours)
		case TakeBase:
			write(c.Base)
		default:
			b.WriteString(strings.Repeat("<", 7) + " ours\n")
			write(c.Ours)
			b.WriteString(strings.Repeat("=", 7) + "\n")
			write(c.Theirs)
			b.WriteString(strings.Repeat(">", 7) + " theirs\n")
		}
	}
	return b.String()
}

// HasMarkers reports whether a text still holds conflict markers.
func HasMarkers(text string) bool {
	for _, l := range lines(text) {
		l = strings.TrimRight(l, "\r\n")
		if strings.HasPrefix(l, "<<<<<<< ") || l == strings.Repeat("=", 7) || strings.HasPrefix(l, ">>>>>>> ") {
			return true
		}
	}
	return false
}
