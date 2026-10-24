package app

import (
	"runtime"
	"sync"
	"time"
)

// Coloring a file's lines by their tokens is the slow part of showing a
// diff: it goes through a queue that takes the file asked for last first, so
// that the one drawn now is not left behind those scrolled past, and drops
// the files no longer drawn.

// staleAfter is how long after a file was last drawn its tokens are no
// longer worth working out: it scrolled away, or another file was chosen.
const staleAfter = 750 * time.Millisecond

// markShown notes that a file's lines are drawn, and starts what they wait
// for.
func (tab *repoTab) markShown(rv *review, f *reviewFile) {
	f.seen.Store(time.Now().UnixNano())
	tab.loadFile(rv, f)
}

// stale reports whether a file was drawn, but not lately.
func (f *reviewFile) stale() bool {
	seen := f.seen.Load()
	return seen != 0 && time.Since(time.Unix(0, seen)) > staleAfter
}

// tokenQueue runs the coloring of files, the last asked for first.
type tokenQueue struct {
	mu      sync.Mutex
	jobs    []func()
	running int
}

func (q *tokenQueue) push(fn func()) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = append(q.jobs, fn)
	// A file's two sides are colored at once.
	if q.running < max(2, runtime.GOMAXPROCS(0)/2) {
		q.running++
		go q.run()
	}
}

func (q *tokenQueue) run() {
	for {
		q.mu.Lock()
		n := len(q.jobs)
		if n == 0 {
			q.running--
			q.mu.Unlock()
			return
		}
		fn := q.jobs[n-1]
		q.jobs[n-1] = nil
		q.jobs = q.jobs[:n-1]
		q.mu.Unlock()
		fn()
	}
}

// tokenize runs fn in the queue of coloring, or as the background work of
// tests.
func (a *app) tokenize(fn func()) {
	if a.win == nil {
		a.background(fn)
		return
	}
	a.tokens.push(fn)
}
