# MyFork

A native git client for macOS, built with [MyGo](https://github.com/egoist/mygo):
Go all the way down, drawn on the GPU, with no webview and no cgo. The goal is
the best git client on the Mac: one that opens at once, keeps the change you are
reading in front of you, and gets out of the way of the commands you run all day.

It drives the `git` on your `PATH` (set `MYFORK_GIT` to use another), so it does what
your git does, with your configuration, hooks and credentials.

## What it does

**Review.** The window is built around the graph of commits. The sidebar is the
repository: *Local Changes* (⌘1), with how many files there are to stage, and *All
Commits* (⌘2), the branches, remotes, tags, stashes, submodules and worktrees.
Choosing a branch jumps to its commit. Under the graph is the commit chosen, in
three tabs: *Summary* (the message, the author, the parents and the files it
changed), *Changes* (the files as a tree, beside the diff of the one chosen) and
*File Tree* (the code as it was at that commit). ⌥1, ⌥2 and ⌥3 choose those three
tabs, in the main window and in a commit's own window alike. The button beside
the tabs, or ⌘⇧O, opens that commit in a window of its own, with the same tabs.
⌘-click a second commit in the graph, or a second branch, tag or stash in the
sidebar, to compare the two: both stay highlighted, the tabs show what changed
from the older to the newer, with the line of each commit above, and a button
beside them swaps the two. ⌘-click a third to replace the second.

- A pull request's files group by kind (docs, code, tests, config, …), with pills
  that jump to each group and count what you have viewed; a commit's are plain
  folders. Mark a file *Viewed* and it collapses; generated files, lock files and
  huge diffs start collapsed.
- A card's header stays at the top while its lines scroll under it.
- Side by side or unified, drawn by MyGo's diff viewer (`editor.DiffView`: two columns of line numbers, tinted gutters, syntax highlighting, word-level changes, unchanged lines folded and expanded), for commits and pull requests. Changed
  files are read whole, so the unmodified lines between changes expand 100 at a time.
- Pull requests have a page of their own.

**Commit.** *Local Changes* (⌘1) lists the files to stage and those already staged, and shows
the diff of the one chosen beside them: stage or unstage whole files, hunks, or single lines,
and discard the same way. Point at a hunk and a box closes around it, with its buttons; drag
over lines, or click one and Shift-click another, and the box closes around those, so that
*Stage Lines* or *Discard Lines…* takes only them. It works on a new file too. The line between
the two lists moves, so that either gets the room you want. Amend, and optionally push on
commit.

**Rewrite history.** Interactive rebase with pick, reword, edit, squash, fixup and
drop, reordering, a backup branch, autostash and update-refs. Merge, cherry-pick,
revert, reset, stash, tag, worktrees, blame and file history.

**Resolve conflicts.** A 3-way merge editor: local and incoming side by side for each
conflict, use either or both, and an editable result. Files deleted on one side and
binary files get their own choices. A banner follows any merge, rebase, cherry-pick or
revert in progress, with Continue and Abort.

**Pull requests (GitHub).** They have a page of their own (or the pull request button of the toolbar, or the welcome page), so the main window stays on local changes and branches. The page lists those of every repository open and of the *pinned* ones, which need not be open: pin a repository's own checkout (not a linked work tree) from the Repository menu, the Repos filter or the welcome page, and choose which repositories the list shows with the Repos filter. The list, searchable
(`label:bug`, `author:ada`), sorted and filtered, under *Authored* and *Review requested*; choosing one opens its details beside the list, which the expand button gives the whole page.
A repository that is not on this machine works too, from the field at the top of the page. `owner/repo` lists all of its pull requests, as the repositories open do. `owner/repo#123` (also `owner/repo #123`, or a pull request's address) shows that pull request alone, and its entry leaves the list when its tab is closed. Any public repository opens, and any private one the `gh` account can read; without a checkout there is no Check Out or review guide, and the files come from GitHub's own patches. The clock button beside the field reopens what was named lately. The pin button on a pull request of such a repository, or in the Repos filter, *watches* the repository: its pull requests are always listed, kept between runs, as a pinned repository's are, but with no checkout needed. Stop watching it from the same buttons. A repository that is not watched leaves the page with the × in the Repos filter.

The details have a *Summary* (reviewers, labels, description, checks, comments), a *Timeline* (commits and comments in order) and the *Code*, whose file tree can be shown or hidden. A pull request's commits are fetched into the repository so its files show the lines between changes, as a commit's do. List, read (Markdown, checks, commits, files as the same cards
a commit has), comment, review (comment on lines with the + at their edge, alone or into a pending review, then submit it: comment,
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
| ⌘1 ⌘2 ⌘3 | Local Changes, All Commits, Pull Requests |
| ⌥1 ⌥2 ⌥3 | the commit's tabs: Summary, Changes, File Tree |
| ⌥1 ⌥2 ⌥3 ⌥4 | on the pull requests page, the pull request's tabs: Summary, Timeline, Code, Guide |
| ⌘⇧O | open the file chosen (in a commit, a pull request or Local Changes) in the app its type opens with |
| ⌘C, ⌘⇧C | with a list of files focused, copy the path of the file chosen, and its path on disk |
| ⌘B | hide and show the sidebar |
| ⌘F | filter the commits |
| ⌘T, ⌘⇧W, ⌘⇧T | new, close and restore a tab |
| ⌘W | close the window |
| ⌘⇧], ⌘⇧[ | next and previous tab |
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
v0.6.4 module:

```sh
mise install
mise exec -- mygo dev [dir]    # live reload
mise exec -- mygo build        # build/darwin-arm64/MyFork.app and a .dmg
go run ./cmd/myfork ~/Dev/some-repo   # or run directly
```

## CLI

The menu offers **MyFork > Install `myfork` CLI…**, which writes a small shell
script to `~/.local/bin/myfork`, so a terminal can open repositories with

```sh
myfork ~/Dev/some-repo    # `myfork .` opens the current directory
```

The script does not run the app: it opens a `myfork://open?path=…` deep link at
the app by its bundle identifier, so macOS starts MyFork when it does not run
and hands the paths to the one that does, which adds tabs and focuses. It tries
the dev app of `mygo dev` (`com.develiott.myfork.dev`) first, then the installed
one, so the command follows whichever you have built. It returns at once, and
closing the terminal never quits the app. Installing again updates the script,
and replaces the symlink that older versions made. The install needs
`~/.local/bin`, which macOS does not make. (`go run ./cmd/myfork` runs the app
itself.)

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
| `internal/aigen` | commit messages, pull request titles and bodies, and review guides written by Claude Code, Codex or OpenCode, run as programs (see `docs/ai-generation.md`) |
| `cmd/myfork` | the entrypoint: argument parsing, which opens tabs through `app.Run` |
| `app/` | the UI package: every view of the app |
| `app/app.go`, `app/tab.go` | tabs (repositories and worktrees), loading, selection, async work and git actions |
| `app/layout.go` | the window: the sidebar, the header and the tab strip, and the graph over the commit view |
| `app/filetree.go` | the files of a change as folders, under their group when a pull request's, in the sidebar or beside a diff |
| `app/panel.go` | the three tabs under the graph, and the commit's own window |
| `app/review.go`, `app/review_ui.go` | a commit's or a pull request's files in groups, with their headers and viewed marks |
| `app/diffsurface.go`, `app/codestyle.go`, `app/diffstyle.go` | the files in MyGo's diff viewer, the threads and drafts under their lines, staging hunks and lines; the colors the cards and the file windows use |
| `app/view.go`, `app/sidebar.go`, `app/history.go`, `app/details.go`, `app/workdir.go` | the welcome page, the two views of the sidebar and its source list, the commit graph and its details, Local Changes |
| `app/dialog.go`, `app/ops.go`, `app/sheet.go` | operation dialogs, preferences, the sheets of Git LFS, the merge editor and the rebase |
| `app/fileactions.go` | the context menu of a file (Open, Open With, Reset File to, Blame/Timeline…, History…, Stash, Save as…, copy paths) and ⇧⌘O |
| `app/filewindow.go` | the windows of a file: History, and Blame with its timeline and the list of commits |
| `app/workdiff.go` | the diff of Local Changes: the viewer of the file chosen, with Stage and Discard on the hunk under the pointer and on the lines chosen |
| `app/rebase_ui.go`, `app/mergeui.go`, `app/lfs_ui.go` | the interactive rebase sheet, the merge editor, Git LFS |
| `app/prs.go`, `app/prs_ui.go`, `app/pullspage.go` | pull requests: loading and actions, the details (summary, timeline, code) and the dialogs, and the page with the list |
| `app/githubmd.go` | comments, descriptions and reviews from GitHub as it shows them: `<details>` that fold, hidden `<!-- -->` notes, emoji shortcodes |
| `app/remote.go` | repositories named on the pull requests page that are not on this machine: tabs without a checkout, opened from `owner/repo #123` |
| `app/pins.go` | the pinned repositories: which repositories the pull requests page lists, and pinning and unpinning them |
| `app/stacks.go` | stacked pull requests: the stack menu, merging, rebasing and unstacking a stack |
| `app/ai.go`, `app/guide.go` | the AI preferences, the commit message button, and the review guide tab |
| `app/menu.go`, `app/store.go`, `app/cli.go`, `app/myfork.sh` | the menu bar, the `myfork` CLI script (`myfork.sh`, embedded) and its installer, and settings kept in the user's config directory |
