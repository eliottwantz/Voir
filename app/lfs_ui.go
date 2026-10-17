package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
)

// lfsState is the Git LFS sheet: what the repository tracks and stores, and
// the commands that move the objects.
type lfsState struct {
	tab       *repoTab
	available bool
	enabled   bool
	loading   bool
	patterns  []git.LFSPattern
	files     []git.LFSFile
	status    string
	pattern   string
	remote    string
	busy      string
	output    string
	err       string
	list      ui.ListState
}

// lfsSheet opens Git LFS for the repository shown.
func (a *app) lfsSheet(tab *repoTab) {
	st := &lfsState{tab: tab, available: git.LFSAvailable(), loading: true}
	a.sheet = &sheet{open: true, title: "Git LFS — " + tab.name, body: st.view, state: st}
	st.refresh()
}

// refresh reads what LFS tracks and stores.
func (st *lfsState) refresh() {
	if !st.available {
		st.loading = false
		return
	}
	repo, a := st.tab.repo, st.tab.app
	a.background(func() {
		enabled := repo.LFSEnabled()
		var patterns []git.LFSPattern
		var files []git.LFSFile
		var status string
		var err error
		if enabled {
			patterns, err = repo.LFSPatterns()
			if err == nil {
				files, err = repo.LFSFiles()
			}
			if err == nil {
				status, err = repo.LFSStatus()
			}
		}
		a.update(func() {
			st.loading, st.enabled = false, enabled
			st.patterns, st.files, st.status = patterns, files, status
			if err != nil {
				st.err = errorText(err)
			}
		})
	})
}

// run does an LFS command off the main thread and shows what it printed.
func (st *lfsState) run(title string, fn func(ctx context.Context, r *git.Repo) (string, error)) {
	if st.busy != "" {
		return
	}
	st.busy, st.err, st.output = title, "", ""
	repo, a := st.tab.repo, st.tab.app
	a.background(func() {
		out, err := fn(context.Background(), repo)
		a.update(func() {
			st.busy = ""
			st.output = out
			if err != nil {
				st.err = errorText(err)
			}
			st.refresh()
			st.tab.workStale = true
			st.tab.reload()
		})
	})
}

func (st *lfsState) view(c *ui.Context) {
	t := c.Theme()
	mono := "SF Mono, Menlo, monospace"
	switch {
	case !st.available:
		ui.Column(c).Gap(8).Padding(16).Children(func() {
			ui.Text(c, "Git LFS is not installed").FontSize(14).Bold()
			ui.Text(c, "Install git-lfs (for example with `brew install git-lfs`), then open this again.").TextColor(t.TextMuted)
		})
		return
	case st.loading:
		ui.Spinner(c).Margin(20).Label("Loading")
		return
	}
	idle := st.busy == ""
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(12).Padding(0, 6, 0, 0).Children(func() {
			if !st.enabled {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(10).Radius(8).Background(t.Warning.Alpha(0.14)).Children(func() {
					ui.Icon(c, iconWarning).FontSize(15).TextColor(t.Warning)
					ui.Text(c, "Git LFS is not set up in this repository.").Grow(1)
					if ui.PrimaryButton(c, "Install Git LFS").Disabled(!idle).Clicked() {
						st.run("Install", func(_ context.Context, r *git.Repo) (string, error) { return "", r.LFSInstall() })
					}
				})
			}

			ui.Text(c, "Tracked patterns").FontSize(13).FontWeight(600)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				in := ui.TextInput(c, &st.pattern).Placeholder("*.psd, assets/**, big-file.bin").Grow(1).Label("Pattern to track")
				add := strings.TrimSpace(st.pattern) != "" && idle
				if (ui.Button(c, "Track").Disabled(!add).Clicked() || (in.Submitted() && add)) && add {
					p := strings.TrimSpace(st.pattern)
					st.pattern = ""
					st.run("Track", func(_ context.Context, r *git.Repo) (string, error) { return "", r.LFSTrack(p) })
				}
			})
			if len(st.patterns) == 0 {
				ui.Text(c, "Nothing is tracked yet.").FontSize(12).TextColor(t.TextMuted)
			}
			for _, p := range st.patterns {
				pat := p
				ui.Row(c).Key("pattern:"+pat.Pattern).Gap(8).AlignItems(ui.Center).Padding(2, 8).Radius(6).Background(subtleBg).Children(func() {
					ui.Text(c, pat.Pattern).Font(mono).FontSize(12.5).Grow(1)
					ui.Text(c, pat.Source).FontSize(11.5).TextColor(t.TextMuted)
					if ui.Button(c, "Untrack").Disabled(!idle).Clicked() {
						st.run("Untrack", func(_ context.Context, r *git.Repo) (string, error) { return "", r.LFSUntrack(pat.Pattern) })
					}
				})
			}

			ui.Text(c, fmt.Sprintf("Files in this commit (%d)", len(st.files))).FontSize(13).FontWeight(600)
			if len(st.files) == 0 {
				ui.Text(c, "No file of HEAD is stored with Git LFS.").FontSize(12).TextColor(t.TextMuted)
			}
			for i, f := range st.files {
				if i == 200 {
					ui.Text(c, fmt.Sprintf("… and %d more", len(st.files)-200)).FontSize(12).TextColor(t.TextMuted)
					break
				}
				ff := f
				ui.Row(c).Key("file:" + ff.Path).Gap(8).AlignItems(ui.Center).Children(func() {
					icon := iconCloud
					tip := "Only the pointer is here: fetch and pull to download the file"
					if ff.Present {
						icon, tip = iconCheck, "The file is downloaded"
					}
					ui.Icon(c, icon).FontSize(13).TextColor(t.TextMuted).Tooltip(tip)
					ui.Text(c, ff.Path).FontSize(12.5).SingleLine().Grow(1).MinWidth(0)
					ui.Text(c, ff.OID[:min(10, len(ff.OID))]).Font(mono).FontSize(11).TextColor(t.TextMuted)
				})
			}

			if st.status != "" {
				ui.Text(c, "Status").FontSize(13).FontWeight(600)
				ui.Text(c, st.status).Font(mono).FontSize(11.5).Selectable().TextColor(t.TextMuted)
			}

			ui.Text(c, "Objects").FontSize(13).FontWeight(600)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				remotes := append([]string{"Default remote"}, st.tab.remoteNames()...)
				if st.remote == "" {
					st.remote = remotes[0]
				}
				ui.Select(c, &st.remote, remotes).Width(160).Label("Remote")
				remote := func() string {
					if st.remote == "Default remote" {
						return ""
					}
					return st.remote
				}
				if ui.Button(c, "Fetch").Disabled(!idle || !st.enabled).Tooltip("Download the objects of the branch checked out").Clicked() {
					rem := remote()
					st.run("Fetch", func(ctx context.Context, r *git.Repo) (string, error) { return "Fetched", r.LFSFetch(ctx, rem, false) })
				}
				if ui.Button(c, "Fetch All").Disabled(!idle || !st.enabled).Tooltip("Download the objects of every branch and tag").Clicked() {
					rem := remote()
					st.run("Fetch all", func(ctx context.Context, r *git.Repo) (string, error) { return "Fetched", r.LFSFetch(ctx, rem, true) })
				}
				if ui.Button(c, "Pull").Disabled(!idle || !st.enabled).Tooltip("Download the objects and check them out").Clicked() {
					rem := remote()
					st.run("Pull", func(ctx context.Context, r *git.Repo) (string, error) { return "Pulled", r.LFSPull(ctx, rem) })
				}
				if ui.Button(c, "Prune").Disabled(!idle || !st.enabled).Tooltip("Delete old local objects that are pushed").Clicked() {
					st.run("Prune", func(_ context.Context, r *git.Repo) (string, error) { return r.LFSPrune() })
				}
				if st.busy != "" {
					ui.Spinner(c).Label(st.busy)
					ui.Text(c, st.busy+"…").FontSize(12).TextColor(t.TextMuted)
				}
			})
			if st.output != "" {
				ui.Text(c, st.output).Font(mono).FontSize(11.5).Selectable().TextColor(t.TextMuted)
			}
			if st.err != "" {
				ui.Text(c, st.err).FontSize(12).TextColor(t.Danger).Selectable()
			}
		})
	})
}
