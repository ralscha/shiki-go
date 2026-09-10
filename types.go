// Package shiki provides TextMate syntax highlighting compatible with Shiki.
// It embeds the bundled grammars, themes, and Oniguruma engine and requires
// neither Node.js nor cgo at runtime. Offsets use UTF-16 code units, as in Shiki.
package shiki

import (
	"bytes"
	"encoding/json"
	"shiki-go/internal/textmate"
	"strings"
	"time"
)

const Version = "4.4.3"

type Language = textmate.Grammar
type Rule = textmate.Rule
type CaptureMap = textmate.CaptureMap
type StringList = textmate.StringList

type FontStyle int

const (
	FontStyleNone          FontStyle = 0
	FontStyleItalic        FontStyle = 1
	FontStyleBold          FontStyle = 2
	FontStyleUnderline     FontStyle = 4
	FontStyleStrikethrough FontStyle = 8
)

type ThemeSetting struct {
	Name        string     `json:"name,omitempty"`
	Scope       StringList `json:"scope,omitempty"`
	Settings    ThemeStyle `json:"settings"`
	scopeString bool
}

func (s *ThemeSetting) UnmarshalJSON(b []byte) error {
	type alias ThemeSetting
	var raw struct {
		*alias
		Scope json.RawMessage `json:"scope"`
	}
	raw.alias = (*alias)(s)
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if len(raw.Scope) > 0 {
		if err := json.Unmarshal(raw.Scope, &s.Scope); err != nil {
			return err
		}
		s.scopeString = raw.Scope[0] == '"'
	}
	return nil
}
func (s ThemeSetting) MarshalJSON() ([]byte, error) {
	m := map[string]any{"settings": s.Settings}
	if s.Name != "" {
		m["name"] = s.Name
	}
	if s.Scope != nil {
		if s.scopeString {
			m["scope"] = strings.Join(s.Scope, ",")
		} else {
			m["scope"] = s.Scope
		}
	}
	return json.Marshal(m)
}

type ThemeStyle struct {
	Foreground string  `json:"foreground,omitempty"`
	Background string  `json:"background,omitempty"`
	FontStyle  *string `json:"fontStyle,omitempty"`
}
type Theme struct {
	Name              string            `json:"name"`
	DisplayName       string            `json:"displayName,omitempty"`
	Type              string            `json:"type,omitempty"`
	FG                string            `json:"fg,omitempty"`
	BG                string            `json:"bg,omitempty"`
	Settings          []ThemeSetting    `json:"settings,omitempty"`
	TokenColors       []ThemeSetting    `json:"tokenColors,omitempty"`
	Colors            map[string]any    `json:"colors,omitempty"`
	ColorReplacements map[string]string `json:"colorReplacements,omitzero"`
	colorsOrder       []string
}

func (t *Theme) UnmarshalJSON(data []byte) error {
	type alias Theme
	if err := json.Unmarshal(data, (*alias)(t)); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	t.colorsOrder = objectKeys(fields["colors"])
	return nil
}

type LanguageInfo struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
}
type ThemeInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Type        string `json:"type"`
}
type HighlighterOptions struct {
	Engine             RegexEngine
	Langs              []string
	Themes             []string
	Languages          []*Language
	ThemeRegistrations []*Theme
	LangAlias          map[string]string
}

// Options controls tokenization and rendering. Theme accepts a name or *Theme.
// Nil optional fields select Shiki defaults; false disables boolean-or-string options.
type Options struct {
	Lang              string         `json:"lang,omitempty"`
	Theme             any            `json:"theme,omitempty"`
	Themes            map[string]any `json:"themes,omitempty"`
	ThemeOrder        []string       `json:"themeOrder,omitempty"`
	DefaultColor      any            `json:"defaultColor,omitempty"`
	CSSVariablePrefix string         `json:"cssVariablePrefix,omitempty"`
	ColorsRendering   string         `json:"colorsRendering,omitempty"`
	// ANSIColorLevel selects no color (0), 16 colors (1), 256 colors (2),
	// or true color (3). Nil uses true color for deterministic library output.
	ANSIColorLevel        *int           `json:"ansiColorLevel,omitempty"`
	IncludeExplanation    any            `json:"includeExplanation,omitempty"`
	ColorReplacements     map[string]any `json:"colorReplacements,omitempty"`
	ColorReplacementOrder []string       `json:"colorReplacementOrder,omitempty"`
	TokenizeMaxLineLength int            `json:"tokenizeMaxLineLength,omitempty"`
	// A nil duration uses 500ms per line. A pointer to zero disables the limit.
	TokenizeTimeLimit    *time.Duration `json:"-"`
	GrammarState         *GrammarState  `json:"-"`
	GrammarContextCode   string         `json:"grammarContextCode,omitempty"`
	RootStyle            any            `json:"rootStyle,omitempty"`
	MergeWhitespaces     any            `json:"mergeWhitespaces,omitempty"`
	MergeSameStyleTokens bool           `json:"mergeSameStyleTokens,omitempty"`
	Structure            string         `json:"structure,omitempty"`
	TabIndex             any            `json:"tabindex,omitempty"`
	Meta                 map[string]any `json:"meta,omitempty"`
	Data                 map[string]any `json:"data,omitempty"`
	Transformers         []Transformer  `json:"-"`
	Decorations          []Decoration   `json:"decorations,omitempty"`
	metaOrder            []string
}

func objectKeys(data []byte) []string {
	d := json.NewDecoder(bytes.NewReader(data))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return nil
	}
	keys := []string{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return nil
		}
		var v json.RawMessage
		if d.Decode(&v) != nil {
			return nil
		}
		if s, ok := key.(string); ok {
			keys = append(keys, s)
		}
	}
	return keys
}
func (o *Options) UnmarshalJSON(data []byte) error {
	type alias Options
	raw := struct {
		*alias
		TokenizeTimeLimit *float64 `json:"tokenizeTimeLimit"`
	}{alias: (*alias)(o)}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.TokenizeTimeLimit != nil {
		duration := time.Duration(*raw.TokenizeTimeLimit * float64(time.Millisecond))
		o.TokenizeTimeLimit = &duration
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if len(o.ThemeOrder) == 0 {
		o.ThemeOrder = objectKeys(fields["themes"])
	}
	o.metaOrder = objectKeys(fields["meta"])
	if len(o.ColorReplacementOrder) == 0 {
		o.ColorReplacementOrder = objectKeys(fields["colorReplacements"])
	}
	return nil
}

type ScopeExplanation struct {
	ScopeName    string          `json:"scopeName"`
	ThemeMatches *[]ThemeSetting `json:"themeMatches,omitempty"`
}
type TokenExplanation struct {
	Content string             `json:"content"`
	Scopes  []ScopeExplanation `json:"scopes"`
}
type TokenStyle struct {
	Color     string            `json:"color,omitempty"`
	BGColor   string            `json:"bgColor,omitempty"`
	FontStyle FontStyle         `json:"fontStyle,omitempty"`
	HTMLStyle Style             `json:"htmlStyle,omitempty"`
	HTMLAttrs map[string]string `json:"htmlAttrs,omitempty"`
	hasFont   bool
	attrOrder []string
}
type Token struct {
	Content string `json:"content"`
	Offset  int    `json:"offset"`
	TokenStyle
	Type        *int                  `json:"type,omitempty"`
	Explanation []TokenExplanation    `json:"explanation,omitempty"`
	Variants    map[string]TokenStyle `json:"variants,omitempty"`
	styled      bool
}

func (t Token) MarshalJSON() ([]byte, error) {
	type alias Token
	if t.Variants != nil {
		variants := map[string]map[string]any{}
		for name, v := range t.Variants {
			b, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			m := map[string]any{}
			if err = json.Unmarshal(b, &m); err != nil {
				return nil, err
			}
			if v.hasFont {
				m["fontStyle"] = v.FontStyle
			}
			variants[name] = m
		}
		return json.Marshal(struct {
			alias
			Variants map[string]map[string]any `json:"variants"`
		}{alias(t), variants})
	}
	if t.styled {
		return json.Marshal(struct {
			alias
			FontStyle FontStyle `json:"fontStyle"`
			Color     string    `json:"color"`
		}{alias(t), t.FontStyle, t.Color})
	}
	if t.hasFont {
		return json.Marshal(struct {
			alias
			FontStyle FontStyle `json:"fontStyle"`
		}{alias(t), t.FontStyle})
	}
	return json.Marshal(alias(t))
}

func (t *Token) UnmarshalJSON(data []byte) error {
	type alias Token
	if err := json.Unmarshal(data, (*alias)(t)); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	t.hasFont = len(fields["fontStyle"]) > 0
	t.styled = t.hasFont && len(fields["color"]) > 0
	t.attrOrder = objectKeys(fields["htmlAttrs"])
	if len(fields["variants"]) > 0 {
		var variants map[string]map[string]json.RawMessage
		if err := json.Unmarshal(fields["variants"], &variants); err != nil {
			return err
		}
		for key, fields := range variants {
			style := t.Variants[key]
			style.hasFont = len(fields["fontStyle"]) > 0
			style.attrOrder = objectKeys(fields["htmlAttrs"])
			t.Variants[key] = style
		}
	}
	return nil
}

type TokensResult struct {
	Tokens       [][]Token     `json:"tokens"`
	FG           string        `json:"fg"`
	BG           string        `json:"bg"`
	ThemeName    string        `json:"themeName,omitempty"`
	RootStyle    string        `json:"rootStyle,omitempty"`
	GrammarState *GrammarState `json:"grammarState,omitempty"`
}

// GrammarState is an immutable snapshot reusable across highlight calls.
type GrammarState struct {
	Lang    string
	Themes  []string
	state   *textmate.State
	owner   *textmate.Tokenizer
	initial bool
}

func InitialGrammarState(lang string, themes ...string) *GrammarState {
	return &GrammarState{Lang: lang, Themes: append([]string(nil), themes...), initial: true}
}

func (s *GrammarState) GetScopes() []string {
	if s == nil || s.state == nil {
		return nil
	}
	return s.state.Scopes()
}

func (s *GrammarState) MarshalJSON() ([]byte, error) {
	theme := ""
	if len(s.Themes) > 0 {
		theme = s.Themes[0]
	}
	scopes := s.GetScopes()
	if scopes == nil {
		scopes = []string{}
	}
	return json.Marshal(struct {
		Lang   string   `json:"lang"`
		Theme  string   `json:"theme"`
		Themes []string `json:"themes"`
		Scopes []string `json:"scopes"`
	}{s.Lang, theme, s.Themes, scopes})
}

// Style preserves CSS property order for byte-for-byte HTML compatibility.
type Style []CSSProperty
type CSSProperty struct{ Name, Value string }

func (s Style) MarshalJSON() ([]byte, error) {
	if raw, ok := s.Raw(); ok {
		return json.Marshal(raw)
	}
	m := map[string]string{}
	for _, p := range s {
		m[p.Name] = p.Value
	}
	return json.Marshal(m)
}

// RawStyle preserves a literal CSS declaration string without reformatting it.
func RawStyle(css string) Style { return Style{{Name: "", Value: css}} }
func (s Style) Raw() (string, bool) {
	if len(s) == 1 && s[0].Name == "" {
		return s[0].Value, true
	}
	return "", false
}
func (s *Style) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = nil
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		*s = RawStyle(raw)
		return nil
	}
	var fields map[string]string
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*s = Style{}
	for _, key := range objectKeys(data) {
		s.Set(key, fields[key])
	}
	return nil
}
func (s *Style) Set(name, value string) {
	for i := range *s {
		if (*s)[i].Name == name {
			(*s)[i].Value = value
			return
		}
	}
	*s = append(*s, CSSProperty{name, value})
}
func (s Style) Get(name string) string {
	for _, p := range s {
		if p.Name == name {
			return p.Value
		}
	}
	return ""
}

// Node represents a HAST root, element, or text node.
type Node struct {
	Type          string         `json:"type"`
	TagName       string         `json:"tagName,omitempty"`
	Properties    map[string]any `json:"properties,omitempty"`
	Children      []*Node        `json:"children,omitempty"`
	Content       *Node          `json:"content,omitempty"`
	Value         string         `json:"value,omitempty"`
	Data          map[string]any `json:"data,omitempty"`
	GrammarState  *GrammarState  `json:"-"`
	propertyOrder []string
}
type TransformerContext struct {
	Highlighter     *Highlighter
	Options         *Options
	Source          string
	Tokens          [][]Token
	Root, Pre, Code *Node
	Lines           []*Node
	Meta            map[string]any
	Structure       string
}

func (ctx *TransformerContext) CodeToTokens(code string, options Options) (*TokensResult, error) {
	return ctx.Highlighter.CodeToTokens(code, options)
}
func (ctx *TransformerContext) CodeToHAST(code string, options Options) (*Node, error) {
	return ctx.Highlighter.CodeToHAST(code, options)
}
func (ctx *TransformerContext) AddClassToHAST(node *Node, classes ...string) *Node {
	return AddClassToHAST(node, classes...)
}

// Transformer hooks run in order. Returning nil preserves the current node/tokens.
type Transformer struct {
	Name        string
	Enforce     string
	Preprocess  func(*TransformerContext, string) (string, error)
	Tokens      func(*TransformerContext, [][]Token) ([][]Token, error)
	Span        func(*TransformerContext, *Node, int, int, *Node, Token) (*Node, error)
	Line        func(*TransformerContext, *Node, int) (*Node, error)
	Code        func(*TransformerContext, *Node) (*Node, error)
	Pre         func(*TransformerContext, *Node) (*Node, error)
	Root        func(*TransformerContext, *Node) (*Node, error)
	Postprocess func(*TransformerContext, string) (string, error)
}
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}
type Decoration struct {
	Start         any                       `json:"start"`
	End           any                       `json:"end"`
	TagName       string                    `json:"tagName,omitempty"`
	Properties    map[string]any            `json:"properties,omitempty"`
	AlwaysWrap    bool                      `json:"alwaysWrap,omitempty"`
	Transform     func(*Node, string) *Node `json:"-"`
	propertyOrder []string
}

func (d *Decoration) UnmarshalJSON(data []byte) error {
	type alias Decoration
	if err := json.Unmarshal(data, (*alias)(d)); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	d.propertyOrder = objectKeys(fields["properties"])
	return nil
}

//go:fix inline
func String(s string) *string { return new(s) }
