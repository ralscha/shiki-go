package shiki

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

func assertReferenceJSON(t *testing.T, actual any, reference json.RawMessage) {
	t.Helper()
	b, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(reference, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JSON mismatch\ngot: %s\nwant: %s", b, reference)
	}
}

func TestAdvancedReferenceParity(t *testing.T) {
	data, err := os.ReadFile("testdata/advanced.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Grammars   []*Language
		Themes     []*Theme
		Normalized []json.RawMessage
		Cases      []struct {
			Name, Code, HTML string
			Prefix           *string
			Options          Options
			Tokens, Variants json.RawMessage
		}
		ANSI []struct {
			Name, Code, ANSI string
			Options          Options
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	h, err := NewHighlighter(HighlighterOptions{Langs: []string{"js", "markdown"}, Languages: fixture.Grammars, Themes: []string{"github-dark", "github-light", "rose-pine", "vitesse-dark"}, ThemeRegistrations: fixture.Themes})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, h)
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			unlimited := time.Duration(0)
			tc.Options.TokenizeTimeLimit = &unlimited
			if tc.Prefix != nil {
				state, err := h.GetLastGrammarState(*tc.Prefix, tc.Options)
				if err != nil {
					t.Fatal(err)
				}
				tc.Options.GrammarState = state
			}
			tokens, err := h.CodeToTokens(tc.Code, tc.Options)
			if err != nil {
				t.Fatal(err)
			}
			assertReferenceJSON(t, tokens, tc.Tokens)
			html, err := h.CodeToHTML(tc.Code, tc.Options)
			if err != nil {
				t.Fatal(err)
			}
			if html != tc.HTML {
				t.Errorf("HTML mismatch\ngot: %s\nwant: %s", html, tc.HTML)
			}
			if len(tc.Variants) > 0 {
				variants, err := h.CodeToTokensWithThemes(tc.Code, tc.Options)
				if err != nil {
					t.Fatal(err)
				}
				assertReferenceJSON(t, variants, tc.Variants)
			}
		})
	}
	for i, theme := range fixture.Themes {
		t.Run("normalize-"+theme.Name, func(t *testing.T) { assertReferenceJSON(t, NormalizeTheme(theme), fixture.Normalized[i]) })
	}
	for _, tc := range fixture.ANSI {
		t.Run("ansi-"+tc.Name, func(t *testing.T) {
			ansi, err := h.CodeToANSI(tc.Code, tc.Options)
			if err != nil {
				t.Fatal(err)
			}
			if ansi != tc.ANSI {
				t.Errorf("ANSI mismatch\ngot: %q\nwant: %q", ansi, tc.ANSI)
			}
		})
	}
}
