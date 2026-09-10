package transformers

import (
	"encoding/json"
	"maps"
	"regexp"
	shiki "shiki-go"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"
)

type BracketPair struct {
	Opener          string   `json:"opener"`
	Closer          string   `json:"closer"`
	ScopesAllowList []string `json:"scopesAllowList,omitempty"`
	ScopesDenyList  []string `json:"scopesDenyList,omitempty"`
}
type BracketLanguageOptions struct {
	Themes       map[string][]string `json:"themes,omitempty"`
	BracketPairs []BracketPair       `json:"bracketPairs,omitempty"`
}
type ColorizedBracketsOptions struct {
	Themes          map[string][]string               `json:"themes,omitempty"`
	BracketPairs    []BracketPair                     `json:"bracketPairs,omitempty"`
	Langs           map[string]BracketLanguageOptions `json:"langs,omitempty"`
	ExplicitTrigger bool                              `json:"explicitTrigger,omitempty"`
}

var bracketTrigger = regexp.MustCompile(`(^|\s)colorize-brackets($|\s)`)
var bracketSource = regexp.MustCompile(`^source.\w+$`)
var bracketPaletteCache sync.Map

func bracketLanguage(t shiki.Token, fallback string) string {
	if len(t.Explanation) > 0 {
		scopes := t.Explanation[0].Scopes
		for _, scope := range slices.Backward(scopes) {
			if bracketSource.MatchString(scope.ScopeName) {
				return strings.Split(scope.ScopeName, ".")[1]
			}
		}
	}
	return fallback
}
func ignoreBracket(t shiki.Token, allow, deny []string) bool {
	if len(t.Explanation) == 0 {
		return true
	}
	comment, literal, embedded := -1, -1, -1
	for i, s := range t.Explanation[0].Scopes {
		n := s.ScopeName
		if strings.HasPrefix(n, "comment.") {
			comment = i
		}
		if strings.HasPrefix(n, "string.") {
			literal = i
		}
		if strings.HasPrefix(n, "meta.embedded.") || strings.HasPrefix(n, "scope.embedded.") || n == "entity.name.type.instance.jsdoc" || n == "variable.other.jsdoc" || n == "meta.object.liquid" {
			embedded = i
		}
	}
	if comment > embedded || literal > embedded {
		return true
	}
	matches := func(list []string) bool {
		for _, exp := range t.Explanation {
			for _, s := range exp.Scopes {
				for _, pattern := range list {
					if s.ScopeName == pattern || strings.HasPrefix(s.ScopeName, pattern+".") {
						return true
					}
				}
			}
		}
		return false
	}
	return len(allow) > 0 && !matches(allow) || len(deny) > 0 && matches(deny)
}
func splitBracketToken(t shiki.Token, pairs []BracketPair) []shiki.Token {
	if len(pairs) == 0 || ignoreBracket(t, nil, nil) {
		return []shiki.Token{t}
	}
	var values []string
	for _, p := range pairs {
		if p.Opener != "" {
			values = append(values, p.Opener)
		}
		if p.Closer != "" {
			values = append(values, p.Closer)
		}
	}
	sort.SliceStable(values, func(i, j int) bool { return shiki.UTF16Len(values[i]) > shiki.UTF16Len(values[j]) })
	var offsets []int
	position := 0
	for position < len(t.Content) {
		index, length := -1, 0
		for _, v := range values {
			if i := strings.Index(t.Content[position:], v); i >= 0 && (index < 0 || i < index) {
				index, length = i, len(v)
			}
		}
		if index < 0 {
			break
		}
		start := position + index
		offsets = append(offsets, shiki.UTF16Len(t.Content[:start]), shiki.UTF16Len(t.Content[:start+length]))
		position = start + length
	}
	out := shiki.SplitToken(t, offsets)
	if len(out) == 0 {
		return []shiki.Token{t}
	}
	type interval struct{ start, end int }
	ranges := make([]interval, len(t.Explanation))
	start := 0
	for i, exp := range t.Explanation {
		length := shiki.UTF16Len(exp.Content)
		if len(t.Explanation) == 1 {
			length = shiki.UTF16Len(t.Content)
		} else if i == 0 {
			length = shiki.UTF16Len(t.Content) - shiki.UTF16Len(strings.TrimLeftFunc(t.Content, unicode.IsSpace)) + shiki.UTF16Len(strings.TrimLeftFunc(exp.Content, unicode.IsSpace))
		} else if i == len(t.Explanation)-1 {
			length = shiki.UTF16Len(strings.TrimRightFunc(exp.Content, unicode.IsSpace)) + shiki.UTF16Len(t.Content) - shiki.UTF16Len(strings.TrimRightFunc(t.Content, unicode.IsSpace))
		}
		ranges[i] = interval{start, start + length - 1}
		start += length
	}
	for i := range out {
		start := out[i].Offset - t.Offset
		end := start + shiki.UTF16Len(out[i].Content) - 1
		out[i].Explanation = []shiki.TokenExplanation{}
		for _, r := range ranges {
			if start <= r.end && end >= r.start {
				// Preserve the reference transformer's explanation projection.
				out[i].Explanation = append(out[i].Explanation, t.Explanation[len(out[i].Explanation)])
			}
		}
	}
	return out
}
func bracketThemeName(v any) string {
	if name, ok := v.(string); ok {
		return name
	}
	if theme, ok := v.(*shiki.Theme); ok && theme != nil {
		return theme.Name
	}
	b, _ := json.Marshal(v)
	var t struct{ Name string }
	_ = json.Unmarshal(b, &t)
	return t.Name
}
func bracketThemeColors(name string) []string {
	if c, ok := bracketPaletteCache.Load(name); ok {
		return c.([]string)
	}
	var selected string
	for _, info := range shiki.BundledThemes() {
		if strings.HasPrefix(name, info.ID) && info.ID > selected {
			selected = info.ID
		}
	}
	colors := []string{"#FFD700", "#DA70D6", "#179FFF", "rgba(255, 18, 18, 0.8)"}
	if theme, err := shiki.BundledTheme(selected); err == nil {
		if theme.Type == "light" {
			colors = []string{"#0431FA", "#319331", "#7B3814", colors[3]}
		}
		if strings.Contains(selected, "high-contrast") {
			if theme.Type == "light" {
				colors[3] = "#B5200D"
			} else {
				colors[2], colors[3] = "#87CEFA", "rgba(255, 50, 50, 1)"
			}
		}
		base := colors
		colors = []string{}
		for i := range 6 {
			value, _ := theme.Colors["editorBracketHighlight.foreground"+string(rune('1'+i))].(string)
			if value == "" && i < 3 {
				value = base[i]
			}
			if value != "" {
				colors = append(colors, value)
			}
		}
		unexpected, _ := theme.Colors["editorBracketHighlight.unexpectedBracket.foreground"].(string)
		if unexpected == "" {
			unexpected = base[3]
		}
		colors = append(colors, unexpected)
	}
	bracketPaletteCache.Store(name, colors)
	return colors
}
func bracketColor(themes map[string][]string, name string, level int) string {
	colors := themes[name]
	if colors == nil {
		key := ""
		for k := range themes {
			if strings.HasPrefix(name, k) && k > key {
				key = k
			}
		}
		colors = themes[key]
	}
	if len(colors) == 0 {
		colors = bracketThemeColors(name)
	}
	if level < 0 || len(colors) == 1 {
		return colors[len(colors)-1]
	}
	return colors[level%(len(colors)-1)]
}
func assignBracketColor(t *shiki.Token, themes map[string][]string, o *shiki.Options, level int) {
	if o.Theme != nil {
		t.Color = bracketColor(themes, bracketThemeName(o.Theme), level)
		return
	}
	styles := append(shiki.Style{}, t.HTMLStyle...)
	if _, ok := t.HTMLStyle.Raw(); ok {
		styles = shiki.Style{}
	}
	defaultColor := o.DefaultColor
	if defaultColor == nil {
		defaultColor = "light"
	}
	prefix := o.CSSVariablePrefix
	if prefix == "" {
		prefix = "--shiki-"
	}
	keys := append([]string(nil), o.ThemeOrder...)
	var rest []string
	for key := range o.Themes {
		found := false
		for _, k := range keys {
			if key == k {
				found = true
			}
		}
		if !found {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	keys = append(keys, rest...)
	for _, key := range keys {
		prop := prefix + key
		if key == defaultColor {
			prop = "color"
		}
		styles.Set(prop, bracketColor(themes, bracketThemeName(o.Themes[key]), level))
	}
	if defaultColor == "light-dark()" {
		styles.Set("color", "light-dark("+styles.Get(prefix+"light")+","+styles.Get(prefix+"dark")+")")
	}
	t.HTMLStyle = styles
}

// ColorizedBrackets colors matching brackets by nesting depth, respecting string,
// comment, and embedded-language scopes. The final palette entry marks errors.
func ColorizedBrackets(options ...ColorizedBracketsOptions) shiki.Transformer {
	var o ColorizedBracketsOptions
	if len(options) > 0 {
		o = options[0]
	}
	pairs := o.BracketPairs
	if pairs == nil {
		pairs = []BracketPair{{Opener: "[", Closer: "]"}, {Opener: "{", Closer: "}"}, {Opener: "(", Closer: ")"}, {Opener: "<", Closer: ">", ScopesAllowList: []string{"punctuation.definition.typeparameters.begin.ts", "punctuation.definition.typeparameters.end.ts", "entity.name.type.instance.jsdoc"}}}
	}
	jinja := []BracketPair{{Opener: "[", Closer: "]"}, {Opener: "{", Closer: "}"}, {Opener: "(", Closer: ")"}, {Opener: "{{", Closer: "}}"}, {Opener: "{%", Closer: "%}"}}
	langs := map[string]BracketLanguageOptions{"html": {BracketPairs: []BracketPair{}}, "jinja": {BracketPairs: jinja}, "liquid": {BracketPairs: jinja}}
	maps.Copy(langs, o.Langs)
	config := func(lang string) BracketLanguageOptions {
		c := BracketLanguageOptions{Themes: o.Themes, BracketPairs: pairs}
		if v, ok := langs[lang]; ok {
			if v.Themes != nil {
				c.Themes = v.Themes
			}
			if v.BracketPairs != nil {
				c.BracketPairs = v.BracketPairs
			}
		}
		return c
	}
	enabled := func(ctx *shiki.TransformerContext) bool {
		raw, _ := ctx.Options.Meta["__raw"].(string)
		return !o.ExplicitTrigger || bracketTrigger.MatchString(raw)
	}
	return shiki.Transformer{Name: "colorizedBrackets", Preprocess: func(ctx *shiki.TransformerContext, code string) (string, error) {
		if enabled(ctx) && (ctx.Options.IncludeExplanation == nil || ctx.Options.IncludeExplanation == false || ctx.Options.IncludeExplanation == "") {
			ctx.Options.IncludeExplanation = "scopeName"
		}
		return code, nil
	}, Tokens: func(ctx *shiki.TransformerContext, tokens [][]shiki.Token) ([][]shiki.Token, error) {
		if !enabled(ctx) {
			return nil, nil
		}
		for i, line := range tokens {
			var out []shiki.Token
			for _, t := range line {
				out = append(out, splitBracketToken(t, config(bracketLanguage(t, ctx.Options.Lang)).BracketPairs)...)
			}
			tokens[i] = out
		}
		var stack []*shiki.Token
		for i := range tokens {
			for j := range tokens[i] {
				t := &tokens[i][j]
				c := config(bracketLanguage(*t, ctx.Options.Lang))
				content := strings.TrimSpace(t.Content)
				var pair *BracketPair
				for k := range c.BracketPairs {
					p := &c.BracketPairs[k]
					if p.Opener == content || p.Closer == content {
						pair = p
						break
					}
				}
				if pair == nil || ignoreBracket(*t, pair.ScopesAllowList, pair.ScopesDenyList) {
					continue
				}
				if content == pair.Opener {
					stack = append(stack, t)
					continue
				}
				index := len(stack) - 1
				for index >= 0 && strings.TrimSpace(stack[index].Content) != pair.Opener {
					index--
				}
				if index < 0 {
					assignBracketColor(t, c.Themes, ctx.Options, -1)
					continue
				}
				for k := len(stack) - 1; k > index; k-- {
					assignBracketColor(stack[k], c.Themes, ctx.Options, -1)
				}
				opener := stack[index]
				stack = stack[:index]
				assignBracketColor(t, c.Themes, ctx.Options, len(stack))
				assignBracketColor(opener, c.Themes, ctx.Options, len(stack))
			}
		}
		for _, t := range stack {
			assignBracketColor(t, config(ctx.Options.Lang).Themes, ctx.Options, -1)
		}
		return tokens, nil
	}}
}
