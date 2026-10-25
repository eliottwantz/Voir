package app

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/eliott/myfork/internal/github"
)

// convoItem is an entry of the conversation of a pull request: a comment or a
// review, with the threads of inline comments it opened.
type convoItem struct {
	// id tells it from the others, the review's or the first thread's.
	id   int64
	when time.Time
	user github.User
	// comment is nil for inline comments whose review the conversation does
	// not list, because it said nothing but them.
	comment *github.Comment
	threads []*github.Thread
}

// state is the review's verdict, "" for a comment.
func (it *convoItem) state() string {
	if it.comment == nil {
		return ""
	}
	return it.comment.State
}

func (it *convoItem) body() string {
	if it.comment == nil {
		return ""
	}
	return it.comment.Body
}

// conversation lists what was said on the pull request, oldest first: the
// comments and reviews, each review with its threads, then the threads of
// reviews that are not in the conversation, as one entry per review.
func (pv *prView) conversation() []convoItem {
	var out []convoItem
	listed := map[int64]bool{}
	for i := range pv.convo {
		cm := &pv.convo[i]
		out = append(out, convoItem{id: cm.ID, when: cm.CreatedAt, user: cm.User, comment: cm})
		if cm.State != "" {
			listed[cm.ID] = true
		}
	}
	if pv.reviews != nil {
		loose := map[int64]*convoItem{}
		var looseOrder []*convoItem
		for _, th := range pv.reviews.Threads {
			if th.Pending || len(th.Comments) == 0 {
				continue
			}
			if th.ReviewID != 0 && listed[th.ReviewID] {
				for i := range out {
					if out[i].comment != nil && out[i].comment.State != "" && out[i].id == th.ReviewID {
						out[i].threads = append(out[i].threads, th)
					}
				}
				continue
			}
			// One entry per review, or per thread when its review is not known.
			key := th.ReviewID
			if key == 0 {
				key = -th.RootID
			}
			it := loose[key]
			if it == nil {
				first := th.Comments[0]
				it = &convoItem{id: key, when: first.CreatedAt, user: first.Author}
				loose[key] = it
				looseOrder = append(looseOrder, it)
			}
			it.threads = append(it.threads, th)
		}
		for _, it := range looseOrder {
			out = append(out, *it)
		}
	}
	slices.SortStableFunc(out, func(a, b convoItem) int { return a.when.Compare(b.when) })
	return out
}

// unresolved counts the review threads still open, which can keep the pull
// request from being merged.
func (pv *prView) unresolved() int {
	if pv.reviews == nil {
		return 0
	}
	n := 0
	for _, th := range pv.reviews.Threads {
		if !th.Resolved && !th.Pending {
			n++
		}
	}
	return n
}

// threadCode is the file a thread is on, and the lines it is about, as the
// diff showed them.
func threadCode(c *ui.Context, rv *review, th *github.Thread) {
	t := c.Theme()
	pal := diffPaletteFor(t)
	var lines []string
	for _, l := range strings.Split(th.DiffHunk, "\n") {
		if l != "" && !strings.HasPrefix(l, "@@") {
			lines = append(lines, l)
		}
	}
	// The hunk ends on the thread's last line; a range shows whole, else a few
	// lines of context.
	n := 4
	if th.StartLine != 0 && th.StartLine < th.Line {
		n = th.Line - th.StartLine + 1
	}
	lines = lines[max(0, len(lines)-min(n, 12)):]
	folded := rv.collapsed[th.RootID]
	head := ui.Row(c).FillWidth().Gap(4).Padding(4, 8).AlignItems(ui.Center).Background(subtleBg)
	if !folded {
		head.BorderWidth(0, 0, 1, 0).BorderColor(pal.cardBorder)
	}
	head.Children(func() {
		chevron, label := iconChevronDown, "Collapse "+th.Path
		if folded {
			chevron, label = iconChevronRight, "Expand "+th.Path
		}
		if iconButton(c, chevron, label).Size(24, 24).Clicked() {
			rv.collapsed[th.RootID] = !folded
		}
		ui.Text(c, th.Path).Font(mono).FontSize(12).SingleLine().MinWidth(0).Shrink(1)
		ui.Text(c, threadLabel(th)).FontSize(11.5).TextColor(t.TextMuted).Shrink(0)
	})
	if folded || len(lines) == 0 {
		return
	}
	ui.Column(c).FillWidth().BorderWidth(0, 0, 1, 0).BorderColor(pal.cardBorder).Children(func() {
		for i, l := range lines {
			row := ui.Text(c.Key(fmt.Sprint("code-", i)), l).Font(mono).FontSize(12).FillWidth().Padding(1, 12).TextColor(pal.code)
			switch l[0] {
			case '+':
				row.Background(pal.addBg)
			case '-':
				row.Background(pal.delBg)
			}
		}
	})
}
