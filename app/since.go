package app

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/voir/internal/git"
	"github.com/eliott/voir/internal/github"
)

// sinceReview is what commitSel is while the Code tab shows the changes made
// to the pull request since the viewer last reviewed it.
const sinceReview = "since-review"

// sinceView is the changes since the viewer's last review: from the head
// they reviewed to the head now. forced is set when the head reviewed is
// not in the history any more, as after a force-push; pairs then tell how
// each commit changed, and closed has the pairs whose diff is folded away.
type sinceView struct {
	from   string
	at     time.Time
	forced bool
	pairs  []git.RangePair
	closed map[int]bool
}

// lastReview is the head the viewer last reviewed the pull request at: that
// of their last review on GitHub, or the one remembered here when they last
// sent a review or viewed every file, whichever is later.
func (t *repoTab) lastReview(pv *prView) (reviewedHead, bool) {
	if pv == nil || t.gh == nil {
		return reviewedHead{}, false
	}
	last, ok := t.app.store.reviewed(*t.gh, pv.number)
	if rv, found := pv.reviews.LastBy(pv.viewer); found && (!ok || rv.SubmittedAt.After(last.At)) {
		last, ok = reviewedHead{SHA: rv.CommitID, At: rv.SubmittedAt}, true
	}
	return last, ok && last.SHA != ""
}

// changedSinceReview reports whether the pull request moved on since the
// viewer last reviewed it, and says how: the commits added, or that it was
// force-pushed.
func (t *repoTab) changedSinceReview(pv *prView) (reviewedHead, string, bool) {
	last, ok := t.lastReview(pv)
	head := pv.pr.Head.SHA
	if !ok || head == "" || sameCommit(last.SHA, head) {
		return last, "", false
	}
	for i, cm := range pv.commits {
		if sameCommit(cm.SHA, last.SHA) {
			return last, plural(len(pv.commits)-1-i, "new commit", "new commits"), true
		}
	}
	return last, "force-pushed", true
}

// sameCommit reports whether two hashes, either abbreviated, name the same
// commit.
func sameCommit(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// rememberReviewed notes that the viewer reviewed the pull request at its
// head now.
func (t *repoTab) rememberReviewed(pv *prView, head string) {
	if pv == nil || t.gh == nil || head == "" {
		return
	}
	t.app.store.setReviewed(*t.gh, pv.number, reviewedHead{SHA: head, At: time.Now()})
}

// noteAllViewed remembers the head of a pull request as reviewed once every
// file of its changes, or of those since the last review, is viewed.
func (t *repoTab) noteAllViewed(rv *review) {
	if len(rv.files) == 0 {
		return
	}
	for _, f := range rv.files {
		if !f.viewed {
			return
		}
	}
	for _, pv := range t.prTabs {
		if pv.rv == rv || (pv.commitRV == rv && pv.commitSel == sinceReview) {
			t.rememberReviewed(pv, rv.hash)
		}
	}
}

// showSinceReview makes the Code tab show what changed since the viewer's
// last review: the patch from the head they reviewed to the head now, or,
// when that head was force-pushed away, how each commit changed, by git
// range-diff. It is read off the main thread, fetching the commits first.
func (t *repoTab) showSinceReview(pv *prView) {
	last, ok := t.lastReview(pv)
	head := pv.pr.Head.SHA
	if !ok || head == "" {
		return
	}
	pv.commitSel, pv.commitMenu = sinceReview, false
	rv := newReview()
	rv.pr, rv.hash, rv.base, rv.cmp, rv.loading = true, head, last.SHA, true, true
	pv.commitRV = rv
	sv := &sinceView{from: last.SHA, at: last.At, closed: map[int]bool{}}
	pv.since = sv
	if t.remote() {
		t.showRemoteSince(pv, rv, sv)
		return
	}
	repo, remote, n, pr, o, a := t.repo, t.ghRemote, pv.number, pv.pr, t.diffOptions(), t.app
	from := last.SHA
	a.background(func() {
		ctx := context.Background()
		var parts []git.FilePart
		var pairs []git.RangePair
		forced := false
		base, err := a.fetchPR(ctx, repo, remote, n, pr)
		if err == nil && !repo.HasCommit(from) {
			// GitHub keeps a commit force-pushed away for a while, and lets
			// it be fetched by its hash.
			repo.FetchCommit(ctx, remote, from)
			if !repo.HasCommit(from) {
				err = fmt.Errorf("the commit you reviewed, %s, could not be fetched from %s: it may have been force-pushed away and cleaned up", shortSHA(from), remote)
			}
		}
		if err == nil {
			if repo.IsAncestor(from, head) {
				var patch string
				if patch, err = repo.RangePatch(from, head, o); err == nil {
					parts = git.SplitPatchFiles(patch)
				}
			} else {
				forced = true
				var oldBase string
				if oldBase, err = repo.MergeBase(from, base); err == nil {
					pairs, err = repo.RangeDiff(oldBase, from, base, head)
				}
			}
		}
		a.update(func() {
			if pv.commitRV != rv {
				return
			}
			rv.loading = false
			if err != nil {
				rv.err = errorText(err)
				return
			}
			sv.forced, sv.pairs = forced, pairs
			rv.local = true
			t.layoutReview(rv, parts)
		})
	})
}

// showRemoteSince is showSinceReview for a repository that is not here, as
// GitHub compares the two heads. Without the commits, a force-push cannot be
// told commit by commit.
func (t *repoTab) showRemoteSince(pv *prView, rv *review, sv *sinceView) {
	repo, head, a := *t.gh, rv.hash, t.app
	a.background(func() {
		ctx := context.Background()
		cl, err := a.client(ctx, repo.Host)
		var cmpd github.Comparison
		if err == nil {
			cmpd, err = cl.Compare(ctx, repo, sv.from, head)
		}
		if err == nil && cmpd.Status != "ahead" && cmpd.Status != "identical" {
			err = fmt.Errorf("the branch was force-pushed since your review of %s: open the repository on this Mac to see how each commit changed", shortSHA(sv.from))
		}
		a.update(func() {
			if pv.commitRV != rv {
				return
			}
			rv.loading = false
			if err != nil {
				rv.err = errorText(err)
				return
			}
			t.layoutReview(rv, partsOf(cmpd.Files))
		})
	})
}

func shortSHA(sha string) string { return sha[:min(7, len(sha))] }

// sinceBanner says what the Code tab shows while it shows the changes since
// the viewer's last review.
func (tab *repoTab) sinceBanner(c *ui.Context, pv *prView, sv *sinceView) {
	t := c.Theme()
	ui.Row(c.Key("since-banner")).Shrink(0).Padding(10, 18).Gap(8).AlignItems(ui.Center).Background(headerBg(t)).BorderWidth(0, 0, 1, 0).BorderColor(hairline).Children(func() {
		ui.Icon(c, iconEye).FontSize(14).TextColor(t.Accent).Shrink(0)
		what, tip := "Changes since your last review", ""
		if sv.forced {
			what, tip = "Force-pushed since your last review", "How each commit changed, by git range-diff"
		}
		ui.Text(c, what).FontSize(13).FontWeight(600).SingleLine().MinWidth(0).Shrink(1).Tooltip(tip)
		if !sv.at.IsZero() {
			ui.Text(c, relativeTime(c.Now(), sv.at)).FontSize(12).TextColor(t.TextMuted).Shrink(0)
		}
		ui.Spacer(c)
		ui.Textf(c, "%s → %s", shortSHA(sv.from), shortSHA(pv.pr.Head.SHA)).Font(mono).FontSize(12).TextColor(refColor).Selectable().Shrink(0)
	})
}

// rangeDiffView is how each commit changed after a force-push: the commits
// matched old with new, and the diff of the patches of those changed.
func (tab *repoTab) rangeDiffView(c *ui.Context, sv *sinceView) {
	t := c.Theme()
	pal := diffPaletteFor(t)
	if len(sv.pairs) == 0 {
		ui.Column(c).Grow(1).Center().Children(func() {
			ui.Text(c, "No commit changed").FontSize(13).TextColor(t.TextMuted)
		})
		return
	}
	same := 0
	for _, p := range sv.pairs {
		if p.Status == '=' {
			same++
		}
	}
	ui.Scroll(c.Key("range-diff")).Grow(1).MinHeight(0).Background(pal.appBg).Children(func() {
		ui.Column(c).Padding(12, 18, 24).Gap(10).Children(func() {
			if same == len(sv.pairs) {
				ui.Text(c, "The branch was rebased: no commit changed.").FontSize(12.5).TextColor(t.TextMuted)
			}
			for i, p := range sv.pairs {
				tab.rangePair(c, sv, i, p)
			}
		})
	})
}

// rangeStatus is how a pair of commits of a range-diff is said and colored.
func rangeStatus(t *ui.Theme, s byte) (string, ui.Color) {
	pal := diffPaletteFor(t)
	switch s {
	case '=':
		return "Unchanged", t.TextMuted
	case '!':
		return "Changed", t.Accent
	case '<':
		return "Dropped", pal.delBar
	default:
		return "Added", pal.addBar
	}
}

func (tab *repoTab) rangePair(c *ui.Context, sv *sinceView, i int, p git.RangePair) {
	t := c.Theme()
	pal := diffPaletteFor(t)
	label, color := rangeStatus(t, p.Status)
	hasDiff := p.Diff != ""
	ui.Column(c.Key(fmt.Sprint("pair", i))).Shrink(0).Radius(8).Border(1, hairline).Background(pal.codeBg).Children(func() {
		head := ui.ButtonBase(c.Key("head")).Height(38).Padding(0, 12).Gap(8).AlignItems(ui.Center).Justify(ui.Start).Label(p.Subject)
		if hasDiff && head.Clicked() {
			sv.closed[i] = !sv.closed[i]
		}
		head.Children(func() {
			if hasDiff {
				icon := iconChevronDown
				if sv.closed[i] {
					icon = iconChevronRight
				}
				ui.Icon(c, icon).FontSize(12).TextColor(t.TextMuted).Shrink(0)
			}
			ui.Text(c, label).FontSize(11.5).FontWeight(600).TextColor(color).Width(74).Shrink(0)
			ui.Text(c, cmp.Or(p.OldSHA, "-------")).Font(mono).FontSize(12).TextColor(t.TextMuted).Shrink(0)
			ui.Text(c, "→").FontSize(12).TextColor(t.TextMuted).Shrink(0)
			ui.Text(c, cmp.Or(p.NewSHA, "-------")).Font(mono).FontSize(12).TextColor(refColor).Shrink(0)
			ui.Text(c, p.Subject).FontSize(13).SingleLine().MinWidth(0).Shrink(1)
		})
		if !hasDiff || sv.closed[i] {
			return
		}
		ui.Column(c.Key("diff")).Padding(6, 0).BorderWidth(1, 0, 0, 0).BorderColor(hairline).Children(func() {
			for j, line := range strings.Split(strings.TrimRight(p.Diff, "\n"), "\n") {
				row := ui.Row(c.Key(j)).Padding(0, 12)
				fg := pal.code
				// The first column says how the line of the patch changed
				// between the two versions.
				switch {
				case strings.HasPrefix(line, "@@"):
					fg = t.TextMuted
				case strings.HasPrefix(line, "+"):
					row.Background(pal.addBg)
				case strings.HasPrefix(line, "-"):
					row.Background(pal.delBg)
				}
				row.Children(func() {
					ui.Text(c, line).Font(mono).FontSize(12).TextColor(fg).Selectable()
				})
			}
		})
	})
}
