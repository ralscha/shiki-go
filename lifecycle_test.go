package shiki

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type testCloser interface{ Close() error }

func closeOnCleanup(t testing.TB, closer testCloser) {
	t.Helper()
	t.Cleanup(func() {
		if err := closer.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
}

func TestGrammarStateLifecycle(t *testing.T) {
	h, err := NewHighlighter(HighlighterOptions{Langs: []string{"js"}, Themes: []string{"github-dark", "github-light"}, LangAlias: map[string]string{"custom-js": "js"}})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, h)
	o := Options{Lang: "custom-js", Theme: "github-dark"}
	state, err := h.GetLastGrammarState("/* open", o)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.LoadLanguage("ruby"); err != nil {
		t.Fatal(err)
	}
	o.GrammarState = state
	got, err := h.CodeToTokens("comment */ const x = 1", o)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tokens[0][0].Color != "#6A737D" {
		t.Fatal(got.Tokens)
	}
	other, err := NewHighlighter(HighlighterOptions{Langs: []string{"js"}, Themes: []string{"github-dark"}})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, other)
	if _, err := other.CodeToTokens("comment", Options{Lang: "js", Theme: "github-dark", GrammarState: state}); err == nil {
		t.Fatal("state from another highlighter accepted")
	}
	if _, err := h.CodeToTokens("comment", Options{Lang: "js", Theme: "github-light", GrammarState: state}); err == nil {
		t.Fatal("state with missing theme accepted")
	}
	if _, err := h.CodeToTokens("comment", Options{Lang: "ruby", Theme: "github-dark", GrammarState: state}); err == nil {
		t.Fatal("state for another language accepted")
	}
	initial := InitialGrammarState("javascript", "github-dark")
	if scopes := initial.GetScopes(); scopes != nil {
		t.Fatalf("initial grammar state has scopes: %v", scopes)
	}
	encoded, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"scopes":[]`) {
		t.Fatalf("initial grammar state JSON has unexpected scopes: %s", encoded)
	}
	if _, err := h.CodeToTokens("const x = 1", Options{Lang: "js", Theme: "github-dark", GrammarState: initial}); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.CodeToHTML("code", o); err == nil {
		t.Fatal("disposed highlighter accepted highlighting")
	}
	if err := h.LoadLanguage("go"); err == nil {
		t.Fatal("disposed highlighter accepted loading")
	}
}

func TestNormalizeThemePreservesExistingColorReplacements(t *testing.T) {
	theme := NormalizeTheme(&Theme{
		Name:              "custom",
		ColorReplacements: map[string]string{"#00000001": "existing"},
		Settings: []ThemeSetting{{
			Scope:    StringList{"keyword"},
			Settings: ThemeStyle{Foreground: "oklch(50% 0.1 120)"},
		}},
	})
	if got := theme.ColorReplacements["#00000001"]; got != "existing" {
		t.Fatalf("existing replacement was overwritten: %q", got)
	}
	if got := theme.Settings[1].Settings.Foreground; got != "#00000002" {
		t.Fatalf("non-hex color placeholder = %q, want #00000002", got)
	}
	if got := theme.ColorReplacements["#00000002"]; got != "oklch(50% 0.1 120)" {
		t.Fatalf("replacement for generated placeholder = %q", got)
	}
}

func TestRegistrationIsolationAndErrors(t *testing.T) {
	var grammar Language
	if err := json.Unmarshal([]byte(`{"name":"custom","scopeName":"source.custom","patterns":[{"match":"hello","name":"keyword.control"}]}`), &grammar); err != nil {
		t.Fatal(err)
	}
	theme := &Theme{Name: "custom", Settings: []ThemeSetting{{Settings: ThemeStyle{Foreground: "#112233", Background: "#ffffff"}}, {Scope: StringList{"keyword"}, Settings: ThemeStyle{Foreground: "#ff0000"}}}}
	h, err := NewHighlighter(HighlighterOptions{Languages: []*Language{&grammar}, ThemeRegistrations: []*Theme{theme}})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, h)
	grammar.Patterns[0].Match = "goodbye"
	theme.Settings[1].Settings.Foreground = "#00ff00"
	loaded, err := h.GetTheme("custom")
	if err != nil {
		t.Fatal(err)
	}
	loaded.Settings[1].Settings.Foreground = "#0000ff"
	result, err := h.CodeToTokens("hello", Options{Lang: "custom", Theme: "custom"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Tokens[0][0].Color != "#FF0000" {
		t.Fatal("caller mutation changed highlighter", result.Tokens)
	}
	for _, options := range []Options{{Lang: "missing", Theme: "custom"}, {Lang: "custom", Theme: "missing"}, {Lang: "custom"}, {Lang: "custom", Themes: map[string]any{}}, {Lang: "custom", Themes: map[string]any{"dark": "custom"}}} {
		if _, err := h.CodeToTokens("hello", options); err == nil {
			t.Errorf("invalid options accepted: %+v", options)
		}
	}
	if _, err := h.CodeToTokensBase("plain", Options{Lang: "text"}); err != nil {
		t.Fatal(err)
	}
	if err := h.LoadLanguageRegistrations(&Language{Name: "dependent", ScopeName: "source.dependent", EmbeddedLangs: []string{"missing"}}); err == nil {
		t.Fatal("missing dependency accepted")
	}
	if _, err := NewHighlighter(HighlighterOptions{LangAlias: map[string]string{"a": "b", "b": "a"}}); err == nil {
		t.Fatal("alias cycle accepted")
	}
}

type trackedEngine struct {
	RegexEngine
	scans  atomic.Int32
	closed atomic.Bool
}

func (e *trackedEngine) NewScanner(patterns []string) (RegexScanner, error) {
	e.scans.Add(1)
	return e.RegexEngine.NewScanner(patterns)
}
func (e *trackedEngine) Close() error { e.closed.Store(true); return e.RegexEngine.Close() }
func TestSharedEngineOwnership(t *testing.T) {
	engine, err := NewOnigurumaEngine()
	if err != nil {
		t.Fatal(err)
	}
	tracked := &trackedEngine{RegexEngine: engine}
	closeOnCleanup(t, tracked)
	options := HighlighterOptions{Engine: tracked, Langs: []string{"js"}, Themes: []string{"github-dark"}}
	first, err := NewHighlighter(options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewHighlighter(options)
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, second)
	if _, err := first.CodeToHTML("const x = 1", Options{Lang: "js", Theme: "github-dark"}); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if tracked.closed.Load() {
		t.Fatal("closing highlighter closed caller-owned engine")
	}
	if _, err := second.CodeToHTML("const x = 1", Options{Lang: "js", Theme: "github-dark"}); err != nil {
		t.Fatal(err)
	}
	if tracked.scans.Load() == 0 {
		t.Fatal("supplied engine was not used")
	}
}

func TestConcurrentHighlighting(t *testing.T) {
	h, err := NewHighlighter(HighlighterOptions{Langs: []string{"js", "python"}, Themes: []string{"github-dark", "github-light"}})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, h)
	var wg sync.WaitGroup
	errors := make(chan error, 12)
	for i := range 12 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for range 5 {
				theme := "github-dark"
				if i%2 != 0 {
					theme = "github-light"
				}
				html, err := h.CodeToHTML("const x = 1", Options{Lang: "js", Theme: theme})
				if err != nil {
					errors <- err
					return
				}
				if !strings.Contains(html, "shiki "+theme) {
					errors <- fmt.Errorf("unexpected theme: %s", html)
					return
				}
				if err := h.LoadLanguage("ruby"); err != nil {
					errors <- err
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestConcurrentSingletonDisposal(t *testing.T) {
	t.Cleanup(func() {
		if err := DisposeSingleton(); err != nil {
			t.Errorf("dispose singleton: %v", err)
		}
	})
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 3 {
				h, err := GetSingletonHighlighter(HighlighterOptions{})
				if err != nil {
					t.Error(err)
					return
				}
				_ = h.Close()
			}
		})
	}
	wg.Wait()
}

func TestTransformerLifecycle(t *testing.T) {
	h, err := NewHighlighter(HighlighterOptions{Langs: []string{"js"}, Themes: []string{"github-dark"}})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, h)
	var order []string
	mark := func(name, enforce string) Transformer {
		return Transformer{Name: name, Enforce: enforce, Preprocess: func(ctx *TransformerContext, code string) (string, error) {
			order = append(order, name)
			return code, nil
		}}
	}
	tr := Transformer{Name: "custom", Preprocess: func(ctx *TransformerContext, code string) (string, error) {
		ctx.Meta["processed"] = true
		return strings.ReplaceAll(code, "PLACEHOLDER", "42"), nil
	}, Span: func(ctx *TransformerContext, n *Node, line, column int, parent *Node, tok Token) (*Node, error) {
		if ctx.Meta["processed"] != true {
			return nil, errors.New("transformer context lost")
		}
		if tok.Content == "42" {
			n.TagName = "mark"
		}
		return n, nil
	}, Postprocess: func(ctx *TransformerContext, html string) (string, error) { return html + "<!--done-->", nil }}
	html, err := h.CodeToHTML("const x = PLACEHOLDER", Options{Lang: "js", Theme: "github-dark", MergeWhitespaces: false, Transformers: []Transformer{mark("post", "post"), mark("normal", ""), mark("pre", "pre"), tr}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "pre,normal,post" || !strings.Contains(html, "<mark") || !strings.HasSuffix(html, "<!--done-->") {
		t.Fatal(order, html)
	}
	sentinel := errors.New("hook failed")
	_, err = h.CodeToHTML("x", Options{Lang: "js", Theme: "github-dark", Transformers: []Transformer{{Tokens: func(*TransformerContext, [][]Token) ([][]Token, error) { return nil, sentinel }}}})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
}
