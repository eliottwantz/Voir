package app

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/eliott/myfork/internal/git"
	"github.com/eliott/myfork/internal/merge"
)

type mergeMode uint8

const (
	mergeText   mergeMode = iota // both sides have text: the 3-way view
	mergeGone                    // one side deleted the file
	mergeBinary                  // the file is not text
)

// mergeState is the sheet that resolves one conflicted file: the two sides
// chunk by chunk, and the result below, which can be edited too.
type mergeState struct {
	tab     *repoTab
	path    string
	loading bool
	err     string
	mode    mergeMode
	// gone says which side deleted the file: "local" or "incoming".
	gone string

	chunks  []merge.Chunk
	choices []merge.Choice

	result    *editor.State
	generated string
	version   uint64
	manual    bool
	height    float32
	saving    bool
}

// sides names the two sides: a rebase replays your commits onto the other
// branch, so what git calls "ours" is the branch, and "theirs" your commit.
func (st *mergeState) sides() (local, incoming string) {
	if st.tab.opState == "rebase" {
		return "Branch rebased onto", "Your commit"
	}
	return "Local", "Incoming"
}

// mergeDialog opens the merge editor on a conflicted file.
func (a *app) mergeEditor(tab *repoTab, p string) {
	st := &mergeState{tab: tab, path: p, loading: true, height: 360}
	a.sheet = &sheet{open: true, title: "Resolve " + p, body: st.view, state: st, wide: true}
	repo := tab.repo
	a.background(func() {
		ours, errO := repo.ReadFile(":2", p)
		base, _ := repo.ReadFile(":1", p)
		theirs, errT := repo.ReadFile(":3", p)
		a.update(func() {
			st.loading = false
			switch {
			case errO != nil || errT != nil:
				st.mode = mergeGone
				st.gone = "local"
				if errO == nil {
					st.gone = "incoming"
				}
				return
			case bytes.IndexByte(ours, 0) >= 0 || bytes.IndexByte(theirs, 0) >= 0 || bytes.IndexByte(base, 0) >= 0:
				st.mode = mergeBinary
				return
			}
			chunks, err := merge.Merge3(string(ours), string(base), string(theirs))
			if err != nil {
				st.err = errorText(err)
				return
			}
			st.chunks = chunks
			st.choices = make([]merge.Choice, len(chunks))
			buf := editor.NewBuffer("")
			buf.SetLanguage(editor.LanguageFor(path.Base(p)))
			st.result = &editor.State{Buffer: buf, LineNumbers: editor.LineNumbersOn}
			st.regenerate()
		})
	})
}

// regenerate writes the result from the choices made.
func (st *mergeState) regenerate() {
	st.generated = merge.Resolve(st.chunks, st.choices)
	st.result.Buffer.Set(st.generated)
	st.version = st.result.Buffer.Version()
	st.manual = false
}

func (st *mergeState) choose(i int, c merge.Choice) {
	if st.choices[i] == c {
		c = merge.Unresolved
	}
	st.choices[i] = c
	st.regenerate()
}

func (st *mergeState) chooseAll(c merge.Choice) {
	for i, ch := range st.chunks {
		if ch.Conflict {
			st.choices[i] = c
		}
	}
	st.regenerate()
}

// unresolved counts the conflicts not yet chosen.
func (st *mergeState) unresolved() int {
	n := 0
	for i, c := range st.chunks {
		if c.Conflict && st.choices[i] == merge.Unresolved {
			n++
		}
	}
	return n
}

// text is the result as it stands.
func (st *mergeState) text() string { return st.result.Buffer.String() }

// canSave: the result has no markers left, chosen or edited away.
func (st *mergeState) canSave() bool {
	return st.result != nil && !merge.HasMarkers(st.text()) && !st.saving
}

func (st *mergeState) save() {
	if !st.canSave() {
		return
	}
	st.saving = true
	text, p, repo := st.text(), st.path, st.tab.repo
	a := st.tab.app
	a.background(func() {
		full := filepath.Join(repo.Root, p)
		mode := os.FileMode(0o644)
		if fi, err := os.Stat(full); err == nil {
			mode = fi.Mode().Perm()
		}
		err := os.WriteFile(full, []byte(text), mode)
		if err == nil {
			err = repo.Stage(p)
		}
		a.update(func() {
			st.saving = false
			if err != nil {
				st.err = errorText(err)
				return
			}
			a.sheet = nil
			st.tab.workStale = true
			st.tab.reload()
			a.say("%s resolved", p)
		})
	})
}

func (st *mergeState) view(c *ui.Context) {
	t := c.Theme()
	local, incoming := st.sides()
	switch {
	case st.loading:
		spinner(c, "Loading").Margin(20)
		return
	case st.err != "":
		ui.Text(c, st.err).TextColor(t.Danger).Selectable()
		return
	case st.mode == mergeBinary:
		ui.Column(c).Gap(10).Padding(16).Children(func() {
			ui.Text(c, st.path+" is not a text file: choose a whole side.").FontSize(13)
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c, "Use "+local).Clicked() {
					st.takeSide("ours")
				}
				if ui.Button(c, "Use "+incoming).Clicked() {
					st.takeSide("theirs")
				}
			})
		})
		return
	case st.mode == mergeGone:
		deleted, kept := local, incoming
		if st.gone == "incoming" {
			deleted, kept = incoming, local
		}
		ui.Column(c).Gap(10).Padding(16).Children(func() {
			ui.Text(c, fmt.Sprintf("%s was deleted on the %s side and changed on the %s side.", st.path, strings.ToLower(deleted), strings.ToLower(kept))).FontSize(13)
			ui.Row(c).Gap(8).Children(func() {
				if ui.PrimaryButton(c, "Keep the File").Clicked() {
					st.keepOrDelete(false)
				}
				if ui.Button(c, "Delete the File").Clicked() {
					st.keepOrDelete(true)
				}
			})
		})
		return
	}

	// The text changes the user made in the result take it from the choices.
	if v := st.result.Buffer.Version(); v != st.version {
		st.version = v
		if st.result.Buffer.String() != st.generated {
			st.manual = true
		}
	}
	total := merge.Conflicts(st.chunks)
	left := st.unresolved()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
		msg := fmt.Sprintf("%d of %d conflicts chosen", total-left, total)
		if total == 0 {
			msg = "No conflicting lines: git merged both sides"
		}
		ui.Text(c, msg).FontSize(12.5).TextColor(t.TextMuted).Grow(1)
		if total > 0 {
			if ui.Button(c, "All "+local).Clicked() {
				st.chooseAll(merge.TakeOurs)
			}
			if ui.Button(c, "All "+incoming).Clicked() {
				st.chooseAll(merge.TakeTheirs)
			}
		}
	})
	ui.SplitVertical(c, &st.height, func() {
		ui.Scroll(c).Fill().MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(8).Padding(0, 4, 0, 0).Children(func() {
				k := 0
				for i := range st.chunks {
					if st.chunks[i].Conflict {
						k++
						st.conflictView(c, i, k, total, local, incoming)
					} else {
						st.commonView(c, i)
					}
				}
			})
		})
	}, func() {
		ui.Column(c).Fill().MinHeight(0).Gap(4).Children(func() {
			ui.Row(c).Gap(8).Shrink(0).Padding(4, 0, 0).Children(func() {
				ui.Text(c, "Result").FontSize(12).FontWeight(600)
				if st.manual {
					ui.Text(c, "edited by hand: choosing a side again replaces the edits").FontSize(11.5).TextColor(t.Warning)
				}
			})
			editor.View(c, st.result).Grow(1).FillWidth().MinHeight(0).Radius(6).Border(1, hairline)
		})
	}).Grow(1).MinHeight(0)
	ui.Row(c).Gap(8).Justify(ui.End).AlignItems(ui.Center).Shrink(0).Children(func() {
		if st.saving {
			spinner(c, "Saving")
		}
		save := ui.PrimaryButton(c, "Save and Stage").Disabled(!st.canSave())
		if !st.canSave() && !st.saving {
			save.Tooltip("Choose a side for every conflict, or remove the markers from the result")
		}
		if save.Clicked() {
			st.save()
		}
	})
}

// commonView shows the lines both sides agree on, trimmed to the lines
// around the conflicts.
func (st *mergeState) commonView(c *ui.Context, i int) {
	t := c.Theme()
	ls := st.chunks[i].Common
	const keep = 3
	var shown []string
	hidden := 0
	switch {
	case len(ls) <= 2*keep+1:
		shown = ls
	case i == 0:
		shown, hidden = ls[len(ls)-keep:], len(ls)-keep
	case i == len(st.chunks)-1:
		shown, hidden = ls[:keep], len(ls)-keep
	default:
		shown, hidden = append(append([]string{}, ls[:keep]...), ls[len(ls)-keep:]...), len(ls)-2*keep
	}
	mono := "SF Mono, Menlo, monospace"
	ui.Column(c).Key(fmt.Sprint("common", i)).FillWidth().Padding(2, 8).Children(func() {
		if hidden > 0 && i == 0 {
			ui.Text(c, fmt.Sprintf("⋯ %s unchanged", plural(hidden, "line", "lines"))).FontSize(11).TextColor(t.TextMuted)
		}
		half := len(shown)
		if hidden > 0 && i != 0 && i != len(st.chunks)-1 {
			half = keep
		}
		for j, l := range shown {
			if j == half && hidden > 0 {
				ui.Text(c, fmt.Sprintf("⋯ %s unchanged", plural(hidden, "line", "lines"))).FontSize(11).TextColor(t.TextMuted)
			}
			ui.Text(c, strings.TrimRight(strings.ReplaceAll(l, "\t", "    "), "\r\n")).Font(mono).FontSize(12).SingleLine().TextColor(t.TextMuted)
		}
		if hidden > 0 && i == len(st.chunks)-1 {
			ui.Text(c, fmt.Sprintf("⋯ %s unchanged", plural(hidden, "line", "lines"))).FontSize(11).TextColor(t.TextMuted)
		}
	})
}

func (st *mergeState) conflictView(c *ui.Context, i, n, total int, local, incoming string) {
	t := c.Theme()
	ch := st.chunks[i]
	cur := st.choices[i]
	mono := "SF Mono, Menlo, monospace"
	side := func(name string, lines []string, col ui.Color, chosen bool) {
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Radius(6).Border(1, col.Alpha(map[bool]float32{false: 0.35, true: 0.9}[chosen])).Background(col.Alpha(0.07)).Children(func() {
			ui.Text(c, name).FontSize(11).FontWeight(600).TextColor(col).Padding(3, 8)
			body := strings.TrimRight(strings.ReplaceAll(strings.Join(lines, ""), "\t", "    "), "\r\n")
			if len(lines) == 0 {
				ui.Text(c, "(nothing)").FontSize(12).Italic().TextColor(t.TextMuted).Padding(2, 8, 6)
				return
			}
			ui.Text(c, body).Font(mono).FontSize(12).Selectable().Padding(2, 8, 6)
		})
	}
	btn := func(label string, ch merge.Choice) {
		var b ui.Element
		key := fmt.Sprint(i, label)
		if cur == ch {
			b = ui.PrimaryButton(c.Key(key), label)
		} else {
			b = ui.Button(c.Key(key), label)
		}
		if b.Clicked() {
			st.choose(i, ch)
		}
	}
	ui.Column(c).Key(fmt.Sprint("conflict", i)).FillWidth().Gap(6).Padding(8).Radius(8).Background(subtleBg).Border(1, hairline).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("Conflict %d of %d", n, total)).FontSize(12.5).FontWeight(600).Grow(1)
			btn("Use "+local, merge.TakeOurs)
			btn("Use "+incoming, merge.TakeTheirs)
			btn("Both", merge.TakeBoth)
			btn("Both, reversed", merge.TakeBothBack)
		})
		ui.Row(c).Gap(8).FillWidth().AlignItems(ui.Stretch).Children(func() {
			side(local, ch.Ours, ui.Hex("#f59e0b"), cur == merge.TakeOurs || cur == merge.TakeBoth || cur == merge.TakeBothBack)
			side(incoming, ch.Theirs, ui.Hex("#3b82f6"), cur == merge.TakeTheirs || cur == merge.TakeBoth || cur == merge.TakeBothBack)
		})
	})
}

func (st *mergeState) takeSide(side string) {
	tab := st.tab
	p := st.path
	tab.app.sheet = nil
	tab.do("Resolve", func(r *git.Repo) error { return r.ResolveConflict(p, side) })
}

func (st *mergeState) keepOrDelete(del bool) {
	tab := st.tab
	p := st.path
	tab.app.sheet = nil
	tab.do("Resolve", func(r *git.Repo) error {
		if del {
			_, err := r.Do("rm", "-q", "--", p)
			return err
		}
		_, err := r.Do("add", "--", p)
		return err
	})
}
