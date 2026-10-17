// Package github talks to GitHub's REST API for the pull requests of a
// repository. It signs in with the token of the GitHub CLI (`gh auth token`),
// so there is nothing to configure and nothing stored here.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Repo names a repository on a GitHub host.
type Repo struct {
	Host, Owner, Name string
}

func (r Repo) String() string { return r.Owner + "/" + r.Name }

// WebURL is the repository's page.
func (r Repo) WebURL() string { return "https://" + r.Host + "/" + r.Owner + "/" + r.Name }

var (
	scpRemote = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):(.+?)(?:\.git)?/?$`)
)

// ParseRemote reads a remote's URL: git@github.com:o/r.git,
// https://github.com/o/r, ssh://git@github.com/o/r.git.
func ParseRemote(raw string) (Repo, bool) {
	raw = strings.TrimSpace(raw)
	var host, path string
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return Repo{}, false
		}
		host, path = u.Hostname(), strings.TrimPrefix(u.Path, "/")
	} else if m := scpRemote.FindStringSubmatch(raw); m != nil {
		host, path = m[1], m[2]
	} else {
		return Repo{}, false
	}
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Repo{}, false
	}
	return Repo{Host: host, Owner: parts[0], Name: parts[1]}, true
}

// IsGitHubHost reports whether a host is GitHub.com, or a GitHub Enterprise
// host that the GitHub CLI is signed in to.
func IsGitHubHost(host string) bool {
	if host == "github.com" {
		return true
	}
	return Token(context.Background(), host) != ""
}

// Token returns the token the GitHub CLI holds for a host, "" when it is not
// installed or not signed in.
func Token(ctx context.Context, host string) string {
	gh, err := exec.LookPath("gh")
	if err != nil {
		return ""
	}
	out, err := exec.CommandContext(ctx, gh, "auth", "token", "--hostname", host).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// apiBase is the REST API's address on a host.
func apiBase(host string) string {
	if host == "github.com" {
		return "https://api.github.com"
	}
	return "https://" + host + "/api/v3"
}

// Client is a signed-in client for one repository's host.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewClient signs in to a host with the GitHub CLI's token.
func NewClient(ctx context.Context, host string) (*Client, error) {
	tok := Token(ctx, host)
	if tok == "" {
		return nil, ErrNotSignedIn
	}
	return &Client{BaseURL: apiBase(host), Token: tok, HTTP: &http.Client{Timeout: 30 * time.Second}}, nil
}

// ErrNotSignedIn says the GitHub CLI has no token: run `gh auth login`.
var ErrNotSignedIn = errors.New("not signed in to GitHub: run `gh auth login` in a terminal")

// Error is an error the API answered with.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("GitHub: %s (%d)", e.Message, e.Status)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) (http.Header, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	u := path
	if !strings.HasPrefix(path, "http") {
		u = c.BaseURL + path
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "MyFork")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Message string `json:"message"`
			Errors  []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		json.Unmarshal(data, &e)
		msg := e.Message
		for _, x := range e.Errors {
			if x.Message != "" {
				msg += ": " + x.Message
			}
		}
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return resp.Header, &Error{Status: resp.StatusCode, Message: msg}
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.Header, fmt.Errorf("GitHub: unreadable answer: %w", err)
		}
	}
	return resp.Header, nil
}

var nextLink = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// list reads every page of a list.
func list[T any](ctx context.Context, c *Client, path string, max int) ([]T, error) {
	var all []T
	next := path
	for next != "" && len(all) < max {
		var page []T
		h, err := c.do(ctx, "GET", next, nil, &page)
		if err != nil {
			return all, err
		}
		all = append(all, page...)
		next = ""
		if m := nextLink.FindStringSubmatch(h.Get("Link")); m != nil {
			next = m[1]
		}
	}
	return all, nil
}

// User is a GitHub account.
type User struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
	HTMLURL   string `json:"html_url"`
}

// Ref is one end of a pull request.
type Ref struct {
	Ref  string `json:"ref"`
	SHA  string `json:"sha"`
	Repo *struct {
		FullName string `json:"full_name"`
		CloneURL string `json:"clone_url"`
	} `json:"repo"`
}

// Label is a label of a pull request.
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// PR is a pull request.
type PR struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	State     string     `json:"state"` // open, closed
	Draft     bool       `json:"draft"`
	Merged    bool       `json:"merged"`
	MergedAt  *time.Time `json:"merged_at"`
	Locked    bool       `json:"locked"`
	User      User       `json:"user"`
	Head      Ref        `json:"head"`
	Base      Ref        `json:"base"`
	HTMLURL   string     `json:"html_url"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Labels    []Label    `json:"labels"`
	// RequestedReviewers are the accounts asked to review, who have not yet.
	RequestedReviewers []User `json:"requested_reviewers"`
	Comments           int    `json:"comments"`
	Commits            int    `json:"commits"`
	Additions          int    `json:"additions"`
	Deletions          int    `json:"deletions"`
	ChangedFiles       int    `json:"changed_files"`
	Mergeable          *bool  `json:"mergeable"`
	MergeableState     string `json:"mergeable_state"`
	// Stack places the pull request in its stack, when it is in one; only a
	// single pull request read in full says it.
	Stack *StackRef `json:"stack"`
	// AutoMerge is set while the pull request merges itself once it can.
	AutoMerge *struct {
		MergeMethod string `json:"merge_method"`
	} `json:"auto_merge"`
}

// StateLabel is "Draft", "Open", "Merged" or "Closed".
func (p PR) StateLabel() string {
	switch {
	case p.Merged:
		return "Merged"
	case p.State == "closed":
		return "Closed"
	case p.Draft:
		return "Draft"
	}
	return "Open"
}

func (r Repo) path(format string, args ...any) string {
	return "/repos/" + url.PathEscape(r.Owner) + "/" + url.PathEscape(r.Name) + fmt.Sprintf(format, args...)
}

// Viewer is the signed-in account.
func (c *Client) Viewer(ctx context.Context) (User, error) {
	var u User
	_, err := c.do(ctx, "GET", "/user", nil, &u)
	return u, err
}

// CommitAuthor is the account a commit's author email belongs to; ok is false
// when GitHub does not know the commit or links the email to no account.
func (c *Client) CommitAuthor(ctx context.Context, r Repo, sha string) (u User, ok bool, err error) {
	var out struct {
		Author *User `json:"author"`
	}
	if _, err = c.do(ctx, "GET", r.path("/commits/%s", url.PathEscape(sha)), nil, &out); err != nil {
		return User{}, false, err
	}
	if out.Author == nil || out.Author.Login == "" {
		return User{}, false, nil
	}
	return *out.Author, true, nil
}

// DefaultBranch is the branch pull requests go to by default.
func (c *Client) DefaultBranch(ctx context.Context, r Repo) (string, error) {
	var out struct {
		DefaultBranch string `json:"default_branch"`
	}
	_, err := c.do(ctx, "GET", r.path(""), nil, &out)
	return out.DefaultBranch, err
}

// PRs lists the pull requests with a state: open, closed or all.
func (c *Client) PRs(ctx context.Context, r Repo, state string) ([]PR, error) {
	prs, err := list[PR](ctx, c, r.path("/pulls?state=%s&sort=updated&direction=desc&per_page=100", url.QueryEscape(state)), 300)
	for i := range prs {
		// A list does not say "merged", only when.
		prs[i].Merged = prs[i].Merged || prs[i].MergedAt != nil
	}
	return prs, err
}

// PRStatus is what a list of pull requests does not tell: how the review
// stands, how the checks fare, and how many lines it changes.
type PRStatus struct {
	// Review is approved, changes (requested) or required (still to be
	// reviewed), "" when there is no decision.
	Review string
	// Checks is passing, failing or pending, "" when there are none.
	Checks string
	// Additions and Deletions are the lines the pull request changes, which
	// a list does not count.
	Additions, Deletions int
}

// StackPlace is where a pull request stands in its stack of pull requests.
type StackPlace struct {
	Position int // from 1, the one nearest the base branch
	Size     int
}

// prNodes reads the pull requests in a state (open, closed or all) with a
// GraphQL query that takes $owner, $repo, $states and $cursor and has the
// shape repository.pullRequests{pageInfo, nodes}, a page of 100 at a time up
// to three pages.
func prNodes[N any](ctx context.Context, c *Client, r Repo, state, query string) ([]N, error) {
	states := map[string][]string{"open": {"OPEN"}, "closed": {"CLOSED", "MERGED"}}[state]
	if states == nil {
		states = []string{"OPEN", "CLOSED", "MERGED"}
	}
	var out []N
	var cursor any
	for page := 0; page < 3; page++ {
		var data struct {
			Repository *struct {
				PullRequests struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []N `json:"nodes"`
				} `json:"pullRequests"`
			} `json:"repository"`
		}
		vars := map[string]any{"owner": r.Owner, "repo": r.Name, "states": states, "cursor": cursor}
		if err := c.graphql(ctx, query, vars, &data); err != nil {
			return nil, err
		}
		if data.Repository == nil {
			return out, nil
		}
		prs := data.Repository.PullRequests
		out = append(out, prs.Nodes...)
		if !prs.PageInfo.HasNextPage {
			break
		}
		cursor = prs.PageInfo.EndCursor
	}
	return out, nil
}

const prStatusesQuery = `
query PRStatuses($owner: String!, $repo: String!, $states: [PullRequestState!], $cursor: String) {
  repository(owner: $owner, name: $repo) {
    pullRequests(first: 100, states: $states, orderBy: {field: UPDATED_AT, direction: DESC}, after: $cursor) {
      pageInfo { hasNextPage endCursor }
      nodes { number reviewDecision additions deletions commits(last: 1) { nodes { commit { statusCheckRollup { state } } } } }
    }
  }
}`

// PRStatuses reads the status of the pull requests in a state (open, closed
// or all), by number.
func (c *Client) PRStatuses(ctx context.Context, r Repo, state string) (map[int]PRStatus, error) {
	nodes, err := prNodes[struct {
		Number         int    `json:"number"`
		ReviewDecision string `json:"reviewDecision"`
		Additions      int    `json:"additions"`
		Deletions      int    `json:"deletions"`
		Commits        struct {
			Nodes []struct {
				Commit struct {
					StatusCheckRollup *struct {
						State string `json:"state"`
					} `json:"statusCheckRollup"`
				} `json:"commit"`
			} `json:"nodes"`
		} `json:"commits"`
	}](ctx, c, r, state, prStatusesQuery)
	if err != nil {
		return nil, err
	}
	out := map[int]PRStatus{}
	for _, nd := range nodes {
		st := PRStatus{Review: map[string]string{"APPROVED": "approved", "CHANGES_REQUESTED": "changes", "REVIEW_REQUIRED": "required"}[nd.ReviewDecision], Additions: nd.Additions, Deletions: nd.Deletions}
		if len(nd.Commits.Nodes) > 0 && nd.Commits.Nodes[0].Commit.StatusCheckRollup != nil {
			st.Checks = map[string]string{"SUCCESS": "passing", "FAILURE": "failing", "ERROR": "failing", "PENDING": "pending", "EXPECTED": "pending"}[nd.Commits.Nodes[0].Commit.StatusCheckRollup.State]
		}
		out[nd.Number] = st
	}
	return out, nil
}

// The stack of a pull request is a query of its own: a host that does not know
// stacks fails it, and the statuses must not fail with it.
const prStacksQuery = `
query PRStacks($owner: String!, $repo: String!, $states: [PullRequestState!], $cursor: String) {
  repository(owner: $owner, name: $repo) {
    pullRequests(first: 100, states: $states, orderBy: {field: UPDATED_AT, direction: DESC}, after: $cursor) {
      pageInfo { hasNextPage endCursor }
      nodes { number stackEntry { position stack { size } } }
    }
  }
}`

// ErrStacksUnsupported says the host has no stacked pull requests: the
// repository does not have them, or the host predates them.
var ErrStacksUnsupported = errors.New("this host does not have stacked pull requests")

// stacksUnsupported maps what a host without stacks answers to
// ErrStacksUnsupported, and passes any other error through.
func stacksUnsupported(err error) error {
	var e *Error
	switch {
	case err == nil:
		return nil
	case errors.As(err, &e) && e.Status == http.StatusNotFound:
		return ErrStacksUnsupported
	case strings.Contains(err.Error(), "stackEntry") || strings.Contains(err.Error(), "PullRequestStack"):
		return ErrStacksUnsupported
	}
	return err
}

// PRStacks reads where the pull requests in a state (open, closed or all)
// stand in their stacks, by number; those in none are left out. A host without
// stacks answers ErrStacksUnsupported.
func (c *Client) PRStacks(ctx context.Context, r Repo, state string) (map[int]StackPlace, error) {
	nodes, err := prNodes[struct {
		Number     int `json:"number"`
		StackEntry *struct {
			Position int `json:"position"`
			Stack    struct {
				Size int `json:"size"`
			} `json:"stack"`
		} `json:"stackEntry"`
	}](ctx, c, r, state, prStacksQuery)
	if err != nil {
		return nil, stacksUnsupported(err)
	}
	out := map[int]StackPlace{}
	for _, nd := range nodes {
		if nd.StackEntry != nil && nd.StackEntry.Stack.Size > 1 {
			out[nd.Number] = StackPlace{Position: nd.StackEntry.Position, Size: nd.StackEntry.Stack.Size}
		}
	}
	return out, nil
}

// PR reads one pull request in full.
func (c *Client) PR(ctx context.Context, r Repo, n int) (PR, error) {
	var p PR
	_, err := c.do(ctx, "GET", r.path("/pulls/%d", n), nil, &p)
	return p, err
}

// File is a file a pull request changes.
type File struct {
	Filename         string `json:"filename"`
	PreviousFilename string `json:"previous_filename"`
	Status           string `json:"status"` // added, removed, modified, renamed, copied, changed
	Additions        int    `json:"additions"`
	Deletions        int    `json:"deletions"`
	Patch            string `json:"patch"`
}

// Files lists the files of a pull request.
func (c *Client) Files(ctx context.Context, r Repo, n int) ([]File, error) {
	return list[File](ctx, c, r.path("/pulls/%d/files?per_page=100", n), 3000)
}

// FilePatch is the file's patch with the header git prints, which GitHub
// leaves out.
func (f File) FilePatch() string {
	if f.Patch == "" {
		return ""
	}
	a, b := "a/"+f.Filename, "b/"+f.Filename
	if f.PreviousFilename != "" {
		a = "a/" + f.PreviousFilename
	}
	switch f.Status {
	case "added":
		a = "/dev/null"
	case "removed":
		b = "/dev/null"
	}
	var s strings.Builder
	fmt.Fprintf(&s, "diff --git a/%s b/%s\n", firstNonEmpty(f.PreviousFilename, f.Filename), f.Filename)
	switch f.Status {
	case "added":
		s.WriteString("new file mode 100644\n")
	case "removed":
		s.WriteString("deleted file mode 100644\n")
	}
	fmt.Fprintf(&s, "--- %s\n+++ %s\n%s", a, b, f.Patch)
	if !strings.HasSuffix(f.Patch, "\n") {
		s.WriteString("\n")
	}
	return s.String()
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Commit is a commit of a pull request.
type Commit struct {
	SHA string `json:"sha"`
	// Author is the account the commit's author email belongs to, nil when
	// GitHub links it to none.
	Author *User `json:"author"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Name string    `json:"name"`
			Date time.Time `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

// Subject is the first line of the message.
func (c Commit) Subject() string {
	s, _, _ := strings.Cut(c.Commit.Message, "\n")
	return s
}

// Commits lists the commits of a pull request.
func (c *Client) Commits(ctx context.Context, r Repo, n int) ([]Commit, error) {
	return list[Commit](ctx, c, r.path("/pulls/%d/commits?per_page=100", n), 1000)
}

// Comment is a comment of the conversation, or a review.
type Comment struct {
	ID        int64     `json:"id"`
	User      User      `json:"user"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	// State is set for a review: APPROVED, CHANGES_REQUESTED, COMMENTED.
	State string `json:"state"`
}

// Conversation lists the comments and the reviews of a pull request,
// oldest first.
func (c *Client) Conversation(ctx context.Context, r Repo, n int) ([]Comment, error) {
	comments, err := list[Comment](ctx, c, r.path("/issues/%d/comments?per_page=100", n), 1000)
	if err != nil {
		return nil, err
	}
	type review struct {
		Comment
		SubmittedAt time.Time `json:"submitted_at"`
	}
	reviews, err := list[review](ctx, c, r.path("/pulls/%d/reviews?per_page=100", n), 500)
	if err != nil {
		return comments, err
	}
	for _, rv := range reviews {
		if rv.State == "PENDING" || (rv.Body == "" && rv.State == "COMMENTED") {
			continue
		}
		rv.CreatedAt = rv.SubmittedAt
		comments = append(comments, rv.Comment)
	}
	sortByTime(comments)
	return comments, nil
}

func sortByTime(cs []Comment) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && cs[j].CreatedAt.Before(cs[j-1].CreatedAt); j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}

// AddComment adds a comment to the conversation.
func (c *Client) AddComment(ctx context.Context, r Repo, n int, body string) (Comment, error) {
	var out Comment
	_, err := c.do(ctx, "POST", r.path("/issues/%d/comments", n), map[string]string{"body": body}, &out)
	return out, err
}

// Check is the result of a check run or a status.
type Check struct {
	Name  string
	State string // success, failure, pending, neutral
	URL   string
}

// Checks lists the checks of a commit and a summary: success, failure or
// pending, "" when there are none.
func (c *Client) Checks(ctx context.Context, r Repo, sha string) ([]Check, string, error) {
	var runs struct {
		CheckRuns []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			HTMLURL    string `json:"html_url"`
		} `json:"check_runs"`
	}
	if _, err := c.do(ctx, "GET", r.path("/commits/%s/check-runs?per_page=100", sha), nil, &runs); err != nil {
		return nil, "", err
	}
	var checks []Check
	for _, cr := range runs.CheckRuns {
		st := "pending"
		if cr.Status == "completed" {
			switch cr.Conclusion {
			case "success":
				st = "success"
			case "neutral", "skipped":
				st = "neutral"
			default:
				st = "failure"
			}
		}
		checks = append(checks, Check{Name: cr.Name, State: st, URL: cr.HTMLURL})
	}
	var combined struct {
		Statuses []struct {
			Context   string `json:"context"`
			State     string `json:"state"`
			TargetURL string `json:"target_url"`
		} `json:"statuses"`
	}
	if _, err := c.do(ctx, "GET", r.path("/commits/%s/status", sha), nil, &combined); err == nil {
		for _, s := range combined.Statuses {
			st := s.State
			if st == "error" {
				st = "failure"
			}
			checks = append(checks, Check{Name: s.Context, State: st, URL: s.TargetURL})
		}
	}
	summary := ""
	for _, ch := range checks {
		switch {
		case ch.State == "failure":
			return checks, "failure", nil
		case ch.State == "pending":
			summary = "pending"
		case summary == "":
			summary = "success"
		}
	}
	return checks, summary, nil
}

// NewPR is what creating a pull request needs.
type NewPR struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Draft bool   `json:"draft"`
}

// Create opens a pull request.
func (c *Client) Create(ctx context.Context, r Repo, n NewPR) (PR, error) {
	var p PR
	_, err := c.do(ctx, "POST", r.path("/pulls"), n, &p)
	return p, err
}

// Merge merges a pull request: method is merge, squash or rebase.
func (c *Client) Merge(ctx context.Context, r Repo, n int, method, title string) error {
	body := map[string]string{"merge_method": method}
	if title != "" {
		body["commit_title"] = title
	}
	_, err := c.do(ctx, "PUT", r.path("/pulls/%d/merge", n), body, nil)
	return err
}

// SetState closes or reopens a pull request.
func (c *Client) SetState(ctx context.Context, r Repo, n int, state string) error {
	_, err := c.do(ctx, "PATCH", r.path("/pulls/%d", n), map[string]string{"state": state}, nil)
	return err
}

// graphql runs a query or a mutation: at api.github.com/graphql, or
// <host>/api/graphql on Enterprise.
func (c *Client) graphql(ctx context.Context, query string, vars map[string]any, data any) error {
	endpoint := strings.TrimSuffix(c.BaseURL, "/v3") + "/graphql"
	var out struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if _, err := c.do(ctx, "POST", endpoint, map[string]any{"query": query, "variables": vars}, &out); err != nil {
		return err
	}
	if len(out.Errors) > 0 {
		return errors.New(out.Errors[0].Message)
	}
	if data != nil && len(out.Data) > 0 {
		return json.Unmarshal(out.Data, data)
	}
	return nil
}

// nodeID is the GraphQL id of a pull request, which the mutations the REST
// API lacks need.
func (c *Client) nodeID(ctx context.Context, r Repo, n int) (string, error) {
	var p struct {
		NodeID string `json:"node_id"`
	}
	_, err := c.do(ctx, "GET", r.path("/pulls/%d", n), nil, &p)
	return p.NodeID, err
}

// MarkReady takes a draft out of draft (GraphQL's markPullRequestReadyForReview).
func (c *Client) MarkReady(ctx context.Context, r Repo, n int) error {
	id, err := c.nodeID(ctx, r, n)
	if err != nil {
		return err
	}
	return c.graphql(ctx, `mutation($id: ID!) { markPullRequestReadyForReview(input: {pullRequestId: $id}) { pullRequest { id } } }`, map[string]any{"id": id}, nil)
}

// ConvertToDraft puts a pull request back to draft.
func (c *Client) ConvertToDraft(ctx context.Context, r Repo, n int) error {
	id, err := c.nodeID(ctx, r, n)
	if err != nil {
		return err
	}
	return c.graphql(ctx, `mutation($id: ID!) { convertPullRequestToDraft(input: {pullRequestId: $id}) { pullRequest { id } } }`, map[string]any{"id": id}, nil)
}

// SetAutoMerge makes a pull request merge itself once its checks pass and it
// is approved, or stops it from: method is merge, squash or rebase.
func (c *Client) SetAutoMerge(ctx context.Context, r Repo, n int, enable bool, method string) error {
	id, err := c.nodeID(ctx, r, n)
	if err != nil {
		return err
	}
	if !enable {
		return c.graphql(ctx, `mutation($id: ID!) { disablePullRequestAutoMerge(input: {pullRequestId: $id}) { pullRequest { id } } }`, map[string]any{"id": id}, nil)
	}
	return c.graphql(ctx, `mutation($id: ID!, $method: PullRequestMergeMethod!) { enablePullRequestAutoMerge(input: {pullRequestId: $id, mergeMethod: $method}) { pullRequest { id } } }`,
		map[string]any{"id": id, "method": strings.ToUpper(method)}, nil)
}

// StackRef is where a pull request stands in its stack.
type StackRef struct {
	Number   int `json:"number"`
	Position int `json:"position"` // from 1, the one nearest the base branch
	Size     int `json:"size"`
	Base     struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

// StackEntry is a pull request of a stack.
type StackEntry struct {
	Number   int        `json:"number"`
	Title    string     `json:"title"`
	State    string     `json:"state"` // open, closed
	Draft    bool       `json:"draft"`
	MergedAt *time.Time `json:"merged_at"`
	HTMLURL  string     `json:"html_url"`
	Head     Ref        `json:"head"`
	Base     Ref        `json:"base"`
}

// Merged reports whether the pull request was merged.
func (e StackEntry) Merged() bool { return e.MergedAt != nil }

// StateLabel is "Draft", "Open", "Merged" or "Closed".
func (e StackEntry) StateLabel() string {
	return PR{State: e.State, Draft: e.Draft, Merged: e.Merged()}.StateLabel()
}

// Stack is a chain of pull requests that build on each other.
type Stack struct {
	Number int  `json:"number"`
	Open   bool `json:"open"`
	Base   struct {
		Ref string `json:"ref"`
	} `json:"base"`
	// Entries are the pull requests, from the one nearest the base branch.
	Entries []StackEntry `json:"pull_requests"`
}

// Stack reads a stack by its number. A host without stacks answers
// ErrStacksUnsupported.
func (c *Client) Stack(ctx context.Context, r Repo, number int) (Stack, error) {
	var s Stack
	_, err := c.do(ctx, "GET", r.path("/stacks/%d", number), nil, &s)
	return s, stacksUnsupported(err)
}

// StackOf reads the stack a pull request is in; ok is false when it is in
// none. A host without stacks answers ErrStacksUnsupported.
func (c *Client) StackOf(ctx context.Context, r Repo, n int) (s Stack, ok bool, err error) {
	var stacks []Stack
	if _, err = c.do(ctx, "GET", r.path("/stacks?pull_request=%d", n), nil, &stacks); err != nil {
		return Stack{}, false, stacksUnsupported(err)
	}
	if len(stacks) == 0 {
		return Stack{}, false, nil
	}
	return stacks[0], true, nil
}

// CreateStack stacks pull requests that build on each other, given from the one
// nearest the base branch; there are at least two.
func (c *Client) CreateStack(ctx context.Context, r Repo, numbers []int) (Stack, error) {
	var s Stack
	_, err := c.do(ctx, "POST", r.path("/stacks"), map[string][]int{"pull_requests": numbers}, &s)
	return s, stacksUnsupported(err)
}

// AddToStack puts pull requests on top of a stack, given from its current top
// upward: only the new ones.
func (c *Client) AddToStack(ctx context.Context, r Repo, stack int, numbers []int) (Stack, error) {
	var s Stack
	_, err := c.do(ctx, "POST", r.path("/stacks/%d/add", stack), map[string][]int{"pull_requests": numbers}, &s)
	return s, stacksUnsupported(err)
}

// Unstack takes the pull requests out of a stack. GitHub leaves those that are
// queued for merge or have auto-merge on; remaining says how many stay.
func (c *Client) Unstack(ctx context.Context, r Repo, stack int) (remaining int, err error) {
	var s Stack
	// A stack with none left is answered with no content.
	if _, err = c.do(ctx, "POST", r.path("/stacks/%d/unstack", stack), nil, &s); err != nil {
		return 0, stacksUnsupported(err)
	}
	return len(s.Entries), nil
}

// MergeStackResult is how a request to merge a stack stands: pending (still
// running), merged, enqueued (in the merge queue) or failed.
type MergeStackResult struct {
	Status  string `json:"status"`
	Details struct {
		Message string `json:"message"`
		UUID    string `json:"uuid"`
	} `json:"details"`
}

// MergeStack merges the stack's pull requests up to and including n, all or
// none, by method (merge, squash or rebase). sha is the head of n that the
// merge is meant for: it fails if n has moved on. GitHub does it in the
// background: poll MergeStackStatus while the result is pending.
func (c *Client) MergeStack(ctx context.Context, r Repo, n int, method, sha string) (MergeStackResult, error) {
	var res MergeStackResult
	body := map[string]string{"merge_method": method, "merge_action": "default"}
	if sha != "" {
		body["sha"] = sha
	}
	_, err := c.do(ctx, "PUT", r.path("/pulls/%d/merge-async", n), body, &res)
	return res, err
}

// MergeStackStatus reads a merge of a stack again.
func (c *Client) MergeStackStatus(ctx context.Context, r Repo, n int, uuid string) (MergeStackResult, error) {
	var res MergeStackResult
	_, err := c.do(ctx, "GET", r.path("/pulls/%d/merge-async/%s", n, url.PathEscape(uuid)), nil, &res)
	return res, err
}

// BranchAccess is what the account can do to the branch of a pull request.
type BranchAccess struct {
	// HeadRepo is the repository the branch is in ("owner/name"), "" when it
	// was deleted.
	HeadRepo string
	// Permission is the account's permission there: ADMIN, MAINTAIN, WRITE,
	// TRIAGE or READ; "" when unknown.
	Permission string
}

// CanPush reports whether the account can push to the branch.
func (b BranchAccess) CanPush() bool {
	switch b.Permission {
	case "ADMIN", "MAINTAIN", "WRITE":
		return true
	}
	return false
}

// BranchAccess reads, for each pull request, where its branch is and whether
// the account can push to it.
func (c *Client) BranchAccess(ctx context.Context, r Repo, numbers []int) (map[int]BranchAccess, error) {
	out := map[int]BranchAccess{}
	if len(numbers) == 0 {
		return out, nil
	}
	var q strings.Builder
	q.WriteString("query($owner:String!,$repo:String!){repository(owner:$owner,name:$repo){")
	for i, n := range numbers {
		fmt.Fprintf(&q, "pr%d:pullRequest(number:%d){number headRepository{nameWithOwner viewerPermission}} ", i, n)
	}
	q.WriteString("}}")
	var data struct {
		Repository map[string]*struct {
			Number         int `json:"number"`
			HeadRepository *struct {
				NameWithOwner    string `json:"nameWithOwner"`
				ViewerPermission string `json:"viewerPermission"`
			} `json:"headRepository"`
		} `json:"repository"`
	}
	if err := c.graphql(ctx, q.String(), map[string]any{"owner": r.Owner, "repo": r.Name}, &data); err != nil {
		return nil, err
	}
	for _, pr := range data.Repository {
		if pr == nil {
			continue
		}
		b := BranchAccess{}
		if pr.HeadRepository != nil {
			b = BranchAccess{HeadRepo: pr.HeadRepository.NameWithOwner, Permission: pr.HeadRepository.ViewerPermission}
		}
		out[pr.Number] = b
	}
	return out, nil
}
