package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eliott/voir/internal/github"
)

func TestAvatarCache(t *testing.T) {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	png.Encode(&buf, img)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Query().Get("s") != "64" {
			t.Errorf("no size in %s", r.URL)
		}
		w.Write(buf.Bytes())
	}))
	defer srv.Close()

	dir := t.TempDir()
	wait := func(c *avatarCache) {
		t.Helper()
		for i := 0; i < 200 && c.image("Octocat", srv.URL+"/u/1") == nil; i++ {
			time.Sleep(10 * time.Millisecond)
		}
		if c.image("Octocat", srv.URL+"/u/1") == nil {
			t.Fatal("picture never came")
		}
	}

	c := newAvatarCache()
	c.dir, c.fetch = dir, true
	if c.image("octocat", srv.URL) != nil {
		t.Fatal("picture came synchronously")
	}
	wait(c)
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}

	// A new session reads the disk, not the network.
	c2 := newAvatarCache()
	c2.dir, c2.fetch = dir, true
	wait(c2)
	if hits.Load() != 1 {
		t.Fatalf("hits = %d after reading the disk, want 1", hits.Load())
	}

	// Tests fetch nothing.
	if newAvatarCache().image("octocat", srv.URL) != nil || hits.Load() != 1 {
		t.Fatal("fetched while off")
	}
}

func TestNoreplyLogin(t *testing.T) {
	for email, want := range map[string]string{
		"12345+octocat@users.noreply.github.com": "octocat",
		"octocat@users.noreply.github.com":       "octocat",
		"me@example.com":                         "",
		"@users.noreply.github.com":              "",
	} {
		if got, _ := noreplyLogin(email); got != want {
			t.Errorf("noreplyLogin(%q) = %q, want %q", email, got, want)
		}
	}
}

func TestCommitImageAsksOncePerCommit(t *testing.T) {
	var asks atomic.Int32
	c := newAvatarCache()
	c.dir, c.fetch = t.TempDir(), true
	lookup := func() (github.User, bool) { asks.Add(1); return github.User{}, false }
	for range 3 {
		c.commitImage("Me@Example.com", "abc", lookup)
	}
	time.Sleep(50 * time.Millisecond)
	if asks.Load() != 1 {
		t.Fatalf("asked %d times, want 1", asks.Load())
	}
}
