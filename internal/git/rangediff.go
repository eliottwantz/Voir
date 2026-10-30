package git

import (
	"context"
	"regexp"
	"strings"
)

// IsAncestor reports whether commit a is in the history of commit b.
func (r *Repo) IsAncestor(a, b string) bool {
	_, err := r.Read("merge-base", "--is-ancestor", a, b)
	return err == nil
}

// FetchCommit fetches one commit of a remote by its hash, as GitHub lets a
// commit no branch holds any more be fetched.
func (r *Repo) FetchCommit(ctx context.Context, remote, sha string) error {
	_, err := r.DoContext(ctx, "fetch", "--no-tags", "--no-write-fetch-head", remote, sha)
	return err
}

// RangePair is a commit of an old series matched with its new version, as
// git range-diff matches them.
type RangePair struct {
	// OldN and NewN are the commits' places in their series, from 1; 0 when
	// the commit is in one series only.
	OldN, NewN     int
	OldSHA, NewSHA string // abbreviated; "" when in one series only
	// Status is '=' for a commit unchanged, '!' for one changed, '<' for one
	// dropped and '>' for one added.
	Status  byte
	Subject string
	// Diff is the difference between the two versions' patches, a diff of
	// diffs, for a commit changed.
	Diff string
}

var rangeHeader = regexp.MustCompile(`^\s*(\d+|-):\s+([0-9a-f]+|-+)\s+([=!<>])\s+(\d+|-):\s+([0-9a-f]+|-+)\s?(.*)$`)

// RangeDiff matches the commits of oldBase..oldHead with those of
// newBase..newHead, and tells how each changed, as after a force-push.
func (r *Repo) RangeDiff(oldBase, oldHead, newBase, newHead string) ([]RangePair, error) {
	out, err := r.Read("range-diff", "--no-color", oldBase+".."+oldHead, newBase+".."+newHead)
	if err != nil {
		return nil, err
	}
	return ParseRangeDiff(string(out)), nil
}

// ParseRangeDiff reads the output of git range-diff.
func ParseRangeDiff(out string) []RangePair {
	var pairs []RangePair
	var diff []string
	flush := func() {
		if n := len(pairs); n > 0 && len(diff) > 0 {
			pairs[n-1].Diff = strings.Join(diff, "\n") + "\n"
		}
		diff = diff[:0]
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if m := rangeHeader.FindStringSubmatch(line); m != nil {
			flush()
			p := RangePair{Status: m[3][0], Subject: m[6]}
			p.OldN, p.OldSHA = rangeSide(m[1], m[2])
			p.NewN, p.NewSHA = rangeSide(m[4], m[5])
			pairs = append(pairs, p)
			continue
		}
		if len(pairs) == 0 {
			continue
		}
		// The diff of a pair is indented by four spaces under its header.
		diff = append(diff, strings.TrimPrefix(line, "    "))
	}
	flush()
	// A blank line ends each diff: it is not part of it.
	for i := range pairs {
		pairs[i].Diff = strings.TrimRight(pairs[i].Diff, "\n")
		if pairs[i].Diff != "" {
			pairs[i].Diff += "\n"
		}
	}
	return pairs
}

func rangeSide(n, sha string) (int, string) {
	if n == "-" {
		return 0, ""
	}
	i := 0
	for _, c := range n {
		i = i*10 + int(c-'0')
	}
	return i, sha
}
