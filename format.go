package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eliott/myfork/internal/git"
)

// relativeTime says how long ago a time was, as "3 hours ago".
func relativeTime(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < 45*time.Second:
		return "just now"
	case d < 90*time.Second:
		return "a minute ago"
	case d < 45*time.Minute:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()+0.5))
	case d < 90*time.Minute:
		return "an hour ago"
	case d < 22*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()+0.5))
	case d < 36*time.Hour:
		return "yesterday"
	case d < 26*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24+0.5))
	case d < 320*24*time.Hour:
		return t.Format("Jan 2")
	}
	return t.Format("Jan 2, 2006")
}

// dateColumn is how the commit list writes a date: today's by their time.
func dateColumn(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return "Today " + t.Format("15:04")
	}
	if t.Year() == now.Year() {
		return t.Format("Jan 2 15:04")
	}
	return t.Format("Jan 2, 2006")
}

func abbreviateHome(p string) string {
	if home, err := os.UserHomeDir(); err == nil && (p == home || strings.HasPrefix(p, home+string(filepath.Separator))) {
		return "~" + p[len(home):]
	}
	return p
}

// errorText is what to tell the user about an error.
func errorText(err error) string {
	var ge *git.Error
	if errors.As(err, &ge) {
		return ge.Message()
	}
	return err.Error()
}

// firstLine is the first line of a text.
func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// statusLetter is the letter and the meaning Fork shows for a file's change.
func statusLetter(s byte) (letter string, meaning string) {
	switch s {
	case 'A':
		return "A", "Added"
	case 'M':
		return "M", "Modified"
	case 'D':
		return "D", "Deleted"
	case 'R':
		return "R", "Renamed"
	case 'C':
		return "C", "Copied"
	case 'T':
		return "T", "Type changed"
	case 'U':
		return "!", "Conflict"
	case '?':
		return "?", "Untracked"
	}
	return "·", ""
}

// dirAndBase splits a path for display: the file's name, and its folder.
func dirAndBase(p string) (dir, base string) {
	i := strings.LastIndexByte(p, '/')
	if i < 0 {
		return "", p
	}
	return p[:i+1], p[i+1:]
}
