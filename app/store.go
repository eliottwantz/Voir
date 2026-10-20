package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// Settings are the preferences, kept between runs.
type Settings struct {
	Theme            string     `json:"theme"`      // system, light, dark
	DiffLayout       string     `json:"diffLayout"` // split, unified
	IgnoreWhitespace bool       `json:"ignoreWhitespace"`
	WrapLines        bool       `json:"wrapLines"`
	ContextLines     int        `json:"contextLines"`
	CodeFontSize     float32    `json:"codeFontSize"`
	CodeFont         string     `json:"codeFont"`
	SortByDate       bool       `json:"sortByDate"` // commits by date instead of topologically
	HideTags         bool       `json:"hideTags"`
	HideStashes      bool       `json:"hideStashes"`
	FirstParent      bool       `json:"firstParent"`
	AutoFetch        bool       `json:"autoFetch"`
	AlwaysFetchTags  bool       `json:"alwaysFetchTags"`
	PushOnCommit     bool       `json:"pushOnCommit"`
	SubjectLimit     int        `json:"subjectLimit"`
	GraphLanes       int        `json:"graphLanes"`    // lanes drawn in the commit graph; the rest fold into the last
	MergeMessage     string     `json:"mergeMessage"`  // default, title, title_description
	SquashMessage    string     `json:"squashMessage"` // default, title, title_description
	MergeMethod      string     `json:"mergeMethod"`   // the last way a pull request was merged: merge, squash or rebase
	AI               AISettings `json:"ai"`
}

// AISettings say which model writes commit messages, pull requests and review
// guides. The model, effort and program are kept for each provider, so that
// switching back finds them.
type AISettings struct {
	Provider string            `json:"provider"` // claude, codex, opencode; "" is claude
	Models   map[string]string `json:"models"`
	Efforts  map[string]string `json:"efforts"`
	Binaries map[string]string `json:"binaries"`
	// Style is "", conventional_commits, repo_conventions or custom.
	Style  string `json:"style"`
	Custom string `json:"custom"`
	// IgnoreTemplates stops a pull request from following the repository's
	// template.
	IgnoreTemplates bool `json:"ignoreTemplates"`
	// GuideNotes are the maintainer's preferences for review guides.
	GuideNotes string `json:"guideNotes"`
}

func defaultSettings() Settings {
	return Settings{Theme: "system", DiffLayout: "split", ContextLines: 3, CodeFontSize: 12, SubjectLimit: 72, GraphLanes: 30}
}

// stored is the file the app keeps in the user's config directory.
type stored struct {
	Settings Settings `json:"settings"`
	Recent   []string `json:"recent"`
	// Pinned are the roots of the repositories whose pull requests are listed
	// without being open.
	Pinned    []string `json:"pinned"`
	Tabs      []string `json:"tabs"`
	Active    int      `json:"active"`
	SidebarW  float32  `json:"sidebarWidth"`
	ListH     float32  `json:"listHeight"`
	FilesW    float32  `json:"filesWidth"`
	StageH    float32  `json:"stageHeight"`
	SourceDir string   `json:"sourceDir"`
}

type store struct {
	mu   sync.Mutex
	path string
	data stored
}

// newStore is a store as an app that has never run is set up: the defaults of
// every pane, which tests use too.
func newStore() *store {
	return &store{data: stored{Settings: defaultSettings(), SidebarW: 230, ListH: 330, FilesW: 280, StageH: 260}}
}

func openStore() *store {
	s := newStore()
	dir := configDir()
	if dir == "" {
		return s
	}
	s.path = filepath.Join(dir, "config.json")
	b, err := os.ReadFile(s.path)
	if err != nil {
		// Before config.json, the state lived in the platform's config dir.
		if old, oerr := os.UserConfigDir(); oerr == nil {
			b, err = os.ReadFile(filepath.Join(old, "MyFork", "state.json"))
		}
	}
	if err == nil {
		json.Unmarshal(b, &s.data)
		if s.data.Settings.Theme == "" {
			s.data.Settings = defaultSettings()
		}
		if s.data.SidebarW < 120 {
			s.data.SidebarW = 230
		}
		if s.data.ListH < 120 {
			s.data.ListH = 330
		}
		if s.data.FilesW < 120 {
			s.data.FilesW = 280
		}
		if s.data.StageH < 40 {
			s.data.StageH = 260
		}
	}
	return s
}

// configDir is $XDG_CONFIG_HOME/myfork, or ~/.config/myfork.
func configDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "myfork")
}

func (s *store) save() {
	if s.path == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(s.path), 0o755)
	os.WriteFile(s.path, b, 0o644)
}

func (s *store) settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Settings
}

func (s *store) update(fn func(*Settings)) {
	s.mu.Lock()
	fn(&s.data.Settings)
	s.mu.Unlock()
	s.save()
}

// recent lists the repositories opened lately that are still on disk.
func (s *store) recent() []string {
	s.mu.Lock()
	all := slices.Clone(s.data.Recent)
	s.mu.Unlock()
	return slices.DeleteFunc(all, func(root string) bool {
		_, err := os.Stat(root)
		return err != nil
	})
}

// addRecent puts a repository first in the recent ones.
func (s *store) addRecent(root string) {
	s.mu.Lock()
	s.data.Recent = slices.DeleteFunc(s.data.Recent, func(p string) bool { return p == root })
	s.data.Recent = append([]string{root}, s.data.Recent...)
	if len(s.data.Recent) > 20 {
		s.data.Recent = s.data.Recent[:20]
	}
	s.mu.Unlock()
	s.save()
}

func (s *store) clearRecent() {
	s.mu.Lock()
	s.data.Recent = nil
	s.mu.Unlock()
	s.save()
}

// pinned lists the repositories pinned that are still on disk.
func (s *store) pinned() []string {
	s.mu.Lock()
	all := slices.Clone(s.data.Pinned)
	s.mu.Unlock()
	return slices.DeleteFunc(all, func(root string) bool {
		_, err := os.Stat(root)
		return err != nil
	})
}

// isPinned reports whether a repository is pinned.
func (s *store) isPinned(root string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.data.Pinned, root)
}

// pin adds a repository to the pinned ones, last, and says whether it was not
// there already.
func (s *store) pin(root string) bool {
	s.mu.Lock()
	if slices.Contains(s.data.Pinned, root) {
		s.mu.Unlock()
		return false
	}
	s.data.Pinned = append(s.data.Pinned, root)
	s.mu.Unlock()
	s.save()
	return true
}

func (s *store) unpin(root string) {
	s.mu.Lock()
	s.data.Pinned = slices.DeleteFunc(s.data.Pinned, func(p string) bool { return p == root })
	s.mu.Unlock()
	s.save()
}
