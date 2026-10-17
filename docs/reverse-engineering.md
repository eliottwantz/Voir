# Fork.app, reverse engineered

Target: `/Applications/Fork.app`, Fork 2.70.2 (`com.DanPristupov.Fork`), arm64,
executable SHA-256 `8623e5ba…1526a7`. Method: REA 6.4.0 (`npx rea-agents@6.4.0`,
CLI; the REA MCP server was not registered in the session) plus ordinary shell
tools, labeled below. Nothing was executed or decompiled: this is static
bundle, plist, Mach-O and Interface Builder evidence only.

Each claim says whether it is an **observation** (read from the artifact),
an **inference**, or **unknown**.

## Evidence

| ID | Operation | Used for |
|---|---|---|
| `ev_b6dd8d5a…23c3` | `inspect_plist` | bundle identity, URL schemes, ATS, Sparkle feed |
| `ev_bdaec9a5…adf9` | `inspect_artifact` | bundle inventory (not mined further) |
| `ev_ce8fae58…a65b` | `inspect_macho` | linked libraries, load commands, imports |
| `ev_6ea9523e…eadc` | `decode_interface_builder` (first 64 archives of the bundle) | only Paddle and Sparkle nibs; discarded |
| `ev_a9bbe5f0…3c8f` | `decode_interface_builder` on a scratch bundle holding 41 of Fork's own nibs, copied unchanged | window and menu structure |

The second decode ran against copies of the nibs in `/tmp`, because REA caps one
run at 64 archives and spent them on the embedded frameworks. The copies are
byte-identical to the originals; the original bundle was never written to.
Shell-only observations (not REA Evidence): `ls`/`file` of `Contents/`, `strings`
of the executable.

## What Fork is

**Observed**

- A native AppKit app, `NSPrincipalClass = NSApplication`, `NSMainNibFile = MainMenu`,
  built with Xcode 26.3 for macOS 26.2 SDK, minimum macOS 10.15. Not Electron:
  no ASAR, no JavaScript runtime.
- Mixed Swift and Objective-C: it links `libswiftCore`, `libswiftAppKit`,
  `libswiftFoundation`, and the UI types are Swift (`_TtC4Fork…` class names in
  the nibs, about 620 distinct types in the executable's strings).
- Frameworks: AppKit, WebKit, Security, UserNotifications, IOKit, plus
  embedded **Sparkle 2.8.1** (updates, feed `https://fork.dev/update/feed.xml`)
  and **Paddle** (licensing).
- **No libgit2.** The executable has no `libgit2` string and links no git
  library. It bundles a complete git in `Resources/git-instance`
  (`bin/git`, `libexec`, `git-lfs`) and `gitflow-avh`, and builds each
  operation as a git command whose output it parses. Strings show the
  `--pretty=format:` templates, `--porcelain`, `for-each-ref --format`,
  `diff-index` and so on.
- Helpers: `fork_cli` (a command-line launcher, with install/uninstall
  scripts), `fork_askpasswd` (credentials), `fork_ri` (rebase-interactive
  editor stand-in, **inferred** from the name and the `RebaseInteractive` nib).
- WebKit is used for HTML resources: `statistics.html/js` with Chart.js and
  moment (repository statistics), `markdown.css` and `md-ai-output.css`
  (rendered Markdown for the AI review feature).
- URL schemes `forkapp`, `sourcetree`, `x-github-client`; it opens folders
  (`public.directory`).
- Integrations seen as classes: GitHub, GitHub Enterprise, GitLab, Bitbucket,
  Bitbucket Server, Gitea (login nibs and `*Service` types), issue trackers
  (`BugtrackerRules`), custom commands, Git Flow (start/finish feature,
  hotfix, release), Git LFS, Lean Branching, worktrees, submodules, SSH key
  generation, GPG signing.
- Architecture, **inferred** from class names: one command object per git
  operation (`CreateBranch`, `MergeBranch`, `CherryPick`, `PushMultipleTags`,
  `RefreshRepositoryCommand`, `GetCustomCommandsGitCommand`…), a
  `RepositoryController` per tab with `RepositoryData`, `RepositoryStatusManager`,
  `RevisionStorage` and `RevisionListGraphView`, one nib per dialog.

## Windows, screens and controls (observed in the nibs)

- **Main menu** (`MainMenu.nib`): Fork (About, Preferences…, Accounts…,
  Configure SSH Keys…, Check for Updates…, Fork Activation…), File (New
  Tab/Window, Open…, Open Recent, Clone…, Create New Local Repository…, Quick
  Launch…, Close, Restore Closed Tab), Edit, **Repository** (Add .gitignore…,
  Fetch…/Quick Fetch, Pull…/Quick Pull, Push…/Quick Push, Save Stash…/Quick
  Stash, Checkout Branch…, New Branch…, New Tag…, New Worktree…, Git Flow, Git
  LFS, Apply Patch…, Bisect, Show in Finder, Open in Terminal, Repository
  Treemap…, Repository Statistics…, Performance Benchmark…, Settings for This
  Repository…), **View** (Show Uncommitted Changes, Show All Commits, Commit
  Details ▸ Summary/Changes/File Tree/Hide, Show HEAD, Hide Tags, Hide Stashes
  in Commit List, Show Lost Commits (Reflog), Collapse All Merges (First
  Parent only), Filter Current Branch, Refresh, Show Toolbar), Window, Help.
  The key equivalents are present but not mapped to items here (they are
  separate archive entries), so MyFork's shortcuts are its own choice.
- **Repository tab**: a sidebar with Workspace, Branches, Remotes, Tags,
  Stashes, Submodules; a revision list (columns Graph, Description, Author,
  Date, Commit; a header, a status bar "Filtered by…", a context search
  "Search SHA, author or message"); commit details with Summary
  (Author, Committer, SHA, Parents, Refs, "Expand All"), Changes, File Tree.
- **Local Changes**: lists headed "Unstaged"/"Staged" with Stage/Unstage
  buttons and "untracked files hidden", a commit message box with Amend, a
  `FileViewHeader` over the diff, a diff selection popover (hunk/line staging).
- **Diff views**: text, binary (side-by-side, swipe, onion-skin image diff),
  submodule diff ("Update Submodule", "Open Submodule"), merge conflict view
  (Merge in Fork, Merge in External Tool, Local/Remote change), blame, file
  history.
- **Operation dialogs**: Clone (URL, account, location, Test Connection),
  Create Branch (name, "Check out after create", Local changes: Don't change /
  Stash and reapply / Discard), Pull (remote, branch, "Rebase instead of
  merge", "Stash and reapply local changes"), Push (branch, remote, To,
  "Create tracking reference", "Push all tags", "Force push"), Fetch ("Fetch all
  remotes"), Save Stash, Rebase (interactive: Pick/Edit/Reword/Squash/Fixup/
  Drop, "Backup current state with temporary branch"), Reset, Revert,
  Cherry-pick, Git Flow and Lean Branching dialogs.
- **Preferences**: General (Appearance System/Light/Dark, "Compact branch
  labels", "Sort commits: Topologically/By date", "Fetch remotes
  automatically", "Always include tags into fetch", "Automatically push on
  commit", diff font, "Disable syntax highlighting", "Show -+ marks"), Git
  (git instance, ENV PATH, global user), Commit (spell check, page guide,
  subject limits), Integrations, Updates, Custom commands.

**Unknown** (no static evidence, and no runtime observation was made): window
geometry and spacing, exact colors and the graph's lane drawing, keyboard
shortcuts, string tables beyond English, what the AI review calls, licensing
checks, and how Fork schedules its git processes. A runtime capture
(`capture_native_ui_scenario`) would answer the visual questions; it launches
the target, so it was not done without being asked.

## What MyFork builds from it

Built with MyGo's native UI (no webview, no cgo), `github.com/egoist/mygo`
v0.5.5 from `mise.toml`, packaged with the `mygo` CLI from the same tool.
godiff was the model for the UI idioms (a `git` package that shells out, a
tabbed window with a drawn title bar, `ui.List`, the diff viewer, menus).

| Fork feature | MyFork |
|---|---|
| Tabs of repositories, Quick Launch, Open Recent, Restore Closed Tab | done |
| Clone, create local repository | done (no accounts or Test Connection) |
| Sidebar: workspace, branches, remotes, tags, stashes, submodules, worktrees | done, with context menus |
| Revision list with drawn graph, ref labels, search by SHA/author/message | done, paged 400 at a time |
| Commit details: Summary, Changes, File Tree; file history; blame | done |
| Local Changes: stage/unstage, hunk and **line-level** stage/unstage/discard, amend, commit | done: a "Select Lines" mode with a checkbox per changed line |
| Fetch, Pull (merge/rebase, stash and reapply), Push (tracking, tags, force-with-lease) | done |
| Branch create/checkout/rename/delete/merge/rebase, tags, stash save/apply/pop/drop, cherry-pick, revert, reset | done |
| **Interactive rebase** (Pick/Reword/Edit/Squash/Fixup/Drop, reorder, backup branch, autostash, update refs) | done: from a commit's or a branch's menu; stops for Edit and conflicts, and the banner continues or aborts |
| **3-way merge** of conflicts (Merge in Fork), and files deleted on one side | done: Local and Incoming per conflict, Both, an editable result; no external-tool launcher |
| **Git LFS** (status, track/untrack, fetch, pull, prune, install) | done in one sheet; LFS pointers are named as such in diffs; no locking |
| **Pull requests** (GitHub only) | done: list in the sidebar, conversation with Markdown and checks, commits, files with the diff viewer, comment, merge (merge/squash/rebase), close/reopen, mark ready, check out locally, create (pushing the branch first) |
| Accounts for GitLab, Bitbucket, Gitea; issue trackers | not done: GitHub only, signed in with `gh auth token` |
| Preferences (appearance, sorting, diff options, fetch/push behavior) | partial |
| Git Flow, bisect, reflog | not done |
| Statistics/treemap, AI review, custom commands, image/submodule diffs | not done |
| Bundled git, `fork_cli`, Sparkle/Paddle | not wanted: uses the system `git`; no updater or licensing |

Design choices that are **not** observed from Fork: the Local Changes and All
Commits views are separate (Fork's exact arrangement is unknown), the
keyboard shortcuts, the colors, the tab strip living in the title bar, and the
native-UI look.
