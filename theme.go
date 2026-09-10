package shiki

import (
	"encoding/json"
	"fmt"
	"github.com/ralscha/shiki-go/internal/textmate"
	"regexp"
	"sort"
	"strings"
)

// NormalizeTheme clones a theme and fills Shiki's default colors and settings.
func NormalizeTheme(raw *Theme) *Theme {
	if raw == nil {
		return nil
	}
	b, _ := json.Marshal(raw)
	var t Theme
	_ = json.Unmarshal(b, &t)
	if raw.colorsOrder != nil {
		t.colorsOrder = append([]string(nil), raw.colorsOrder...)
	}
	if t.Settings == nil && t.TokenColors != nil {
		t.Settings = t.TokenColors
		t.TokenColors = nil
	}
	if t.Type == "" {
		t.Type = "dark"
	}
	if t.ColorReplacements == nil {
		t.ColorReplacements = map[string]string{}
	}
	fg, bg := t.FG, t.BG
	if fg == "" || bg == "" {
		for _, s := range t.Settings {
			if s.Name == "" && (s.Scope == nil || s.scopeString && len(s.Scope) == 1 && s.Scope[0] == "") {
				if s.Settings.Foreground != "" {
					fg = s.Settings.Foreground
				}
				if s.Settings.Background != "" {
					bg = s.Settings.Background
				}
				break
			}
		}
		if fg == "" {
			fg, _ = t.Colors["editor.foreground"].(string)
		}
		if bg == "" {
			bg, _ = t.Colors["editor.background"].(string)
		}
		if fg == "" {
			if t.Type == "light" {
				fg = "#333333"
			} else {
				fg = "#bbbbbb"
			}
		}
		if bg == "" {
			if t.Type == "light" {
				bg = "#fffffe"
			} else {
				bg = "#1e1e1e"
			}
		}
		t.FG, t.BG = fg, bg
	}
	firstIsGlobal := len(t.Settings) > 0 && (t.Settings[0].Scope == nil || t.Settings[0].scopeString && len(t.Settings[0].Scope) == 1 && t.Settings[0].Scope[0] == "")
	if !firstIsGlobal {
		t.Settings = append([]ThemeSetting{{Settings: ThemeStyle{Foreground: t.FG, Background: t.BG}}}, t.Settings...)
	}
	replacementMap := map[string]string{}
	count := 0
	replace := func(value string) string {
		if hex, ok := replacementMap[value]; ok {
			return hex
		}
		var hex string
		for {
			count++
			hex = fmt.Sprintf("#%08x", count)
			if _, exists := t.ColorReplacements[hex]; !exists {
				break
			}
		}
		replacementMap[value] = hex
		t.ColorReplacements[hex] = value
		return hex
	}
	for i := range t.Settings {
		s := &t.Settings[i].Settings
		if s.Foreground != "" && !strings.HasPrefix(s.Foreground, "#") {
			s.Foreground = replace(s.Foreground)
		}
		if s.Background != "" && !strings.HasPrefix(s.Background, "#") {
			s.Background = replace(s.Background)
		}
	}
	for _, key := range t.colorsOrder {
		if key == "editor.foreground" || key == "editor.background" || strings.HasPrefix(key, "terminal.ansi") {
			if value, ok := t.Colors[key].(string); ok && !strings.HasPrefix(value, "#") {
				t.Colors[key] = replace(value)
			}
		}
	}
	return &t
}

type themeRule struct {
	depth   int
	parents []string
	font    FontStyle
	fg, bg  string
}

func (r *themeRule) overwrite(other themeRule) {
	r.depth = max(r.depth, other.depth)
	if other.font != -1 {
		r.font = other.font
	}
	if other.fg != "" {
		r.fg = other.fg
	}
	if other.bg != "" {
		r.bg = other.bg
	}
}

type themeTrie struct {
	main     themeRule
	parents  []themeRule
	children map[string]*themeTrie
}

func (n *themeTrie) insert(scope string, r themeRule) {
	if scope != "" {
		head, tail, _ := strings.Cut(scope, ".")
		if n.children == nil {
			n.children = map[string]*themeTrie{}
		}
		child := n.children[head]
		if child == nil {
			child = &themeTrie{main: n.main, parents: append([]themeRule(nil), n.parents...)}
			n.children[head] = child
		}
		child.insert(tail, r)
		return
	}
	if len(r.parents) == 0 {
		n.main.overwrite(r)
		return
	}
	for i := range n.parents {
		if strings.Join(n.parents[i].parents, " ") == strings.Join(r.parents, " ") {
			n.parents[i].overwrite(r)
			return
		}
	}
	if r.font == -1 {
		r.font = n.main.font
	}
	if r.fg == "" {
		r.fg = n.main.fg
	}
	if r.bg == "" {
		r.bg = n.main.bg
	}
	n.parents = append(n.parents, r)
}
func (n *themeTrie) match(scope string) []themeRule {
	if scope != "" {
		head, tail, _ := strings.Cut(scope, ".")
		if child := n.children[head]; child != nil {
			return child.match(tail)
		}
	}
	rules := append(append([]themeRule(nil), n.parents...), n.main)
	sort.SliceStable(rules, func(i, j int) bool {
		a, b := rules[i], rules[j]
		if a.depth != b.depth {
			return a.depth > b.depth
		}
		x, y := 0, 0
		for x < len(a.parents) && y < len(b.parents) {
			if a.parents[x] == ">" {
				x++
			}
			if b.parents[y] == ">" {
				y++
			}
			if x >= len(a.parents) || y >= len(b.parents) {
				break
			}
			if len(a.parents[x]) != len(b.parents[y]) {
				return len(a.parents[x]) > len(b.parents[y])
			}
			x++
			y++
		}
		return len(a.parents) > len(b.parents)
	})
	return rules
}

type compiledTheme struct {
	theme    *Theme
	root     *themeTrie
	defaults themeRule
	cache    map[string]themeRule
}

func parseFontStyle(s *string) FontStyle {
	if s == nil {
		return -1
	}
	var f FontStyle
	for v := range strings.FieldsSeq(*s) {
		switch v {
		case "italic":
			f |= 1
		case "bold":
			f |= 2
		case "underline":
			f |= 4
		case "strikethrough":
			f |= 8
		}
	}
	return f
}

var validThemeColor = regexp.MustCompile(`(?i)^#(?:[\da-f]{3}|[\da-f]{4}|[\da-f]{6}|[\da-f]{8})$`)

func themeColor(s string) string {
	if !validThemeColor.MatchString(s) {
		return ""
	}
	return strings.ToUpper(s)
}
func compileTheme(raw *Theme) *compiledTheme {
	theme := NormalizeTheme(raw)
	t := &compiledTheme{theme: theme, root: &themeTrie{main: themeRule{font: -1}}, defaults: themeRule{fg: "#000000", bg: "#FFFFFF"}, cache: map[string]themeRule{}}
	type parsed struct {
		scope string
		rule  themeRule
		index int
	}
	var rules []parsed
	for i, s := range theme.Settings {
		scopes := s.Scope
		if scopes == nil {
			scopes = []string{""}
		}
		for _, scope := range scopes {
			parts := strings.Split(strings.TrimSpace(scope), " ")
			scope = parts[len(parts)-1]
			parents := append([]string(nil), parts[:len(parts)-1]...)
			for i, j := 0, len(parents)-1; i < j; i, j = i+1, j-1 {
				parents[i], parents[j] = parents[j], parents[i]
			}
			depth := 0
			if scope != "" {
				depth = strings.Count(scope, ".") + 1
			}
			rules = append(rules, parsed{scope, themeRule{depth, parents, parseFontStyle(s.Settings.FontStyle), themeColor(s.Settings.Foreground), themeColor(s.Settings.Background)}, i})
		}
	}
	sort.SliceStable(rules, func(i, j int) bool {
		a, b := rules[i], rules[j]
		if a.scope != b.scope {
			return a.scope < b.scope
		}
		ap, bp := a.rule.parents, b.rule.parents
		if len(ap) != len(bp) {
			return len(ap) < len(bp)
		}
		for n := range ap {
			if ap[n] != bp[n] {
				return ap[n] < bp[n]
			}
		}
		return a.index < b.index
	})
	for _, p := range rules {
		if p.scope == "" {
			t.defaults.overwrite(p.rule)
		} else {
			t.root.insert(p.scope, p.rule)
		}
	}
	return t
}
func parentsMatch(scopes, parents []string) bool {
	j := len(scopes) - 1
	for i := 0; i < len(parents); i++ {
		p := parents[i]
		must := false
		if p == ">" {
			i++
			if i >= len(parents) {
				return false
			}
			p = parents[i]
			must = true
		}
		for j >= 0 && !textmate.ScopeMatches(scopes[j], p) {
			if must {
				return false
			}
			j--
		}
		if j < 0 {
			return false
		}
		j--
	}
	return true
}
func (t *compiledTheme) style(scopes []string) themeRule {
	key := strings.Join(scopes, " ")
	if s, ok := t.cache[key]; ok {
		return s
	}
	style := t.defaults
	for i, scope := range scopes {
		for _, r := range t.root.match(scope) {
			if parentsMatch(scopes[:i], r.parents) {
				style.overwrite(r)
				break
			}
		}
	}
	t.cache[key] = style
	return style
}
func (t *compiledTheme) explain(scopes []string, scopeOnly bool) []ScopeExplanation {
	result := make([]ScopeExplanation, len(scopes))
	for i, scope := range scopes {
		result[i].ScopeName = scope
		if scopeOnly {
			continue
		}
		matches := []ThemeSetting{}
		for _, setting := range t.theme.Settings {
			for _, selector := range setting.Scope {
				parts := strings.Fields(selector)
				if len(parts) == 0 || !textmate.ScopeMatches(scope, parts[len(parts)-1]) {
					continue
				}
				parents := append([]string(nil), parts[:len(parts)-1]...)
				for a, b := 0, len(parents)-1; a < b; a, b = a+1, b-1 {
					parents[a], parents[b] = parents[b], parents[a]
				}
				if parentsMatch(scopes[:i], parents) {
					matches = append(matches, setting)
					break
				}
			}
		}
		result[i].ThemeMatches = &matches
	}
	return result
}
