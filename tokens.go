package shiki

import (
	"fmt"
	"maps"
	"regexp"
	"shiki-go/internal/textmate"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
)

type SourceLine struct {
	Content string
	Offset  int
	Ending  string
}

func UTF16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}
func byteIndex(s string, offset int) int {
	if offset <= 0 {
		return 0
	}
	n := 0
	for i, r := range s {
		if n >= offset {
			return i
		}
		n += utf16.RuneLen(r)
	}
	return len(s)
}
func utf16Slice(s string, start, end int) string { return s[byteIndex(s, start):byteIndex(s, end)] }
func SplitLines(code string) []SourceLine {
	var lines []SourceLine
	offset := 0
	for {
		i := strings.IndexByte(code, '\n')
		if i < 0 {
			lines = append(lines, SourceLine{code, offset, ""})
			break
		}
		line, ending := code[:i], "\n"
		if strings.HasSuffix(line, "\r") {
			line = line[:len(line)-1]
			ending = "\r\n"
		}
		lines = append(lines, SourceLine{line, offset, ending})
		offset += UTF16Len(line) + len(ending)
		code = code[i+1:]
	}
	return lines
}
func colorReplacements(theme *Theme, o Options) map[string]string {
	r := map[string]string{}
	maps.Copy(r, theme.ColorReplacements)
	keys := append([]string(nil), o.ColorReplacementOrder...)
	// A native Go map has no insertion order. Apply theme-specific entries first,
	// then global entries, unless the caller supplies an explicit order.
	var nested, flat []string
	for k, v := range o.ColorReplacements {
		if hasString(keys, k) {
			continue
		}
		if _, ok := v.(string); ok {
			flat = append(flat, k)
		} else {
			nested = append(nested, k)
		}
	}
	sort.Strings(nested)
	sort.Strings(flat)
	keys = append(append(keys, nested...), flat...)
	for _, k := range keys {
		v := o.ColorReplacements[k]
		switch v := v.(type) {
		case string:
			r[k] = v
		case map[string]string:
			if k == theme.Name {
				maps.Copy(r, v)
			}
		case map[string]any:
			if k == theme.Name {
				for a, b := range v {
					if s, ok := b.(string); ok {
						r[a] = s
					}
				}
			}
		}
	}
	return r
}

func rootColorReplacements(input any, o Options) map[string]string {
	if name, ok := input.(string); ok {
		return colorReplacements(&Theme{Name: name}, o)
	}
	t, err := themeInput(input)
	if err != nil {
		return nil
	}
	return colorReplacements(t, o)
}

var tokenTypePattern = regexp.MustCompile(`\b(comment|string|regex|meta\.embedded)\b`)

func tokenType(scopes []string) int {
	v := 0
	for _, scope := range scopes {
		switch tokenTypePattern.FindString(scope) {
		case "comment":
			v = 1
		case "string":
			v = 2
		case "regex":
			v = 3
		case "meta.embedded":
			v = 0
		}
	}
	return v
}
func (h *Highlighter) base(code string, o Options) ([][]Token, *GrammarState, error) {
	lang, err := h.resolveAlias(o.Lang)
	if err != nil {
		return nil, nil, err
	}
	lines := SplitLines(code)
	out := make([][]Token, len(lines))
	if isPlainLang(lang) || o.Theme == "none" {
		for i, line := range lines {
			out[i] = []Token{{Content: line.Content, Offset: line.Offset}}
		}
		return out, nil, nil
	}
	theme, err := h.getTheme(o.Theme)
	if err != nil {
		return nil, nil, err
	}
	if lang == "ansi" {
		return tokenizeANSI(code, theme.theme, o), nil, nil
	}
	grammar := h.languages[lang]
	if grammar == nil {
		return nil, nil, fmt.Errorf("shiki: language %q is not loaded", lang)
	}
	tokenizer := h.tokenizers[lang]
	if tokenizer == nil {
		tokenizer = textmate.Compile(grammar, h.scopes, h.engine, h.injectionScopes[grammar.ScopeName]...)
		h.tokenizers[lang] = tokenizer
	}
	var state *textmate.State
	if o.GrammarState != nil {
		s := o.GrammarState
		if s.Lang != lang {
			return nil, nil, fmt.Errorf("shiki: grammar state language %q does not match %q", s.Lang, lang)
		}
		if !hasString(s.Themes, theme.theme.Name) {
			return nil, nil, fmt.Errorf("shiki: grammar state themes do not contain %q", theme.theme.Name)
		}
		if !s.initial && s.owner != tokenizer {
			return nil, nil, fmt.Errorf("shiki: grammar state belongs to a different grammar instance")
		}
		state = s.state
	} else if o.GrammarContextCode != "" {
		context := o
		context.GrammarContextCode = ""
		_, s, err := h.base(o.GrammarContextCode, context)
		if err != nil {
			return nil, nil, err
		}
		state = s.state
	}
	replacements := colorReplacements(theme.theme, o)
	limit := 500 * time.Millisecond
	if o.TokenizeTimeLimit != nil {
		limit = *o.TokenizeTimeLimit
	}
	explain := o.IncludeExplanation != nil && o.IncludeExplanation != false
	typeOnly := o.IncludeExplanation == "tokenType"
	scopeOnly := o.IncludeExplanation == "scopeName"
	for i, line := range lines {
		out[i] = []Token{}
		if line.Content == "" {
			continue
		}
		if o.TokenizeMaxLineLength > 0 && UTF16Len(line.Content) >= o.TokenizeMaxLineLength {
			out[i] = []Token{{Content: line.Content, Offset: line.Offset, styled: true}}
			continue
		}
		raw, next, err := tokenizer.TokenizeLine(line.Content, state, limit)
		if err != nil {
			return nil, nil, err
		}
		state = next
		var lastStyle themeRule
		lastType := -1
		for _, r := range raw {
			style := theme.style(r.Scopes)
			kind := tokenType(r.Scopes)
			content := line.Content[r.Start:r.End]
			token := Token{Content: content, Offset: line.Offset + UTF16Len(line.Content[:r.Start]), Color: applyReplacement(style.fg, replacements), FontStyle: style.font, styled: true}
			if typeOnly {
				token.Type = &kind
			} else if explain {
				token.Explanation = []TokenExplanation{{Content: content, Scopes: theme.explain(r.Scopes, scopeOnly)}}
			}
			n := len(out[i])
			if n > 0 && style.fg == lastStyle.fg && style.bg == lastStyle.bg && style.font == lastStyle.font && kind == lastType {
				prev := &out[i][n-1]
				prev.Content += content
				prev.Explanation = append(prev.Explanation, token.Explanation...)
			} else {
				out[i] = append(out[i], token)
			}
			lastStyle, lastType = style, kind
		}
	}
	return out, &GrammarState{Lang: lang, Themes: []string{theme.theme.Name}, state: state, owner: tokenizer}, nil
}
func (h *Highlighter) CodeToTokensBase(code string, o Options) ([][]Token, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.check(); err != nil {
		return nil, err
	}
	tokens, _, err := h.base(code, o)
	return tokens, err
}
func (h *Highlighter) GetLastGrammarState(code string, o Options) (*GrammarState, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.check(); err != nil {
		return nil, err
	}
	_, state, err := h.base(code, o)
	if err == nil && state == nil {
		err = fmt.Errorf("shiki: this language or theme has no grammar state")
	}
	return state, err
}
func (h *Highlighter) CodeToTokens(code string, o Options) (*TokensResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.check(); err != nil {
		return nil, err
	}
	if o.Themes != nil {
		return h.multipleThemes(code, o)
	}
	if o.Theme == nil {
		return nil, fmt.Errorf("shiki: either theme or themes must be provided")
	}
	tokens, state, err := h.base(code, o)
	if err != nil {
		return nil, err
	}
	theme, err := h.getTheme(o.Theme)
	if err != nil {
		return nil, err
	}
	r := rootColorReplacements(o.Theme, o)
	return &TokensResult{Tokens: tokens, FG: applyReplacement(theme.theme.FG, r), BG: applyReplacement(theme.theme.BG, r), ThemeName: theme.theme.Name, GrammarState: state}, nil
}
func SplitToken(token Token, offsets []int) []Token {
	last := 0
	out := []Token{}
	length := UTF16Len(token.Content)
	for _, off := range offsets {
		off = max(0, min(off, length))
		if off > last {
			copy := token
			copy.Content = utf16Slice(token.Content, last, off)
			copy.Offset += last
			out = append(out, copy)
		}
		last = off
	}
	if last < length {
		copy := token
		copy.Content = utf16Slice(token.Content, last, length)
		copy.Offset += last
		out = append(out, copy)
	}
	return out
}
func SplitTokens(tokens [][]Token, breakpoints []int) [][]Token {
	sorted := append([]int(nil), breakpoints...)
	sort.Ints(sorted)
	out := make([][]Token, len(tokens))
	for i, line := range tokens {
		out[i] = []Token{}
		for _, token := range line {
			var offsets []int
			for j := sort.SearchInts(sorted, token.Offset+1); j < len(sorted) && sorted[j] < token.Offset+UTF16Len(token.Content); j++ {
				offsets = append(offsets, sorted[j]-token.Offset)
			}
			if len(offsets) == 0 {
				out[i] = append(out[i], token)
			} else {
				out[i] = append(out[i], SplitToken(token, offsets)...)
			}
		}
	}
	return out
}
