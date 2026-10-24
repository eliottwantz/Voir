// Package highlight colors source code by its tokens, with Chroma's lexers
// and the palettes of codiff's Licht and Dunkel themes.
package highlight

import (
	"container/list"
	"crypto/sha256"
	"path/filepath"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// Class is the kind of a token, which picks its color.
type Class uint8

const (
	Plain Class = iota
	Comment
	Preproc
	Keyword
	Type
	LangConst
	Function
	ClassName
	Exception
	Number
	String
	Escape
	Regexp
	Tag
	Attribute
	Property
	Heading
	Inserted
	Deleted
	NumClasses
)

// Seg is a run of a line's bytes of one class.
type Seg struct {
	Start, End int32
	Class      Class
}

// MaxBytes bounds the files worth highlighting.
const MaxBytes = 1 << 20

var (
	lexerMu    sync.Mutex
	lexerCache = map[string]chroma.Lexer{}
)

// Lexer returns the lexer for a file name, nil for plain text. Lexers are
// kept by the file's extension, or its name without one: finding one
// tries the patterns of every lexer.
func Lexer(name string) chroma.Lexer {
	base := name
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	key := base
	if i := strings.LastIndexByte(base, '.'); i > 0 && !specialName(base) {
		key = "*" + strings.ToLower(base[i:])
	}
	lexerMu.Lock()
	l, ok := lexerCache[key]
	lexerMu.Unlock()
	if ok {
		return l
	}
	l = findLexer(name)
	lexerMu.Lock()
	lexerCache[key] = l
	lexerMu.Unlock()
	return l
}

var (
	specialOnce     sync.Once
	specialPatterns []string
)

// specialName reports whether a lexer matches a file name by more than
// its extension, as CMakeLists.txt or *.d.ts, or the name ends as backups
// do: its lexer is found by its whole name.
func specialName(base string) bool {
	specialOnce.Do(func() {
		for _, l := range lexers.GlobalLexerRegistry.Lexers {
			for _, glob := range l.Config().Filenames {
				ext, ok := strings.CutPrefix(glob, "*.")
				if !ok || strings.ContainsAny(ext, ".*?[") {
					specialPatterns = append(specialPatterns, glob)
				}
			}
		}
	})
	for _, suffix := range []string{"~", ".bak", ".old", ".orig", ".dpkg-dist", ".dpkg-old", ".ucf-dist", ".ucf-new", ".ucf-old", ".rpmnew", ".rpmorig", ".rpmsave"} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	for _, glob := range specialPatterns {
		if ok, _ := filepath.Match(glob, base); ok {
			return true
		}
	}
	return false
}

func findLexer(name string) chroma.Lexer {
	l := lexers.Match(name)
	if l == nil {
		base := name
		if i := strings.LastIndexByte(base, '/'); i >= 0 {
			base = base[i+1:]
		}
		switch base {
		case "Makefile", "makefile", "GNUmakefile":
			l = lexers.Get("make")
		case "Dockerfile":
			l = lexers.Get("docker")
		}
	}
	if l == nil {
		return nil
	}
	return chroma.Coalesce(l)
}

// Lines tokenizes lines of the file named name, and returns the segments of
// each line, nil for plain text. The segments are shared with a cache of the
// texts tokenized lately: they are read, never changed.
func Lines(name string, lines []string) [][]Seg {
	lexer, text, key, ok := prepare(name, lines)
	if !ok {
		return nil
	}
	if out, ok := cache.get(key); ok {
		return out
	}
	it, err := lexer.Tokenise(nil, text)
	if err != nil {
		return nil
	}
	out := make([][]Seg, len(lines))
	row, col := 0, 0
	for tok := it(); tok != chroma.EOF; tok = it() {
		class := classOf(tok.Type)
		v := tok.Value
		for v != "" && row < len(lines) {
			nl := strings.IndexByte(v, '\n')
			part := v
			if nl >= 0 {
				part = v[:nl]
			}
			if part != "" {
				end := min(col+len(part), len(lines[row]))
				if class != Plain && end > col {
					segs := out[row]
					if n := len(segs); n > 0 && segs[n-1].Class == class && int(segs[n-1].End) == col {
						segs[n-1].End = int32(end)
					} else {
						segs = append(segs, Seg{Start: int32(col), End: int32(end), Class: class})
					}
					out[row] = segs
				}
				col = end
			}
			if nl < 0 {
				break
			}
			v = v[nl+1:]
			row++
			col = 0
		}
	}
	cache.put(key, out)
	return out
}

// cacheBytes bounds the memory the segments of the texts highlighted last
// keep: tokenizing is slow, and a diff reads the same files again when one
// goes back to a file, a commit or a pull request, or the next commit has
// the file as the last had it.
const cacheBytes = 48 << 20

type cacheKey struct {
	lexer string
	sum   [sha256.Size]byte
}

type cacheEntry struct {
	key  cacheKey
	segs [][]Seg
	size int
}

// lru holds the segments of texts by their lexer and text, the most recently
// used first.
type lru struct {
	mu    sync.Mutex
	order list.List
	byKey map[cacheKey]*list.Element
	size  int
}

var cache = lru{byKey: map[cacheKey]*list.Element{}}

func (c *lru) get(k cacheKey) ([][]Seg, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.byKey[k]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*cacheEntry).segs, true
}

func (c *lru) put(k cacheKey, segs [][]Seg) {
	size := 64 + 24*len(segs)
	for _, l := range segs {
		size += 12 * cap(l)
	}
	if size > cacheBytes/4 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.byKey[k]; ok {
		return
	}
	c.byKey[k] = c.order.PushFront(&cacheEntry{key: k, segs: segs, size: size})
	c.size += size
	for c.size > cacheBytes {
		last := c.order.Back()
		e := last.Value.(*cacheEntry)
		c.order.Remove(last)
		delete(c.byKey, e.key)
		c.size -= e.size
	}
}

// prepare finds the lexer of lines and their text, and the key of their
// segments in the cache; ok is false for plain text, none or too much.
func prepare(name string, lines []string) (lexer chroma.Lexer, text string, key cacheKey, ok bool) {
	lexer = Lexer(name)
	if lexer == nil || len(lines) == 0 {
		return
	}
	size := 0
	for _, l := range lines {
		size += len(l) + 1
	}
	if size > MaxBytes {
		return
	}
	text = strings.Join(lines, "\n") + "\n"
	key = cacheKey{lexer: lexer.Config().Name, sum: sha256.Sum256([]byte(text))}
	return lexer, text, key, true
}

func classOf(t chroma.TokenType) Class {
	switch t {
	case chroma.KeywordType:
		return Type
	case chroma.KeywordConstant, chroma.NameBuiltinPseudo, chroma.NameVariableMagic:
		return LangConst
	case chroma.NameFunction, chroma.NameFunctionMagic, chroma.NameBuiltin, chroma.NameDecorator:
		return Function
	case chroma.NameClass:
		return ClassName
	case chroma.NameException:
		return Exception
	case chroma.NameConstant, chroma.LiteralStringSymbol:
		return Number
	case chroma.NameTag, chroma.NameEntity:
		return Tag
	case chroma.NameAttribute:
		return Attribute
	case chroma.NameProperty, chroma.NameLabel:
		return Property
	case chroma.LiteralStringEscape:
		return Escape
	case chroma.LiteralStringRegex:
		return Regexp
	case chroma.CommentPreproc, chroma.CommentPreprocFile:
		return Preproc
	case chroma.GenericHeading, chroma.GenericSubheading:
		return Heading
	case chroma.GenericInserted:
		return Inserted
	case chroma.GenericDeleted:
		return Deleted
	}
	switch t.Category() {
	case chroma.Keyword, chroma.Operator:
		return Keyword
	case chroma.Comment:
		return Comment
	}
	switch t.SubCategory() {
	case chroma.LiteralString:
		return String
	case chroma.LiteralNumber:
		return Number
	}
	return Plain
}
