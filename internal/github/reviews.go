package github

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// Side is the side of a diff a review comment is on: LEFT for the old
// file's lines, RIGHT for the new file's.
type Side string

const (
	Left  Side = "LEFT"
	Right Side = "RIGHT"
)

// Thread is a review comment and its replies, on a line or a range of lines
// of a file.
type Thread struct {
	RootID int64
	// NodeID identifies the thread in GraphQL, to resolve it; "" when not
	// known.
	NodeID       string
	Path         string
	Side         Side
	Line         int // the last line; 0 once outdated
	StartLine    int // the first line of a range, else 0
	OriginalLine int
	// Outdated is set once the lines it was on changed.
	Outdated bool
	Resolved bool
	// Pending is a draft of the viewer's review, which only they see.
	Pending  bool
	Comments []*ThreadComment
}

// ThreadComment is a comment of a thread.
type ThreadComment struct {
	ID        int64
	Author    User
	Body      string
	CreatedAt time.Time
	URL       string
	Pending   bool
}

// PendingReview is the viewer's review not yet submitted.
type PendingReview struct {
	ID     int64
	NodeID string
	Body   string
}

// Reviews are the review threads of a pull request, and the viewer's pending
// review.
type Reviews struct {
	Threads []*Thread
	Pending *PendingReview
}

// PendingComments counts the drafts of the viewer's pending review.
func (r *Reviews) PendingComments() int {
	if r == nil {
		return 0
	}
	n := 0
	for _, t := range r.Threads {
		if t.Pending {
			n += len(t.Comments)
		}
	}
	return n
}

type reviewCommentJSON struct {
	ID           int64     `json:"id"`
	InReplyToID  int64     `json:"in_reply_to_id"`
	Path         string    `json:"path"`
	Side         Side      `json:"side"`
	Line         *int      `json:"line"`
	StartLine    *int      `json:"start_line"`
	OriginalLine *int      `json:"original_line"`
	User         *User     `json:"user"`
	Body         string    `json:"body"`
	CreatedAt    time.Time `json:"created_at"`
	HTMLURL      string    `json:"html_url"`
}

type reviewJSON struct {
	ID     int64  `json:"id"`
	NodeID string `json:"node_id"`
	State  string `json:"state"`
	Body   string `json:"body"`
}

// Reviews returns the review threads and the viewer's pending review of a
// pull request.
func (c *Client) Reviews(ctx context.Context, r Repo, n int) (*Reviews, error) {
	comments, err := list[reviewCommentJSON](ctx, c, r.path("/pulls/%d/comments?per_page=100", n), 3000)
	if err != nil {
		return nil, err
	}
	reviews, err := list[reviewJSON](ctx, c, r.path("/pulls/%d/reviews?per_page=100", n), 500)
	if err != nil {
		return nil, err
	}
	var pending []reviewCommentJSON
	for _, rv := range reviews {
		if rv.State == "PENDING" {
			if pending, err = list[reviewCommentJSON](ctx, c, r.path("/pulls/%d/reviews/%d/comments?per_page=100", n, rv.ID), 3000); err != nil {
				return nil, err
			}
		}
	}
	// Resolution is GraphQL's alone: unknown without it.
	res, _ := c.resolutions(ctx, r, n)
	return normalize(comments, reviews, pending, res), nil
}

// resolution is a thread's state in GraphQL, which REST does not tell.
type resolution struct {
	nodeID   string
	resolved bool
}

const reviewThreadsQuery = `
query ReviewThreads($owner: String!, $repo: String!, $number: Int!, $cursor: String) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      reviewThreads(first: 100, after: $cursor) {
        pageInfo { hasNextPage endCursor }
        nodes { id isResolved comments(first: 1) { nodes { fullDatabaseId } } }
      }
    }
  }
}`

// resolutions returns whether each thread is resolved, by the id of its first
// comment.
func (c *Client) resolutions(ctx context.Context, r Repo, n int) (map[int64]resolution, error) {
	out := map[int64]resolution{}
	var cursor any
	for {
		var data struct {
			Repository *struct {
				PullRequest *struct {
					ReviewThreads struct {
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
						Nodes []struct {
							ID         string `json:"id"`
							IsResolved bool   `json:"isResolved"`
							Comments   struct {
								Nodes []struct {
									FullDatabaseID string `json:"fullDatabaseId"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		}
		vars := map[string]any{"owner": r.Owner, "repo": r.Name, "number": n, "cursor": cursor}
		if err := c.graphql(ctx, reviewThreadsQuery, vars, &data); err != nil {
			return nil, err
		}
		if data.Repository == nil || data.Repository.PullRequest == nil {
			return out, nil
		}
		rt := data.Repository.PullRequest.ReviewThreads
		for _, nd := range rt.Nodes {
			if len(nd.Comments.Nodes) == 0 {
				continue
			}
			var id int64
			fmt.Sscan(nd.Comments.Nodes[0].FullDatabaseID, &id)
			if id != 0 {
				out[id] = resolution{nodeID: nd.ID, resolved: nd.IsResolved}
			}
		}
		if !rt.PageInfo.HasNextPage {
			return out, nil
		}
		cursor = rt.PageInfo.EndCursor
	}
}

// normalize threads the comments under their first by in_reply_to_id, and
// finds the viewer's pending review.
func normalize(comments []reviewCommentJSON, reviews []reviewJSON, pending []reviewCommentJSON, res map[int64]resolution) *Reviews {
	out := &Reviews{}
	threads := map[int64]*Thread{}
	seen := map[int64]bool{}
	add := func(c reviewCommentJSON, isPending bool) {
		if seen[c.ID] {
			return
		}
		seen[c.ID] = true
		root := c.ID
		if c.InReplyToID != 0 {
			root = c.InReplyToID
		}
		t := threads[root]
		if t == nil {
			t = &Thread{RootID: root, Path: c.Path, Side: c.Side, Pending: isPending}
			if t.Side == "" {
				t.Side = Right
			}
			switch {
			case c.Line != nil:
				t.Line = *c.Line
			case isPending && c.OriginalLine != nil:
				// A draft of the viewer's review has no line of its own yet,
				// but is on the lines of the head, as it was written.
				t.Line = *c.OriginalLine
			default:
				t.Outdated = true
			}
			if c.StartLine != nil {
				t.StartLine = *c.StartLine
			}
			if c.OriginalLine != nil {
				t.OriginalLine = *c.OriginalLine
			}
			if r, ok := res[root]; ok {
				t.NodeID, t.Resolved = r.nodeID, r.resolved
			}
			threads[root] = t
			out.Threads = append(out.Threads, t)
		}
		tc := &ThreadComment{ID: c.ID, Body: c.Body, CreatedAt: c.CreatedAt, URL: c.HTMLURL, Pending: isPending}
		if c.User != nil {
			tc.Author = *c.User
		}
		t.Comments = append(t.Comments, tc)
	}
	// Roots come before their replies, as GitHub lists them by id.
	sort.SliceStable(comments, func(i, j int) bool { return comments[i].ID < comments[j].ID })
	for _, c := range comments {
		add(c, false)
	}
	for _, c := range pending {
		add(c, true)
	}
	for _, rv := range reviews {
		if rv.State == "PENDING" {
			out.Pending = &PendingReview{ID: rv.ID, NodeID: rv.NodeID, Body: rv.Body}
		}
	}
	return out
}

// NewComment is a review comment to post on a line, or on the lines
// StartLine…Line of a side.
type NewComment struct {
	Body      string
	CommitID  string // the head commit the lines are of
	Path      string
	Side      Side
	Line      int
	StartLine int
}

func (n NewComment) fields() map[string]any {
	m := map[string]any{"path": n.Path, "body": n.Body, "side": n.Side, "line": n.Line}
	if n.StartLine != 0 && n.StartLine != n.Line {
		m["start_line"] = n.StartLine
		m["start_side"] = n.Side
	}
	return m
}

// Comment posts a review comment at once, outside a review.
func (c *Client) Comment(ctx context.Context, r Repo, n int, nc NewComment) error {
	body := nc.fields()
	body["commit_id"] = nc.CommitID
	_, err := c.do(ctx, http.MethodPost, r.path("/pulls/%d/comments", n), body, nil)
	return err
}

const addThreadMutation = `
mutation AddPendingThread($reviewId: ID!, $path: String!, $body: String!, $line: Int!, $side: DiffSide!, $startLine: Int, $startSide: DiffSide) {
  addPullRequestReviewThread(input: {pullRequestReviewId: $reviewId, path: $path, body: $body, line: $line, side: $side, startLine: $startLine, startSide: $startSide}) {
    thread { id }
  }
}`

// AddToReview adds a comment to the viewer's pending review, which it starts
// when there is none.
func (c *Client) AddToReview(ctx context.Context, r Repo, n int, pending *PendingReview, nc NewComment) error {
	if pending == nil {
		// REST attaches comments to a review as it starts only.
		body := map[string]any{"commit_id": nc.CommitID, "comments": []any{nc.fields()}}
		_, err := c.do(ctx, http.MethodPost, r.path("/pulls/%d/reviews", n), body, nil)
		return err
	}
	vars := map[string]any{"reviewId": pending.NodeID, "path": nc.Path, "body": nc.Body, "line": nc.Line, "side": nc.Side, "startLine": nil, "startSide": nil}
	if nc.StartLine != 0 && nc.StartLine != nc.Line {
		vars["startLine"], vars["startSide"] = nc.StartLine, nc.Side
	}
	return c.graphql(ctx, addThreadMutation, vars, nil)
}

// SubmitReview finishes a review: event is COMMENT, APPROVE or
// REQUEST_CHANGES. It submits the viewer's pending review with its comments
// when there is one, else a review of its own; comments written meanwhile
// go with it.
func (c *Client) SubmitReview(ctx context.Context, r Repo, n int, pending *PendingReview, event, body string, comments []NewComment) error {
	payload := map[string]any{"event": event}
	if body != "" {
		payload["body"] = body
	}
	path := r.path("/pulls/%d/reviews", n)
	if pending != nil {
		for _, nc := range comments {
			if err := c.AddToReview(ctx, r, n, pending, nc); err != nil {
				return err
			}
		}
		path = fmt.Sprintf("%s/%d/events", path, pending.ID)
	} else if len(comments) > 0 {
		fields := make([]any, len(comments))
		for i, nc := range comments {
			fields[i] = nc.fields()
		}
		payload["commit_id"] = comments[0].CommitID
		payload["comments"] = fields
	}
	_, err := c.do(ctx, http.MethodPost, path, payload, nil)
	return err
}

// Reply answers a thread, at once.
func (c *Client) Reply(ctx context.Context, r Repo, n int, root int64, body string) error {
	_, err := c.do(ctx, http.MethodPost, r.path("/pulls/%d/comments/%d/replies", n, root), map[string]any{"body": body}, nil)
	return err
}

// EditComment changes the text of a review comment.
func (c *Client) EditComment(ctx context.Context, r Repo, id int64, body string) error {
	_, err := c.do(ctx, http.MethodPatch, r.path("/pulls/comments/%d", id), map[string]any{"body": body}, nil)
	return err
}

// DeleteComment deletes a review comment.
func (c *Client) DeleteComment(ctx context.Context, r Repo, id int64) error {
	_, err := c.do(ctx, http.MethodDelete, r.path("/pulls/comments/%d", id), nil, nil)
	return err
}

const resolveMutation = `mutation Resolve($id: ID!) { resolveReviewThread(input: {threadId: $id}) { thread { id } } }`
const unresolveMutation = `mutation Unresolve($id: ID!) { unresolveReviewThread(input: {threadId: $id}) { thread { id } } }`

// Resolve resolves a thread, or unresolves it.
func (c *Client) Resolve(ctx context.Context, nodeID string, resolved bool) error {
	q := resolveMutation
	if !resolved {
		q = unresolveMutation
	}
	return c.graphql(ctx, q, map[string]any{"id": nodeID}, nil)
}
