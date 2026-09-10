package shiki

import (
	"regexp"
	"strings"
)

var languageAttribute = regexp.MustCompile(`:?lang=["']([^"']+)["']`)
var codeFence = regexp.MustCompile("(?:```|~~~)([\\w-]+)")
var latexBegin = regexp.MustCompile(`\\begin\{([\w-]+)\}`)
var scriptLanguage = regexp.MustCompile(`(?i)<script\s+(?:type|lang)=["']([^"']+)["']`)
var frontmatter = regexp.MustCompile(`(?s)^\s*---\r?\n.*?\r?\n---(?:\r?\n|\s*$)`)

// GuessEmbeddedLanguages finds language hints in fences, SFC tags, LaTeX
// environments and YAML frontmatter. knownOnly filters to the bundled catalog.
func GuessEmbeddedLanguages(code string, knownOnly bool) []string {
	langs := []string{}
	add := func(lang string) {
		lang = strings.ToLower(strings.TrimSpace(lang))
		if lang != "" && !hasString(langs, lang) {
			langs = append(langs, lang)
		}
	}
	for _, re := range []*regexp.Regexp{languageAttribute, codeFence, latexBegin} {
		for _, m := range re.FindAllStringSubmatch(code, -1) {
			add(m[1])
		}
	}
	for _, m := range scriptLanguage.FindAllStringSubmatch(code, -1) {
		s := strings.ToLower(strings.TrimSpace(m[1]))
		if i := strings.LastIndexByte(s, '/'); i >= 0 {
			s = s[i+1:]
		}
		add(s)
	}
	if frontmatter.MatchString(code) {
		add("yaml")
	}
	if !knownOnly {
		return langs
	}
	known := map[string]bool{}
	for _, info := range catalog.Languages {
		known[info.ID] = true
		for _, alias := range info.Aliases {
			known[alias] = true
		}
	}
	out := []string{}
	for _, lang := range langs {
		if known[lang] {
			out = append(out, lang)
		}
	}
	return out
}
func (h *Highlighter) GetBundledLanguages() []LanguageInfo { return BundledLanguages() }
func (h *Highlighter) GetBundledThemes() []ThemeInfo       { return BundledThemes() }
func GetLastGrammarState(code string, o Options) (*GrammarState, error) {
	h, err := shorthand(code, o)
	if err != nil {
		return nil, err
	}
	return h.GetLastGrammarState(code, o)
}
