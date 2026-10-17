MyFork is a native macOS git client written in Go with [MyGo](https://github.com/egoist/mygo) (`github.com/egoist/mygo/ui`): GPU-drawn, no webview, no cgo. It shells out to the `git` on `PATH` (override with `MYFORK_GIT`) and signs in to GitHub through `gh auth token`. `README.md` has the feature list and the file-by-file layout table.

## Commands

The `mygo` CLI comes from mise, and the app depends on the released `mygo` module (v0.5.5). Do not use `go tool mygo`, a `tool` directive, or a `replace` to a local mygo checkout; keep `mise.toml` and `go.mod` versions in step.

```sh
mise install
mise exec -- mygo dev [dir]       # live reload (MYFORK_DEVTOOLS=1 turns on the MyGo inspector, off so that ⇧⌘C is the app's)
mise exec -- mygo build           # build/darwin-arm64/MyFork.app and .dmg
go run ./cmd/myfork ~/Dev/some-repo  # run directly
mise exec -- mygo vet .           # MyGo checks: stored elements, goroutine captures
CGO_ENABLED=0 go test ./...       # all tests
CGO_ENABLED=0 go test -run TestFilesTabListsTheChangedFiles ./app   # one test
MYFORK_SNAPSHOTS=/tmp/snaps go test ./app         # also writes PNGs of the views
MYFORK_LIVE=1 go test ./internal/github          # reads a public repo with your gh token
MYFORK_REPO=~/Dev/some-repo go test -run Real ./app  # times reviewing a real repo's biggest recent commit
```

## Architecture

- `internal/*` is UI-free: `git` (wraps the git CLI: log, status, diffs, patches, rebase, LFS, every operation), `github` (REST client, reviews), `graph` (commit graph lanes), `merge` (3-way merge chunks), `highlight` (Chroma), `worddiff`, `fileicon`.
- The entrypoint is `cmd/myfork`, and `app` is the UI package: `app.Run` builds the window and wiring, and holds the MyGo single-instance lock, so a later `myfork` launch forwards its paths (see `cliPaths`) to the one that runs, which opens tabs and focuses. `app` (`app/app.go`) holds window-level state: tabs, `sideTab` (the sidebar view, which also decides what the main area shows), dialog/sheet, store. Each open repository or linked worktree is a `repoTab` (`app/tab.go`) with its own loaded data, selection, diff panes, review state and busy flag. Git actions go through `repoTab.do`/`doThen`/`doCancelable`, which run git off the UI thread and reload.
- The main area is the review surface: `app/review.go` builds a commit's or PR's files into grouped cards; `app/review_ui.go` draws them; `app/diffrows.go`/`app/diffrender.go`/`app/diffstyle.go` turn patches into rows and paint them. Local Changes use `app/workdir.go`/`app/workdiff.go` (hunk and line staging). `app/filetree.go` is the sidebar Files view. PRs are `app/prs.go` (loading/actions) and `app/prs_ui.go` (view/dialogs).
- Settings, recent repos and open tabs persist via `app/store.go` in the user config dir.

## Conventions and gotchas

- You can read the MyGo source at `/Users/eliott/Dev/mygo` (its `docs/ui` and `ui/doc.go` are the API reference) and example projects built with it, such as godiff (`/Users/eliott/Dev/godiff`) and Mayoi (`/Users/eliott/Dev/Mayoi`). These are reading references only: the app builds against the released v0.5.5, so verify any API against that version.
- Follow `.agents/skills/mygo-maintenance/SKILL.md` for MyGo rules: never store `ui.Element` values (store data and `ui.Handle`), give stateful widgets stable `c.Key(...)` identity, keep slow I/O off the UI thread and publish via `Window.Update`, keep drawing callbacks paint-only.
- `app.background(fn)` runs `fn` on a goroutine in the app but synchronously in tests (no window); tests can set `a.hold = true` and call `a.runHeld()` to control timing. `a.newGitHub` and `a.fetchPR` are swappable hooks so tests use a fake GitHub.
- Tests render views headlessly with `ui.NewTester` against real repositories built on the fly (`demoRepo` in `app_test.go`, with a private `GIT_CONFIG_GLOBAL`). Prefer that pattern over mocking git.
- `docs/reverse-engineering.md` is a static analysis of Fork.app for feature reference, not part of the build.
