package github

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Found is a pull request the account is involved in, as a search finds it,
// with the repository it is of and how its review and checks stand.
type Found struct {
	Repo   Repo
	PR     PR
	Status PRStatus
}

const involvedQuery = `
query Involved($q: String!, $cursor: String) {
  search(query: $q, type: ISSUE, first: 100, after: $cursor) {
    pageInfo { hasNextPage endCursor }
    nodes {
      ... on PullRequest {
        number title state isDraft merged mergedAt createdAt updatedAt url
        headRefName headRefOid baseRefName
        additions deletions changedFiles mergeable reviewDecision
        author { login avatarUrl url }
        repository { nameWithOwner }
        comments { totalCount }
        labels(first: 20) { nodes { name color description } }
        reviewRequests(first: 20) { nodes { requestedReviewer { ... on User { login avatarUrl url } } } }
        commits(last: 1) { totalCount nodes { commit { statusCheckRollup { state } } } }
      }
    }
  }
}`

type involvedNode struct {
	Number         int        `json:"number"`
	Title          string     `json:"title"`
	State          string     `json:"state"`
	IsDraft        bool       `json:"isDraft"`
	Merged         bool       `json:"merged"`
	MergedAt       *time.Time `json:"mergedAt"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	URL            string     `json:"url"`
	HeadRefName    string     `json:"headRefName"`
	HeadRefOid     string     `json:"headRefOid"`
	BaseRefName    string     `json:"baseRefName"`
	Additions      int        `json:"additions"`
	Deletions      int        `json:"deletions"`
	ChangedFiles   int        `json:"changedFiles"`
	Mergeable      string     `json:"mergeable"`
	ReviewDecision string     `json:"reviewDecision"`
	Author         *User      `json:"author"`
	Repository     struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Comments struct {
		TotalCount int `json:"totalCount"`
	} `json:"comments"`
	Labels struct {
		Nodes []Label `json:"nodes"`
	} `json:"labels"`
	ReviewRequests struct {
		Nodes []struct {
			RequestedReviewer *User `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"reviewRequests"`
	Commits struct {
		TotalCount int `json:"totalCount"`
		Nodes      []struct {
			Commit struct {
				StatusCheckRollup *struct {
					State string `json:"state"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

// searchPRs reads the pull requests a search finds, three pages of 100 at most.
func (c *Client) searchPRs(ctx context.Context, query string) ([]involvedNode, error) {
	var out []involvedNode
	var cursor any
	for page := 0; page < 3; page++ {
		var data struct {
			Search struct {
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []involvedNode `json:"nodes"`
			} `json:"search"`
		}
		if err := c.graphql(ctx, involvedQuery, map[string]any{"q": query, "cursor": cursor}, &data); err != nil {
			return out, err
		}
		for _, n := range data.Search.Nodes {
			if n.Number > 0 && n.Repository.NameWithOwner != "" {
				out = append(out, n)
			}
		}
		if !data.Search.PageInfo.HasNextPage {
			break
		}
		cursor = data.Search.PageInfo.EndCursor
	}
	return out, nil
}

// Involved finds the pull requests, in any repository of a host, that the
// account has a part in: it wrote them, is assigned to or asked to review
// them, has reviewed or commented on them, or is mentioned in them. State is
// open, closed or all. The account is the viewer's, who is returned with them.
func (c *Client) Involved(ctx context.Context, host, state string) (User, []Found, error) {
	viewer, err := c.Viewer(ctx)
	if err != nil {
		return viewer, nil, err
	}
	scope := "is:pr archived:false sort:updated-desc"
	switch state {
	case "open":
		scope += " is:open"
	case "closed":
		scope += " is:closed"
	}
	queries := []string{"involves:", "review-requested:", "reviewed-by:"}
	nodes := make([][]involvedNode, len(queries))
	errs := make([]error, len(queries))
	var wg sync.WaitGroup
	for i, q := range queries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			nodes[i], errs[i] = c.searchPRs(ctx, scope+" "+q+viewer.Login)
		}()
	}
	wg.Wait()
	var found []Found
	at := map[string]int{}
	for i, list := range nodes {
		for _, n := range list {
			key := fmt.Sprint(n.Repository.NameWithOwner, "#", n.Number)
			j, seen := at[key]
			if !seen {
				j = len(found)
				at[key] = j
				found = append(found, n.found(host))
			}
			// A team's request does not name the account among the reviewers.
			if q := queries[i]; q == "review-requested:" && !hasUser(found[j].PR.RequestedReviewers, viewer.Login) && found[j].PR.State == "open" {
				found[j].PR.RequestedReviewers = append(found[j].PR.RequestedReviewers, viewer)
			}
		}
	}
	// What the searches found is shown, when any of them did.
	for _, e := range errs {
		if e != nil && len(found) == 0 {
			return viewer, nil, e
		}
	}
	return viewer, found, nil
}

func hasUser(users []User, login string) bool {
	for _, u := range users {
		if u.Login == login {
			return true
		}
	}
	return false
}

func (n involvedNode) found(host string) Found {
	owner, name, _ := strings.Cut(n.Repository.NameWithOwner, "/")
	merged := n.Merged || n.State == "MERGED"
	p := PR{
		Number: n.Number, Title: n.Title, State: strings.ToLower(n.State), Draft: n.IsDraft, Merged: merged, MergedAt: n.MergedAt,
		Head: Ref{Ref: n.HeadRefName, SHA: n.HeadRefOid}, Base: Ref{Ref: n.BaseRefName}, HTMLURL: n.URL,
		CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt, Labels: n.Labels.Nodes,
		Comments: n.Comments.TotalCount, Commits: n.Commits.TotalCount, Additions: n.Additions, Deletions: n.Deletions, ChangedFiles: n.ChangedFiles,
	}
	if merged {
		p.State = "closed"
	}
	if n.Author != nil {
		p.User = *n.Author
	}
	for _, r := range n.ReviewRequests.Nodes {
		// A team asked to review has no login of its own here.
		if r.RequestedReviewer != nil && r.RequestedReviewer.Login != "" {
			p.RequestedReviewers = append(p.RequestedReviewers, *r.RequestedReviewer)
		}
	}
	st := PRStatus{
		Review:      map[string]string{"APPROVED": "approved", "CHANGES_REQUESTED": "changes", "REVIEW_REQUIRED": "required"}[n.ReviewDecision],
		Conflicting: n.Mergeable == "CONFLICTING", Additions: n.Additions, Deletions: n.Deletions,
	}
	if len(n.Commits.Nodes) > 0 && n.Commits.Nodes[0].Commit.StatusCheckRollup != nil {
		st.Checks = map[string]string{"SUCCESS": "passing", "FAILURE": "failing", "ERROR": "failing", "PENDING": "pending", "EXPECTED": "pending"}[n.Commits.Nodes[0].Commit.StatusCheckRollup.State]
	}
	return Found{Repo: Repo{Host: host, Owner: owner, Name: name}, PR: p, Status: st}
}
