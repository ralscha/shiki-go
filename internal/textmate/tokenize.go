package textmate

import (
	"regexp"
	"shiki-go/internal/oniguruma"
	"slices"
	"strconv"
	"strings"
	"time"
)

// State is immutable after TokenizeLine returns and may be reused for branching.
type State struct {
	parent        *State
	rule          *compiledRule
	names, scopes []string
	end           string
	enter, anchor int
	beginEOL      bool
}

func (s *State) Scopes() []string {
	out := []string{}
	for current := s; current != nil; current = current.parent {
		if n := len(current.names); n > 0 {
			out = append(out, current.names[n-1])
		}
	}
	return out
}

type Token struct {
	Start, End int
	Scopes     []string
}
type lineTokens struct {
	tokens []Token
	last   int
}

func (l *lineTokens) produce(scopes []string, end int) {
	if end <= l.last {
		return
	}
	l.tokens = append(l.tokens, Token{l.last, end, scopes})
	l.last = end
}
func pushScopes(base []string, name string) []string {
	if name == "" {
		return base
	}
	out := append([]string(nil), base...)
	return append(out, strings.Fields(name)...)
}
func resetState(s *State) *State {
	if s == nil {
		return nil
	}
	copy := *s
	copy.parent = resetState(s.parent)
	copy.enter = -1
	copy.anchor = -1
	return &copy
}

func (t *Tokenizer) TokenizeLine(line string, state *State, limit time.Duration) ([]Token, *State, error) {
	first := state == nil
	if first {
		state = &State{rule: t.root, names: []string{t.grammar.ScopeName}, scopes: []string{t.grammar.ScopeName}, enter: -1, anchor: -1}
	} else {
		state = resetState(state)
	}
	tokens := &lineTokens{}
	text := line + "\n"
	state, err := t.tokenize(text, 0, state, tokens, first, true, limit, 0)
	if err != nil {
		return nil, nil, err
	}
	var out []Token
	for _, tok := range tokens.tokens {
		if tok.Start >= len(line) {
			break
		}
		tok.End = min(tok.End, len(line))
		out = append(out, tok)
	}
	return out, state, nil
}

type ruleMatch struct {
	rule     *compiledRule
	captures []oniguruma.Capture
}

func findOptions(first bool, pos, anchor int) int {
	options := 0
	if !first {
		options |= 1
	}
	if pos != anchor {
		options |= 4
	}
	return options
}
func (t *Tokenizer) match(text string, pos int, state *State, first bool, anchor int) (*ruleMatch, error) {
	s, err := t.scanner(state.rule, state.end, false)
	if err != nil {
		return nil, err
	}
	options := findOptions(first, pos, anchor)
	m, err := s.native.Find(text, pos, options)
	if err != nil {
		return nil, err
	}
	var best *ruleMatch
	if m != nil {
		best = &ruleMatch{s.rules[m.Index], m.Captures}
	}
	var injected *ruleMatch
	priority := 0
	for _, inj := range t.injections {
		if !inj.Match(state.scopes) {
			continue
		}
		s, err := t.scanner(inj.rule, "", true)
		if err != nil {
			return nil, err
		}
		m, err := s.native.Find(text, pos, options)
		if err != nil {
			return nil, err
		}
		if m != nil && (injected == nil || m.Captures[0].Start < injected.captures[0].Start) {
			injected = &ruleMatch{s.rules[m.Index], m.Captures}
			priority = inj.Priority
			if m.Captures[0].Start == pos {
				break
			}
		}
	}
	if injected != nil && (best == nil || injected.captures[0].Start < best.captures[0].Start || (priority == -1 && injected.captures[0].Start == best.captures[0].Start)) {
		best = injected
	}
	return best, nil
}

func (t *Tokenizer) tokenize(text string, pos int, state *State, tokens *lineTokens, first, checkWhile bool, limit time.Duration, depth int) (*State, error) {
	if depth > 100 {
		tokens.produce(state.scopes, len(text))
		return state, nil
	}
	anchor := -1
	if checkWhile {
		if state.beginEOL {
			anchor = 0
		}
		var whiles []*State
		for s := state; s != nil; s = s.parent {
			if s.rule.kind == 3 {
				whiles = append(whiles, s)
			}
		}
		for _, s := range slices.Backward(whiles) {

			pat := s.end
			if pat == "" {
				pat = s.rule.while
			}
			key := "while:" + strconv.Itoa(s.rule.id) + ":" + pat
			scan := t.scanners[key]
			if scan == nil {
				native, err := t.engine.NewScanner([]string{pat})
				if err != nil {
					return nil, err
				}
				scan = &scanner{native: native}
				t.scanners[key] = scan
			}
			m, err := scan.native.Find(text, pos, findOptions(first, pos, anchor))
			if err != nil {
				return nil, err
			}
			if m == nil {
				state = s.parent
				break
			}
			whole := m.Captures[0]
			tokens.produce(s.scopes, whole.Start)
			if err := t.capture(text, first, s, tokens, s.rule.whileCaptures, m.Captures, depth); err != nil {
				return nil, err
			}
			tokens.produce(s.scopes, whole.End)
			anchor = whole.End
			if whole.End > pos {
				pos = whole.End
				first = false
			}
		}
	}
	return t.scan(text, pos, state, tokens, first, anchor, limit, depth)
}

func (t *Tokenizer) scan(text string, pos int, state *State, tokens *lineTokens, first bool, anchor int, limit time.Duration, depth int) (*State, error) {
	started := time.Now()
	for steps := 0; ; steps++ {
		if steps > 100000 || (limit > 0 && time.Since(started) > limit) {
			tokens.produce(state.scopes, len(text))
			return state, nil
		}
		m, err := t.match(text, pos, state, first, anchor)
		if err != nil {
			return nil, err
		}
		if m == nil {
			tokens.produce(state.scopes, len(text))
			return state, nil
		}
		caps := m.captures
		whole := caps[0]
		advanced := whole.End > pos
		tokens.produce(state.scopes, whole.Start)
		if m.rule == nil {
			popped := state
			endState := *state
			endState.scopes = state.names
			if err := t.capture(text, first, &endState, tokens, state.rule.endCaptures, caps, depth); err != nil {
				return nil, err
			}
			tokens.produce(endState.scopes, whole.End)
			if state.parent != nil {
				state = state.parent
			}
			anchor = popped.anchor
			if !advanced && popped.enter == pos {
				tokens.produce(popped.scopes, len(text))
				return popped, nil
			}
		} else {
			r := m.rule
			before := state
			names := pushScopes(state.scopes, resolveName(r.raw.Name, text, caps))
			state = &State{parent: state, rule: r, names: names, scopes: names, enter: pos, anchor: anchor, beginEOL: whole.End == len(text)}
			captures := r.captures
			if r.kind >= 2 {
				captures = r.beginCaptures
			}
			if err := t.capture(text, first, state, tokens, captures, caps, depth); err != nil {
				return nil, err
			}
			tokens.produce(state.scopes, whole.End)
			if r.kind >= 2 {
				anchor = whole.End
				state.scopes = pushScopes(names, resolveName(r.raw.ContentName, text, caps))
				pattern := r.end
				if r.kind == 3 {
					pattern = r.while
				}
				state.end = resolveBackrefs(pattern, text, caps)
				if !advanced {
					for s := before; s != nil && s.enter == pos; s = s.parent {
						if s.rule == r {
							tokens.produce(before.scopes, len(text))
							return before, nil
						}
					}
				}
			} else {
				state = before
				if !advanced {
					if state.parent != nil {
						state = state.parent
					}
					tokens.produce(state.scopes, len(text))
					return state, nil
				}
			}
		}
		if whole.End > pos {
			pos = whole.End
			first = false
		}
	}
}

func (t *Tokenizer) capture(text string, first bool, state *State, tokens *lineTokens, rules []*compiledRule, caps []oniguruma.Capture, depth int) error {
	type local struct {
		scopes []string
		end    int
	}
	var stack []local
	for i := 0; i < min(len(rules), len(caps)); i++ {
		r := rules[i]
		cap := caps[i]
		if r == nil || cap.Start < 0 || cap.End <= cap.Start {
			continue
		}
		if cap.Start > caps[0].End {
			break
		}
		for len(stack) > 0 && stack[len(stack)-1].end <= cap.Start {
			top := stack[len(stack)-1]
			tokens.produce(top.scopes, top.end)
			stack = stack[:len(stack)-1]
		}
		scopes := state.scopes
		if len(stack) > 0 {
			scopes = stack[len(stack)-1].scopes
		}
		tokens.produce(scopes, cap.Start)
		if r.raw.Patterns != nil {
			names := pushScopes(state.scopes, resolveName(r.raw.Name, text, caps))
			scopes := pushScopes(names, resolveName(r.raw.ContentName, text, caps))
			clone := &State{parent: state, rule: r, names: names, scopes: scopes, enter: cap.Start, anchor: -1}
			if _, err := t.tokenize(text[:min(cap.End, len(text))], cap.Start, clone, tokens, first && cap.Start == 0, false, 0, depth+1); err != nil {
				return err
			}
		} else if name := resolveName(r.raw.Name, text, caps); name != "" {
			stack = append(stack, local{pushScopes(scopes, name), cap.End})
		}
	}
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		tokens.produce(top.scopes, top.end)
		stack = stack[:len(stack)-1]
	}
	return nil
}

var backrefs = regexp.MustCompile(`\\(\d+)`)
var nameRefs = regexp.MustCompile(`\$(\d+)|\$\{(\d+):/(downcase|upcase)\}`)

func captured(text string, caps []oniguruma.Capture, n string) string {
	i, _ := strconv.Atoi(n)
	if i >= len(caps) || caps[i].Start < 0 || caps[i].End > len(text) {
		return ""
	}
	return text[caps[i].Start:caps[i].End]
}
func resolveBackrefs(pattern, text string, caps []oniguruma.Capture) string {
	return backrefs.ReplaceAllStringFunc(pattern, func(s string) string { return regexp.QuoteMeta(captured(text, caps, s[1:])) })
}
func resolveName(name, text string, caps []oniguruma.Capture) string {
	return nameRefs.ReplaceAllStringFunc(name, func(s string) string {
		m := nameRefs.FindStringSubmatch(s)
		n := m[1]
		if n == "" {
			n = m[2]
		}
		v := strings.TrimLeft(captured(text, caps, n), ".")
		switch m[3] {
		case "downcase":
			v = strings.ToLower(v)
		case "upcase":
			v = strings.ToUpper(v)
		}
		return v
	})
}
