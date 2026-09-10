package textmate

import "regexp"
import "strings"

func ScopeMatches(scope, pattern string) bool {
	return scope == pattern || strings.HasPrefix(scope, pattern+".")
}

type Matcher func([]string) bool
type Selector struct {
	Match    Matcher
	Priority int
}

var selectorTokens = regexp.MustCompile(`[LR]:|[\w.:][\w.:\-]*|[,|\-()]`)

func ParseSelector(input string) []Selector {
	p := selectorParser{tokens: selectorTokens.FindAllString(input, -1)}
	var out []Selector
	for p.pos < len(p.tokens) {
		priority := 0
		if p.peek() == "L:" {
			priority = -1
			p.pos++
		} else if p.peek() == "R:" {
			priority = 1
			p.pos++
		}
		before := p.pos
		out = append(out, Selector{p.conjunction(), priority})
		if p.peek() != "," || p.pos == before {
			break
		}
		p.pos++
	}
	return out
}

type selectorParser struct {
	tokens []string
	pos    int
}

func (p *selectorParser) peek() string {
	if p.pos >= len(p.tokens) {
		return ""
	}
	return p.tokens[p.pos]
}
func (p *selectorParser) operand() Matcher {
	switch p.peek() {
	case "-":
		p.pos++
		m := p.operand()
		return func(s []string) bool { return m != nil && !m(s) }
	case "(":
		p.pos++
		var ms []Matcher
		for {
			ms = append(ms, p.conjunction())
			if p.peek() != "|" && p.peek() != "," {
				break
			}
			p.pos++
		}
		if p.peek() == ")" {
			p.pos++
		}
		return func(s []string) bool {
			for _, m := range ms {
				if m(s) {
					return true
				}
			}
			return false
		}
	case "", ")", "|", ",":
		return nil
	}
	var ids []string
	for {
		s := p.peek()
		if s == "" || s == "(" || s == ")" || s == "-" || s == "|" || s == "," {
			break
		}
		ids = append(ids, s)
		p.pos++
	}
	return func(scopes []string) bool {
		i := 0
		for _, id := range ids {
			found := false
			for i < len(scopes) {
				s := scopes[i]
				i++
				if ScopeMatches(s, id) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
}
func (p *selectorParser) conjunction() Matcher {
	var ms []Matcher
	for {
		m := p.operand()
		if m == nil {
			break
		}
		ms = append(ms, m)
	}
	return func(s []string) bool {
		for _, m := range ms {
			if !m(s) {
				return false
			}
		}
		return true
	}
}
