// Lexers contains lexers for the syn package and methods for creating syn Lexers
package lexers

import (
	"embed"
	"io/fs"

	"github.com/ddkwork/golibrary/mylog"

	"github.com/jeffwilliams/syn"
)

//go:embed embedded
var embedded embed.FS

// GlobalLexerRegistry is the global LexerRegistry of Lexers.
var GlobalLexerRegistry = func() *syn.LexerRegistry {
	reg := syn.NewLexerRegistry()
	// index(reg)
	paths := mylog.Check2(fs.Glob(embedded, "embedded/*.xml"))

	for _, path := range paths {
		//		mylog.Trace("xml path", path)
		lex := mylog.Check2(syn.NewLexerFromXMLFS(embedded, path))
		// TODO: save the errors here and allow retrieving them

		reg.Register(lex)

	}
	return reg
}()

// Names of all lexers, optionally including aliases.
func Names(withAliases bool) []string {
	return GlobalLexerRegistry.Names(withAliases)
}

// Get a Lexer by name, alias or file extension. Returns nil when no matching lexer is found.
func Get(name string) *syn.Lexer {
	return GlobalLexerRegistry.Get(name)
}

// MatchMimeType attempts to find a lexer for the given MIME type. Returns nil when no matching lexer is found.
func MatchMimeType(mimeType string) *syn.Lexer {
	return GlobalLexerRegistry.MatchMimeType(mimeType)
}

// Match returns the first lexer matching filename. Returns nil when no matching lexer is found.
func Match(filename string) *syn.Lexer {
	return GlobalLexerRegistry.Match(filename)
}
