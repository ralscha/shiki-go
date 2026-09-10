package shiki

import (
	"encoding/json"
	"fmt"
	"shiki-go/internal/textmate"
	"slices"
	"sort"
	"strings"
	"sync"
)

// Highlighter owns reusable grammar, theme, and regex caches. It is safe for
// concurrent calls. Close releases its WebAssembly memory and compiled scanners.
type Highlighter struct {
	mu                                sync.Mutex
	engine                            RegexEngine
	ownsEngine                        bool
	languages                         map[string]*Language
	scopes                            map[string]*Language
	themes                            map[string]*compiledTheme
	aliases                           map[string]string
	langOrder, themeOrder, aliasOrder []string
	tokenizers                        map[string]*textmate.Tokenizer
	activeGrammars                    map[string]bool
	ensuredScopes                     map[string]bool
	injectionScopes                   map[string][]string
	deferGrammarInit                  bool
	closed                            bool
}

func NewHighlighter(options HighlighterOptions) (*Highlighter, error) {
	engine := options.Engine
	if engine == nil {
		var err error
		engine, err = NewOnigurumaEngine()
		if err != nil {
			return nil, err
		}
	}
	h := &Highlighter{engine: engine, ownsEngine: options.Engine == nil, languages: map[string]*Language{}, scopes: map[string]*Language{}, themes: map[string]*compiledTheme{}, aliases: map[string]string{}, tokenizers: map[string]*textmate.Tokenizer{}}
	h.activeGrammars = map[string]bool{}
	h.ensuredScopes = map[string]bool{}
	h.injectionScopes = map[string][]string{}
	h.deferGrammarInit = true
	fail := func(err error) (*Highlighter, error) { _ = h.Close(); return nil, err }
	for _, name := range options.Themes {
		if err := h.LoadTheme(name); err != nil {
			return fail(err)
		}
	}
	for _, theme := range options.ThemeRegistrations {
		if err := h.LoadTheme(theme); err != nil {
			return fail(err)
		}
	}
	for _, name := range options.Langs {
		if err := h.LoadLanguage(name); err != nil {
			return fail(err)
		}
	}
	if err := h.LoadLanguageRegistrations(options.Languages...); err != nil {
		return fail(err)
	}
	aliasKeys := make([]string, 0, len(options.LangAlias))
	for alias := range options.LangAlias {
		aliasKeys = append(aliasKeys, alias)
	}
	sort.Strings(aliasKeys)
	for _, alias := range aliasKeys {
		h.setAlias(alias, options.LangAlias[alias])
	}
	for alias := range h.aliases {
		if _, err := h.resolveAlias(alias); err != nil {
			return fail(err)
		}
	}
	h.deferGrammarInit = false
	h.initializeLanguages(h.langOrder)
	return h, nil
}
func CreateHighlighter(options HighlighterOptions) (*Highlighter, error) {
	return NewHighlighter(options)
}
func (h *Highlighter) check() error {
	if h.closed {
		return fmt.Errorf("shiki: highlighter is disposed")
	}
	return nil
}
func (h *Highlighter) setAlias(alias, target string) {
	if _, ok := h.aliases[alias]; !ok {
		h.aliasOrder = append(h.aliasOrder, alias)
	}
	h.aliases[alias] = target
}
func (h *Highlighter) resolveAlias(name string) (string, error) {
	seen := map[string]bool{}
	for h.aliases[name] != "" {
		if seen[name] {
			return "", fmt.Errorf("shiki: circular language alias at %q", name)
		}
		seen[name] = true
		name = h.aliases[name]
	}
	return name, nil
}
func (h *Highlighter) ResolveLangAlias(name string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.check(); err != nil {
		return "", err
	}
	return h.resolveAlias(name)
}
func isPlainLang(name string) bool {
	return name == "" || name == "text" || name == "txt" || name == "plain" || name == "plaintext"
}
func isSpecialLang(name string) bool { return isPlainLang(name) || name == "ansi" }
func (h *Highlighter) invalidate() {
	for _, t := range h.tokenizers {
		t.Close()
	}
	h.tokenizers = map[string]*textmate.Tokenizer{}
}
func (h *Highlighter) register(l *Language) error {
	if l == nil || l.Name == "" || l.ScopeName == "" {
		return fmt.Errorf("shiki: a language requires name and scopeName")
	}
	if h.languages[l.Name] != nil {
		return nil
	}
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	var clone Language
	if err = json.Unmarshal(b, &clone); err != nil {
		return err
	}
	h.languages[l.Name] = &clone
	h.scopes[l.ScopeName] = &clone
	h.langOrder = append(h.langOrder, l.Name)
	for _, alias := range l.Aliases {
		h.setAlias(alias, l.Name)
	}
	return nil
}

// Shiki initializes grammar registrations before tokenization. Lazy dependency
// reloads clear TextMate's injection table while its ensured-scope cache remains.
// Keep that registration state separate from our lazily compiled scanners.
func (h *Highlighter) initializeLanguages(names []string) {
	if h.deferGrammarInit {
		return
	}
	visiting := map[string]bool{}
	var initialize func(string)
	initialize = func(name string) {
		if h.activeGrammars[name] || visiting[name] {
			return
		}
		visiting[name] = true
		defer delete(visiting, name)
		g := h.languages[name]
		h.activeGrammars[name] = true
		if !h.ensuredScopes[g.ScopeName] {
			h.ensuredScopes[g.ScopeName] = true
			var injections []string
			parts := strings.Split(g.ScopeName, ".")
			for i := 1; i <= len(parts); i++ {
				target := strings.Join(parts[:i], ".")
				for _, other := range h.langOrder {
					candidate := h.languages[other]
					if hasString(candidate.InjectTo, target) {
						injections = append(injections, candidate.ScopeName)
					}
				}
			}
			h.injectionScopes[g.ScopeName] = injections
		}
		for _, other := range h.langOrder {
			candidate := h.languages[other]
			if other != name && !visiting[other] && hasString(candidate.EmbeddedLangsLazy, name) {
				h.activeGrammars[other] = false
				delete(h.injectionScopes, candidate.ScopeName)
				if t := h.tokenizers[other]; t != nil {
					t.Close()
					delete(h.tokenizers, other)
				}
				initialize(other)
			}
		}
	}
	for _, name := range names {
		initialize(name)
	}
}
func (h *Highlighter) loadLanguage(name string, seen map[string]bool) error {
	if isSpecialLang(name) {
		return nil
	}
	resolved, err := h.resolveAlias(name)
	if err != nil {
		return err
	}
	name = resolved
	if h.languages[name] != nil || seen[name] {
		return nil
	}
	l, err := BundledLanguage(name)
	if err != nil {
		return err
	}
	seen[name] = true
	for _, dep := range append(append([]string(nil), l.EmbeddedLangs...), l.EmbeddedLanguages...) {
		if err := h.loadLanguage(dep, seen); err != nil {
			return err
		}
	}
	return h.register(l)
}
func (h *Highlighter) LoadLanguage(names ...string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.check(); err != nil {
		return err
	}
	start := len(h.langOrder)
	for _, name := range names {
		if err := h.loadLanguage(name, map[string]bool{}); err != nil {
			return err
		}
	}
	h.initializeLanguages(h.langOrder[start:])
	return nil
}
func (h *Highlighter) LoadLanguageRegistrations(languages ...*Language) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.check(); err != nil {
		return err
	}
	start := len(h.langOrder)
	available := map[string]bool{}
	for name := range h.languages {
		available[name] = true
	}
	for _, l := range languages {
		if l == nil || l.Name == "" || l.ScopeName == "" {
			return fmt.Errorf("shiki: a language requires name and scopeName")
		}
		available[l.Name] = true
	}
	for _, l := range languages {
		for _, dep := range append(append([]string(nil), l.EmbeddedLangs...), l.EmbeddedLanguages...) {
			if !available[dep] {
				return fmt.Errorf("shiki: missing language %q required by %q", dep, l.Name)
			}
		}
	}
	for _, l := range languages {
		if err := h.register(l); err != nil {
			return err
		}
	}
	h.initializeLanguages(h.langOrder[start:])
	return nil
}
func themeInput(value any) (*Theme, error) {
	switch v := value.(type) {
	case *Theme:
		if v == nil {
			return nil, fmt.Errorf("shiki: nil theme")
		}
		return v, nil
	case Theme:
		return &v, nil
	case map[string]any:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var theme Theme
		err = json.Unmarshal(b, &theme)
		return &theme, err
	default:
		return nil, fmt.Errorf("shiki: invalid theme input %T", value)
	}
}
func (h *Highlighter) loadTheme(value any) (*compiledTheme, error) {
	if name, ok := value.(string); ok {
		if name == "none" {
			return &compiledTheme{theme: &Theme{Name: "none", Type: "dark"}}, nil
		}
		if t := h.themes[name]; t != nil {
			return t, nil
		}
		raw, err := BundledTheme(name)
		if err != nil {
			return nil, err
		}
		value = raw
	}
	raw, err := themeInput(value)
	if err != nil {
		return nil, err
	}
	t := compileTheme(raw)
	if _, ok := h.themes[t.theme.Name]; !ok {
		h.themeOrder = append(h.themeOrder, t.theme.Name)
	}
	h.themes[t.theme.Name] = t
	return t, nil
}
func (h *Highlighter) LoadTheme(values ...any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.check(); err != nil {
		return err
	}
	for _, v := range values {
		if _, err := h.loadTheme(v); err != nil {
			return err
		}
	}
	return nil
}
func (h *Highlighter) getTheme(value any) (*compiledTheme, error) {
	if value == nil {
		if len(h.themeOrder) == 0 {
			return nil, fmt.Errorf("shiki: no theme loaded")
		}
		value = h.themeOrder[0]
	}
	if name, ok := value.(string); ok {
		if name == "none" {
			return &compiledTheme{theme: &Theme{Name: "none", Type: "dark"}}, nil
		}
		if t := h.themes[name]; t != nil {
			return t, nil
		}
		return nil, fmt.Errorf("shiki: theme %q is not loaded", name)
	}
	return h.loadTheme(value)
}
func (h *Highlighter) GetTheme(value any) (*Theme, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.check(); err != nil {
		return nil, err
	}
	t, err := h.getTheme(value)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(t.theme)
	var out Theme
	_ = json.Unmarshal(b, &out)
	return &out, nil
}
func (h *Highlighter) GetLanguage(name string) (*Language, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.check(); err != nil {
		return nil, err
	}
	name, err := h.resolveAlias(name)
	if err != nil {
		return nil, err
	}
	l := h.languages[name]
	if l == nil {
		return nil, fmt.Errorf("shiki: language %q is not loaded", name)
	}
	b, _ := json.Marshal(l)
	var out Language
	_ = json.Unmarshal(b, &out)
	return &out, nil
}
func (h *Highlighter) GetLoadedLanguages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := append([]string{}, h.langOrder...)
	for _, name := range h.aliasOrder {
		if !hasString(out, name) {
			out = append(out, name)
		}
	}
	return out
}
func (h *Highlighter) GetLoadedThemes() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string{}, h.themeOrder...)
}
func (h *Highlighter) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	h.invalidate()
	h.languages = nil
	h.scopes = nil
	h.themes = nil
	h.langOrder = nil
	h.aliasOrder = nil
	h.themeOrder = nil
	if h.ownsEngine {
		return h.engine.Close()
	}
	return nil
}
func (h *Highlighter) Dispose() error { return h.Close() }

func (h *Highlighter) IsClosed() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closed
}

var singleton struct {
	sync.Mutex
	h *Highlighter
}

func GetSingletonHighlighter(options HighlighterOptions) (*Highlighter, error) {
	singleton.Lock()
	defer singleton.Unlock()
	if singleton.h == nil || singleton.h.IsClosed() {
		h, err := NewHighlighter(options)
		if err != nil {
			return nil, err
		}
		singleton.h = h
		return h, nil
	}
	h := singleton.h
	if err := h.LoadLanguage(options.Langs...); err != nil {
		return nil, err
	}
	if err := h.LoadLanguageRegistrations(options.Languages...); err != nil {
		return nil, err
	}
	for _, t := range options.Themes {
		if err := h.LoadTheme(t); err != nil {
			return nil, err
		}
	}
	for _, t := range options.ThemeRegistrations {
		if err := h.LoadTheme(t); err != nil {
			return nil, err
		}
	}
	return h, nil
}
func DisposeSingleton() error {
	singleton.Lock()
	defer singleton.Unlock()
	if singleton.h == nil {
		return nil
	}
	err := singleton.h.Close()
	singleton.h = nil
	return err
}
func shorthand(code string, options Options) (*Highlighter, error) {
	themes := []string{}
	custom := []*Theme{}
	inputs := []any{}
	if options.Theme != nil {
		inputs = append(inputs, options.Theme)
	}
	for _, theme := range options.Themes {
		inputs = append(inputs, theme)
	}
	for _, v := range inputs {
		if name, ok := v.(string); ok {
			themes = append(themes, name)
		} else {
			t, err := themeInput(v)
			if err != nil {
				return nil, err
			}
			custom = append(custom, t)
		}
	}
	langs := append([]string{options.Lang}, GuessEmbeddedLanguages(code, true)...)
	return GetSingletonHighlighter(HighlighterOptions{Langs: langs, Themes: themes, ThemeRegistrations: custom})
}
func CodeToTokens(code string, options Options) (*TokensResult, error) {
	h, err := shorthand(code, options)
	if err != nil {
		return nil, err
	}
	return h.CodeToTokens(code, options)
}
func CodeToTokensBase(code string, options Options) ([][]Token, error) {
	h, err := shorthand(code, options)
	if err != nil {
		return nil, err
	}
	return h.CodeToTokensBase(code, options)
}
func hasString(values []string, value string) bool {
	return slices.Contains(values, value)
}
func applyReplacement(color string, replacements map[string]string) string {
	if v := replacements[strings.ToLower(color)]; v != "" {
		return v
	}
	return color
}
