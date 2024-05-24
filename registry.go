package syn

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
)

var ignoredSuffixes = [...]string{
	// Editor backups
	"~", ".bak", ".old", ".orig",
	// Debian and derivatives apt/dpkg/ucf backups
	".dpkg-dist", ".dpkg-old", ".ucf-dist", ".ucf-new", ".ucf-old",
	// Red Hat and derivatives rpm backups
	".rpmnew", ".rpmorig", ".rpmsave",
	// Build system input/template files
	".in",
}

// LexerRegistry is a registry of Lexers.
type LexerRegistry struct {
	Lexers  []*Lexer
	byName  map[string]*Lexer
	byAlias map[string]*Lexer
}

// NewLexerRegistry creates a new LexerRegistry of Lexers.
func NewLexerRegistry() *LexerRegistry {
	return &LexerRegistry{
		byName:  map[string]*Lexer{},
		byAlias: map[string]*Lexer{},
	}
}

// Names of all lexers, optionally including aliases.
func (l *LexerRegistry) Names(withAliases bool) []string {
	out := []string{}
	for _, lexer := range l.Lexers {
		config := lexer.cfg().Config
		out = append(out, config.Name)
		if withAliases {
			out = append(out, config.Aliases...)
		}
	}
	sort.Strings(out)
	return out
}

// Get a Lexer by name, alias or file extension.
func (l *LexerRegistry) Get(name string) *Lexer {
	if lexer := l.byName[name]; lexer != nil {
		return lexer
	}
	if lexer := l.byAlias[name]; lexer != nil {
		return lexer
	}
	if lexer := l.byName[strings.ToLower(name)]; lexer != nil {
		return lexer
	}
	if lexer := l.byAlias[strings.ToLower(name)]; lexer != nil {
		return lexer
	}

	candidates := prioritisedLexers{}
	// Try file extension.
	if lexer := l.Match("filename." + name); lexer != nil {
		candidates = append(candidates, lexer)
	}
	// Try exact filename.
	if lexer := l.Match(name); lexer != nil {
		candidates = append(candidates, lexer)
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Sort(candidates)
	return candidates[0]
}

// MatchMimeType attempts to find a lexer for the given MIME type.
func (l *LexerRegistry) MatchMimeType(mimeType string) *Lexer {
	matched := prioritisedLexers{}
	for _, l := range l.Lexers {
		for _, lmt := range l.cfg().Config.MimeTypes {
			if mimeType == lmt {
				matched = append(matched, l)
			}
		}
	}
	if len(matched) != 0 {
		sort.Sort(matched)
		return matched[0]
	}
	return nil
}

// Match returns the first lexer matching filename.
func (l *LexerRegistry) Match(filename string) *Lexer {
	filename = filepath.Base(filename)
	matched := prioritisedLexers{}
	// First, try primary filename matches.
	for _, lexer := range l.Lexers {
		config := lexer.cfg().Config
		for _, glob := range config.Filenames {
			mylog.Check2(filepath.Match(glob, filename))
			// nolint
		}
	}
	if len(matched) > 0 {
		sort.Sort(matched)
		return matched[0]
	}
	return nil
}

// Register a Lexer with the LexerRegistry.
func (l *LexerRegistry) Register(lexer *Lexer) *Lexer {
	// lexer.SetRegistry(l)

	config := lexer.cfg().Config
	l.byName[config.Name] = lexer
	l.byName[strings.ToLower(config.Name)] = lexer
	for _, alias := range config.Aliases {
		l.byAlias[alias] = lexer
		l.byAlias[strings.ToLower(alias)] = lexer
	}
	l.Lexers = append(l.Lexers, lexer)
	return lexer
}
