package app

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/eliott/myfork/internal/git"
	"github.com/eliott/myfork/internal/github"
)

const (
	// avatarMaxAge is how long a picture on disk is trusted before GitHub is asked again.
	avatarMaxAge = 7 * 24 * time.Hour
	// avatarMaxSize bounds what is read of a picture.
	avatarMaxSize = 2 << 20
)

// avatarCache keeps the pictures of GitHub's users: in memory for the
// session, and on disk between sessions. Each is fetched once, off the main
// thread, and the window redraws as it arrives.
type avatarCache struct {
	mu      sync.Mutex
	images  map[string]*ui.Bitmap // nil once a fetch failed: not retried this session
	loading map[string]bool
	// authors are the accounts commit emails belong to, found by asking
	// GitHub about a commit; asked holds the commits already asked about.
	authors map[string]github.User
	asked   map[string]bool

	// dir holds the pictures as files; "" keeps none.
	dir string
	// fetch is off in tests, which have no window and no network.
	fetch  bool
	client *http.Client
	// loaded runs when a picture arrives.
	loaded func()
}

func newAvatarCache() *avatarCache {
	c := &avatarCache{
		images:  map[string]*ui.Bitmap{},
		loading: map[string]bool{},
		authors: map[string]github.User{},
		asked:   map[string]bool{},
		client:  &http.Client{Timeout: 20 * time.Second},
	}
	if dir, err := os.UserCacheDir(); err == nil {
		c.dir = filepath.Join(dir, "MyFork", "avatars")
	}
	return c
}

// image returns a user's picture, or nil until it comes (then the avatar
// shows their initials). from is the picture's address as GitHub gave it,
// "" to use the login's public one.
func (c *avatarCache) image(login, from string) *ui.Bitmap {
	if login == "" {
		return nil
	}
	key := strings.ToLower(login)
	c.mu.Lock()
	defer c.mu.Unlock()
	if img, ok := c.images[key]; ok || !c.fetch || c.loading[key] {
		return img
	}
	c.loading[key] = true
	go c.load(key, login, from)
	return nil
}

// commitImage returns the picture of a commit's author, nil until it comes
// or when GitHub knows no account for them. A git commit holds a name and
// an email, not a login: a GitHub noreply email names the login, and for the
// others lookup asks GitHub about this commit, off the main thread; one
// answer serves every commit of that email.
func (c *avatarCache) commitImage(email, sha string, lookup func() (github.User, bool)) *ui.Bitmap {
	email = strings.ToLower(email)
	if login, ok := noreplyLogin(email); ok {
		return c.image(login, "")
	}
	c.mu.Lock()
	u, known := c.authors[email]
	ask := !known && c.fetch && email != "" && !c.asked[sha]
	if ask {
		c.asked[sha] = true
	}
	c.mu.Unlock()
	if known {
		return c.image(u.Login, u.AvatarURL)
	}
	if ask {
		go func() {
			u, ok := lookup()
			if !ok {
				return
			}
			c.mu.Lock()
			c.authors[email] = u
			c.mu.Unlock()
			if c.loaded != nil {
				c.loaded()
			}
		}()
	}
	return nil
}

// noreplyLogin reads the login from a GitHub noreply email, which is
// login@users.noreply.github.com or id+login@users.noreply.github.com.
func noreplyLogin(email string) (string, bool) {
	name, ok := strings.CutSuffix(email, "@users.noreply.github.com")
	if !ok {
		return "", false
	}
	if _, login, found := strings.Cut(name, "+"); found {
		name = login
	}
	return name, name != ""
}

func (c *avatarCache) load(key, login, from string) {
	img := c.fromDisk(key)
	if img == nil {
		if data := c.download(login, from); data != nil {
			if img, _ = ui.DecodeBitmap(data); img != nil {
				c.toDisk(key, data)
			}
		}
	}
	c.mu.Lock()
	c.images[key] = img
	delete(c.loading, key)
	c.mu.Unlock()
	if img != nil && c.loaded != nil {
		c.loaded()
	}
}

func (c *avatarCache) path(key string) string {
	if c.dir == "" {
		return ""
	}
	return filepath.Join(c.dir, url.PathEscape(key))
}

// fromDisk reads a picture saved lately.
func (c *avatarCache) fromDisk(key string) *ui.Bitmap {
	p := c.path(key)
	if p == "" {
		return nil
	}
	info, err := os.Stat(p)
	if err != nil || time.Since(info.ModTime()) > avatarMaxAge {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	img, err := ui.DecodeBitmap(data)
	if err != nil {
		os.Remove(p)
		return nil
	}
	return img
}

func (c *avatarCache) toDisk(key string, data []byte) {
	p := c.path(key)
	if p == "" || os.MkdirAll(c.dir, 0o755) != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, p)
	}
}

// download fetches a picture at 64 pixels, nil when it can't.
func (c *avatarCache) download(login, from string) []byte {
	if from == "" {
		from = "https://github.com/" + url.PathEscape(login) + ".png"
	}
	if strings.Contains(from, "?") {
		from += "&s=64"
	} else {
		from += "?s=64"
	}
	res, err := c.client.Get(from)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, avatarMaxSize))
	if err != nil {
		return nil
	}
	return data
}

// commitAvatar shows a commit's author: their GitHub picture when the
// repository is on GitHub and knows them, their initials otherwise.
func (tab *repoTab) commitAvatar(c *ui.Context, cm *git.Commit) ui.Element {
	return tab.authorAvatar(c, cm.Author, cm.AuthorEmail, cm.Hash)
}

// authorAvatar is the picture of the author of a commit.
func (tab *repoTab) authorAvatar(c *ui.Context, author, email, hash string) ui.Element {
	var lookup func() (github.User, bool)
	if tab.gh != nil {
		repo, a := *tab.gh, tab.app
		lookup = func() (github.User, bool) {
			ctx := context.Background()
			cl, err := a.client(ctx, repo.Host)
			if err != nil {
				return github.User{}, false
			}
			u, ok, err := cl.CommitAuthor(ctx, repo, hash)
			return u, ok && err == nil
		}
	} else {
		lookup = func() (github.User, bool) { return github.User{}, false }
	}
	return ui.Avatar(c, author, tab.app.avatars.commitImage(email, hash, lookup))
}
