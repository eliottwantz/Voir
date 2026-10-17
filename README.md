# MyFork

A git client modeled on [Fork](https://fork.dev), built with
[MyGo](https://github.com/egoist/mygo)'s native UI: Go, no webview, no cgo.
How Fork was studied, and what is and is not rebuilt, is in
[docs/reverse-engineering.md](docs/reverse-engineering.md).

It drives the `git` on your `PATH` (set `MYFORK_GIT` to use another), as Fork
drives its bundled one. Interactive rebase, a 3-way merge editor, line-level
staging, Git LFS (needs `git-lfs`) and GitHub pull requests are built in.

Pull requests are GitHub only, and sign in with the GitHub CLI: run
`gh auth login` once, and MyFork asks `gh auth token` for the token. It stores
nothing. Remotes on GitHub Enterprise work when `gh` is signed in to that host.

## Run

The `mygo` CLI comes from `mise.toml`, and the app depends on the released
`mygo` v0.5.5 module:

```sh
mise install
mise exec -- mygo dev [dir]    # live reload
mise exec -- mygo build        # build/darwin-arm64/MyFork.app and a .dmg
go run . ~/Dev/some-repo       # or run directly
```

## Test

```sh
CGO_ENABLED=0 go test ./...                      # git, graph, and the views without a window
mise exec -- mygo vet .                          # MyGo's checks for stored elements and goroutine captures
MYFORK_SNAPSHOTS=/tmp/snaps go test .            # also writes PNGs of the views
MYFORK_LIVE=1 go test ./internal/github          # reads a public repository with your gh token
```

## Layout

| | |
|---|---|
| `internal/git` | the git command line: log, refs, status, diffs, blame, and every operation |
| `internal/graph` | the commit graph's lanes |
| `internal/merge` | 3-way merge into chunks that are resolved one by one |
| `internal/github` | pull requests over GitHub's REST API, signed in with `gh` |
| `app.go`, `tab.go` | tabs, loading, selection, async work and git actions |
| `view.go`, `sidebar.go`, `history.go`, `details.go`, `workdir.go` | the window, sidebar, commit list, commit details, Local Changes |
| `dialog.go`, `ops.go`, `sheet.go` | operation dialogs, preferences, File History and Blame |
| `rebase_ui.go`, `lines.go`, `mergeui.go`, `lfs_ui.go` | the interactive rebase sheet, line selection, the merge editor, Git LFS |
| `prs.go`, `prs_ui.go` | pull requests: loading and actions, the sidebar section, the view and the dialogs |
| `menu.go`, `store.go` | the menu bar, and settings kept in the user's config directory |
