package shiki

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestReferenceParity(t *testing.T) {
	runReferenceFixtures(t, "testdata/parity.json")
}
func TestRealWorldReferenceParity(t *testing.T) {
	runReferenceFixtures(t, "testdata/realworld.json")
}
func runReferenceFixtures(t *testing.T, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Code, HTML string
		Options          Options
		Tokens           json.RawMessage
		HAST             json.RawMessage
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	h, err := NewHighlighter(HighlighterOptions{Langs: []string{"javascript", "typescript", "go", "python", "rust", "html", "css", "json", "yaml", "markdown", "shellscript", "ruby", "sql", "vue", "tsx"}, Themes: []string{"github-dark", "github-light", "nord", "vitesse-dark"}})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, h)
	for _, tc := range cases {
		if err := h.LoadLanguage(tc.Options.Lang); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			unlimited := time.Duration(0)
			tc.Options.TokenizeTimeLimit = &unlimited
			if name, ok := tc.Options.Theme.(string); ok {
				if err := h.LoadTheme(name); err != nil {
					t.Fatal(err)
				}
			}
			got, err := h.CodeToTokens(tc.Code, tc.Options)
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var actual, want any
			_ = json.Unmarshal(b, &actual)
			_ = json.Unmarshal(tc.Tokens, &want)
			if !reflect.DeepEqual(actual, want) {
				_ = os.MkdirAll(".cache/mismatches", 0755)
				_ = os.WriteFile(".cache/mismatches/"+tc.Name+".got.json", b, 0644)
				_ = os.WriteFile(".cache/mismatches/"+tc.Name+".want.json", tc.Tokens, 0644)
				t.Errorf("tokens mismatch\ngot:  %s\nwant: %s", b, tc.Tokens)
			}
			html, err := h.CodeToHTML(tc.Code, tc.Options)
			if err != nil {
				t.Fatal(err)
			}
			if html != tc.HTML {
				t.Errorf("HTML mismatch\ngot:  %s\nwant: %s", html, tc.HTML)
			}
			if len(tc.HAST) > 0 {
				node, err := h.CodeToHAST(tc.Code, tc.Options)
				if err != nil {
					t.Fatal(err)
				}
				data, _ := json.Marshal(node)
				var actual, want any
				_ = json.Unmarshal(data, &actual)
				_ = json.Unmarshal(tc.HAST, &want)
				if !reflect.DeepEqual(actual, want) {
					t.Errorf("HAST mismatch\ngot: %s\nwant: %s", data, tc.HAST)
				}
			}
		})
	}
}
