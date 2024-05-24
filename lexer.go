// Package syn implements a syntax highlighter meant to be used in text editors.
//
// The syn package exports a Lexer type which can be used to lex text for a specific language
// and return an Iterator that can iterate over the Tokens of the text. Each step in the iteration parses
// the next token at that time.
//
// Lexers are normally created using the lexers subpackage. For example:
//
//	import "github.com/jeffwilliams/syn/lexers"
//
//	lexer = lexers.Get("Go")
package syn

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/dlclark/regexp2"

	"github.com/jeffwilliams/syn/internal/config"
)

type Lexer struct {
	config *config.Lexer
	rules  rules
}

func newLexer(r rules) *Lexer {
	return &Lexer{
		rules: r,
	}
}

// NewLexerFromXML creates a new lexer given an XML file containing a definition of a lexer.
func NewLexerFromXMLFile(xmlLexerConfigFile string) (*Lexer, error) {
	f := mylog.Check2(os.Open(xmlLexerConfigFile))

	return NewLexerFromXML(f)
}

// NewLexerFromXML creates a new lexer given an XML file containing a definition of a lexer. The file is opened
// using the specified FS.
func NewLexerFromXMLFS(fsys fs.FS, xmlLexerConfigFile string) (*Lexer, error) {
	f := mylog.Check2(fsys.Open(xmlLexerConfigFile))

	return NewLexerFromXML(f)
}

// NewLexerFromXML creates a new lexer given an XML definition of a lexer.
func NewLexerFromXML(rdr io.Reader) (*Lexer, error) {
	lexModel := mylog.Check2(config.DecodeLexer(rdr))

	bld := newLexerBuilder(lexModel)
	lex := mylog.Check2(bld.Build())

	debugf("NewLexerFromXML: lexer rules:\n%s\n", lex.rules)

	return lex, nil
}

func (l *Lexer) Tokenise(text []rune) Iterator {
	return l.tokeniseAt(text, nil)
}

// tokeniseAt is currently broken. It only works when state is nil.
func (l *Lexer) tokeniseAt(text []rune, state IteratorState) Iterator {
	stripped, offsetMap := ensureLF(text)
	innerIter := newIterator(stripped, l.rules)
	// TODO: when we use coalesce and we save the state, the coalescer state is actually
	// 1 or more tokens ahead of what has been returned during iteration so far, and the
	// coalescer's stored token(s) match the previous unmodified text.
	// How can we update the coalescer when we change the text so that it knows if it's
	// internal token is still valid?

	outerIter := coalesce(adjustForLF(text, innerIter, offsetMap.iterator()))

	// outerIter := adjustForLF(text, innerIter, offsetMap.iterator())
	if state != nil {
		if cState, ok := state.(*coalescerState); ok {
			if cState.accumSet {
				cState.accumSet = false
				cState.AddToIndex(-cState.accum.Length())
			}
		}
		outerIter.SetState(state)
	}
	// fmt.Printf("Lexer.TokeniseAt: text: %s\n", string(text))
	return outerIter
}

func (l *Lexer) cfg() *config.Lexer {
	return l.config
}

type lexerBuilder struct {
	cfg   *config.Lexer
	lexer *Lexer
}

func newLexerBuilder(cfg *config.Lexer) lexerBuilder {
	return lexerBuilder{
		cfg: cfg,
		lexer: &Lexer{
			rules:  newRules(),
			config: cfg,
		},
	}
}

func (lb *lexerBuilder) Build() (*Lexer, error) {
	mylog.CheckIgnore(lb.validate())
	mylog.Check(lb.build())

	lb.resolveIncludes()

	return lb.lexer, nil
}

func (lb *lexerBuilder) validate() error {
	foundRoot := false
	for _, s := range lb.cfg.Rules.States {
		if s.Name == "root" {
			foundRoot = true
		}
	}

	if !foundRoot {
		return fmt.Errorf("No 'root' state is defined")
	}

	var missing []string

	stateNames := map[string]struct{}{}
	for _, state := range lb.cfg.Rules.States {
		stateNames[state.Name] = struct{}{}
	}

	for _, state := range lb.cfg.Rules.States {
		for _, rule := range state.Rules {
			if rule.Push == nil || rule.Push.State == "" {
				continue
			}

			if _, ok := stateNames[rule.Push.State]; !ok {
				missing = append(missing, rule.Push.State)
			}
		}
	}

	return lb.makeMissingError(missing)
}

func (r lexerBuilder) makeMissingError(missing []string) error {
	if missing == nil || len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("The following states are referred to from rules, but aren't defined: %v\n",
		missing)
}

func (lb *lexerBuilder) build() error {
	for _, xmlState := range lb.cfg.Rules.States {

		seq := mylog.Check2(lb.ruleSequence(xmlState.Rules))

		s := state{xmlState.Name, seq}
		lb.lexer.rules.AddState(s)
	}

	for _, xmlState := range lb.cfg.Rules.States {
		mylog.Check(lb.createCombinedStates(&xmlState))
	}

	return nil
}

func (lb *lexerBuilder) ruleSequence(crs []config.Rule) ([]rule, error) {
	rules := make([]rule, len(crs))
	for i, cr := range crs {
		mylog.Check(lb.checkRule(&cr))

		r := mylog.Check2(lb.makeRule(cr.Pattern))

		lb.updatePushForCombinedState(&r, &cr)
		mylog.Check(lb.setRuleFieldsFrom(&r, &cr))

		rules[i] = r
	}
	return rules, nil
}

func (lb *lexerBuilder) makeRule(pattern string) (r rule, err error) {
	pat := `\A` + pattern

	var re *regexp2.Regexp
	re = mylog.Check2(regexp2.Compile(pat, regexp2.Multiline))

	re.MatchTimeout = time.Millisecond * 250

	r = rule{
		pattern: re,
	}
	return
}

// updatePushForCombinedState helps to handle the <combined> element. The combined element
// under a rule requests the lexer to combine all the rules from two states to make a new
// state, and then have the rule push that state. This function replaces the push statement
// on the rule to push the combined state's name
func (lb *lexerBuilder) updatePushForCombinedState(r *rule, cr *config.Rule) {
	if cr.Combined == nil {
		return
	}

	stateName := lb.combinedStateName(cr.Combined)
	r.pushState = stateName
}

// createCombinedStates helps to handle the <combined> element. The combined element
// under a rule requests the lexer to combine all the rules from two states to make a new
// state, and then have the rule push that state. This function creates the combined state
// and adds it to the lexer's states. The states the combined element references must exist
// by the time this is called.
func (lb *lexerBuilder) createCombinedStates(state *config.State) error {
	for _, rule := range state.Rules {
		mylog.Check(lb.createCombinedStateInRule(state.Name, &rule))
	}
	return nil
}

func (lb *lexerBuilder) createCombinedStateInRule(stateName string, cr *config.Rule) error {
	if cr.Combined == nil {
		return nil
	}

	combinedStateName := lb.combinedStateName(cr.Combined)
	if lb.lexer.rules.Contains(combinedStateName) {
		return nil
	}

	combinedState := state{
		name: combinedStateName,
	}

	for _, substateName := range cr.Combined.States {
		substate, ok := lb.lexer.rules.Get(substateName)
		if !ok {
			return fmt.Errorf("The state %s refered to from a combined element under state %s does not exist",
				substate, stateName)
		}

		combinedState.rules = append(combinedState.rules, substate.rules...)
	}

	lb.lexer.rules.AddState(combinedState)

	return nil
}

func (lb *lexerBuilder) combinedStateName(c *config.Combined) string {
	var buf bytes.Buffer
	buf.WriteString("__combined_")
	buf.WriteString(strings.Join(c.States, "__"))
	return buf.String()
}

func (lb *lexerBuilder) setRuleFieldsFrom(r *rule, cr *config.Rule) error {
	if cr.Token != nil {
		typ := mylog.Check2(TokenTypeString(cr.Token.Type))

		r.tok = typ
	}

	if cr.Pop != nil {
		r.popDepth = cr.Pop.Depth
	}

	if cr.Push != nil {
		r.pushState = cr.Push.State
	}

	if cr.Include != nil {
		r.include = cr.Include.State
	}

	if cr.ByGroups != nil {
		for _, e := range cr.ByGroups.ByGroupsElements {
			ge := byGroupElement{}
			switch v := e.V.(type) {
			case *config.Token:
				typ := mylog.Check2(TokenTypeString(v.Type))

				ge.tok = typ
			case *config.UsingSelf:
				ge.useSelfState = v.State
			}
			r.byGroups = append(r.byGroups, ge)
		}
	}

	if cr.UsingSelf != nil {
		r.useSelfState = cr.UsingSelf.State
	}

	return nil
}

func (lb *lexerBuilder) checkRule(r *config.Rule) error {
	// A rule may have only of the following sets:
	// 1. A token and _either_ a push or pop
	// 2. An Include
	// 3. A ByGroups

	if r.Pattern == "" && r.Push == nil && r.Pop == nil && r.Include == nil {
		return fmt.Errorf("Rule has no pattern, no include, no push and no pop statement. This is not supported.")
	}

	if r.Pop != nil && r.Push != nil {
		return fmt.Errorf("Rule contains both a push and a pop")
	}

	if r.Token != nil {
		if r.Include != nil {
			return fmt.Errorf("a rule has both a Token and an Include")
		}
		if r.ByGroups != nil {
			return fmt.Errorf("a rule has both a Token and a ByGroups")
		}
	}

	if r.Include != nil {
		if r.ByGroups != nil {
			return fmt.Errorf("a rule has both an Include and a ByGroups")
		}
	}

	if r.Combined != nil && (r.Push != nil || r.Pop != nil || r.Include != nil) {
		return fmt.Errorf("a rule has both a Combined and either a Push, Pop or Include")
	}

	return nil
}

func (lb *lexerBuilder) resolveIncludes() error {
	newRules := map[string]state{}

	for name, st := range lb.lexer.rules.rules {
		list := mylog.Check2(lb.resolveIncludesIn(st.rules))

		st.rules = list

		newRules[name] = st
	}
	lb.lexer.rules.rules = newRules

	return nil
}

func (lb *lexerBuilder) resolveIncludesIn(rules []rule) (newRules []rule, err error) {
	newRules = make([]rule, 0, len(rules))

	for _, rl := range rules {
		if rl.include != "" {
			includeState, ok := lb.lexer.rules.Get(rl.include)
			if !ok {
				mylog.Check(fmt.Errorf("A rule includes the state named '%s' but there is no such state in the lexer", rl.include))
				return
			}

			var resolved []rule
			resolved = mylog.Check2(lb.resolveIncludesIn(includeState.rules))

			newRules = append(newRules, resolved...)

			continue
		}

		newRules = append(newRules, rl)
	}

	return
}

type prioritisedLexers []*Lexer

func (l prioritisedLexers) Len() int      { return len(l) }
func (l prioritisedLexers) Swap(i, j int) { l[i], l[j] = l[j], l[i] }
func (l prioritisedLexers) Less(i, j int) bool {
	ip := l[i].cfg().Config.Priority
	if ip == 0 {
		ip = 1
	}
	jp := l[j].cfg().Config.Priority
	if jp == 0 {
		jp = 1
	}
	return ip > jp
}
