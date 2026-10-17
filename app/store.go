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
	Theme            string  `json:"theme"`      // system, light, dark
	DiffLayout       string  `json:"diffLayout"` // split, unified
	IgnoreWhitespace bool    `json:"ignoreWhitespace"`
	WrapLines        bool    `json:"wrapLines"`
	ContextLines     int     `json:"contextLines"`
	CodeFontSize     float32 `json:"codeFontSize"`
	CodeFont         string  `json:"codeFont"`
	SortByDate       bool    `json:"sortByDate"` // commits by date instead of topologically
	HideTags         bool    `json:"hideTags"`
	HideStashes      bool    `json:"hideStashes"`
	FirstParent      bool    `json:"firstParent"`
	AutoFetch        bool    `json:"autoFetch"`
	AlwaysFetchTags  bool    `json:"alwaysFetchTags"`
	PushOnCommit     bool    `json:"pushOnCommit"`
	SubjectLimit     int     `json:"subjectLimit"`
	CompactLabels    bool    `json:"compactLabels"`
}

func defaultSettings() Settings {
	return Settings{Theme: "system", DiffLayout: "split", ContextLines: 3, CodeFontSize: 12, SubjectLimit: 72}
}

// stored is the file the app keeps in the user's config directory.
type stored struct {
	Settings  Settings `json:"settings"`
	Recent    []string `json:"recent"`
	Tabs      []string `json:"tabs"`
	Active    int      `json:"active"`
	SidebarW  float32  `json:"sidebarWidth"`
	ListH     float32  `json:"listHeight"`
	FilesW    float32  `json:"filesWidth"`
	SourceDir string   `json:"sourceDir"`
}

type store struct {
	mu   sync.Mutex
	path string
	data stored
}

func openStore() *store {
	s := &store{data: stored{Settings: defaultSettings(), SidebarW: 230, ListH: 330, FilesW: 280}}
	dir, err := os.UserConfigDir()
	if err != nil {
		return s
	}
	s.path = filepath.Join(dir, "MyFork", "state.json")
	if b, err := os.ReadFile(s.path); err == nil {
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
	}
	return s
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
