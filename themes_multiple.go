package shiki

import (
	"fmt"
	"sort"
	"strings"
)

func GetTokenStyleObject(t TokenStyle) Style {
	var s Style
	if t.Color != "" {
		s.Set("color", t.Color)
	}
	if t.BGColor != "" {
		s.Set("background-color", t.BGColor)
	}
	if t.FontStyle&FontStyleItalic != 0 {
		s.Set("font-style", "italic")
	}
	if t.FontStyle&FontStyleBold != 0 {
		s.Set("font-weight", "bold")
	}
	var dec []string
	if t.FontStyle&FontStyleUnderline != 0 {
		dec = append(dec, "underline")
	}
	if t.FontStyle&FontStyleStrikethrough != 0 {
		dec = append(dec, "line-through")
	}
	if len(dec) > 0 {
		s.Set("text-decoration", strings.Join(dec, " "))
	}
	return s
}
func StringifyTokenStyle(s Style) string {
	if raw, ok := s.Raw(); ok {
		return raw
	}
	values := make([]string, len(s))
	for i, p := range s {
		values[i] = p.Name + ":" + p.Value
	}
	return strings.Join(values, ";")
}
func tokenStyle(t Token) Style {
	if t.HTMLStyle != nil {
		if raw, ok := t.HTMLStyle.Raw(); !ok || raw != "" {
			return t.HTMLStyle
		}
	}
	return GetTokenStyleObject(t.TokenStyle)
}
func themeKeys(o Options) []string {
	keys := []string{}
	for _, key := range o.ThemeOrder {
		if hastTruthy(o.Themes[key]) && !hasString(keys, key) {
			keys = append(keys, key)
		}
	}
	rest := []string{}
	for key, v := range o.Themes {
		if hastTruthy(v) && !hasString(keys, key) {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	// Conventional order mirrors object literals in Shiki examples.
	if len(o.ThemeOrder) == 0 {
		for _, key := range []string{"light", "dark"} {
			if hasString(rest, key) {
				keys = append(keys, key)
			}
		}
	}
	for _, key := range rest {
		if !hasString(keys, key) {
			keys = append(keys, key)
		}
	}
	return keys
}
func AlignThemesTokenization(themes ...[][]Token) [][][]Token {
	out := make([][][]Token, len(themes))
	if len(themes) == 0 {
		return out
	}
	var breaks []int
	for _, theme := range themes {
		for _, line := range theme {
			for _, token := range line {
				breaks = append(breaks, token.Offset, token.Offset+UTF16Len(token.Content))
			}
		}
	}
	for i, theme := range themes {
		out[i] = SplitTokens(theme, breaks)
	}
	return out
}
func (h *Highlighter) variants(code string, o Options) ([][]Token, *GrammarState, error) {
	keys := themeKeys(o)
	if len(keys) == 0 {
		return nil, nil, fmt.Errorf("shiki: themes must not be empty")
	}
	all := make([][][]Token, len(keys))
	var state *GrammarState
	names := []string{}
	for i, key := range keys {
		single := o
		single.Themes = nil
		single.Theme = o.Themes[key]
		tokens, s, err := h.base(code, single)
		if err != nil {
			return nil, nil, err
		}
		all[i] = tokens
		if s != nil {
			state = s
			for _, name := range s.Themes {
				if !hasString(names, name) {
					names = append(names, name)
				}
			}
		}
	}
	all = AlignThemesTokenization(all...)
	out := make([][]Token, len(all[0]))
	for i, line := range all[0] {
		out[i] = make([]Token, len(line))
		for j, token := range line {
			v := Token{Content: token.Content, Offset: token.Offset, Explanation: token.Explanation, Variants: map[string]TokenStyle{}}
			for k, key := range keys {
				style := all[k][i][j].TokenStyle
				style.hasFont = all[k][i][j].styled
				v.Variants[key] = style
			}
			out[i][j] = v
		}
	}
	if state != nil {
		state.Themes = names
	}
	return out, state, nil
}
func (h *Highlighter) CodeToTokensWithThemes(code string, o Options) ([][]Token, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.check(); err != nil {
		return nil, err
	}
	tokens, _, err := h.variants(code, o)
	return tokens, err
}
func CodeToTokensWithThemes(code string, o Options) ([][]Token, error) {
	h, err := shorthand(code, o)
	if err != nil {
		return nil, err
	}
	return h.CodeToTokensWithThemes(code, o)
}
func (h *Highlighter) multipleThemes(code string, o Options) (*TokensResult, error) {
	keys := themeKeys(o)
	if len(keys) == 0 {
		return nil, fmt.Errorf("shiki: themes must not be empty")
	}
	defaultColor := o.DefaultColor
	if defaultColor == nil {
		defaultColor = "light"
	}
	hasDefault := defaultColor != false && defaultColor != ""
	lightDark := defaultColor == "light-dark()"
	if name, ok := defaultColor.(string); ok && hasDefault && !lightDark {
		if !hasString(keys, name) {
			return nil, fmt.Errorf("shiki: themes must contain defaultColor %q", name)
		}
		sort.SliceStable(keys, func(i, j int) bool { return keys[i] == name && keys[j] != name })
	}
	if lightDark && len(keys) > 1 && (!hasString(keys, "light") || !hasString(keys, "dark")) {
		return nil, fmt.Errorf("shiki: light-dark() requires light and dark themes")
	}
	tokens, state, err := h.variants(code, o)
	if err != nil {
		return nil, err
	}
	prefix := o.CSSVariablePrefix
	if prefix == "" {
		prefix = "--shiki-"
	}
	cssVars := o.ColorsRendering != "none"
	for i, line := range tokens {
		for j, token := range line {
			styles := map[string]Style{}
			propertyNames := []string{}
			for _, key := range keys {
				styles[key] = GetTokenStyleObject(token.Variants[key])
				for _, p := range styles[key] {
					if !hasString(propertyNames, p.Name) {
						propertyNames = append(propertyNames, p.Name)
					}
				}
			}
			merged := Style{}
			for k, key := range keys {
				for _, prop := range propertyNames {
					value := styles[key].Get(prop)
					if value == "" {
						value = "inherit"
					}
					suffix := "-" + prop
					switch prop {
					case "color":
						suffix = ""
					case "background-color":
						suffix = "-bg"
					}
					variable := prefix + key + suffix
					if k == 0 && hasDefault && (prop == "color" || prop == "background-color") {
						if lightDark && len(keys) > 1 {
							light, dark := styles["light"].Get(prop), styles["dark"].Get(prop)
							if light == "" {
								light = "inherit"
							}
							if dark == "" {
								dark = "inherit"
							}
							merged.Set(prop, "light-dark("+light+", "+dark+")")
							if cssVars {
								merged.Set(variable, value)
							}
						} else {
							merged.Set(prop, value)
						}
					} else if cssVars {
						merged.Set(variable, value)
					}
				}
			}
			token.Variants = nil
			token.HTMLStyle = merged
			tokens[i][j] = token
		}
	}
	fgs, bgs := map[string]string{}, map[string]string{}
	names := []string{}
	for _, key := range keys {
		t, err := h.getTheme(o.Themes[key])
		if err != nil {
			return nil, err
		}
		r := rootColorReplacements(o.Themes[key], o)
		fgs[key] = applyReplacement(t.theme.FG, r)
		bgs[key] = applyReplacement(t.theme.BG, r)
		names = append(names, t.theme.Name)
	}
	mapColors := func(colors map[string]string, bg bool) string {
		parts := []string{}
		for i, key := range keys {
			value := colors[key]
			if value == "" {
				value = "inherit"
			}
			suffix := ""
			if bg {
				suffix = "-bg"
			}
			variable := prefix + key + suffix + ":" + value
			if i == 0 && hasDefault {
				if lightDark && len(keys) > 1 {
					light, dark := colors["light"], colors["dark"]
					if light == "" {
						light = "inherit"
					}
					if dark == "" {
						dark = "inherit"
					}
					parts = append(parts, "light-dark("+light+", "+dark+");"+variable)
				} else {
					parts = append(parts, value)
				}
			} else if cssVars {
				parts = append(parts, variable)
			}
		}
		return strings.Join(parts, ";")
	}
	fg, bg := mapColors(fgs, false), mapColors(bgs, true)
	rootStyle := ""
	if !hasDefault {
		rootStyle = fg + ";" + bg
	}
	return &TokensResult{Tokens: tokens, FG: fg, BG: bg, ThemeName: "shiki-themes " + strings.Join(names, " "), RootStyle: rootStyle, GrammarState: state}, nil
}
