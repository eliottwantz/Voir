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
	Number         int       `json:"number"`
	Title          string    `json:"title"`
	Body           string    `json:"body"`
	State          string    `json:"state"` // open, closed
	Draft          bool      `json:"draft"`
	Merged         bool      `json:"merged"`
	Locked         bool      `json:"locked"`
	User           User      `json:"user"`
	Head           Ref       `json:"head"`
	Base           Ref       `json:"base"`
	HTMLURL        string    `json:"html_url"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Labels         []Label   `json:"labels"`
	Comments       int       `json:"comments"`
	Commits        int       `json:"commits"`
	Additions      int       `json:"additions"`
	Deletions      int       `json:"deletions"`
	ChangedFiles   int       `json:"changed_files"`
	Mergeable      *bool     `json:"mergeable"`
	MergeableState string    `json:"mergeable_state"`
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
	return list[PR](ctx, c, r.path("/pulls?state=%s&sort=updated&direction=desc&per_page=100", url.QueryEscape(state)), 300)
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
	SHA    string `json:"sha"`
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

// MarkReady takes a draft out of draft.
func (c *Client) MarkReady(ctx context.Context, r Repo, n int) error {
	// The REST API cannot; this is GraphQL's markPullRequestReadyForReview,
	// which needs the node id.
	var p struct {
		NodeID string `json:"node_id"`
	}
	if _, err := c.do(ctx, "GET", r.path("/pulls/%d", n), nil, &p); err != nil {
		return err
	}
	q := map[string]any{"query": `mutation($id: ID!) { markPullRequestReadyForReview(input: {pullRequestId: $id}) { pullRequest { id } } }`, "variables": map[string]string{"id": p.NodeID}}
	// api.github.com/graphql, or <host>/api/graphql on Enterprise.
	graphql := strings.TrimSuffix(c.BaseURL, "/v3") + "/graphql"
	var out struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if _, err := c.do(ctx, "POST", graphql, q, &out); err != nil {
		return err
	}
	if len(out.Errors) > 0 {
		return errors.New(out.Errors[0].Message)
	}
	return nil
}
