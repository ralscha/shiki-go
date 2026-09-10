package textmate

import (
	"fmt"
	"maps"
	"shiki-go/internal/oniguruma"
	"sort"
	"strconv"
	"strings"
)

type compiledRule struct {
	id                                                  int
	raw                                                 *Rule
	patterns                                            []*compiledRule
	captures, beginCaptures, endCaptures, whileCaptures []*compiledRule
	match, begin, end, while                            string
	kind                                                int // 0 include, 1 match, 2 begin/end, 3 begin/while
	missing                                             bool
}
type injection struct {
	Selector
	rule *compiledRule
}
type RegexScanner interface {
	Find(string, int, int) (*oniguruma.Match, error)
	Close() error
}
type RegexEngine interface {
	NewScanner([]string) (RegexScanner, error)
	Close() error
}
type scanner struct {
	native RegexScanner
	rules  []*compiledRule
}
type Tokenizer struct {
	engine     RegexEngine
	grammar    *Grammar
	grammars   map[string]*Grammar
	roots      map[string]*Rule
	repos      map[string]map[string]*Rule
	compiled   map[*Rule]*compiledRule
	root       *compiledRule
	injections []injection
	scanners   map[string]*scanner
}

func Compile(grammar *Grammar, grammars map[string]*Grammar, engine RegexEngine, orderedScopes ...string) *Tokenizer {
	t := &Tokenizer{engine: engine, grammar: grammar, grammars: grammars, roots: map[string]*Rule{}, repos: map[string]map[string]*Rule{}, compiled: map[*Rule]*compiledRule{}, scanners: map[string]*scanner{}}
	root, repo := t.external(grammar.ScopeName)
	t.root = t.compile(root, repo)
	// Grammar-local injections precede external ones at the same priority.
	keys := append([]string(nil), grammar.InjectionOrder...)
	var rest []string
	for k := range grammar.Injections {
		found := false
		for _, key := range keys {
			if key == k {
				found = true
			}
		}
		if !found {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	keys = append(keys, rest...)
	for _, selector := range keys {
		r := t.compile(grammar.Injections[selector], repo)
		for _, s := range ParseSelector(selector) {
			t.injections = append(t.injections, injection{s, r})
		}
	}
	scopes := orderedScopes
	for _, scope := range scopes {
		g := grammars[scope]
		if g.InjectionSelector == "" {
			continue
		}
		raw, rp := t.external(scope)
		r := t.compile(raw, rp)
		for _, s := range ParseSelector(g.InjectionSelector) {
			t.injections = append(t.injections, injection{s, r})
		}
	}
	sort.SliceStable(t.injections, func(i, j int) bool { return t.injections[i].Priority < t.injections[j].Priority })
	return t
}
func (t *Tokenizer) external(scope string) (*Rule, map[string]*Rule) {
	if r := t.roots[scope]; r != nil {
		return r, t.repos[scope]
	}
	g := t.grammars[scope]
	if g == nil {
		return nil, nil
	}
	r := &Rule{Name: g.ScopeName, Patterns: g.Patterns}
	repo := map[string]*Rule{}
	maps.Copy(repo, g.Repository)
	t.roots[scope] = r
	t.repos[scope] = repo
	repo["$self"] = r
	if scope == t.grammar.ScopeName {
		repo["$base"] = r
	} else {
		base, _ := t.external(t.grammar.ScopeName)
		repo["$base"] = base
	}
	return r, repo
}
func (t *Tokenizer) compile(raw *Rule, repo map[string]*Rule) *compiledRule {
	if raw == nil {
		return nil
	}
	if c := t.compiled[raw]; c != nil {
		return c
	}
	c := &compiledRule{id: len(t.compiled) + 1, raw: raw}
	t.compiled[raw] = c
	if raw.Match != "" {
		c.kind = 1
		c.match = normalizeRegex(raw.Match)
	} else if raw.Begin != nil {
		c.kind = 2
		c.begin = normalizeRegex(*raw.Begin)
		c.end = "\uffff"
		if raw.End != nil {
			c.end = normalizeRegex(*raw.End)
		}
		if raw.While != nil {
			c.kind = 3
			c.while = normalizeRegex(*raw.While)
		}
	} else if raw.Repository != nil {
		r := map[string]*Rule{}
		maps.Copy(r, repo)
		maps.Copy(r, raw.Repository)
		repo = r
	}
	caps := func(m, fallback map[string]*Rule) []*compiledRule {
		if m == nil {
			m = fallback
		}
		maxID := -1
		for k := range m {
			if n, e := strconv.Atoi(k); e == nil && n >= 0 && n < 65536 && n > maxID {
				maxID = n
			}
		}
		out := make([]*compiledRule, maxID+1)
		for k, r := range m {
			n, e := strconv.Atoi(k)
			if e == nil && n >= 0 && n < len(out) {
				out[n] = t.compile(r, repo)
			}
		}
		return out
	}
	c.captures = caps(raw.Captures, nil)
	c.beginCaptures = caps(raw.BeginCaptures, raw.Captures)
	c.endCaptures = caps(raw.EndCaptures, raw.Captures)
	c.whileCaptures = caps(raw.WhileCaptures, raw.Captures)
	patterns := raw.Patterns
	if patterns == nil && raw.Include != "" {
		patterns = []*Rule{{Include: raw.Include}}
	}
	for _, p := range patterns {
		var r *compiledRule
		if p.Include == "" {
			r = t.compile(p, repo)
		} else {
			inc := p.Include
			if inc == "$self" || inc == "$base" {
				r = t.compile(repo[inc], repo)
			} else if strings.HasPrefix(inc, "#") {
				r = t.compile(repo[inc[1:]], repo)
			} else {
				scope, name, has := strings.Cut(inc, "#")
				external, externalRepo := t.external(scope)
				if has {
					external = externalRepo[name]
				}
				r = t.compile(external, externalRepo)
			}
		}
		if r != nil && (r.kind == 1 || !r.missing || len(r.patterns) != 0) {
			c.patterns = append(c.patterns, r)
		}
	}
	c.missing = len(c.patterns) != len(patterns)
	return c
}
func normalizeRegex(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			if s[i] == 'z' {
				b.WriteString(`$(?!\n)(?<!\n)`)
			} else {
				b.WriteByte('\\')
				b.WriteByte(s[i])
			}
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
func flatten(r *compiledRule, seen map[int]bool, out *[]*compiledRule) {
	if r == nil || seen[r.id] {
		return
	}
	if r.kind != 0 {
		*out = append(*out, r)
		return
	}
	seen[r.id] = true
	for _, p := range r.patterns {
		flatten(p, seen, out)
	}
	delete(seen, r.id)
}
func (t *Tokenizer) scanner(r *compiledRule, end string, own bool) (*scanner, error) {
	key := strconv.Itoa(r.id) + ":" + strconv.FormatBool(own) + ":" + end
	if s := t.scanners[key]; s != nil {
		return s, nil
	}
	var rules []*compiledRule
	if own && r.kind == 1 {
		rules = append(rules, r)
	} else {
		for _, p := range r.patterns {
			flatten(p, map[int]bool{}, &rules)
		}
	}
	var patterns []string
	for _, p := range rules {
		if p.kind == 1 {
			patterns = append(patterns, p.match)
		} else {
			patterns = append(patterns, p.begin)
		}
	}
	if !own && r.kind == 2 {
		if end == "" {
			end = r.end
		}
		if r.raw.ApplyEndPatternLast {
			patterns = append(patterns, end)
			rules = append(rules, nil)
		} else {
			patterns = append([]string{end}, patterns...)
			rules = append([]*compiledRule{nil}, rules...)
		}
	}
	native, err := t.engine.NewScanner(patterns)
	if err != nil {
		return nil, fmt.Errorf("grammar %s rule %d: %w", t.grammar.Name, r.id, err)
	}
	s := &scanner{native, rules}
	t.scanners[key] = s
	return s, nil
}
func (t *Tokenizer) Close() {
	for _, s := range t.scanners {
		_ = s.native.Close()
	}
	t.scanners = map[string]*scanner{}
}
