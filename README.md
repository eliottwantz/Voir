# MyFork

A native git client for macOS, built with [MyGo](https://github.com/egoist/mygo):
Go all the way down, drawn on the GPU, with no webview and no cgo. The goal is
the best git client on the Mac: one that opens at once, keeps the change you are
reading in front of you, and gets out of the way of the commands you run all day.

It drives the `git` on your `PATH` (set `MYFORK_GIT` to use another), so it does what
your git does, with your configuration, hooks and credentials.

## What it does

**Review.** The window is built around the diff. A commit is one surface that
scrolls: its message, then every file as a card.

- Files group by kind (docs, code, tests, config, …), with pills that jump to each
  group and count what you have viewed. Mark a file *Viewed* and it collapses;
  generated files, lock files and huge diffs start collapsed.
- A card's header stays at the top while its lines scroll under it.
- Side by side or unified, drawn as godiff draws it (two columns of line numbers, tinted gutters, syntax highlighting with Chroma, word-level changes), for commits and pull requests. Changed
  files are read whole, so the unmodified lines between changes expand 100 at a time.
- A sidebar with four views: *Files* (the changed files of a commit or a pull request, grouped as Docs, Code, Tests and so on, in folders, with their lines and a total), *History*
  (compact, filterable), *Branches* (branches, remotes, tags, stashes, submodules,
  worktrees) and *Pull Requests*. The commit graph is one button away.

**Commit.** *Local Changes* stages whole files, hunks, or single lines, and unstages or
discards the same way. Point at a hunk and a box closes around it, with its buttons; drag
over lines, or click one and Shift-click another, and the box closes around those, so that
*Stage Lines* or *Discard Lines…* takes only them. It works on a new file too. Amend, and
optionally push on commit.

**Rewrite history.** Interactive rebase with pick, reword, edit, squash, fixup and
drop, reordering, a backup branch, autostash and update-refs. Merge, cherry-pick,
revert, reset, stash, tag, worktrees, blame and file history.

**Resolve conflicts.** A 3-way merge editor: local and incoming side by side for each
conflict, use either or both, and an editable result. Files deleted on one side and
binary files get their own choices. A banner follows any merge, rebase, cherry-pick or
revert in progress, with Continue and Abort.

**Pull requests (GitHub).** A pull request's commits are fetched into the repository so its files show the lines between changes, as a commit's do. List, read (Markdown, checks, commits, files as the same cards
a commit has, their list in the sidebar), comment, review (comment on lines with the + at their edge, alone or into a pending review, then submit it: comment,
approve or request changes), reply to, edit, delete and resolve review threads, merge by merge commit, squash or rebase, close and reopen, mark a
draft ready, check out locally, and create one from the current branch, pushing it first.
It signs in with the GitHub CLI: run `gh auth login` once and MyFork asks
`gh auth token` for the token. It stores nothing. GitHub Enterprise works when `gh` is
signed in to that host.

**Git LFS.** Install, track and untrack patterns, see what is stored, fetch, pull and
prune. Diffs of pointer files say so. Needs `git-lfs`.

**Tabs.** Each repository opens in its own tab, and so does each worktree: a linked
worktree's tab shows its branch, so the checkouts of one repository stay apart. Open one
from the Worktrees list in the sidebar, or tick *Open in a new tab* in New Worktree. A tab's
menu closes the others, moves it, and reveals, copies or opens its folder. Tabs, their order
and the one shown come back at the next launch, and a tab behind the others keeps its
changed mark current.

**Around it.** Quick launch over the recent repositories, clone and create, light and
dark, auto-fetch, a menu bar for everything.

### Keys

| | |
|---|---|
| ⌘1 ⌘2 ⌘3 ⌘4 | Files, History, Branches, Pull Requests |
| ⌘B | hide and show the sidebar |
| ⌘F | filter the history |
| ⌘T, ⌘W, ⌘⇧T | new, close and restore a tab |
| ⌘⇧], ⌘⇧[ | next and previous tab |
| ⌘⇧O | quick launch |
| ⌘⇧F, ⌘⇧L, ⌘⇧P | fetch, pull, push… |
| ⌘⌥B, ⌘⇧B | new branch, checkout branch |
| ⌘R | refresh |

## Not yet

Git Flow, bisect, the reflog, repository statistics, custom commands, image and
submodule diffs, forges other than GitHub, issue trackers, inline review comments on
pull requests, LFS locking, and opening a pull request from a fork. Built and tried on
macOS; MyGo also targets Linux and Windows, which MyFork has not been run on.

## Run

The `mygo` CLI comes from `mise.toml`, and the app depends on the released `mygo`
v0.5.5 module:

```sh
mise install
mise exec -- mygo dev [dir]    # live reload
mise exec -- mygo build        # build/darwin-arm64/MyFork.app and a .dmg, with the bundled CLI
go run ./cmd/myfork ~/Dev/some-repo   # or run directly
```

## CLI

`mygo build` also builds the CLI of the app, the same binary as the GUI, into
the bundle's `Contents/Resources/bin/myfork`. The menu offers **MyFork >
Install `myfork` CLI…**, which symlinks it into `~/.local/bin`, so a terminal
can open repositories with

```sh
myfork ~/Dev/some-repo    # `myfork .` opens the current directory
```

The paths of a follow-up launch reach the app that runs: one instance of
MyFork runs at a time, so the CLI hands them over and exits, and the app adds
tabs and focuses. The install needs `~/.local/bin`, which macOS does not make.

## Test

The views run without a window, against real repositories made on the fly, so the
tests exercise real git, real git-lfs, and a fake GitHub.

```sh
CGO_ENABLED=0 go test ./...                      # git, graph, merge, GitHub, and the views
mise exec -- mygo vet .                          # MyGo's checks for stored elements and goroutine captures
MYFORK_SNAPSHOTS=/tmp/snaps go test ./app         # also writes PNGs of the views
MYFORK_LIVE=1 go test ./internal/github          # reads a public repository with your gh token
MYFORK_REPO=~/Dev/some-repo go test -run Real ./app  # reviews its biggest recent commit, timed, in dark mode
```

## Layout

| | |
|---|---|
| `internal/git` | the git command line: log, refs, status, diffs, blame, patches, LFS, rebase, and every operation |
| `internal/graph` | the commit graph's lanes |
| `internal/merge` | 3-way merge into chunks that are resolved one by one |
| `internal/github` | pull requests over GitHub's REST API, signed in with `gh` |
| `cmd/myfork` | the entrypoint: argument parsing, which opens tabs through `app.Run` |
| `app/` | the UI package: every view of the app |
| `app/app.go`, `app/tab.go` | tabs (repositories and worktrees), loading, selection, async work and git actions |
| `app/layout.go` | the window: sidebar and its four views, header, tab strip |
| `app/filetree.go` | the Files view of the sidebar: the files of a change by group and folder |
| `app/review.go`, `app/review_ui.go` | the surface: a commit's or a pull request's files as cards, groups, viewed marks |
| `app/diffrows.go`, `app/diffrender.go`, `app/diffstyle.go` | the diff inside the cards, as godiff draws it: rows of lines and gaps, word marks, tokens, colors |
| `app/view.go`, `app/sidebar.go`, `app/history.go`, `app/details.go`, `app/workdir.go` | the welcome page, the branches view, the commit graph and its details, Local Changes |
| `app/dialog.go`, `app/ops.go`, `app/sheet.go` | operation dialogs, preferences, File History and Blame |
| `app/workdiff.go` | the diff of Local Changes, drawn row by row: hunk boxes and line selection |
| `app/rebase_ui.go`, `app/mergeui.go`, `app/lfs_ui.go` | the interactive rebase sheet, the merge editor, Git LFS |
| `app/prs.go`, `app/prs_ui.go` | pull requests: loading and actions, the view and the dialogs |
| `app/menu.go`, `app/store.go`, `app/cli.go` | the menu bar, the `myfork` CLI installer, and settings kept in the user's config directory |
